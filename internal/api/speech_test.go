package api_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/HashNuke/morsecode-audio/internal/api"
)

func TestHTTPPCMToMultipartTranscript(t *testing.T) {
	s := api.New()
	r := httptest.NewRequest("POST", "/v1/text-to-speech/morse?output_format=pcm_16000", bytes.NewBufferString(`{"text":"SOS TEST 123","model_id":"morse_v1"}`))
	r.Header.Set("xi-api-key", "synthetic-tts")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Content-Type") != "audio/pcm" {
		t.Fatalf("TTS %d: %s", w.Code, w.Body)
	}
	pcm := append([]byte(nil), w.Body.Bytes()...)
	if len(pcm) < 32000 {
		t.Fatal("missing actual audio")
	}
	for _, format := range []string{"other", "pcm_s16le_16"} {
		data := pcm
		if format == "other" {
			data = wav(pcm)
		}
		w = transcribe(s, data, format)
		if w.Code != 200 {
			t.Fatalf("STT %d: %s", w.Code, w.Body)
		}
		var result struct {
			Text  string `json:"text"`
			Words []struct {
				Text       string `json:"text"`
				Start, End float64
			} `json:"words"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Text != "SOS TEST 123" || len(result.Words) != 5 || result.Words[0].Text != "SOS" {
			t.Fatalf("result: %s", w.Body)
		}
		if result.Words[0].End <= result.Words[0].Start || result.Words[4].Start <= result.Words[0].End {
			t.Fatalf("timing: %s", w.Body)
		}
	}
}

func TestHTTPIndependentSignalAndInvalidRequests(t *testing.T) {
	s := api.New()
	// A square-wave E, followed by a 14-unit silence tail; no production encoder.
	pcm := make([]byte, 16000*60/1000*15*2)
	for i := 0; i < 960; i++ {
		v := int16(3000)
		if (i*1400/16000)%2 != 0 {
			v = -3000
		}
		binary.LittleEndian.PutUint16(pcm[2*i:], uint16(v))
	}
	w := transcribe(s, wav(pcm), "other")
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != 200 || body["text"] != "E" {
		t.Fatalf("independent signal: %d %s", w.Code, w.Body)
	}
	w = transcribe(s, wav(make([]byte, 32000)), "other")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != 200 || body["text"] != "" {
		t.Fatalf("fabricated silence: %d %s", w.Code, w.Body)
	}
	for _, data := range [][]byte{{1, 2, 3}, []byte("RIFFgarbage")} {
		if w := transcribe(s, data, "other"); w.Code != 422 {
			t.Fatalf("invalid upload: %d", w.Code)
		}
	}
	for _, body := range []string{`{"text":"%"}`, `{"text":""}`, `{"text":"E","model_id":"invented"}`, `{"text":"E","unknown":true}`} {
		r := httptest.NewRequest("POST", "/v1/text-to-speech/morse?output_format=pcm_16000", bytes.NewBufferString(body))
		r.Header.Set("xi-api-key", "test")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 422 {
			t.Errorf("invalid request accepted: %s status %d", body, w.Code)
		}
	}
}

func transcribe(s *api.Server, data []byte, format string) *httptest.ResponseRecorder {
	return transcribeFields(s, data, format, nil)
}

func TestSDKNullableTranscriptionFields(t *testing.T) {
	s := api.New()
	pcm := make([]byte, 3200)
	fields := map[string]string{"webhook_metadata": "null", "entity_detection": "null", "entity_redaction": "null"}
	w := transcribeFields(s, pcm, "pcm_s16le_16", fields)
	if w.Code != 200 {
		t.Fatalf("SDK omitted nullable fields: %d %s", w.Code, w.Body)
	}
	fields["entity_detection"] = `"all"`
	if w := transcribeFields(s, pcm, "pcm_s16le_16", fields); w.Code != 422 {
		t.Fatalf("unsupported transformation succeeded: %d", w.Code)
	}
}

func transcribeFields(s *api.Server, data []byte, format string, fields map[string]string) *httptest.ResponseRecorder {
	var body bytes.Buffer
	m := multipart.NewWriter(&body)
	f, _ := m.CreateFormFile("file", "input.wav")
	_, _ = f.Write(data)
	_ = m.WriteField("model_id", "scribe_v2")
	_ = m.WriteField("file_format", format)
	for key, value := range fields {
		_ = m.WriteField(key, value)
	}
	_ = m.Close()
	r := httptest.NewRequest("POST", "/v1/speech-to-text", &body)
	r.Header.Set("Content-Type", m.FormDataContentType())
	r.Header.Set("xi-api-key", "synthetic-stt")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func wav(pcm []byte) []byte {
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+len(pcm)))
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, uint32(16))
	for _, v := range []uint16{1, 1} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	for _, v := range []uint32{16000, 32000} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	for _, v := range []uint16{2, 16} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(pcm)))
	b.Write(pcm)
	return b.Bytes()
}
