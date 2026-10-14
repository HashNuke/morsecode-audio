package api_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/HashNuke/morsecode-audio/internal/api"
	"github.com/HashNuke/morsecode-audio/internal/morse"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func openTTS(t *testing.T, server *httptest.Server, query string, headers http.Header) (*websocket.Conn, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	t.Cleanup(cancel)
	c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/text-to-speech/morse/stream-input?"+query, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("TTS upgrade failed: HTTP %d", status)
	}
	c.SetReadLimit(1 << 20)
	t.Cleanup(func() { c.CloseNow() })
	return c, ctx
}

func sendTTS(t *testing.T, ctx context.Context, c *websocket.Conn, body any) {
	t.Helper()
	if err := wsjson.Write(ctx, c, body); err != nil {
		t.Fatal(err)
	}
}

type ttsCapture struct {
	pcm                  []byte
	original, normalized []string
	final                int
}

func (r *ttsCapture) read(t *testing.T, ctx context.Context, c *websocket.Conn) map[string]any {
	t.Helper()
	m := readSTT(t, ctx, c)
	if m["error"] != nil {
		t.Fatalf("TTS error: %v", m)
	}
	if m["isFinal"] == true {
		r.final++
		return m
	}
	encoded, ok := m["audio"].(string)
	if !ok {
		t.Fatalf("missing audio: %v", m)
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	r.pcm = append(r.pcm, data...)
	for name, dest := range map[string]*[]string{"alignment": &r.original, "normalizedAlignment": &r.normalized} {
		if a, ok := m[name].(map[string]any); ok {
			chars := a["chars"].([]any)
			starts := a["charStartTimesMs"].([]any)
			durations := a["charDurationsMs"].([]any)
			if len(chars) != len(starts) || len(chars) != len(durations) {
				t.Fatal("misaligned character arrays")
			}
			for i, v := range chars {
				if starts[i].(float64) < 0 || durations[i].(float64) < 0 {
					t.Fatal("negative alignment")
				}
				*dest = append(*dest, v.(string))
			}
		}
	}
	return m
}
func (r *ttsCapture) finish(t *testing.T, ctx context.Context, c *websocket.Conn) {
	t.Helper()
	for r.final == 0 {
		r.read(t, ctx, c)
	}
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("end-input close: %v", err)
	}
}

func TestTTSWebSocketIncrementalAlignmentAndFinal(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	c, ctx := openTTS(t, s, "output_format=pcm_16000&auto_mode=true&sync_alignment=true", http.Header{"Xi-Api-Key": {"synthetic"}})
	sendTTS(t, ctx, c, map[string]any{"text": " ", "voice_settings": nil, "generation_config": map[string]any{"chunk_length_schedule": []int{50}}, "try_trigger_generation": true})
	sendTTS(t, ctx, c, map[string]any{"text": " e"})
	var got ttsCapture
	got.read(t, ctx, c) // First PCM must arrive before any following text or EOS.
	if len(got.pcm) == 0 || len(got.pcm) > 1920 {
		t.Fatalf("unexpected first-fragment bytes: %d", len(got.pcm))
	}
	sendTTS(t, ctx, c, map[string]any{"text": "t "})
	sendTTS(t, ctx, c, map[string]any{"text": " e "})
	sendTTS(t, ctx, c, map[string]any{"text": ""})
	got.finish(t, ctx, c)
	wantEncoder, _ := morse.NewEncoder(" et  e ", morse.DefaultConfig())
	want, _ := io.ReadAll(wantEncoder)
	if !bytes.Equal(got.pcm, want) || strings.Join(got.original, "") != " et  e " || strings.Join(got.normalized, "") != "ET E" || got.final != 1 {
		t.Fatalf("fragmented output bytes=%d originals=%q normalized=%q final=%d", len(got.pcm), strings.Join(got.original, ""), strings.Join(got.normalized, ""), got.final)
	}
}

func TestTTSWebSocketFlushKeepsConnectionUsable(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	c, ctx := openTTS(t, s, "output_format=pcm_16000", http.Header{"Xi-Api-Key": {"synthetic"}})
	sendTTS(t, ctx, c, map[string]any{"text": " "})
	for _, text := range []string{"E", "T"} {
		sendTTS(t, ctx, c, map[string]any{"text": text, "flush": true})
		wantEncoder, _ := morse.NewEncoder(text, morse.DefaultConfig())
		want, _ := io.ReadAll(wantEncoder)
		var got ttsCapture
		for len(got.pcm) < len(want) {
			got.read(t, ctx, c)
			if got.final != 0 {
				t.Fatal("flush closed the text stream")
			}
		}
		if !bytes.Equal(got.pcm, want) || strings.Join(got.normalized, "") != text {
			t.Fatal("flush lost text or pending tail")
		}
	}
	sendTTS(t, ctx, c, map[string]any{"text": ""})
	var end ttsCapture
	end.finish(t, ctx, c)
	if len(end.pcm) != 0 {
		t.Fatal("empty EOS fabricated audio")
	}
}

func TestTTSWebSocketAuthenticationLocations(t *testing.T) {
	handler := api.New()
	s := httptest.NewServer(handler)
	defer s.Close()
	for _, kind := range []string{"header", "bearer-header", "query-bearer", "initial-key", "initial-hyphen-key", "initial-bearer", "token"} {
		t.Run(kind, func(t *testing.T) {
			query := "output_format=pcm_16000"
			headers := http.Header{"Origin": {"https://browser.example"}}
			init := map[string]any{"text": " "}
			switch kind {
			case "header":
				headers.Set("xi-api-key", "synthetic")
			case "bearer-header":
				headers.Set("Authorization", "Bearer synthetic")
			case "query-bearer":
				query += "&authorization=" + url.QueryEscape("Bearer synthetic")
			case "initial-key":
				init["xi_api_key"] = "synthetic"
			case "initial-hyphen-key":
				init["xi-api-key"] = "synthetic"
			case "initial-bearer":
				init["authorization"] = "Bearer synthetic"
			case "token":
				query += "&single_use_token=" + url.QueryEscape(issueToken(t, handler, "tts_websocket"))
			}
			c, ctx := openTTS(t, s, query, headers)
			sendTTS(t, ctx, c, init)
			sendTTS(t, ctx, c, map[string]any{"text": "E", "flush": true})
			sendTTS(t, ctx, c, map[string]any{"text": ""})
			var got ttsCapture
			got.finish(t, ctx, c)
			if len(got.pcm) != 28800 {
				t.Fatalf("authenticated E: %d bytes", len(got.pcm))
			}
		})
	}
}

func TestTTSWebSocketInvalidInitializationAndInput(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	for _, test := range []struct {
		name          string
		key           bool
		first, second any
		status        string
	}{
		{"anonymous", false, map[string]any{"text": " "}, nil, "needs_authorization"},
		{"wrong-init", true, map[string]any{"text": "E"}, nil, "invalid_request"},
		{"unsupported-text", true, map[string]any{"text": " "}, map[string]any{"text": "%", "flush": true}, "unsupported_character"},
		{"missing-text", true, map[string]any{"text": " "}, map[string]any{"flush": true}, "invalid_request"},
		{"credential-change", true, map[string]any{"text": " "}, map[string]any{"text": "E", "xi_api_key": "changed"}, "invalid_request"},
		{"unknown-field", true, map[string]any{"text": " "}, map[string]any{"text": "E", "surprise": true}, "invalid_request"},
	} {
		t.Run(test.name, func(t *testing.T) {
			headers := http.Header{}
			if test.key {
				headers.Set("xi-api-key", "synthetic")
			}
			c, ctx := openTTS(t, s, "output_format=pcm_16000", headers)
			sendTTS(t, ctx, c, test.first)
			if test.second != nil {
				sendTTS(t, ctx, c, test.second)
			}
			message := readSTT(t, ctx, c)
			if message["error"] != test.status {
				t.Fatalf("protocol error: %v", message)
			}
			if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
				t.Fatalf("invalid input close: %v", err)
			}
		})
	}
}

func TestTTSWebSocketTokenPrecedenceAndReplay(t *testing.T) {
	handler := api.New()
	s := httptest.NewServer(handler)
	defer s.Close()
	token := issueToken(t, handler, "tts_websocket")
	c, ctx := openTTS(t, s, "single_use_token="+token, http.Header{})
	sendTTS(t, ctx, c, map[string]any{"text": " "})
	sendTTS(t, ctx, c, map[string]any{"text": ""})
	var got ttsCapture
	got.finish(t, ctx, c)
	for _, value := range []string{token, "", issueToken(t, handler, "batch_scribe")} {
		c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(s.URL, "http")+"/v1/text-to-speech/morse/stream-input?single_use_token="+value, &websocket.DialOptions{HTTPHeader: http.Header{"Xi-Api-Key": {"synthetic-fallback"}}})
		if c != nil {
			c.CloseNow()
		}
		if err == nil || resp == nil || resp.StatusCode != 401 {
			t.Fatal("invalid TTS token fell back to header")
		}
	}
}

func TestTTSWebSocketOptionAndFrameErrors(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	for _, q := range []string{"output_format=wav_16000", "inactivity_timeout=181", "auto_mode=maybe", "auto_mode=true&auto_mode=false", "enable_ssml_parsing=true", "seed=-1", "model_id=unknown", "surprise=1"} {
		c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(s.URL, "http")+"/v1/text-to-speech/morse/stream-input?"+q, &websocket.DialOptions{HTTPHeader: http.Header{"Xi-Api-Key": {"synthetic"}}})
		if c != nil {
			c.CloseNow()
		}
		if err == nil || resp == nil || resp.StatusCode != 422 {
			t.Fatalf("invalid options accepted: %s", q)
		}
	}
	for _, data := range [][]byte{[]byte(`{"text":" "} {}`), []byte(`{"text":4}`), []byte(`[]`), []byte(`{"text":" ","generation_config":{"chunk_length_schedule":[1]}}`)} {
		c, ctx := openTTS(t, s, "", http.Header{"Xi-Api-Key": {"synthetic"}})
		if err := c.Write(ctx, websocket.MessageText, data); err != nil {
			t.Fatal(err)
		}
		if m := readSTT(t, ctx, c); m["error"] == nil {
			t.Fatal("invalid frame accepted")
		}
		_, _, _ = c.Read(ctx)
	}
}
