package loadtest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HashNuke/morsecode-audio/internal/api"
	"github.com/coder/websocket"
)

func TestLoadChecksRealSpeechBoundaries(t *testing.T) {
	server := httptest.NewServer(api.New())
	defer server.Close()
	target, err := newTarget(server.URL, "SOS")
	if err != nil {
		t.Fatal(err)
	}
	defer target.client.CloseIdleConnections()
	for _, scenario := range []string{"tts", "batch-stt", "realtime-stt"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			first, err := target.exercise(ctx, scenario)
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "batch-stt" && first <= 0 {
				t.Fatal("missing first audio/partial timing")
			}
		})
	}
}

func TestLoadChecksRejectBadResponses(t *testing.T) {
	for _, code := range []int{200, 429, 503} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "audio/pcm")
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"text":"WRONG"}`))
		}))
		target, err := newTarget(server.URL, "SOS")
		if err != nil {
			t.Fatal(err)
		}
		for _, scenario := range []string{"tts", "batch-stt", "realtime-stt"} {
			_, err := target.exercise(context.Background(), scenario)
			if err == nil {
				t.Fatalf("accepted HTTP %d for %s", code, scenario)
			}
			if (code == 429) != errors.Is(err, errCapacity) {
				t.Fatalf("capacity classification: %v", err)
			}
		}
		target.client.CloseIdleConnections()
		server.Close()
	}
}

func TestLoadSummarySeparatesErrorsFromLatency(t *testing.T) {
	r := summarize("tts", time.Second, []sample{
		{elapsed: 10 * time.Millisecond, first: time.Millisecond},
		{elapsed: 20 * time.Millisecond, first: 2 * time.Millisecond},
		{err: errCapacity}, {err: errors.New("bad response")},
	})
	if r.Attempts != 4 || r.Success != 2 || r.Capacity != 1 || r.Errors != 1 || r.SuccessPerSecond != 2 || r.Latency.P95 != 20 || r.FirstEvent.P95 != 2 {
		t.Fatalf("incorrect summary: %+v", r)
	}
}

func TestLoadRequiresExplicitOrigin(t *testing.T) {
	for _, origin := range []string{"", "https://user:pass@example.com", "https://example.com/path", "https://example.com?key=x", "ftp://example.com"} {
		if _, err := newTarget(origin, "SOS"); err == nil {
			t.Fatalf("accepted %q", origin)
		}
	}
}

func TestCanceledLoadDoesNotStartRequests(t *testing.T) {
	target, err := newTarget("http://127.0.0.1:1", "SOS")
	if err != nil {
		t.Fatal(err)
	}
	defer target.client.CloseIdleConnections()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := target.phase(ctx, "tts", 4, time.Second, time.Second)
	if r.Attempts != 0 {
		t.Fatalf("started work after cancellation: %+v", r)
	}
}

func TestRealtimeTTSLoadUsesIncrementalWebSocketInput(t *testing.T) {
	app := api.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			http.Error(w, "HTTP fallback forbidden", 404)
			return
		}
		app.ServeHTTP(w, r)
	}))
	defer server.Close()
	target, err := newTarget(server.URL, "SOS")
	if err != nil {
		t.Fatal(err)
	}
	defer target.client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if first, err := target.exercise(ctx, "realtime-tts"); err != nil || first <= 0 {
		t.Fatalf("realtime TTS load failed: first=%s err=%v", first, err)
	}
}

func TestRealtimePhaseHoldsOneConnectionPerClient(t *testing.T) {
	for _, scenario := range []string{"realtime-stt", "realtime-tts"} {
		t.Run(scenario, func(t *testing.T) {
			var connections atomic.Int64
			app := api.New()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				connections.Add(1)
				app.ServeHTTP(w, r)
			}))
			defer server.Close()
			target, err := newTarget(server.URL, "E")
			if err != nil {
				t.Fatal(err)
			}
			defer target.client.CloseIdleConnections()
			duration := 100 * time.Millisecond
			result := target.phase(context.Background(), scenario, 2, duration, time.Second)
			if result.Attempts != 2 || result.Success != 2 || result.Errors != 0 || connections.Load() != 2 {
				t.Fatalf("must hold two original connections: %+v, connections=%d", result, connections.Load())
			}
			if result.CompletedTurns < 4 || result.ShortestSessionSeconds < duration.Seconds() {
				t.Fatalf("sessions did not stay open with repeated turns: %+v", result)
			}
		})
	}
}

func TestRealtimePhaseDoesNotReplaceRejectedConnections(t *testing.T) {
	var connections atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { connections.Add(1); w.WriteHeader(429) }))
	defer server.Close()
	target, _ := newTarget(server.URL, "E")
	defer target.client.CloseIdleConnections()
	result := target.phase(context.Background(), "realtime-tts", 3, 100*time.Millisecond, time.Second)
	if result.Attempts != 3 || result.Capacity != 3 || connections.Load() != 3 {
		t.Fatalf("rejected sessions were retried: %+v", result)
	}
}

func TestPersistentSessionCancellationClosesOriginalSocket(t *testing.T) {
	connected := make(chan struct{})
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		close(connected)
		for {
			if _, _, err := conn.Read(r.Context()); err != nil {
				close(closed)
				return
			}
		}
	}))
	defer server.Close()
	target, _ := newTarget(server.URL, "E")
	defer target.client.CloseIdleConnections()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan report, 1)
	go func() { done <- target.phase(ctx, "realtime-tts", 1, time.Minute, time.Second) }()
	select {
	case <-connected:
	case <-time.After(time.Second):
		t.Fatal("session never opened")
	}
	cancel()
	select {
	case result := <-done:
		if result.Attempts != 1 || result.Errors != 1 {
			t.Fatalf("cancellation result: %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not stop load test")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("cancellation leaked the original socket")
	}
}
