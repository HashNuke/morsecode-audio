package morse

import (
	"errors"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

// StreamEncoder plans one bounded text fragment at a time. Push results must be
// consumed before the next Push. Only flush adds the utterance's final silence.
type StreamEncoder struct {
	cfg                Config
	textBytes, samples int
	started, wordGap   bool
}

func NewStreamEncoder(cfg Config) (*StreamEncoder, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &StreamEncoder{cfg: cfg}, nil
}

func (s *StreamEncoder) Push(text string, flush bool) (io.Reader, error) {
	if !utf8.ValidString(text) {
		return nil, errors.New("invalid_text")
	}
	if len(text) > s.cfg.MaxTextBytes {
		return nil, errors.New("text_too_large")
	}
	unit := s.cfg.SampleRate * s.cfg.UnitMillis / 1000
	var readers []io.Reader
	if strings.TrimSpace(text) != "" {
		if s.textBytes+len(text) > s.cfg.MaxTextBytes {
			return nil, errors.New("text_too_large")
		}
		encoder, err := NewEncoder(text, s.cfg)
		if err != nil {
			return nil, err
		}
		prefix := 0
		first, _ := utf8.DecodeRuneInString(text)
		if s.started {
			prefix = 3 * unit
			if s.wordGap || unicode.IsSpace(first) {
				prefix = 7 * unit
			}
		}
		body := encoder.Samples - 14*unit
		if s.samples+prefix+body+14*unit > s.cfg.SampleRate*s.cfg.MaxDurationSeconds {
			return nil, errors.New("audio_too_long")
		}
		readers = append(readers, io.LimitReader(silence{}, int64(prefix*2)), io.LimitReader(encoder, int64(body*2)))
		s.samples += prefix + body
		s.textBytes += len(text)
		s.started = true
		last, _ := utf8.DecodeLastRuneInString(text)
		s.wordGap = unicode.IsSpace(last)
	} else if text != "" && s.started {
		s.wordGap = true
	}
	if flush && s.started {
		readers = append(readers, io.LimitReader(silence{}, int64(14*unit*2)))
		s.samples, s.textBytes = 0, 0
		s.started, s.wordGap = false, false
	}
	return io.MultiReader(readers...), nil
}

type silence struct{}

func (silence) Read(p []byte) (int, error) { clear(p); return len(p), nil }
