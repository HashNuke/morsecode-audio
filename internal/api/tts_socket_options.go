package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strconv"
	"time"

	"github.com/HashNuke/morsecode-audio/internal/audio"
)

type ttsSocketOptions struct {
	format audio.OutputFormat
	auto   bool
	idle   time.Duration
}

func parseTTSSocketOptions(q url.Values) (ttsSocketOptions, error) {
	o := ttsSocketOptions{idle: 20 * time.Second}
	for key, values := range q {
		if len(values) != 1 {
			return o, errors.New("duplicate_parameter")
		}
		switch key {
		case "output_format", "model_id", "auto_mode", "sync_alignment", "enable_logging", "inactivity_timeout", "single_use_token", "authorization", "seed", "language_code", "enable_ssml_parsing", "apply_text_normalization":
		default:
			return o, errors.New("unsupported_parameter")
		}
	}
	var err error
	o.format, err = audio.ParseOutputFormat(q.Get("output_format"), true)
	if err != nil {
		return o, err
	}
	if !ttsModel(q.Get("model_id")) {
		return o, errors.New("model_not_found")
	}
	for _, key := range []string{"auto_mode", "sync_alignment", "enable_logging", "enable_ssml_parsing"} {
		if values, ok := q[key]; ok {
			if values[0] != "true" && values[0] != "false" {
				return o, errors.New("invalid_boolean")
			}
			if key == "auto_mode" {
				o.auto = values[0] == "true"
			}
			if key == "enable_ssml_parsing" && values[0] == "true" {
				return o, errors.New("unsupported_feature")
			}
		}
	}
	if values, ok := q["inactivity_timeout"]; ok {
		seconds, err := strconv.Atoi(values[0])
		if err != nil || seconds < 1 || seconds > 180 {
			return o, errors.New("invalid_inactivity_timeout")
		}
		o.idle = time.Duration(seconds) * time.Second
	}
	if value := q.Get("apply_text_normalization"); value != "" && value != "auto" && value != "off" {
		return o, errors.New("unsupported_feature")
	}
	if values, ok := q["seed"]; ok {
		if _, err := strconv.ParseUint(values[0], 10, 32); err != nil {
			return o, errors.New("invalid_seed")
		}
	}
	if value := q.Get("language_code"); value != "" && !languagePattern.MatchString(value) {
		return o, errors.New("invalid_language")
	}
	return o, nil
}

func ttsModel(model string) bool {
	return model == "" || model == "morse_v1" || model == "eleven_multilingual_v2" || model == "eleven_flash_v2_5" || model == "eleven_v3"
}

type ttsGenerationConfig struct {
	Schedule []int `json:"chunk_length_schedule"`
}
type ttsSocketVoiceSettings struct {
	Stability    *float64 `json:"stability"`
	Similarity   *float64 `json:"similarity_boost"`
	Style        *float64 `json:"style"`
	Speed        *float64 `json:"speed"`
	SpeakerBoost *bool    `json:"use_speaker_boost"`
}
type ttsSocketMessage struct {
	Text          *string                 `json:"text"`
	Flush         bool                    `json:"flush"`
	Trigger       bool                    `json:"try_trigger_generation"`
	Voice         *ttsSocketVoiceSettings `json:"voice_settings"`
	Generation    *ttsGenerationConfig    `json:"generation_config"`
	Generator     *ttsGenerationConfig    `json:"generator_config"`
	Dictionaries  []json.RawMessage       `json:"pronunciation_dictionary_locators"`
	Key           *string                 `json:"xi_api_key"`
	HyphenKey     *string                 `json:"xi-api-key"`
	Authorization *string                 `json:"authorization"`
}

func parseTTSSocketMessage(data []byte) (ttsSocketMessage, error) {
	var m ttsSocketMessage
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&m) != nil || d.Decode(new(any)) != io.EOF || m.Text == nil {
		return m, errors.New("invalid_request")
	}
	return validateTTSSocketMessage(m)
}

func validateTTSSocketMessage(m ttsSocketMessage) (ttsSocketMessage, error) {
	if len(m.Dictionaries) != 0 {
		return m, errors.New("unsupported_feature")
	}
	if m.Voice != nil {
		for _, value := range []*float64{m.Voice.Stability, m.Voice.Similarity, m.Voice.Style} {
			if value != nil && !(*value >= 0 && *value <= 1) {
				return m, errors.New("invalid_voice_settings")
			}
		}
		if m.Voice.Speed != nil && *m.Voice.Speed != 1 {
			return m, errors.New("unsupported_feature")
		}
	}
	if m.Generation != nil && m.Generator != nil {
		return m, errors.New("invalid_request")
	}
	if m.Generator != nil {
		m.Generation = m.Generator
	}
	if m.Generation != nil {
		if len(m.Generation.Schedule) == 0 || len(m.Generation.Schedule) > 16 {
			return m, errors.New("invalid_generation_config")
		}
		for _, n := range m.Generation.Schedule {
			if n < 50 || n > 500 {
				return m, errors.New("invalid_generation_config")
			}
		}
	}
	return m, nil
}
