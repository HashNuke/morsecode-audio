package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"sync"
	"time"
)

type tokenRecord struct {
	purpose string
	expires time.Time
}

// tokenStore retains only random-token digests and protocol metadata, never the
// issuing key or user content. Admission is bounded even when tokens are unused.
type tokenStore struct {
	mu       sync.Mutex
	entries  map[[32]byte]tokenRecord
	capacity int
	now      func() time.Time
	closed   bool
}

func newTokenStore() *tokenStore {
	return &tokenStore{entries: make(map[[32]byte]tokenRecord), capacity: 4096, now: time.Now}
}

var errTokenCapacity = errors.New("token_capacity")
var errTokenDraining = errors.New("token_draining")

func (s *tokenStore) issue(purpose string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", errTokenDraining
	}
	now := s.now()
	for key, record := range s.entries {
		if !now.Before(record.expires) {
			delete(s.entries, key)
		}
	}
	if len(s.entries) >= s.capacity {
		return "", errTokenCapacity
	}
	var entropy [32]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	token := "sutkn_" + base64.RawURLEncoding.EncodeToString(entropy[:])
	s.entries[sha256.Sum256([]byte(token))] = tokenRecord{purpose: purpose, expires: now.Add(15 * time.Minute)}
	return token, nil
}

func (s *tokenStore) consume(token, purpose string) bool {
	if len(token) != 49 || purpose == "" {
		return false
	}
	digest := sha256.Sum256([]byte(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	record, found := s.entries[digest]
	if s.closed || !found {
		return false
	}
	if !s.now().Before(record.expires) {
		delete(s.entries, digest)
		return false
	}
	if record.purpose != purpose {
		return false
	}
	delete(s.entries, digest)
	return true
}

func (s *tokenStore) drain() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	clear(s.entries)
}

func (s *Server) issueToken(w http.ResponseWriter, r *http.Request) {
	purpose := r.PathValue("token_type")
	switch purpose {
	case "realtime_scribe", "batch_scribe", "tts_websocket":
	default:
		writeError(w, 422, "invalid_token_type", "Unknown speech token type")
		return
	}
	token, err := s.tokens.issue(purpose)
	if err != nil {
		switch err {
		case errTokenCapacity:
			writeError(w, 429, "too_many_tokens", "Outstanding token capacity is busy")
		case errTokenDraining:
			writeError(w, 503, "not_ready", "Server is draining")
		default:
			writeError(w, 500, "token_creation_failed", "Cannot issue token")
		}
		return
	}
	writeJSON(w, 200, map[string]string{"token": token})
}

func speechTokenPurpose(r *http.Request) string {
	switch {
	case r.Method == "POST" && r.URL.Path == "/v1/speech-to-text":
		return "batch_scribe"
	case r.Method == "GET" && r.URL.Path == "/v1/speech-to-text/realtime":
		return "realtime_scribe"
	default:
		return ""
	}
}
