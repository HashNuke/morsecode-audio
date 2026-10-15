// Package api implements the ElevenLabs-shaped Morse speech HTTP interface.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
)

type Server struct {
	mux        *http.ServeMux
	draining   atomic.Bool
	ttsSlots   chan struct{}
	sockets    *socketSet
	ttsSockets *socketSet
}

func New() *Server { return NewWithLimits(8, 64) }

// NewWithLimits uses startup-validated speech admission limits.
func NewWithLimits(tts, stt int) *Server {
	if tts < 1 || stt < 1 {
		panic("invalid speech capacity")
	}
	s := &Server{mux: http.NewServeMux(), ttsSlots: make(chan struct{}, tts), sockets: newSocketSet()}
	s.sockets.capacity = stt
	s.ttsSockets = newSocketSet()
	s.ttsSockets.capacity = tts
	s.mux.HandleFunc("GET /{$}", home)
	s.mux.HandleFunc("GET /llms.txt", reference)
	s.mux.HandleFunc("GET /assets/inter-variable.woff2", font)
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	s.mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if s.draining.Load() {
			writeError(w, 503, "not_ready", "Server is draining")
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ready"})
	})
	s.mux.Handle("/v1/", s.apiRequests(s.catalogRoutes()))
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// All browser origins may use this public API without credentials.
	if strings.HasPrefix(r.URL.Path, "/v1/") {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == "OPTIONS" {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "content-type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}
func (s *Server) Drain() {
	s.draining.Store(true)
	s.sockets.drain()
	s.ttsSockets.drain()
}

func (s *Server) WaitSockets(ctx context.Context) error {
	for _, sockets := range []*socketSet{s.sockets, s.ttsSockets} {
		select {
		case <-sockets.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *Server) apiRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.draining.Load() {
			writeError(w, 503, "not_ready", "Server is draining")
			return
		}
		_, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			writeError(w, 422, "invalid_request", "Invalid query parameters")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"detail": map[string]string{"status": code, "message": message}})
}
