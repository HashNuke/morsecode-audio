package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/HashNuke/morsecode-audio/internal/api"
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
