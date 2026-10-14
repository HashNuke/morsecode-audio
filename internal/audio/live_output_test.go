package audio_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/HashNuke/morsecode-audio/internal/audio"
)

func TestLiveEncodingEmitsAudioBeforeEndOfInput(t *testing.T) {
	formats := []string{"mp3_22050_32", "mp3_24000_48", "mp3_44100_32", "mp3_44100_64", "mp3_44100_96", "mp3_44100_128", "mp3_44100_192", "opus_48000_32", "opus_48000_64", "opus_48000_96", "opus_48000_128", "opus_48000_192", "ulaw_8000", "alaw_8000"}
	for _, rate := range []string{"8000", "16000", "22050", "24000", "32000", "44100", "48000"} {
		formats = append(formats, "pcm_"+rate)
	}
	for _, name := range formats {
		t.Run(name, func(t *testing.T) {
			format, _ := audio.ParseOutputFormat(name, true)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			input, writer := io.Pipe()
			defer input.Close()
			defer writer.Close()
			stream, err := audio.EncodeLive(ctx, input, format)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			if stream.Size != -1 {
				t.Fatal("unknown live length advertised as finite")
			}
			// Supply only 600 ms and withhold EOF until an actual audio packet arrives.
			pcm := make([]byte, format.Rate*2*3/5)
			for i := 0; i < len(pcm)/2; i++ {
				v := int16(4000 * math.Sin(2*math.Pi*700*float64(i)/float64(format.Rate)))
				binary.LittleEndian.PutUint16(pcm[i*2:], uint16(v))
			}
			release := make(chan struct{})
			producer := make(chan error, 1)
			go func() {
				_, err := writer.Write(pcm)
				select {
				case <-release:
				case <-ctx.Done():
				}
				_ = writer.Close()
				producer <- err
			}()
			packets := audio.NewPackets(stream, format)
			var encoded bytes.Buffer
			var clock float64
			for clock == 0 {
				packet, end, err := packets.Next()
				if err != nil {
					t.Fatalf("no live audio before EOF: %v", err)
				}
				encoded.Write(packet)
				clock = end
			}
			close(release)
			for {
				packet, end, err := packets.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if end < clock {
					t.Fatal("live packet clock regressed")
				}
				clock = end
				encoded.Write(packet)
			}
			if err := <-producer; err != nil {
				t.Fatal(err)
			}
			if clock < .59 || clock > .75 {
				t.Fatalf("live duration %f", clock)
			}
			if format.Kind == "pcm" && !bytes.Equal(encoded.Bytes(), pcm) {
				t.Fatal("live PCM corrupted")
			}
			// Decode the continuous output independently: probing must not discard
			// input while making the first packet available before producer EOF.
			args := []string{"-v", "error", "-protocol_whitelist", "pipe"}
			if format.Kind == "pcm" || format.Kind == "ulaw" || format.Kind == "alaw" {
				input := map[string]string{"pcm": "s16le", "ulaw": "mulaw", "alaw": "alaw"}[format.Kind]
				args = append(args, "-f", input, "-ar", strconv.Itoa(format.Rate), "-ac", "1")
			}
			args = append(args, "-i", "pipe:0", "-f", "s16le", "-ar", "16000", "-ac", "1", "pipe:1")
			cmd := exec.CommandContext(ctx, "ffmpeg", args...)
			cmd.Stdin = bytes.NewReader(encoded.Bytes())
			decoded, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			duration := float64(len(decoded)) / 32000
			if duration < .59 || duration > .75 {
				t.Fatalf("decoded duration %f", duration)
			}
			var energy float64
			for i := 0; i+1 < len(decoded); i += 2 {
				v := float64(int16(binary.LittleEndian.Uint16(decoded[i:])))
				energy += v * v
			}
			if math.Sqrt(energy/float64(len(decoded)/2)) < 2000 {
				t.Fatal("live output lost tone")
			}
		})
	}
}

func TestLiveEncodingClosesInputOnChildFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	format, _ := audio.ParseOutputFormat("mp3_44100_128", true)
	stream, err := audio.EncodeLive(ctx, input, format)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	_, err = stream.Read(make([]byte, 1024))
	if err == nil || ctx.Err() != nil {
		t.Fatal("child failure did not close source before joining input copier")
	}
	if _, err := writer.Write([]byte{0, 0}); err == nil {
		t.Fatal("failed child left source open")
	}
}

func TestLiveEncodingClosesInputOnStartupRejection(t *testing.T) {
	for _, mode := range []string{"wav", "missing_codec", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			format, _ := audio.ParseOutputFormat("mp3_44100_128", true)
			switch mode {
			case "wav":
				format, _ = audio.ParseOutputFormat("wav_16000", false)
			case "missing_codec":
				t.Setenv("PATH", t.TempDir())
			case "cancelled":
				cancel()
			}
			input, writer := io.Pipe()
			defer input.Close()
			defer writer.Close()
			if stream, err := audio.EncodeLive(ctx, input, format); err == nil {
				stream.Close()
				t.Fatal("invalid start accepted")
			}
			closed := make(chan error, 1)
			go func() { _, err := writer.Write([]byte{0, 0}); closed <- err }()
			select {
			case err := <-closed:
				if err == nil {
					t.Fatal("source still writable")
				}
			case <-time.After(time.Second):
				t.Fatal("rejected source not closed")
			}
		})
	}
}

type terminalInput struct {
	closed chan struct{}
	once   sync.Once
}

func (r *terminalInput) Read([]byte) (int, error) { return 0, io.EOF }
func (r *terminalInput) Close() error             { r.once.Do(func() { close(r.closed) }); return nil }

func TestLivePCMCompletionClosesSource(t *testing.T) {
	input := &terminalInput{closed: make(chan struct{})}
	format, _ := audio.ParseOutputFormat("pcm_16000", true)
	stream, err := audio.EncodeLive(context.Background(), input, format)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Read(make([]byte, 2)); err != io.EOF {
		t.Fatalf("empty PCM completion: %v", err)
	}
	if _, err := stream.Read(make([]byte, 2)); err != io.EOF {
		t.Fatalf("PCM EOF changed after source cleanup: %v", err)
	}
	select {
	case <-input.closed:
	default:
		t.Fatal("PCM EOF retained source")
	}
}

type observedInput struct {
	*io.PipeReader
	started chan struct{}
	once    sync.Once
}

func (r *observedInput) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	return r.PipeReader.Read(p)
}

func TestLiveEncodingCancellationJoinsStarvedInput(t *testing.T) {
	for _, name := range []string{"pcm_16000", "mp3_44100_128"} {
		for _, mode := range []string{"cancel", "close"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				reader, writer := io.Pipe()
				defer reader.Close()
				defer writer.Close()
				input := &observedInput{PipeReader: reader, started: make(chan struct{})}
				format, _ := audio.ParseOutputFormat(name, true)
				stream, err := audio.EncodeLive(ctx, input, format)
				if err != nil {
					t.Fatal(err)
				}
				readDone := make(chan error, 1)
				go func() { _, err := stream.Read(make([]byte, 1024)); readDone <- err }()
				select {
				case <-input.started:
				case <-time.After(2 * time.Second):
					t.Fatal("input not read")
				}
				closeDone := make(chan struct{})
				go func() {
					if mode == "cancel" {
						cancel()
					}
					_ = stream.Close()
					close(closeDone)
				}()
				select {
				case <-closeDone:
				case <-time.After(time.Second):
					t.Fatal("close did not join starved input")
				}
				select {
				case err := <-readDone:
					if err == nil {
						t.Fatal("read unexpectedly succeeded")
					}
				case <-time.After(time.Second):
					t.Fatal("read remained blocked")
				}
				if _, err := writer.Write([]byte{0, 0}); err == nil {
					t.Fatal("source remains open")
				}
			})
		}
	}
}
