package api_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/HashNuke/morsecode-audio/internal/api"
)

func TestHTTPDefaultMP3AndBinaryStream(t *testing.T) {
	s := api.New()
	for _, path := range []string{"/v1/text-to-speech/morse", "/v1/text-to-speech/morse/stream"} {
		r := httptest.NewRequest("POST", path, strings.NewReader(`{"text":"E"}`))
		r.Header.Set("xi-api-key", "synthetic")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 200 || w.Header().Get("Content-Type") != "audio/mpeg" {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		if w.Body.Len() < 1000 || w.Body.Bytes()[0] != 0xff {
			t.Fatal("not MP3 frames")
		}
		if strings.HasSuffix(path, "/stream") && !w.Flushed {
			t.Fatal("stream never flushed")
		}
	}
}

func TestSlowReaderWriteFailureReleasesSynthesisCapacity(t *testing.T) {
	s := api.New()
	for range 12 {
		r := httptest.NewRequest("POST", "/v1/text-to-speech/morse/stream", strings.NewReader(`{"text":"`+strings.Repeat("T ", 480)+`"}`))
		r.Header.Set("xi-api-key", "synthetic")
		w := &deadlineWriter{ResponseRecorder: httptest.NewRecorder()}
		s.ServeHTTP(w, r)
		if w.Code != 200 || w.deadline.IsZero() || time.Until(w.deadline) > 31*time.Second {
			t.Fatalf("missing bounded output deadline or capacity leak: %d %v", w.Code, w.deadline)
		}
	}
}

type deadlineWriter struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (w *deadlineWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	return nil
}
func (w *deadlineWriter) Write(p []byte) (int, error) { return 0, os.ErrDeadlineExceeded }

func TestHTTPStreamDisconnectCompletesHandler(t *testing.T) {
	finished := make(chan struct{}, 1)
	s := api.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { finished <- struct{}{} }()
		s.ServeHTTP(w, r)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Almost five minutes of audio: client reads only the first frame.
	body := `{"text":"` + strings.Repeat("T ", 480) + `"}`
	r, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/v1/text-to-speech/morse/stream?output_format=pcm_48000", strings.NewReader(body))
	r.Header.Set("xi-api-key", "synthetic")
	response, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		data, _ := io.ReadAll(response.Body)
		response.Body.Close()
		t.Fatalf("%d %s", response.StatusCode, data)
	}
	if _, err := io.ReadFull(response.Body, make([]byte, 256)); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("handler retained disconnected stream")
	}
}

func TestHTTPStreamPreflightErrorsContainNoAudio(t *testing.T) {
	s := api.New()
	for _, tc := range []struct{ format, body string }{
		{"wav_16000", `{"text":"E"}`},
		{"mp3_44100_128", `{"text":"%"}`},
		{"pcm_16000", `{"text":"` + strings.Repeat("T ", 600) + `"}`},
	} {
		r := httptest.NewRequest("POST", "/v1/text-to-speech/morse/stream?output_format="+tc.format, bytes.NewBufferString(tc.body))
		r.Header.Set("xi-api-key", "synthetic")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 422 || w.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}
}
