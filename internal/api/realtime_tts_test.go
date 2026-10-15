package api_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HashNuke/morsecode-audio/internal/api"
	"github.com/HashNuke/morsecode-audio/internal/morse"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func dialTTS(t *testing.T, server *httptest.Server, rate int) (*websocket.Conn, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	c, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+fmt.Sprintf("/v1/text-to-speech/morse/stream-input?model_id=morse_v1&output_format=pcm_%d&auto_mode=true", rate), nil)
	if err != nil {
		t.Fatalf("TTS upgrade: %v (%v)", err, response)
	}
	t.Cleanup(func() { c.CloseNow() })
	return c, ctx
}

func ttsMessage(t *testing.T, ctx context.Context, c *websocket.Conn, message any) {
	t.Helper()
	if err := wsjson.Write(ctx, c, message); err != nil {
		t.Fatal(err)
	}
}

func ttsAudio(t *testing.T, ctx context.Context, c *websocket.Conn, size int) []byte {
	t.Helper()
	var audio []byte
	for len(audio) < size {
		message := readSTT(t, ctx, c)
		if message["isFinal"] != false {
			t.Fatalf("unexpected TTS event: %v", message)
		}
		encoded, _ := message["audio"].(string)
		pcm, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(pcm) == 0 || len(pcm) > 4096 || len(pcm)%2 != 0 {
			t.Fatal("invalid bounded PCM frame")
		}
		audio = append(audio, pcm...)
	}
	if len(audio) != size {
		t.Fatal("unexpected audio tail")
	}
	return audio
}

func TestRealtimeTTSStreamsBeforeEndOfInput(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	for _, rate := range []int{8000, 16000, 24000, 48000} {
		c, ctx := dialTTS(t, s, rate)
		ttsMessage(t, ctx, c, map[string]any{"text": " ", "voice_settings": map[string]any{"stability": .5, "similarity_boost": .8, "speed": 1}})
		ttsMessage(t, ctx, c, map[string]any{"text": "S", "try_trigger_generation": true})
		// Audio must arrive before the rest of the text is even sent.
		first := ttsAudio(t, ctx, c, rate*60/1000*5*2)
		ttsMessage(t, ctx, c, map[string]any{"text": "OS"})
		ttsMessage(t, ctx, c, map[string]any{"text": ""})
		cfg := morse.DefaultConfig()
		cfg.SampleRate = rate
		encoder, _ := morse.NewEncoder("SOS", cfg)
		want, _ := io.ReadAll(encoder)
		actual := append(first, ttsAudio(t, ctx, c, len(want)-len(first))...)
		if !bytes.Equal(actual, want) {
			t.Fatal("fragmented synthesis changed waveform or timing")
		}
		message := readSTT(t, ctx, c)
		if message["isFinal"] != true || message["audio"] != nil {
			t.Fatalf("missing ElevenLabs final: %v", message)
		}
		if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
			t.Fatalf("expected normal EOS close: %v", err)
		}
	}
}

func TestRealtimeTTSFlushKeepsConnectionAndFinishesOneTail(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	c, ctx := dialTTS(t, s, 16000)
	ttsMessage(t, ctx, c, map[string]any{"text": " "})
	for _, text := range []string{"E T", "SOS"} {
		ttsMessage(t, ctx, c, map[string]any{"text": text, "flush": true})
		encoder, _ := morse.NewEncoder(text, morse.DefaultConfig())
		want, _ := io.ReadAll(encoder)
		if actual := ttsAudio(t, ctx, c, len(want)); !bytes.Equal(actual, want) {
			t.Fatal("flush differs from HTTP utterance")
		}
	}
	ttsMessage(t, ctx, c, map[string]any{"text": ""})
	if event := readSTT(t, ctx, c); event["isFinal"] != true {
		t.Fatalf("extra tail or missing final: %v", event)
	}
}

func TestRealtimeTTSRejectsInvalidInitialization(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	c, ctx := dialTTS(t, s, 16000)
	ttsMessage(t, ctx, c, map[string]any{"text": "SOS"})
	if event := readSTT(t, ctx, c); event["error"] == nil {
		t.Fatalf("invalid initialization accepted: %v", event)
	}
}

func TestRealtimeTTSCapacityAndShutdown(t *testing.T) {
	app := api.New()
	s := httptest.NewServer(app)
	defer s.Close()
	for range 8 {
		_, _ = dialTTS(t, s, 16000)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(s.URL, "http")+"/v1/text-to-speech/morse/stream-input", nil)
	if err == nil || response.StatusCode != 429 {
		t.Fatal("ninth TTS session must be rejected")
	}
	if response := request(app, "POST", "/v1/text-to-speech/morse/stream", ""); response.Code != 429 {
		t.Fatal("HTTP and WS synthesis must share capacity")
	}
	// STT has its own pool; all eight TTS sessions must not block recognition.
	_, _ = dialSTT(t, s, "model_id=morse_stt_v1")
	app.Drain()
	if err := app.WaitSockets(ctx); err != nil {
		t.Fatal("shutdown retained realtime speech sessions")
	}
	if response := request(app, "GET", "/v1/text-to-speech/morse/stream-input", ""); response.Code != 503 {
		t.Fatal("draining server accepted a new TTS stream")
	}
}

func TestRealtimeTTSDisconnectReleasesCapacity(t *testing.T) {
	app := api.New()
	finished := make(chan struct{}, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { finished <- struct{}{} }()
		app.ServeHTTP(w, r)
	}))
	defer s.Close()
	for range 10 {
		c, ctx := dialTTS(t, s, 16000)
		ttsMessage(t, ctx, c, map[string]any{"text": " "})
		ttsMessage(t, ctx, c, map[string]any{"text": strings.Repeat("T ", 480)})
		_ = readSTT(t, ctx, c)
		_ = c.CloseNow()
		select {
		case <-finished:
		case <-ctx.Done():
			t.Fatal("disconnected TTS handler retained")
		}
	}
}
