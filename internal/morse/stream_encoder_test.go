package morse

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestStreamingTextPreservesMorseAcrossFragments(t *testing.T) {
	for _, rate := range []int{8000, 16000, 24000, 48000} {
		cfg := DefaultConfig()
		cfg.SampleRate = rate
		stream, err := NewStreamEncoder(cfg)
		if err != nil {
			t.Fatal(err)
		}
		var actual bytes.Buffer
		parts := []string{" s", "o", "s ", " ", "te", "st", " 123 "}
		for _, part := range parts {
			reader, err := stream.Push(part, false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.Copy(&actual, reader); err != nil {
				t.Fatal(err)
			}
		}
		reader, err := stream.Push("", true)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(&actual, reader)
		encoder, _ := NewEncoder(strings.Join(parts, ""), cfg)
		expected, _ := io.ReadAll(encoder)
		if !bytes.Equal(actual.Bytes(), expected) {
			t.Fatalf("fragment spacing or final padding changed at %d Hz", rate)
		}
		reader, err = stream.Push("E", true)
		if err != nil {
			t.Fatal(err)
		}
		next, _ := io.ReadAll(reader)
		encoder, _ = NewEncoder("E", cfg)
		expected, _ = io.ReadAll(encoder)
		if !bytes.Equal(next, expected) {
			t.Fatal("flush must begin a fresh utterance")
		}
	}
}

func TestStreamingTextBoundsApplyAcrossFragments(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxTextBytes = 3
	stream, _ := NewStreamEncoder(cfg)
	for range 3 {
		if _, err := stream.Push("E", false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := stream.Push("E", false); err == nil {
		t.Fatal("per-message input bypassed utterance limit")
	}
	cfg = DefaultConfig()
	cfg.MaxDurationSeconds = 1
	stream, _ = NewStreamEncoder(cfg)
	if _, err := stream.Push("E", false); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Push("E", false); err == nil {
		t.Fatal("duration must reserve final silence")
	}
}
