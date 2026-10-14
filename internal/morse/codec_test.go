package morse_test

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"math/rand"
	"strings"
	"testing"

	"github.com/HashNuke/morsecode-audio/internal/morse"
)

// Handwritten ITU timing fixture: SOS 2, independent of the production alphabet.
var sos2 = []int{1, -1, 1, -1, 1, -3, 3, -1, 3, -1, 3, -3, 1, -1, 1, -1, 1, -7, 1, -1, 1, -1, 3, -1, 3, -1, 3, -14}

func signal(runs []int, rate, frequency int) []byte {
	var out bytes.Buffer
	for _, run := range runs {
		n := int(math.Abs(float64(run))) * rate * 60 / 1000
		for i := 0; i < n; i++ {
			var sample int16
			if run > 0 { // Square wave instead of the encoder's sine oscillator.
				if (i*frequency*2/rate)%2 == 0 {
					sample = 3000
				} else {
					sample = -3000
				}
			}
			_ = binary.Write(&out, binary.LittleEndian, sample)
		}
	}
	return out.Bytes()
}

func TestEncoderITUSpacingAndStreaming(t *testing.T) {
	cfg := morse.DefaultConfig()
	enc, err := morse.NewEncoder("  et\tA\n", cfg)
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := io.ReadAll(enc)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{1, -3, 3, -7, 1, -1, 3, -14}
	unit := cfg.SampleRate * 60 / 1000 * 2
	var runs []int
	for i := 0; i < len(pcm); i += unit {
		kind := -1
		if !bytes.Equal(pcm[i:i+unit], make([]byte, unit)) {
			kind = 1
		}
		if len(runs) > 0 && (runs[len(runs)-1] > 0) == (kind > 0) {
			runs[len(runs)-1] += kind
		} else {
			runs = append(runs, kind)
		}
	}
	if len(runs) != len(want) {
		t.Fatalf("runs %v", runs)
	}
	for i := range want {
		if runs[i] != want[i] {
			t.Fatalf("runs %v", runs)
		}
	}
	enc, err = morse.NewEncoder("ET A", cfg)
	if err != nil {
		t.Fatal(err)
	}
	var chunked bytes.Buffer
	buf := make([]byte, 137)
	for {
		n, err := enc.Read(buf)
		chunked.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(pcm, chunked.Bytes()) {
		t.Fatal("chunk boundaries changed synthesis")
	}
	if enc.Text != "ET A" || enc.Samples != len(pcm)/2 {
		t.Fatalf("metadata: %+v", enc)
	}
}

func TestIndependentFixtureAcrossArbitraryChunks(t *testing.T) {
	for _, rate := range []int{8000, 16000, 22050, 24000, 32000, 44100, 48000} {
		cfg := morse.DefaultConfig()
		cfg.SampleRate = rate
		d, err := morse.NewDecoder(cfg)
		if err != nil {
			t.Fatal(err)
		}
		pcm := signal(sos2, rate, 700)
		rng := rand.New(rand.NewSource(42))
		var events []morse.Event
		for len(pcm) > 0 {
			n := min(len(pcm), 1+rng.Intn(1003))
			e, err := d.Push(pcm[:n])
			if err != nil {
				t.Fatalf("%d Hz: %v", rate, err)
			}
			events = append(events, e...)
			pcm = pcm[n:]
		}
		e, err := d.Flush()
		if err != nil {
			t.Fatalf("%d Hz flush: %v", rate, err)
		}
		events = append(events, e...)
		var finals []string
		for _, e := range events {
			if e.Kind == "final" {
				finals = append(finals, e.Text)
			}
		}
		if len(finals) != 1 || finals[0] != "SOS 2" {
			t.Fatalf("%d Hz: %v", rate, events)
		}
	}
}

func TestFlushAndSilence(t *testing.T) {
	d, _ := morse.NewDecoder(morse.DefaultConfig())
	if e, err := d.Push(signal([]int{1}, 16000, 700)); err != nil || len(e) != 0 {
		t.Fatalf("push: %v %v", e, err)
	}
	e, err := d.Flush()
	if err != nil || len(e) == 0 || e[len(e)-1].Text != "E" {
		t.Fatalf("flush: %v %v", e, err)
	}
	if e, err := d.Flush(); err != nil || len(e) != 0 {
		t.Fatalf("duplicate final: %v %v", e, err)
	}
	if e, err := d.Push(signal([]int{-20}, 16000, 700)); err != nil || len(e) != 0 {
		t.Fatalf("silence: %v %v", e, err)
	}
	if e, err := d.Flush(); err != nil || len(e) != 0 {
		t.Fatalf("silence flush: %v %v", e, err)
	}
}

func TestInvalidSignalsAndBounds(t *testing.T) {
	for _, runs := range [][]int{{2}, {1, -2, 1}, {1, -1, 1, -1, 1, -1, 1, -1, 1, -1, 1, -1, 1, -1, 1}} {
		d, _ := morse.NewDecoder(morse.DefaultConfig())
		_, err := d.Push(signal(runs, 16000, 700))
		if err == nil {
			_, err = d.Flush()
		}
		if err == nil {
			t.Errorf("accepted malformed signal %v", runs)
		}
	}
	d, _ := morse.NewDecoder(morse.DefaultConfig())
	if _, err := d.Push(signal([]int{1}, 16000, 1200)); err == nil {
		t.Fatal("accepted wrong frequency")
	}
	d, _ = morse.NewDecoder(morse.DefaultConfig())
	_, _ = d.Push([]byte{1})
	if _, err := d.Flush(); err == nil {
		t.Fatal("accepted partial sample")
	}
	for _, text := range []string{"", " \t", "%", string([]byte{255}), strings.Repeat("E", 4097)} {
		if _, err := morse.NewEncoder(text, morse.DefaultConfig()); err == nil {
			t.Errorf("accepted invalid text length %d", len(text))
		}
	}
	cfg := morse.DefaultConfig()
	cfg.MaxDurationSeconds = 1
	if _, err := morse.NewEncoder("SOS", cfg); err == nil {
		t.Fatal("accepted excessive synthesis duration")
	}
	d, _ = morse.NewDecoder(cfg)
	if _, err := d.Push(make([]byte, 16000*2*2)); err == nil {
		t.Fatal("accepted excessive input duration")
	}
}

func TestManualCommitPreservesWords(t *testing.T) {
	cfg := morse.DefaultConfig()
	cfg.AutoCommit = false
	d, _ := morse.NewDecoder(cfg)
	e, err := d.Push(signal([]int{1, -14, 3, -14}, 16000, 700))
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range e {
		if event.Kind == "final" {
			t.Fatal("manual mode auto-committed")
		}
	}
	e, err = d.Flush()
	if err != nil || len(e) == 0 || e[len(e)-1].Text != "E T" {
		t.Fatalf("manual: %v %v", e, err)
	}
}

func TestDecodedTimestampsFollowSignal(t *testing.T) {
	d, _ := morse.NewDecoder(morse.DefaultConfig())
	e, err := d.Push(signal([]int{1, -7, 3, -14}, 16000, 700))
	if err != nil {
		t.Fatal(err)
	}
	final := e[len(e)-1]
	if final.Kind != "final" || len(final.Characters) != 3 {
		t.Fatalf("alignment: %+v", final)
	}
	for i, want := range []struct {
		text       string
		start, end float64
	}{{"E", 0, .06}, {" ", .06, .48}, {"T", .48, .66}} {
		c := final.Characters[i]
		if c.Text != want.text || math.Abs(c.Start-want.start) > .011 || math.Abs(c.End-want.end) > .011 {
			t.Fatalf("alignment: %+v expected %+v", c, want)
		}
	}
}
