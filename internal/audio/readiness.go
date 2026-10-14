package audio

import (
	"bytes"
	"context"
	"fmt"
	"io"
)

// CheckCodecs verifies real encoder availability before the server listens.
// It uses synthetic silence only, through the same bounded pipe path as requests.
func CheckCodecs(ctx context.Context) error {
	for _, name := range []string{"mp3_44100_128", "opus_48000_64", "ulaw_8000", "alaw_8000"} {
		format, _ := ParseOutputFormat(name, false)
		samples := format.Rate / 10
		output, err := Encode(ctx, bytes.NewReader(make([]byte, samples*2)), samples, format)
		if err != nil {
			return fmt.Errorf("codec check %s: %w", format.Kind, err)
		}
		n, err := io.Copy(io.Discard, output)
		_ = output.Close()
		if err != nil || n == 0 {
			return fmt.Errorf("codec check %s failed", format.Kind)
		}
	}
	return nil
}
