package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/HashNuke/morsecode-audio/internal/audio"
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
	timed := strings.HasSuffix(r.URL.Path, "/with-timestamps")
	format, err := audio.ParseOutputFormat(r.URL.Query().Get("output_format"), streaming)
	if err != nil {
		writeError(w, 422, "unsupported_output_format", "Unsupported output format")
		return
	}
	cfg := morse.DefaultConfig()
	cfg.SampleRate = format.Rate
	encoder, err := morse.NewEncoder(input.Text, cfg)
	if err != nil {
		writeError(w, 422, err.Error(), "Cannot synthesize this Morse input")
		return
	}
	output, err := audio.Encode(ctx, encoder, encoder.Samples, format)
	if err != nil {
		writeError(w, 503, "codec_unavailable", "Audio codec is unavailable")
		return
	}
	defer output.Close()
	if timed {
		original := originalCharacters(input.Text, encoder.Characters)
		if streaming {
			writeTimedStream(w, ctx, output, format, original, encoder.Characters)
		} else {
			writeTimed(w, ctx, output, original, encoder.Characters)
		}
		return
	}
	buffer := make([]byte, 4096)
	n, readErr := output.Read(buffer)
	if readErr != nil && readErr != io.EOF {
		writeError(w, 502, "audio_encoding_failed", "Audio encoding failed")
		return
	}
	w.Header().Set("Content-Type", format.MIME)
	w.Header().Set("Cache-Control", "no-store")
	if !streaming && output.Size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(output.Size, 10))
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
		n, readErr = output.Read(buffer)
	}
}
