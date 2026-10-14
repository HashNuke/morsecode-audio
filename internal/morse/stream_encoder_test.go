package morse_test

import (
	"bytes"
	"io"
	"math"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/HashNuke/morsecode-audio/internal/morse"
)

func TestStreamEncoderPreservesFragmentGapsAndClock(t *testing.T) {
	cfg := morse.DefaultConfig()
	stream, err := morse.NewStreamEncoder(cfg)
	if err != nil {
		t.Fatal(err)
	}
	unit := cfg.SampleRate * cfg.UnitMillis / 1000
	var pcm bytes.Buffer
	var chars []morse.Character
	for index, text := range []string{"  e", "t\t", " ", "e  "} {
		fragment, err := stream.Append(text)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(fragment)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 && len(data) != unit*2 {
			t.Fatalf("first text appended an utterance gap: %d bytes", len(data))
		}
		pcm.Write(data)
		chars = append(chars, fragment.Characters...)
	}
	final, err := stream.Finish()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(&pcm, final)
	// E(1), letter gap(3), T(3), word gap(7), E(1), utterance tail(14).
	if pcm.Len() != 29*unit*2 {
		t.Fatalf("fragmented timing: got %d bytes", pcm.Len())
	}
	if len(chars) != 4 {
		t.Fatalf("character count: %+v", chars)
	}
	for i, want := range []struct {
		text       string
		start, end int
	}{{"E", 0, 1}, {"T", 4, 7}, {" ", 7, 14}, {"E", 14, 15}} {
		if chars[i].Text != want.text || math.Abs(chars[i].Start-float64(want.start)*.06) > 1e-9 || math.Abs(chars[i].End-float64(want.end)*.06) > 1e-9 {
			t.Fatalf("character %d: %+v", i, chars[i])
		}
	}
	// Inspect the waveform independently of a production decoder or encoder.
	for _, segment := range []struct {
		start, end int
		tone       bool
	}{{0, 1, true}, {1, 4, false}, {4, 7, true}, {7, 14, false}, {14, 15, true}, {15, 29, false}} {
		data := pcm.Bytes()[segment.start*unit*2 : segment.end*unit*2]
		nonzero := false
		for _, b := range data {
			nonzero = nonzero || b != 0
		}
		if nonzero != segment.tone {
			t.Fatalf("tone/gap mismatch at units %d..%d", segment.start, segment.end)
		}
	}
}

func TestStreamEncoderFragmentationMatchesFiniteSynthesis(t *testing.T) {
	text := " \tSoS  test\n123 ?@\u2003eT "
	runes := []rune(text)
	for _, rate := range []int{8000, 16000, 22050, 24000, 32000, 44100, 48000} {
		cfg := morse.DefaultConfig()
		cfg.SampleRate = rate
		whole, err := morse.NewEncoder(text, cfg)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := io.ReadAll(whole)
		rng := rand.New(rand.NewPCG(12, 34))
		for range 12 {
			stream, err := morse.NewStreamEncoder(cfg)
			if err != nil {
				t.Fatal(err)
			}
			var got bytes.Buffer
			var normalized strings.Builder
			var chars []morse.Character
			for off := 0; off < len(runes); {
				end := min(len(runes), off+1+rng.IntN(5))
				fragment, err := stream.Append(string(runes[off:end]))
				if err != nil {
					t.Fatal(err)
				}
				normalized.WriteString(fragment.Text)
				chars = append(chars, fragment.Characters...)
				buffer := make([]byte, 1+rng.IntN(319))
				for {
					n, err := fragment.Read(buffer)
					got.Write(buffer[:n])
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				off = end
			}
			final, err := stream.Finish()
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(&got, final)
			if !bytes.Equal(got.Bytes(), want) || normalized.String() != whole.Text {
				t.Fatalf("fragmentation changed PCM/text at %d Hz", rate)
			}
			if len(chars) != len(whole.Characters) {
				t.Fatalf("alignment count at %d Hz", rate)
			}
			for i, char := range chars {
				want := whole.Characters[i]
				if char.Text != want.Text || math.Abs(char.Start-want.Start) > 1e-9 || math.Abs(char.End-want.End) > 1e-9 {
					t.Fatalf("alignment %d at %d Hz: %+v != %+v", i, rate, char, want)
				}
			}
		}
	}
}

func TestStreamEncoderAtomicValidationAndFinalization(t *testing.T) {
	cfg := morse.DefaultConfig()
	cfg.MaxTextBytes = 3
	stream, err := morse.NewStreamEncoder(cfg)
	if err != nil {
		t.Fatal(err)
	}
	first, err := stream.Append("E")
	if err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	_, _ = io.Copy(&got, first)
	for _, text := range []string{"T%", "TTT", string([]byte{0xff})} {
		if _, err := stream.Append(text); err == nil {
			t.Fatalf("invalid fragment accepted: %q", text)
		}
	}
	second, err := stream.Append(" T")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(&got, second)
	final, err := stream.Finish()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(&got, final)
	whole, _ := morse.NewEncoder("E T", cfg)
	want, _ := io.ReadAll(whole)
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatal("rejected fragment mutated stream")
	}
	if _, err := stream.Append("E"); err == nil {
		t.Fatal("append after finish accepted")
	}
	if _, err := stream.Finish(); err == nil {
		t.Fatal("second tail accepted")
	}

	cfg = morse.DefaultConfig()
	cfg.MaxDurationSeconds = 1
	stream, _ = morse.NewStreamEncoder(cfg)
	_, err = stream.Append("E") // .06 sec E + .84 sec final tail fits.
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Append("E"); err == nil {
		t.Fatal("duration limit did not reserve final tail")
	}
	final, err = stream.Finish()
	if err != nil || final.Samples != cfg.SampleRate*cfg.UnitMillis/1000*14 {
		t.Fatal("duration rejection lost pending tail")
	}

	stream, _ = morse.NewStreamEncoder(morse.DefaultConfig())
	_, _ = stream.Append(" \t")
	final, err = stream.Finish()
	if err != nil || final.Samples != 0 {
		t.Fatal("whitespace-only stream generated audio")
	}
	cfg.SampleRate = 1
	if _, err := morse.NewStreamEncoder(cfg); err == nil {
		t.Fatal("invalid profile accepted")
	}
}
