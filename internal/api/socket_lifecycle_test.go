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

func TestSocketAdmissionDisconnectAndDrain(t *testing.T) {
	api := New()
	api.sockets.capacity = 2
	finished := make(chan string, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { finished <- r.Header.Get("xi-api-key") }()
		api.ServeHTTP(w, r)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/speech-to-text/realtime"
	first := openLifecycleSocket(t, ctx, endpoint, "first")
	second := openLifecycleSocket(t, ctx, endpoint, "second")
	defer first.CloseNow()
	defer second.CloseNow()
	_, response, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPHeader: http.Header{"Xi-Api-Key": []string{"overload"}}})
	if err == nil || response.StatusCode != 429 {
		t.Fatal("unbounded admission", err, response)
	}
	first.CloseNow()
	for {
		select {
		case key := <-finished:
			if key == "first" {
				goto released
			}
		case <-ctx.Done():
			t.Fatal("disconnected handler retained")
		}
	}
released:
	third := openLifecycleSocket(t, ctx, endpoint, "replacement")
	defer third.CloseNow()
	api.Drain()
	if err := api.WaitSockets(ctx); err != nil {
		t.Fatal("drain retained hijacked workers", err)
	}
	for _, conn := range []*websocket.Conn{second, third} {
		if _, _, err := conn.Read(ctx); err == nil {
			t.Fatal("drained socket still active")
		}
	}
	_, response, err = websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPHeader: http.Header{"Xi-Api-Key": []string{"late"}}})
	if err == nil || response.StatusCode != 503 {
		t.Fatal("draining server admitted work", err, response)
	}
}

func TestSocketIdleAndLifetimeReleaseWorker(t *testing.T) {
	for _, mode := range []string{"idle", "lifetime"} {
		t.Run(mode, func(t *testing.T) {
			api := New()
			if mode == "idle" {
				api.sockets.idle = 40 * time.Millisecond
			} else {
				api.sockets.lifetime = 40 * time.Millisecond
			}
			finished := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { defer close(finished); api.ServeHTTP(w, r) }))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			conn := openLifecycleSocket(t, ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/speech-to-text/realtime", "synthetic")
			defer conn.CloseNow()
			if _, _, err := conn.Read(ctx); err == nil {
				t.Fatal("abandoned socket remained open")
			}
			select {
			case <-finished:
			case <-ctx.Done():
				t.Fatal("abandoned handler retained")
			}
			api.Drain()
			if err := api.WaitSockets(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func openLifecycleSocket(t *testing.T, ctx context.Context, endpoint, key string) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPHeader: http.Header{"Xi-Api-Key": []string{key}}})
	if err != nil {
		t.Fatal(err)
	}
	var started map[string]any
	if err := wsjson.Read(ctx, c, &started); err != nil {
		t.Fatal(err)
	}
	if started["message_type"] != "session_started" {
		t.Fatal(started)
	}
	return c
}
