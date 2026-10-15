package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HashNuke/morsecode-audio/internal/api"
)

func TestHealthAndDrain(t *testing.T) {
	s := api.New()
	for _, path := range []string{"/healthz", "/readyz"} {
		w := request(s, "GET", path, "")
		if w.Code != 200 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	s.Drain()
	if w := request(s, "GET", "/readyz", ""); w.Code != 503 {
		t.Fatalf("draining ready: %d", w.Code)
	}
	if w := request(s, "GET", "/healthz", ""); w.Code != 200 {
		t.Fatalf("draining health: %d", w.Code)
	}
	if w := request(s, "GET", "/v1/models", "synthetic"); w.Code != 503 {
		t.Fatalf("new work during drain: %d", w.Code)
	}
}

func TestCatalogIgnoresAPIKey(t *testing.T) {
	s := api.New()
	for _, key := range []string{"arbitrary-one", "another arbitrary key", "", "   "} {
		w := request(s, "GET", "/v1/models", key)
		if w.Code != 200 {
			t.Fatalf("arbitrary key: %d", w.Code)
		}
		var models []struct {
			ID  string `json:"model_id"`
			TTS bool   `json:"can_do_text_to_speech"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &models); err != nil {
			t.Fatal(err)
		}
		if len(models) == 0 || models[0].ID != "morse_v1" || !models[0].TTS {
			t.Fatalf("catalog: %+v", models)
		}
	}
}

func TestVoiceDiscovery(t *testing.T) {
	s := api.New()
	w := request(s, "GET", "/v1/voices", "test")
	var body struct {
		Voices []struct {
			ID string `json:"voice_id"`
		} `json:"voices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Voices) != 1 || body.Voices[0].ID != "morse" {
		t.Fatalf("voices: %s", w.Body)
	}
	if w := request(s, "GET", "/v1/voices/unknown", "test"); w.Code != 404 {
		t.Fatalf("unknown voice: %d", w.Code)
	}
}

func request(h http.Handler, method, path, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	if key != "" {
		r.Header.Set("xi-api-key", key)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
