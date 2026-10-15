package api_test

import (
	"bytes"
	"encoding/binary"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HashNuke/morsecode-audio/internal/api"
)

func TestDefaultTTSIsLocalMorsePCMWithoutExternalCodecs(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	s := api.New()
	for _, suffix := range []string{"", "/stream"} {
		r := httptest.NewRequest("POST", "/v1/text-to-speech/morse"+suffix, strings.NewReader(`{"text":"E","model_id":"morse_v1"}`))
		r.Header.Set("xi-api-key", "any chosen key")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 200 || w.Header().Get("Content-Type") != "audio/pcm" {
			t.Fatalf("PCM response: %d %s", w.Code, w.Body)
		}
		// E is one 60 ms dot, then fourteen units of final silence at 16 kHz.
		pcm := w.Body.Bytes()
		if len(pcm) != 16000*60/1000*15*2 || bytes.Equal(pcm[:1920], make([]byte, 1920)) {
			t.Fatal("missing Morse dot or wrong duration")
		}
		for i := 1920; i < len(pcm); i += 2 {
			if binary.LittleEndian.Uint16(pcm[i:]) != 0 {
				t.Fatal("utterance tail is not silence")
			}
		}
		if suffix != "" && !w.Flushed {
			t.Fatal("PCM stream did not flush")
		}
	}
}

func TestUnsupportedSpeechExtensionsAreExplicit(t *testing.T) {
	s := api.New()
	for _, path := range []string{
		"/v1/text-to-speech/morse/with-timestamps",
		"/v1/text-to-speech/morse/stream/with-timestamps",
		"/v1/single-use-token/realtime_scribe",
	} {
		if w := request(s, "POST", path, "synthetic"); w.Code != 404 {
			t.Errorf("unsupported route %s: %d", path, w.Code)
		}
	}
	for _, format := range []string{"mp3_44100_128", "opus_48000_64", "ulaw_8000", "wav_16000"} {
		r := httptest.NewRequest("POST", "/v1/text-to-speech/morse?output_format="+format, strings.NewReader(`{"text":"E"}`))
		r.Header.Set("xi-api-key", "synthetic")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 422 {
			t.Errorf("unsupported output %s: %d", format, w.Code)
		}
	}
}
