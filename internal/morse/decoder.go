package morse

import (
	"encoding/binary"
	"errors"
	"math"
	"strings"
)

type Event struct {
	Kind       string
	Text       string
	Characters []Character
}

type Character struct {
	Text  string  `json:"text"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// Decoder retains at most one analysis window, one bounded transcript and marks.
// It is owned by one stream; separate streams must use separate decoders.
type Decoder struct {
	cfg                    Config
	carry                  []byte
	window                 int
	total, duration        int
	kind                   int // 0 unknown, 1 silence, 2 tone
	marks, text            string
	gap                    bool
	failure                error
	symbolStart, symbolEnd int
	characters             []Character
}

func NewDecoder(cfg Config) (*Decoder, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	window := cfg.SampleRate / 100 * 2
	return &Decoder{cfg: cfg, window: window, carry: make([]byte, 0, window)}, nil
}

func (d *Decoder) Push(pcm []byte) ([]Event, error) {
	if d.failure != nil {
		return nil, d.failure
	}
	var events []Event
	for len(pcm) > 0 {
		n := min(d.window-len(d.carry), len(pcm))
		d.carry = append(d.carry, pcm[:n]...)
		pcm = pcm[n:]
		if len(d.carry) == d.window {
			if err := d.windowStep(d.carry, &events); err != nil {
				d.failure = err
				return nil, err
			}
			d.carry = d.carry[:0]
		}
	}
	return events, nil
}

func (d *Decoder) Flush() ([]Event, error) {
	if d.failure != nil {
		return nil, d.failure
	}
	if len(d.carry)%2 != 0 {
		d.failure = errors.New("incomplete_sample")
		return nil, d.failure
	}
	var events []Event
	if len(d.carry) > 0 {
		if err := d.windowStep(d.carry, &events); err != nil {
			d.failure = err
			return nil, err
		}
	}
	if d.kind == 2 {
		d.symbolEnd = d.total
		if err := d.finishTone(); err != nil {
			d.failure = err
			return nil, err
		}
	}
	if d.kind == 1 && d.active() && !d.validGap() {
		d.failure = errors.New("invalid_timing")
		return nil, d.failure
	}
	if err := d.finish(&events); err != nil {
		d.failure = err
		return nil, err
	}
	d.carry = d.carry[:0]
	d.kind = 0
	d.duration = 0
	d.gap = false
	return events, nil
}

func (d *Decoder) windowStep(pcm []byte, events *[]Event) error {
	n := len(pcm) / 2
	d.total += n
	if d.total > d.cfg.SampleRate*d.cfg.MaxDurationSeconds {
		return errors.New("audio_too_long")
	}
	kind, err := d.classify(pcm)
	if err != nil {
		return err
	}
	if d.kind != kind {
		if d.kind == 2 {
			d.symbolEnd = d.total - n
			if err := d.finishTone(); err != nil {
				return err
			}
		}
		if d.kind == 1 && d.active() {
			if !d.validGap() {
				return errors.New("invalid_timing")
			}
			if d.duration >= d.unit()*3-d.tolerance() {
				if err := d.letter(events); err != nil {
					return err
				}
			}
			if d.duration >= d.unit()*7-d.tolerance() {
				d.gap = true
			}
		}
		if kind == 2 && d.marks == "" {
			d.symbolStart = d.total - n
		}
		d.kind = kind
		d.duration = 0
	}
	d.duration += n
	if kind == 1 && d.active() {
		if d.duration >= d.unit()*3 {
			if err := d.letter(events); err != nil {
				return err
			}
		}
		if d.duration >= d.unit()*7 {
			d.gap = true
		}
		if d.cfg.AutoCommit && d.duration >= d.unit()*14 {
			return d.finish(events)
		}
	}
	return nil
}

func (d *Decoder) classify(pcm []byte) (int, error) {
	var energy float64
	lastSign, crossings, firstCross, lastCross, activeSamples := 0, 0, 0, 0, 0
	for i := 0; i < len(pcm)/2; i++ {
		x := int(int16(binary.LittleEndian.Uint16(pcm[i*2:])))
		energy += float64(x * x)
		sign := 0
		if x > 300 {
			sign = 1
		}
		if x < -300 {
			sign = -1
		}
		if sign != 0 {
			activeSamples++
			if lastSign != 0 && lastSign != sign {
				if crossings == 0 {
					firstCross = i
				}
				crossings++
				lastCross = i
			}
			lastSign = sign
		}
	}
	if energy <= 300*300*float64(len(pcm)/2) || activeSamples < len(pcm)/4 {
		return 1, nil
	}
	// Very short tail windows inherit the already detected tone; a full window
	// must establish an actual alternating signal before this is permitted.
	if crossings < 2 {
		if len(pcm) < d.window && d.kind == 2 {
			return 2, nil
		}
		return 0, errors.New("unsupported_frequency")
	}
	freq := float64((crossings-1)*d.cfg.SampleRate) / float64(2*(lastCross-firstCross))
	if math.Abs(freq-float64(d.cfg.Frequency)) > 100 {
		return 0, errors.New("unsupported_frequency")
	}
	return 2, nil
}

func (d *Decoder) unit() int                    { return d.cfg.SampleRate * d.cfg.UnitMillis / 1000 }
func (d *Decoder) tolerance() int               { return d.cfg.SampleRate/100 + 1 }
func (d *Decoder) near(samples, units int) bool { return abs(samples-units*d.unit()) <= d.tolerance() }
func (d *Decoder) active() bool                 { return d.marks != "" || d.text != "" }
func (d *Decoder) validGap() bool {
	return d.near(d.duration, 1) || d.near(d.duration, 3) || d.near(d.duration, 7) || d.duration >= d.unit()*14-d.tolerance()
}

func (d *Decoder) finishTone() error {
	if d.near(d.duration, 1) {
		d.marks += "."
	} else if d.near(d.duration, 3) {
		d.marks += "-"
	} else {
		return errors.New("invalid_timing")
	}
	if len(d.marks) > 8 {
		return errors.New("unsupported_signal")
	}
	return nil
}

func (d *Decoder) letter(events *[]Event) error {
	if d.marks == "" {
		return nil
	}
	char, ok := characters[d.marks]
	if !ok {
		return errors.New("unsupported_signal")
	}
	if d.gap && d.text != "" {
		d.text += " "
		previous := d.characters[len(d.characters)-1]
		d.characters = append(d.characters, Character{Text: " ", Start: previous.End, End: float64(d.symbolStart) / float64(d.cfg.SampleRate)})
	}
	d.gap = false
	d.text += char
	d.characters = append(d.characters, Character{Text: char, Start: float64(d.symbolStart) / float64(d.cfg.SampleRate), End: float64(d.symbolEnd) / float64(d.cfg.SampleRate)})
	d.marks = ""
	if len(d.text) > d.cfg.MaxTextBytes {
		return errors.New("transcript_too_large")
	}
	*events = append(*events, Event{Kind: "partial", Text: d.text, Characters: append([]Character(nil), d.characters...)})
	return nil
}

func (d *Decoder) finish(events *[]Event) error {
	if err := d.letter(events); err != nil {
		return err
	}
	if text := strings.TrimSpace(d.text); text != "" {
		*events = append(*events, Event{Kind: "final", Text: text, Characters: append([]Character(nil), d.characters...)})
	}
	d.text = ""
	d.marks = ""
	d.gap = false
	d.characters = nil
	return nil
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
