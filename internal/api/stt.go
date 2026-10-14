package api

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/HashNuke/morsecode-audio/internal/audio"
	"github.com/HashNuke/morsecode-audio/internal/morse"
)

const maxUploadBytes = 12 << 20

func (s *Server) transcribe(w http.ResponseWriter, r *http.Request) {
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(30 * time.Second))
	_ = controller.SetWriteDeadline(time.Now().Add(45 * time.Second))
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+(64<<10))
	defer r.Body.Close()
	fields, file, err := readTranscription(r)
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) || errors.Is(err, errUploadLimit) {
			writeError(w, 413, "request_too_large", "Upload exceeds the service limit")
			return
		}
		writeError(w, 422, "invalid_request", "Expected one bounded multipart audio file")
		return
	}
	model := fields["model_id"]
	if model != "morse_stt_v1" && model != "scribe_v1" && model != "scribe_v2" {
		writeError(w, 422, "model_not_found", "A supported transcription model_id is required")
		return
	}
	granularity := fields["timestamps_granularity"]
	if granularity == "" {
		granularity = "word"
	}
	if granularity != "none" && granularity != "word" && granularity != "character" {
		writeError(w, 422, "invalid_request", "Unknown timestamp granularity")
		return
	}
	pcm, rate := file, 16000
	switch fields["file_format"] {
	case "pcm_s16le_16":
	case "", "other":
		pcm, rate, err = audio.PCMFromWAV(file)
	default:
		err = errors.New("unsupported_file_format")
	}
	if err != nil || len(pcm) == 0 || len(pcm)%2 != 0 {
		writeError(w, 422, "invalid_audio", "Expected supported PCM16 audio")
		return
	}
	cfg := morse.DefaultConfig()
	cfg.SampleRate = rate
	decoder, err := morse.NewDecoder(cfg)
	if err != nil {
		writeError(w, 422, "invalid_audio", "Unsupported sample rate")
		return
	}
	var finals []morse.Event
	for offset := 0; offset < len(pcm); {
		if r.Context().Err() != nil {
			return
		}
		end := min(offset+8192, len(pcm))
		events, pushErr := decoder.Push(pcm[offset:end])
		if pushErr != nil {
			writeError(w, 422, "invalid_audio", "Audio is not supported Morse")
			return
		}
		for _, e := range events {
			if e.Kind == "final" {
				finals = append(finals, e)
			}
		}
		offset = end
	}
	events, err := decoder.Flush()
	if err != nil {
		writeError(w, 422, "invalid_audio", "Incomplete Morse signal")
		return
	}
	for _, e := range events {
		if e.Kind == "final" {
			finals = append(finals, e)
		}
	}
	texts := make([]string, 0, len(finals))
	characters := make([]morse.Character, 0)
	for _, event := range finals {
		texts = append(texts, event.Text)
		if len(characters) > 0 && len(event.Characters) > 0 {
			characters = append(characters, morse.Character{Text: " ", Start: characters[len(characters)-1].End, End: event.Characters[0].Start})
		}
		characters = append(characters, event.Characters...)
	}
	language := fields["language_code"]
	if language == "" {
		language = "und"
	}
	words := []map[string]any{}
	if granularity != "none" {
		words = transcriptWords(characters, granularity == "character")
	}
	writeJSON(w, 200, map[string]any{"language_code": language, "language_probability": 0, "text": strings.Join(texts, " "), "words": words, "audio_duration_secs": float64(len(pcm)) / float64(2*rate)})
}

var errUploadLimit = errors.New("upload_limit")

func readTranscription(r *http.Request) (map[string]string, []byte, error) {
	m, err := r.MultipartReader()
	if err != nil {
		return nil, nil, err
	}
	fields := make(map[string]string)
	var file []byte
	for parts := 0; ; parts++ {
		p, err := m.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		if parts >= 16 {
			p.Close()
			return nil, nil, errUploadLimit
		}
		name := p.FormName()
		if name == "file" {
			if file != nil {
				p.Close()
				return nil, nil, errors.New("duplicate_file")
			}
			file, err = io.ReadAll(io.LimitReader(p, maxUploadBytes+1))
			if len(file) > maxUploadBytes {
				err = errUploadLimit
			}
		} else {
			if _, exists := fields[name]; exists {
				p.Close()
				return nil, nil, errors.New("duplicate_field")
			}
			switch name {
			case "model_id", "file_format", "language_code", "timestamps_granularity", "webhook_metadata", "entity_detection", "entity_redaction":
			default:
				p.Close()
				return nil, nil, errors.New("unsupported_field")
			}
			var value []byte
			value, err = io.ReadAll(io.LimitReader(p, 4097))
			if len(value) > 4096 {
				err = errUploadLimit
			}
			fields[name] = string(value)
			if name == "webhook_metadata" || name == "entity_detection" || name == "entity_redaction" {
				if strings.TrimSpace(string(value)) != "null" {
					err = errors.New("unsupported_field")
				}
			}
		}
		p.Close()
		if err != nil {
			return nil, nil, err
		}
	}
	if file == nil {
		return nil, nil, errors.New("missing_file")
	}
	return fields, file, nil
}

func transcriptWords(chars []morse.Character, includeCharacters bool) []map[string]any {
	words := []map[string]any{}
	for i := 0; i < len(chars); {
		start := i
		if chars[i].Text == " " {
			i++
		} else {
			for i < len(chars) && chars[i].Text != " " {
				i++
			}
		}
		text := ""
		for _, c := range chars[start:i] {
			text += c.Text
		}
		kind := "word"
		if text == " " {
			kind = "spacing"
		}
		// The accepted Morse codebook lookup is deterministic (log(1) = 0).
		// This is not a calibrated probability of recognizing arbitrary speech.
		word := map[string]any{"text": text, "type": kind, "start": chars[start].Start, "end": chars[i-1].End, "logprob": 0}
		if includeCharacters {
			word["characters"] = chars[start:i]
		}
		words = append(words, word)
	}
	return words
}
