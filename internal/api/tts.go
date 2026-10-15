package api

import (
	"context"
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
	select {
	case s.ttsSlots <- struct{}{}:
		defer func() { <-s.ttsSlots }()
	default:
		writeError(w, 429, "too_many_concurrent_requests", "Synthesis capacity is busy")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
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
	streaming := strings.Contains(r.URL.Path, "/stream")
	rate := 16000
	switch r.URL.Query().Get("output_format") {
	case "", "pcm_16000":
	case "pcm_8000":
		rate = 8000
	case "pcm_24000":
		rate = 24000
	case "pcm_48000":
		rate = 48000
	default:
		writeError(w, 422, "unsupported_output_format", "Unsupported output format")
		return
	}
	cfg := morse.DefaultConfig()
	cfg.SampleRate = rate
	encoder, err := morse.NewEncoder(input.Text, cfg)
	if err != nil {
		writeError(w, 422, err.Error(), "Cannot synthesize this Morse input")
		return
	}
	// The encoder plans Morse runs but renders only the next buffer of PCM.
	// Writes apply backpressure; cancellation never requires finishing the audio.
	buffer := make([]byte, 4096)
	n, readErr := encoder.Read(buffer)
	if readErr != nil && readErr != io.EOF {
		writeError(w, 502, "audio_encoding_failed", "Audio encoding failed")
		return
	}
	w.Header().Set("Content-Type", "audio/pcm")
	w.Header().Set("Cache-Control", "no-store")
	if !streaming {
		w.Header().Set("Content-Length", strconv.Itoa(encoder.Samples*2))
	}
	for {
		if ctx.Err() != nil {
			panic(http.ErrAbortHandler)
		}
		if n > 0 {
			if _, err := w.Write(buffer[:n]); err != nil {
				return
			}
			if streaming {
				if err := controller.Flush(); err != nil {
					return
				}
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				panic(http.ErrAbortHandler)
			}
			return
		}
		n, readErr = encoder.Read(buffer)
	}
}
