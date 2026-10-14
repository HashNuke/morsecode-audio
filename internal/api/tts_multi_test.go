package api_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HashNuke/morsecode-audio/internal/api"
	"github.com/coder/websocket"
)

func openMulti(t *testing.T, server *httptest.Server, query string, header http.Header) (*websocket.Conn, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	t.Cleanup(cancel)
	c, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/text-to-speech/morse/multi-stream-input?output_format=pcm_16000&"+query, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatalf("multi upgrade: %v response=%v", err, response)
	}
	c.SetReadLimit(1 << 20)
	t.Cleanup(func() { c.CloseNow() })
	return c, ctx
}

func multiUntilFinal(t *testing.T, ctx context.Context, c *websocket.Conn, id string) int {
	t.Helper()
	size := 0
	for {
		m := readSTT(t, ctx, c)
		if m["error"] != nil || m["contextId"] != id {
			t.Fatalf("unexpected context output: %v", m)
		}
		if m["isFinal"] == true {
			return size
		}
		pcm, err := base64.StdEncoding.DecodeString(m["audio"].(string))
		if err != nil {
			t.Fatal(err)
		}
		size += len(pcm)
	}
}

func TestMultiFlushClosePreservesAudioAndReusesID(t *testing.T) {
	server := httptest.NewServer(api.New())
	defer server.Close()
	c, ctx := openMulti(t, server, "", http.Header{})
	sendTTS(t, ctx, c, map[string]any{"context_id": "same", "text": " ", "xi_api_key": "synthetic"})
	sendTTS(t, ctx, c, map[string]any{"context_id": "same", "text": "E", "flush": true})
	sendTTS(t, ctx, c, map[string]any{"context_id": "same", "close_context": true})
	// Immediate reuse must publish the old flush before the replacement generation.
	sendTTS(t, ctx, c, map[string]any{"context_id": "same", "text": "T", "flush": true})
	if n := multiUntilFinal(t, ctx, c, "same"); n != 28800 {
		t.Fatalf("closed flush E: %d", n)
	}
	if n := multiUntilFinal(t, ctx, c, "same"); n != 32640 {
		t.Fatalf("replacement T: %d", n)
	}
	sendTTS(t, ctx, c, map[string]any{"close_socket": true})
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("close: %v", err)
	}
}

func TestMultiIdleProducerDoesNotBlockOtherContexts(t *testing.T) {
	server := httptest.NewServer(api.New())
	defer server.Close()
	c, ctx := openMulti(t, server, "auto_mode=true", http.Header{"Xi-Api-Key": {"synthetic"}})
	sendTTS(t, ctx, c, map[string]any{"context_id": "waiting", "text": "E"})
	first := readSTT(t, ctx, c)
	if first["contextId"] != "waiting" || first["audio"] == nil {
		t.Fatalf("early output: %v", first)
	}
	// Empty text is a keepalive, not EOS; it must not produce a tail or final.
	sendTTS(t, ctx, c, map[string]any{"context_id": "waiting", "text": ""})
	sendTTS(t, ctx, c, map[string]any{"context_id": "ready", "text": "T", "flush": true})
	if n := multiUntilFinal(t, ctx, c, "ready"); n != 32640 {
		t.Fatalf("independent T: %d", n)
	}
	sendTTS(t, ctx, c, map[string]any{"context_id": "waiting", "close_context": true})
	if n := multiUntilFinal(t, ctx, c, "waiting"); n != 0 {
		t.Fatalf("cancel fabricated tail: %d", n)
	}
	sendTTS(t, ctx, c, map[string]any{"close_socket": true})
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("close: %v", err)
	}
}

func TestMultiCloseSocketFlushesAllContexts(t *testing.T) {
	server := httptest.NewServer(api.New())
	defer server.Close()
	c, ctx := openMulti(t, server, "", http.Header{"Xi-Api-Key": {"synthetic"}})
	sendTTS(t, ctx, c, map[string]any{"context_id": "a", "text": "E"})
	sendTTS(t, ctx, c, map[string]any{"context_id": "b", "text": "T"})
	sendTTS(t, ctx, c, map[string]any{"close_socket": true})
	totals := map[string]int{}
	finals := map[string]int{}
	for {
		_, raw, err := c.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				t.Fatal(err)
			}
			break
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		id, ok := m["contextId"].(string)
		if !ok {
			t.Fatalf("uncorrelated output: %v", m)
		}
		if m["isFinal"] == true {
			finals[id]++
			continue
		}
		if finals[id] != 0 {
			t.Fatal("audio after final")
		}
		pcm, err := base64.StdEncoding.DecodeString(m["audio"].(string))
		if err != nil {
			t.Fatal(err)
		}
		totals[id] += len(pcm)
	}
	if totals["a"] != 28800 || totals["b"] != 32640 || finals["a"] != 1 || finals["b"] != 1 {
		t.Fatalf("totals=%v finals=%v", totals, finals)
	}
}

func TestMultiContextCredentialAndDefaultContext(t *testing.T) {
	server := httptest.NewServer(api.New())
	defer server.Close()
	c, ctx := openMulti(t, server, "", http.Header{})
	sendTTS(t, ctx, c, map[string]any{"text": " ", "xi_api_key": "synthetic"})
	sendTTS(t, ctx, c, map[string]any{"text": "E", "flush": true})
	if n := multiUntilFinal(t, ctx, c, "default"); n != 28800 {
		t.Fatalf("default: %d", n)
	}
	// Initialization credentials may be included on the first message of a new
	// context, as documented by the SDK. They cannot change an active context.
	sendTTS(t, ctx, c, map[string]any{"context_id": "new", "text": "T", "xi_api_key": "synthetic", "flush": true})
	if n := multiUntilFinal(t, ctx, c, "new"); n != 32640 {
		t.Fatalf("new context: %d", n)
	}
	sendTTS(t, ctx, c, map[string]any{"context_id": "new", "text": "E", "xi_api_key": "changed"})
	if m := readSTT(t, ctx, c); m["error"] != "invalid_request" {
		t.Fatalf("credential mutation: %v", m)
	}
}

func TestMultiContextLimitAndTokenReplay(t *testing.T) {
	handler := api.New()
	server := httptest.NewServer(handler)
	defer server.Close()
	token := issueToken(t, handler, "tts_websocket")
	c, ctx := openMulti(t, server, "single_use_token="+token, http.Header{})
	for _, id := range []string{"one", "two", "three", "four", "five", "six"} {
		sendTTS(t, ctx, c, map[string]any{"context_id": id, "text": " "})
	}
	if m := readSTT(t, ctx, c); m["error"] != "too_many_contexts" {
		t.Fatalf("context bound: %v", m)
	}
	_, _, _ = c.Read(ctx)
	conn, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/text-to-speech/morse/multi-stream-input?single_use_token="+token, &websocket.DialOptions{HTTPHeader: http.Header{"Xi-Api-Key": {"synthetic"}}})
	if conn != nil {
		conn.CloseNow()
	}
	if err == nil || response == nil || response.StatusCode != 401 {
		t.Fatal("replayed token fell back to a key")
	}
}

func TestMultiInactiveContextClosesNormally(t *testing.T) {
	server := httptest.NewServer(api.New())
	defer server.Close()
	c, ctx := openMulti(t, server, "inactivity_timeout=1", http.Header{"Xi-Api-Key": {"synthetic"}})
	sendTTS(t, ctx, c, map[string]any{"context_id": "idle", "text": " "})
	if n := multiUntilFinal(t, ctx, c, "idle"); n != 0 {
		t.Fatalf("idle audio: %d", n)
	}
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("idle close: %v", err)
	}
}
