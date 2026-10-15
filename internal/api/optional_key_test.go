package api_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HashNuke/morsecode-audio/internal/api"
)

func TestHTTPSpeechIgnoresAPIKey(t *testing.T) {
	s := api.New()
	for _, key := range []string{"", "arbitrary-key"} {
		for _, path := range []string{"/v1/text-to-speech/morse", "/v1/text-to-speech/morse/stream"} {
			r := httptest.NewRequest("POST", path, strings.NewReader(`{"text":"E"}`))
			r.Header.Set("Content-Type", "application/json")
			if key != "" {
				r.Header.Set("xi-api-key", key)
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != 200 || w.Header().Get("Content-Type") != "audio/pcm" || w.Body.Len() == 0 {
				t.Fatalf("optional key TTS %s: %d", path, w.Code)
			}
		}
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		file, _ := form.CreateFormFile("file", "e.pcm")
		_, _ = file.Write(independentMorse("E", 16000))
		_ = form.WriteField("model_id", "morse_stt_v1")
		_ = form.WriteField("file_format", "pcm_s16le_16")
		_ = form.Close()
		r := httptest.NewRequest("POST", "/v1/speech-to-text", &body)
		r.Header.Set("Content-Type", form.FormDataContentType())
		if key != "" {
			r.Header.Set("xi-api-key", key)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		var reply struct{ Text string }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Text != "E" {
			t.Fatalf("optional key batch STT: %d %s", w.Code, w.Body)
		}
	}
}
