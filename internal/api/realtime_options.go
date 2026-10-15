package api

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/HashNuke/morsecode-audio/internal/morse"
)

type realtimeOptions struct {
	Model      string  `json:"model_id"`
	Format     string  `json:"audio_format"`
	Rate       int     `json:"sample_rate"`
	Commit     string  `json:"commit_strategy"`
	Language   string  `json:"language_code"`
	Timestamps bool    `json:"include_timestamps"`
	Logging    bool    `json:"enable_logging"`
	Silence    float64 `json:"vad_silence_threshold_secs"`
	Keepalive  int     `json:"-"`
}

var languagePattern = regexp.MustCompile(`^[a-z]{2,3}$`)

func parseRealtimeOptions(q url.Values) (realtimeOptions, error) {
	o := realtimeOptions{Model: "scribe_v2_realtime", Format: "pcm_16000", Rate: 16000, Commit: "manual", Language: "und", Silence: .84}
	for key, values := range q {
		if len(values) != 1 {
			return o, errors.New("duplicate_parameter")
		}
		switch key {
		case "model_id", "audio_format", "commit_strategy", "language_code", "include_timestamps", "enable_logging", "vad_silence_threshold_secs", "keepalive_interval_ms":
		default:
			return o, errors.New("unsupported_parameter")
		}
	}
	if value := q.Get("model_id"); value != "" {
		o.Model = value
	}
	if o.Model != "scribe_v2_realtime" && o.Model != "morse_stt_v1" {
		return o, errors.New("model_not_found")
	}
	if value := q.Get("audio_format"); value != "" {
		o.Format = value
	}
	switch o.Format {
	case "pcm_8000", "pcm_16000", "pcm_24000", "pcm_48000":
		o.Rate, _ = strconv.Atoi(strings.SplitN(o.Format, "_", 2)[1])
	default:
		return o, errors.New("unsupported_audio_format")
	}
	if value := q.Get("commit_strategy"); value != "" {
		o.Commit = value
	}
	if o.Commit != "manual" && o.Commit != "vad" {
		return o, errors.New("unsupported_commit_strategy")
	}
	if value := q.Get("language_code"); value != "" {
		if !languagePattern.MatchString(value) {
			return o, errors.New("invalid_language")
		}
		o.Language = value
	}
	if value, ok := q["include_timestamps"]; ok {
		if value[0] != "true" && value[0] != "false" {
			return o, errors.New("invalid_boolean")
		}
		o.Timestamps = value[0] == "true"
	}
	if value, ok := q["enable_logging"]; ok {
		if value[0] != "true" && value[0] != "false" {
			return o, errors.New("invalid_boolean")
		}
	}
	// The no-retention requirement always wins over compatibility logging flags.
	if value, ok := q["vad_silence_threshold_secs"]; ok {
		seconds, err := strconv.ParseFloat(value[0], 64)
		if err != nil || !(seconds >= .84 && seconds <= 30) {
			return o, errors.New("invalid_morse_commit_silence")
		}
		o.Silence = seconds
	}
	if value, ok := q["keepalive_interval_ms"]; ok {
		interval, err := strconv.Atoi(value[0])
		if err != nil || interval < 500 || interval > 10000 {
			return o, errors.New("invalid_keepalive_interval")
		}
		o.Keepalive = interval
	}
	return o, nil
}

func (o realtimeOptions) morseConfig() morse.Config {
	cfg := morse.DefaultConfig()
	cfg.SampleRate = o.Rate
	cfg.AutoCommit = o.Commit == "vad"
	cfg.CommitSilenceMillis = int(o.Silence * 1000)
	cfg.MaxDurationSeconds = 3600
	return cfg
}
