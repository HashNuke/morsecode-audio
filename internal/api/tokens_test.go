package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HashNuke/morsecode-audio/internal/api"
	"github.com/coder/websocket"
)

func issueToken(t *testing.T, s http.Handler, purpose string) string {
	t.Helper()
	w := request(s, "POST", "/v1/single-use-token/"+purpose, "synthetic-token-issuer")
	var result struct {
		Token string `json:"token"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || !strings.HasPrefix(result.Token, "sutkn_") {
		t.Fatalf("token issuance for %s: status %d or invalid response", purpose, w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("token response can be cached")
	}
	return result.Token
}

func TestSpeechTokenIssuance(t *testing.T) {
	s := api.New()
	seen := map[string]bool{}
	for _, purpose := range []string{"realtime_scribe", "batch_scribe", "tts_websocket"} {
		for range 2 {
			token := issueToken(t, s, purpose)
			if seen[token] {
				t.Fatal("token reused across issuance")
			}
			seen[token] = true
		}
	}
	if w := request(s, "POST", "/v1/single-use-token/batch_scribe", ""); w.Code != 401 {
		t.Fatalf("unauthenticated issuance: %d", w.Code)
	}
	if w := request(s, "POST", "/v1/single-use-token/unknown", "synthetic"); w.Code != 422 {
		t.Fatalf("unknown purpose: %d", w.Code)
	}
}

func tokenTranscription(s http.Handler, token, key string) *httptest.ResponseRecorder {
	var body bytes.Buffer
	m := multipart.NewWriter(&body)
	f, _ := m.CreateFormFile("file", "test.pcm")
	_, _ = f.Write(independentMorse("ET", 16000))
	_ = m.WriteField("model_id", "scribe_v2")
	_ = m.WriteField("file_format", "pcm_s16le_16")
	_ = m.Close()
	r := httptest.NewRequest("POST", "/v1/speech-to-text?token="+url.QueryEscape(token), &body)
	r.Header.Set("Content-Type", m.FormDataContentType())
	r.Header.Set("xi-api-key", key)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestBatchTokenPurposeReplayAndRestart(t *testing.T) {
	s := api.New()
	token := issueToken(t, s, "batch_scribe")
	if w := tokenTranscription(api.New(), token, ""); w.Code != 401 {
		t.Fatalf("token survived process replacement: %d", w.Code)
	}
	for _, purpose := range []string{"realtime_scribe", "tts_websocket"} {
		wrong := issueToken(t, s, purpose)
		if w := tokenTranscription(s, wrong, "synthetic-fallback"); w.Code != 401 {
			t.Fatalf("wrong purpose accepted: %d", w.Code)
		}
	}
	w := tokenTranscription(s, token, "")
	var result struct {
		Text string `json:"text"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Text != "ET" {
		t.Fatalf("token STT: status %d", w.Code)
	}
	for _, key := range []string{"", "synthetic-fallback"} {
		if w := tokenTranscription(s, token, key); w.Code != 401 {
			t.Fatalf("replay accepted: %d", w.Code)
		}
	}
	if w := tokenTranscription(s, "", "synthetic-fallback"); w.Code != 401 {
		t.Fatalf("empty explicit token fell back: %d", w.Code)
	}
}

func TestTokenConcurrentConsumptionAndRouteScope(t *testing.T) {
	s := api.New()
	token := issueToken(t, s, "batch_scribe")
	for _, path := range []string{"/v1/models", "/v1/single-use-token/batch_scribe"} {
		if w := request(s, "POST", path+"?token="+url.QueryEscape(token), "synthetic-fallback"); w.Code != 401 {
			t.Fatalf("token escaped route scope: %d", w.Code)
		}
	}
	if w := request(s, "POST", "/v1/speech-to-text?token="+token+"&token="+token, "synthetic-fallback"); w.Code != 401 {
		t.Fatalf("duplicate token accepted: %d", w.Code)
	}
	var wg sync.WaitGroup
	results := make(chan int, 16)
	for range 16 {
		wg.Go(func() { results <- tokenTranscription(s, token, "").Code })
	}
	wg.Wait()
	close(results)
	accepted := 0
	for code := range results {
		if code == 200 {
			accepted++
		} else if code != 401 {
			t.Errorf("unexpected competing response: %d", code)
		}
	}
	if accepted != 1 {
		t.Fatalf("token admitted %d concurrent requests", accepted)
	}
}

func TestBrowserRealtimeTokenAndReplay(t *testing.T) {
	handler := api.New()
	s := httptest.NewServer(handler)
	defer s.Close()
	token := issueToken(t, handler, "realtime_scribe")
	if w := tokenTranscription(handler, token, ""); w.Code != 401 {
		t.Fatalf("realtime token accepted for batch: %d", w.Code)
	}
	// A wrong-purpose attempt does not consume the token for its intended use.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	target := "ws" + strings.TrimPrefix(s.URL, "http") + "/v1/speech-to-text/realtime?token=" + url.QueryEscape(token)
	options := &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"https://browser.example"}}}
	c, resp, err := websocket.Dial(ctx, target, options)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("browser token upgrade failed: status %d", status)
	}
	defer c.CloseNow()
	if message := readSTT(t, ctx, c); message["message_type"] != "session_started" {
		t.Fatalf("session not started: %v", message)
	}
	sendSTT(t, ctx, c, independentMorse("ET", 16000), 16000, true)
	readFinal(t, ctx, c, "ET")
	second, resp, err := websocket.Dial(ctx, target, options)
	if second != nil {
		second.CloseNow()
	}
	if err == nil || resp == nil || resp.StatusCode != 401 {
		t.Fatal("realtime token replay was not rejected")
	}
}

func TestBrowserSpeechPreflight(t *testing.T) {
	s := api.New()
	r := httptest.NewRequest("OPTIONS", "/v1/speech-to-text", nil)
	r.Header.Set("Origin", "https://browser.example")
	r.Header.Set("Access-Control-Request-Method", "POST")
	r.Header.Set("Access-Control-Request-Headers", "content-type,xi-api-key")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != "*" || !strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "xi-api-key") {
		t.Fatalf("preflight: %d %v", w.Code, w.Header())
	}
	if w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("API must not authorize ambient browser credentials")
	}
	r = httptest.NewRequest("POST", "/v1/speech-to-text?token=invalid", nil)
	r.Header.Set("Origin", "https://browser.example")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 401 || w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("browser cannot observe auth failure")
	}
	r = httptest.NewRequest("GET", "/v1/models", nil)
	r.AddCookie(&http.Cookie{Name: "xi-api-key", Value: "synthetic"})
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("ambient cookie authenticated: %d", w.Code)
	}
}
