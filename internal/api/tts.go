package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/HashNuke/morsecode-audio/internal/morse"
)

type ttsRequest struct {
	Text    string `json:"text"`
	ModelID string `json:"model_id"`
}

func (s *Server) synthesize(w http.ResponseWriter, r *http.Request) {
	if !knownVoice(w, r) {
		return
	}
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(20 * time.Second))
	_ = controller.SetWriteDeadline(time.Now().Add(30 * time.Second))
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	defer r.Body.Close()
	var input ttsRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, 422, "invalid_request", "Invalid synthesis request")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, 422, "invalid_request", "Expected one JSON object")
		return
	}
	if input.ModelID != "" && input.ModelID != "morse_v1" && input.ModelID != "eleven_flash_v2_5" && input.ModelID != "eleven_multilingual_v2" && input.ModelID != "eleven_v3" {
		writeError(w, 422, "model_not_found", "Unknown synthesis model")
		return
	}
	format := r.URL.Query().Get("output_format")
	if !strings.HasPrefix(format, "pcm_") {
		writeError(w, 422, "unsupported_output_format", "This checkpoint supports explicit PCM output only")
		return
	}
	rate, err := strconv.Atoi(strings.TrimPrefix(format, "pcm_"))
	if err != nil {
		writeError(w, 422, "unsupported_output_format", "Invalid PCM sample rate")
		return
	}
	cfg := morse.DefaultConfig()
	cfg.SampleRate = rate
	encoder, err := morse.NewEncoder(input.Text, cfg)
	if err != nil {
		writeError(w, 422, err.Error(), "Cannot synthesize this Morse input")
		return
	}
	w.Header().Set("Content-Type", "audio/pcm")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(encoder.Samples*2))
	buffer := make([]byte, 4096)
	for {
		if r.Context().Err() != nil {
			return
		}
		n, err := encoder.Read(buffer)
		if n > 0 {
			if _, err := w.Write(buffer[:n]); err != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}
