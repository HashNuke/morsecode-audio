package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/HashNuke/morsecode-audio/internal/morse"
	"github.com/coder/websocket"
)

type ttsInput struct {
	Text    *string `json:"text"`
	Flush   bool    `json:"flush"`
	Trigger bool    `json:"try_trigger_generation"`
	Voice   *struct {
		Stability  *float64 `json:"stability"`
		Similarity *float64 `json:"similarity_boost"`
		Style      *float64 `json:"style"`
		Boost      *bool    `json:"use_speaker_boost"`
		Speed      *float64 `json:"speed"`
	} `json:"voice_settings"`
	Generation *struct {
		Schedule []int `json:"chunk_length_schedule"`
	} `json:"generation_config"`
	Key           string `json:"xi_api_key"`
	HeaderKey     string `json:"xi-api-key"`
	Authorization string `json:"authorization"`
}

func (s *Server) realtimeTTS(w http.ResponseWriter, r *http.Request) {
	if !knownVoice(w, r) {
		return
	}
	rate, idle, err := ttsSocketOptions(r.URL.Query())
	if err != nil {
		writeError(w, 422, err.Error(), "Unsupported realtime synthesis options")
		return
	}
	// A connected TTS session reserves one synthesis slot, also shared with HTTP.
	select {
	case s.ttsSlots <- struct{}{}:
		defer func() { <-s.ttsSlots }()
	default:
		writeError(w, 429, "too_many_concurrent_requests", "Synthesis capacity is busy")
		return
	}
	socket := s.ttsSockets.accept(w, r)
	if socket == nil {
		return
	}
	defer socket.close()
	socket.conn.SetReadLimit(64 << 10)
	cfg := morse.DefaultConfig()
	cfg.SampleRate = rate
	encoder, _ := morse.NewStreamEncoder(cfg)
	initialized := false
	for {
		kind, data, err := socket.readWithin(idle)
		if err != nil {
			return
		}
		var input ttsInput
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if kind != websocket.MessageText || decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.Text == nil {
			ttsFailure(socket, "invalid_input", "Expected a JSON text message")
			return
		}
		if input.Voice != nil && input.Voice.Speed != nil && *input.Voice.Speed != 1 {
			ttsFailure(socket, "unsupported_voice_settings", "Morse speed is fixed at 20 WPM")
			return
		}
		if input.Generation != nil {
			for _, size := range input.Generation.Schedule {
				if size < 50 || size > 500 {
					ttsFailure(socket, "invalid_generation_config", "Invalid chunk schedule")
					return
				}
			}
		}
		if !initialized {
			if *input.Text != " " || input.Flush {
				ttsFailure(socket, "invalid_initialization", "Initialize with a single space")
				return
			}
			initialized = true
			continue
		}
		final := *input.Text == ""
		audio, err := encoder.Push(*input.Text, input.Flush || final)
		if err != nil {
			ttsFailure(socket, err.Error(), "Text is not supported Morse or exceeds the utterance limit")
			return
		}
		if err := writeTTSAudio(socket, audio); err != nil {
			return
		}
		if final {
			if socket.write(map[string]any{"audio": nil, "isFinal": true}) == nil {
				_ = socket.conn.Close(websocket.StatusNormalClosure, "")
			}
			return
		}
	}
}

func writeTTSAudio(socket *speechSocket, audio io.Reader) error {
	buffer := make([]byte, 4096)
	for {
		if err := socket.ctx.Err(); err != nil {
			return err
		}
		n, err := audio.Read(buffer)
		if n > 0 {
			if writeErr := socket.write(map[string]any{"audio": base64.StdEncoding.EncodeToString(buffer[:n]), "isFinal": false}); writeErr != nil {
				return writeErr
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func ttsFailure(socket *speechSocket, code, message string) {
	_ = socket.write(map[string]string{"error": code, "message": message})
	_ = socket.conn.Close(websocket.StatusPolicyViolation, code)
}

func ttsSocketOptions(q url.Values) (int, time.Duration, error) {
	rate, idle := 16000, 20*time.Second
	for key, values := range q {
		if len(values) != 1 {
			return 0, 0, errors.New("duplicate_parameter")
		}
		value := values[0]
		switch key {
		case "model_id":
			if value != "morse_v1" && value != "eleven_flash_v2_5" && value != "eleven_multilingual_v2" && value != "eleven_v3" {
				return 0, 0, errors.New("model_not_found")
			}
		case "output_format":
			switch value {
			case "pcm_8000":
				rate = 8000
			case "pcm_16000":
				rate = 16000
			case "pcm_24000":
				rate = 24000
			case "pcm_48000":
				rate = 48000
			default:
				return 0, 0, errors.New("unsupported_output_format")
			}
		case "auto_mode", "enable_logging":
			if value != "true" && value != "false" {
				return 0, 0, errors.New("invalid_boolean")
			}
		case "inactivity_timeout":
			seconds, err := strconv.Atoi(value)
			if err != nil || seconds < 1 || seconds > 180 {
				return 0, 0, errors.New("invalid_timeout")
			}
			idle = time.Duration(seconds) * time.Second
		case "sync_alignment", "enable_ssml_parsing":
			if value != "false" {
				return 0, 0, errors.New("unsupported_parameter")
			}
		case "apply_text_normalization":
			if value != "auto" && value != "off" {
				return 0, 0, errors.New("unsupported_parameter")
			}
		default:
			return 0, 0, errors.New("unsupported_parameter")
		}
	}
	return rate, idle, nil
}
