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

func TestTTSSocketDisconnectAndDrainJoinCodecs(t *testing.T) {
	for _, mode := range []string{"disconnect", "drain"} {
		for _, format := range []string{"pcm_16000", "mp3_44100_128"} {
			t.Run(mode+"/"+format, func(t *testing.T) {
				handler := New()
				finished := make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { defer close(finished); handler.ServeHTTP(w, r) }))
				defer server.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
				defer cancel()
				conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/text-to-speech/morse/stream-input?auto_mode=true&output_format="+format, &websocket.DialOptions{HTTPHeader: http.Header{"Xi-Api-Key": {"synthetic"}}})
				if err != nil {
					t.Fatal(err)
				}
				defer conn.CloseNow()
				conn.SetReadLimit(1 << 20)
				if err := wsjson.Write(ctx, conn, map[string]any{"text": " "}); err != nil {
					t.Fatal(err)
				}
				if err := wsjson.Write(ctx, conn, map[string]any{"text": "ETETETETETETETET"}); err != nil {
					t.Fatal(err)
				}
				var packet map[string]any
				if err := wsjson.Read(ctx, conn, &packet); err != nil || packet["audio"] == nil {
					t.Fatalf("no initial output: %v", err)
				}
				if mode == "disconnect" {
					conn.CloseNow()
				} else {
					handler.Drain()
					if err := handler.WaitSockets(ctx); err != nil {
						t.Fatal(err)
					}
				}
				select {
				case <-finished:
				case <-ctx.Done():
					t.Fatal("TTS retained handler/codec after cancellation")
				}
				if len(handler.ttsSlots) != 0 {
					t.Fatal("TTS encoder admission leaked")
				}
				handler.Drain()
				if err := handler.WaitSockets(ctx); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestTTSInitializationDeadlineAndSynthesisCapacity(t *testing.T) {
	for _, route := range []string{"stream-input", "multi-stream-input"} {
		for _, mode := range []string{"initialization", "capacity"} {
			t.Run(route+"/"+mode, func(t *testing.T) {
				handler := New()
				if mode == "initialization" {
					handler.ttsInitTimeout = 40 * time.Millisecond
				}
				if mode == "capacity" {
					for range cap(handler.ttsSlots) {
						handler.ttsSlots <- struct{}{}
					}
				}
				finished := make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { defer close(finished); handler.ServeHTTP(w, r) }))
				defer server.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/text-to-speech/morse/"+route+"?output_format=pcm_16000", nil)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.CloseNow()
				if mode == "capacity" {
					_ = wsjson.Write(ctx, conn, map[string]any{"text": " ", "xi_api_key": "synthetic"})
					_ = wsjson.Write(ctx, conn, map[string]any{"text": "E", "flush": true})
					var message map[string]any
					if err := wsjson.Read(ctx, conn, &message); err != nil || message["error"] != "too_many_concurrent_requests" {
						t.Fatalf("capacity failure: %v %v", message, err)
					}
				}
				if _, _, err := conn.Read(ctx); err == nil {
					t.Fatal("expired/overloaded socket remained open")
				}
				select {
				case <-finished:
				case <-ctx.Done():
					t.Fatal("handler retained")
				}
				handler.Drain()
				if err := handler.WaitSockets(ctx); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestTTSTextQueueLimits(t *testing.T) {
	for _, mode := range []string{"generations", "fragments", "text"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			options, err := parseTTSSocketOptions(map[string][]string{"output_format": {"pcm_16000"}, "auto_mode": {"true"}})
			if err != nil {
				t.Fatal(err)
			}
			jobs := make(chan *ttsGeneration, 1)
			input := ttsTextInput{ctx: ctx, jobs: jobs, options: options, schedule: []int{50}}
			text := "E"
			message := ttsSocketMessage{Text: &text}
			want := ""
			switch mode {
			case "generations":
				message.Flush = true
				if err := input.accept(message); err != nil {
					t.Fatal(err)
				}
				want = "generation_queue_full"
			case "fragments":
				for range 16 {
					if err := input.accept(message); err != nil {
						t.Fatal(err)
					}
				}
				want = "input_queue_full"
			case "text":
				text = strings.Repeat("E", 4097)
				want = "text_too_large"
			}
			if err := input.accept(message); err == nil || err.Error() != want {
				t.Fatalf("limit error: got %v want %s", err, want)
			}
		})
	}
}
