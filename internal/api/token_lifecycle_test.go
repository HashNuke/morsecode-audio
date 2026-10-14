package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTokenExpiryCapacityAndDrain(t *testing.T) {
	s := New()
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	s.tokens.now = func() time.Time { return now }
	s.tokens.capacity = 1
	issue := func(want int) string {
		t.Helper()
		r := httptest.NewRequest("POST", "/v1/single-use-token/batch_scribe", nil)
		r.Header.Set("xi-api-key", "synthetic")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("issuance: got %d want %d", w.Code, want)
		}
		var result struct {
			Token string `json:"token"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &result)
		return result.Token
	}
	consume := func(token string, want int) {
		t.Helper()
		r := httptest.NewRequest("POST", "/v1/speech-to-text?token="+token, nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("consumption: got %d want %d", w.Code, want)
		}
	}
	first := issue(200)
	issue(429)
	now = now.Add(15*time.Minute - time.Nanosecond)
	consume(first, 422) // Auth succeeds; missing multipart body fails afterward.
	consume(first, 401) // A failed admitted operation still consumes the token.
	second := issue(200)
	now = now.Add(15 * time.Minute)
	consume(second, 401)
	third := issue(200)
	now = now.Add(15 * time.Minute)
	fourth := issue(200) // Expired records cannot permanently exhaust capacity.
	consume(third, 401)
	consume(fourth, 422)
	fifth := issue(200)
	s.Drain()
	consume(fifth, 503)
	issue(503)
	if len(s.tokens.entries) != 0 {
		t.Fatal("drain retained token records")
	}
}
