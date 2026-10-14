package audio_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/HashNuke/morsecode-audio/internal/audio"
)

func TestOutputFormatsIndependentlyDecode(t *testing.T) {
	formats := []string{"", "mp3_22050_32", "mp3_24000_48", "mp3_44100_32", "mp3_44100_64", "mp3_44100_96", "mp3_44100_128", "mp3_44100_192", "opus_48000_32", "opus_48000_64", "opus_48000_96", "opus_48000_128", "opus_48000_192", "ulaw_8000", "alaw_8000"}
	for _, rate := range []string{"8000", "16000", "22050", "24000", "32000", "44100", "48000"} {
		formats = append(formats, "pcm_"+rate, "wav_"+rate)
	}
	for _, name := range formats {
		t.Run(name, func(t *testing.T) {
			f, err := audio.ParseOutputFormat(name, false)
			if err != nil {
				t.Fatal(err)
			}
			// Independent 700-Hz signal, not the Morse encoder.
			pcm := make([]byte, f.Rate*2)
			for i := 0; i < f.Rate; i++ {
				v := int16(4000 * math.Sin(2*math.Pi*700*float64(i)/float64(f.Rate)))
				binary.LittleEndian.PutUint16(pcm[i*2:], uint16(v))
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			stream, err := audio.Encode(ctx, bytes.NewReader(pcm), f.Rate, f)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := io.ReadAll(stream)
			if err == nil {
				if n, end := stream.Read(make([]byte, 1)); n != 0 || end != io.EOF {
					t.Fatalf("read after EOF: %d %v", n, end)
				}
			}
			closeErr := stream.Close()
			if err != nil || closeErr != nil {
				t.Fatalf("encode: %v / %v", err, closeErr)
			}
			if len(encoded) == 0 {
				t.Fatal("empty audio")
			}
			if f.Kind != "wav" {
				packets := audio.NewPackets(bytes.NewReader(encoded), f)
				var joined []byte
				var lastTime float64
				for {
					packet, end, err := packets.Next()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					if end < lastTime {
						t.Fatal("packet clock moved backwards")
					}
					joined = append(joined, packet...)
					lastTime = end
				}
				if !bytes.Equal(encoded, joined) || lastTime < .99 || lastTime > 1.16 {
					t.Fatalf("packet loss or incorrect duration %.3f", lastTime)
				}
			}
			args := []string{"-v", "error", "-protocol_whitelist", "pipe"}
			if f.Kind == "pcm" || f.Kind == "ulaw" || f.Kind == "alaw" {
				input := map[string]string{"pcm": "s16le", "ulaw": "mulaw", "alaw": "alaw"}[f.Kind]
				args = append(args, "-f", input, "-ar", strconv.Itoa(f.Rate), "-ac", "1")
			}
			args = append(args, "-i", "pipe:0", "-f", "s16le", "-ar", "16000", "-ac", "1", "pipe:1")
			cmd := exec.CommandContext(ctx, "ffmpeg", args...)
			cmd.Stdin = bytes.NewReader(encoded)
			decoded, err := cmd.Output()
			if err != nil {
				t.Fatalf("independent decode: %v", err)
			}
			// MP3 without a seekable Xing header includes codec priming/padding.
			duration := float64(len(decoded)) / 32000
			if duration < .99 || duration > 1.16 {
				t.Fatalf("duration %.3f", duration)
			}
			var energy float64
			for i := 0; i+1 < len(decoded); i += 2 {
				v := float64(int16(binary.LittleEndian.Uint16(decoded[i:])))
				energy += v * v
			}
			if math.Sqrt(energy/float64(len(decoded)/2)) < 2000 {
				t.Fatal("lost tone")
			}
		})
	}
}

func TestOutputRejectsUnsupportedFormats(t *testing.T) {
	for _, format := range []string{"mp3_16000_128", "pcm_123", "opus_48000_24", "wav_8000/../../file", "mp3"} {
		if _, err := audio.ParseOutputFormat(format, false); err == nil {
			t.Errorf("accepted %q", format)
		}
	}
	if _, err := audio.ParseOutputFormat("wav_16000", true); err == nil {
		t.Fatal("upstream streaming schema excludes WAV")
	}
}

func TestCompressedStreamProducesEarlyBytesAndCancels(t *testing.T) {
	f, _ := audio.ParseOutputFormat("mp3_44100_128", true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	input := &countedSilence{remaining: 44100 * 2 * 300}
	s, err := audio.Encode(ctx, input, 44100*300, f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(s, make([]byte, 2048)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Close joins the encoder process and its stdin copier before inspecting input.
	if input.remaining < 44100*2*290 {
		t.Fatalf("buffered whole input; remaining=%d", input.remaining)
	}
	if ctx.Err() != nil {
		t.Fatal("cancellation exceeded deadline")
	}
}

type countedSilence struct{ remaining int }

func (r *countedSilence) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), r.remaining)
	clear(p[:n])
	r.remaining -= n
	return n, nil
}
