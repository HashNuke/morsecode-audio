package morse

import (
	"errors"
	"io"
	"math"
	"strings"
	"unicode/utf8"
)

type run struct {
	tone    bool
	samples int
}

// Encoder renders PCM lazily. Text/runs are bounded before any audio is emitted.
type Encoder struct {
	Text          string
	Samples       int
	cfg           Config
	runs          []run
	index, offset int
	high          byte
	pending       bool
}

func NewEncoder(text string, cfg Config) (*Encoder, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if !utf8.ValidString(text) {
		return nil, errors.New("invalid_text")
	}
	if len(text) > cfg.MaxTextBytes {
		return nil, errors.New("text_too_large")
	}
	text = strings.Join(strings.Fields(strings.ToUpper(text)), " ")
	if text == "" {
		return nil, errors.New("empty_text")
	}
	if len(text) > cfg.MaxTextBytes {
		return nil, errors.New("text_too_large")
	}
	e := &Encoder{Text: text, cfg: cfg}
	unit := cfg.SampleRate * cfg.UnitMillis / 1000
	add := func(tone bool, units int) {
		e.runs = append(e.runs, run{tone, units * unit})
		e.Samples += units * unit
	}
	for wi, word := range strings.Split(text, " ") {
		if wi > 0 {
			add(false, 7)
		}
		for ci, char := range word {
			pattern, ok := alphabet[char]
			if !ok {
				return nil, errors.New("unsupported_character")
			}
			if ci > 0 {
				add(false, 3)
			}
			for mi, mark := range pattern {
				if mi > 0 {
					add(false, 1)
				}
				if mark == '.' {
					add(true, 1)
				} else {
					add(true, 3)
				}
			}
		}
	}
	add(false, 14)
	if e.Samples > cfg.SampleRate*cfg.MaxDurationSeconds {
		return nil, errors.New("audio_too_long")
	}
	return e, nil
}

func (e *Encoder) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		if e.pending {
			p[n] = e.high
			e.pending = false
			n++
			continue
		}
		if e.index >= len(e.runs) {
			if n > 0 {
				return n, nil
			}
			return 0, io.EOF
		}
		r := e.runs[e.index]
		var sample int16
		if r.tone {
			sample = int16(math.Round(float64(e.cfg.Amplitude) * math.Sin(2*math.Pi*float64(e.cfg.Frequency)*float64(e.offset)/float64(e.cfg.SampleRate))))
		}
		p[n] = byte(sample)
		e.high = byte(uint16(sample) >> 8)
		e.pending = true
		n++
		e.offset++
		if e.offset == r.samples {
			e.index++
			e.offset = 0
		}
	}
	return n, nil
}
