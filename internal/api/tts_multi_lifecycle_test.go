package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestMultiDisconnectAndDrainJoinContextWorkers(t *testing.T) {
	for _, mode := range []string{"disconnect", "drain", "capacity"} {
		t.Run(mode, func(t *testing.T) {
			handler := New()
			if mode == "capacity" {
				for range cap(handler.ttsSlots) {
					handler.ttsSlots <- struct{}{}
				}
			}
			finished := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { defer close(finished); handler.ServeHTTP(w, r) }))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/text-to-speech/morse/multi-stream-input?auto_mode=true", &websocket.DialOptions{HTTPHeader: http.Header{"Xi-Api-Key": {"synthetic"}}})
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			conn.SetReadLimit(1 << 20)
			for _, id := range []string{"a", "b"} {
				if err := wsjson.Write(ctx, conn, map[string]any{"text": "ETETETETETETETET", "context_id": id}); err != nil {
					t.Fatal(err)
				}
			}
			seen := map[string]bool{}
			for {
				var message map[string]any
				if err := wsjson.Read(ctx, conn, &message); err != nil {
					t.Fatal(err)
				}
				if mode == "capacity" {
					if message["error"] != "too_many_concurrent_requests" {
						t.Fatalf("capacity: %v", message)
					}
					_, _, _ = conn.Read(ctx)
					break
				}
				if message["audio"] == nil {
					t.Fatalf("no audio: %v", message)
				}
				seen[message["contextId"].(string)] = true
				if len(seen) == 2 {
					break
				}
			}
			if mode == "disconnect" {
				conn.CloseNow()
			} else {
				handler.Drain()
			}
			select {
			case <-finished:
			case <-ctx.Done():
				t.Fatal("multi handler/codec retained")
			}
			if mode != "capacity" && len(handler.ttsSlots) != 0 {
				t.Fatal("synthesis slot retained")
			}
			handler.Drain()
			if err := handler.WaitSockets(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMultiContextKeepaliveHasIndependentDeadline(t *testing.T) {
	handler := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	options, _ := parseTTSSocketOptions(nil)
	c := &ttsMultiConnection{server: handler, ctx: ctx, options: options, wake: make(chan struct{}, 1)}
	defer func() {
		cancel()
		for _, item := range c.contexts {
			<-item.done
		}
	}()
	text := " "
	a, b := "a", "b"
	for _, id := range []*string{&a, &b} {
		if err := c.accept(ttsMultiMessage{ContextID: id, ttsSocketMessage: ttsSocketMessage{Text: &text}}); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-options.idle)
	for _, item := range c.contexts {
		item.touched = old
	}
	// The protocol's empty text only refreshes b. Expiring a must not close b.
	empty := ""
	if err := c.accept(ttsMultiMessage{ContextID: &b, ttsSocketMessage: ttsSocketMessage{Text: &empty}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.expireContexts(time.Now()); err != nil {
		t.Fatal(err)
	}
	if !c.contexts[0].closing || c.contexts[1].closing {
		t.Fatal("one context's keepalive extended another")
	}
	if c.contexts[1].input.active != nil {
		t.Fatal("empty keepalive fabricated a generation")
	}
}

func TestMultiReadyOutputsRotateWithoutStarvation(t *testing.T) {
	handler := New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket := handler.sockets.accept(w, r)
		if socket == nil {
			return
		}
		defer socket.close()
		c := ttsMultiConnection{socket: socket}
		for _, id := range []string{"a", "b", "c"} {
			item := &ttsMultiContext{id: id, output: make(chan multiOutput, 2)}
			for range 2 {
				item.output <- multiOutput{body: map[string]any{"audio": ""}}
			}
			c.contexts = append(c.contexts, item)
		}
		for range 6 {
			if ready, err := c.publishOne(); err != nil || !ready {
				t.Errorf("scheduler: ready=%v err=%v", ready, err)
				return
			}
		}
		_ = socket.conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	for _, want := range []string{"a", "b", "c", "a", "b", "c"} {
		var message map[string]any
		if err := wsjson.Read(ctx, conn, &message); err != nil {
			t.Fatal(err)
		}
		if message["contextId"] != want {
			t.Fatalf("ready output order: got %v want %s", message, want)
		}
	}
	if _, _, err := conn.Read(ctx); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatal(err)
	}
}
