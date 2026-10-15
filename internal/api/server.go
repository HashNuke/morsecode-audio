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
	mux      *http.ServeMux
	draining atomic.Bool
	ttsSlots chan struct{}
	sockets  *socketSet
}

func New() *Server {
	s := &Server{mux: http.NewServeMux(), ttsSlots: make(chan struct{}, 8), sockets: newSocketSet()}
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	s.mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if s.draining.Load() {
			writeError(w, 503, "not_ready", "Server is draining")
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ready"})
	})
	s.mux.Handle("/v1/", s.auth(s.catalogRoutes()))
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// This public compatibility API uses explicit credentials only, never cookies
	// or ambient browser authentication. All browser origins may use it.
	if strings.HasPrefix(r.URL.Path, "/v1/") {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == "OPTIONS" {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "content-type, xi-api-key")
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}
func (s *Server) Drain() {
	s.draining.Store(true)
	s.sockets.drain()
}

func (s *Server) WaitSockets(ctx context.Context) error {
	select {
	case <-s.sockets.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) auth(next http.Handler) http.Handler {
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
		if strings.TrimSpace(r.Header.Get("xi-api-key")) == "" {
			writeError(w, 401, "needs_authorization", "A nonempty xi-api-key is required")
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
