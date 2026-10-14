package audio_test

import (
	"context"
	"testing"
	"time"

	"github.com/HashNuke/morsecode-audio/internal/audio"
)

func TestCodecReadiness(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := audio.CheckCodecs(ctx); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	if err := audio.CheckCodecs(ctx); err == nil {
		t.Fatal("missing codecs reported ready")
	}
}
