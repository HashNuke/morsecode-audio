package morse

import (
	"errors"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Fragment is finite lazy PCM. Samples counts this fragment only; character
// times use the cumulative stream clock, including gaps carried between inputs.
type Fragment struct {
	io.Reader
	Text       string
	Samples    int
	Characters []Character
}

// StreamEncoder plans successive text fragments without synthesizing an
// utterance tail between them. One owner calls Append/Finish in order; returned
// fragments are independent and must be rendered in that same order. No audio
// buffers, previous text or background workers are retained here.
type StreamEncoder struct {
	cfg                             Config
	inputBytes, samples             int
	hasText, pendingSpace, finished bool
}

func NewStreamEncoder(cfg Config) (*StreamEncoder, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &StreamEncoder{cfg: cfg}, nil
}

// Append validates the entire fragment before committing any state. It reserves
// room for the eventual tail when enforcing the stream's total duration limit.
// Whitespace at a boundary is held until another character establishes a word
// gap; trailing whitespace alone never emits silence or alignment characters.
func (s *StreamEncoder) Append(text string) (*Fragment, error) {
	if s.finished {
		return nil, errors.New("stream_finished")
	}
	if len(text) > s.cfg.MaxTextBytes-s.inputBytes {
		return nil, errors.New("text_too_large")
	}
	if !utf8.ValidString(text) {
		return nil, errors.New("invalid_text")
	}
	if strings.TrimSpace(text) == "" {
		s.inputBytes += len(text)
		s.pendingSpace = s.pendingSpace || (s.hasText && text != "")
		return &Fragment{Reader: &Encoder{cfg: s.cfg}}, nil
	}
	encoder, err := NewEncoder(text, s.cfg)
	if err != nil {
		return nil, err
	}
	unit := s.cfg.SampleRate * s.cfg.UnitMillis / 1000
	tail := unit * 14
	// The finite planner already validated this input and computed its marks.
	// Its utterance tail belongs only at Finish, not after each text fragment.
	encoder.runs = encoder.runs[:len(encoder.runs)-1]
	encoder.Samples -= tail
	first, _ := utf8.DecodeRuneInString(text)
	wordGap := s.hasText && (s.pendingSpace || unicode.IsSpace(first))
	gap := 0
	if s.hasText {
		gap = unit * 3
		if wordGap {
			gap = unit * 7
		}
	}
	if s.samples+gap+encoder.Samples+tail > s.cfg.SampleRate*s.cfg.MaxDurationSeconds {
		return nil, errors.New("audio_too_long")
	}
	offset := float64(s.samples+gap) / float64(s.cfg.SampleRate)
	for i := range encoder.Characters {
		encoder.Characters[i].Start += offset
		encoder.Characters[i].End += offset
	}
	if gap > 0 {
		encoder.runs = append([]run{{samples: gap}}, encoder.runs...)
		encoder.Samples += gap
	}
	if wordGap {
		space := Character{Text: " ", Start: float64(s.samples) / float64(s.cfg.SampleRate), End: offset}
		encoder.Characters = append([]Character{space}, encoder.Characters...)
		encoder.Text = " " + encoder.Text
	}
	last, _ := utf8.DecodeLastRuneInString(text)
	s.pendingSpace = unicode.IsSpace(last)
	s.hasText = true
	s.inputBytes += len(text)
	s.samples += encoder.Samples
	return &Fragment{Reader: encoder, Text: encoder.Text, Samples: encoder.Samples, Characters: encoder.Characters}, nil
}

// Finish emits exactly one utterance tail for nonempty text. An empty stream
// finishes without fabricated audio. Further Append/Finish calls are rejected.
func (s *StreamEncoder) Finish() (*Fragment, error) {
	if s.finished {
		return nil, errors.New("stream_finished")
	}
	s.finished = true
	encoder := &Encoder{cfg: s.cfg}
	if s.hasText {
		encoder.Samples = s.cfg.SampleRate * s.cfg.UnitMillis / 1000 * 14
		encoder.runs = []run{{samples: encoder.Samples}}
		s.samples += encoder.Samples
	}
	return &Fragment{Reader: encoder, Samples: encoder.Samples}, nil
}
