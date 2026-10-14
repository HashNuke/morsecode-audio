package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// OutputFormat is selected from the pinned speech API's finite format set.
type OutputFormat struct {
	Kind    string
	Rate    int
	Bitrate int
	MIME    string
}

func ParseOutputFormat(name string, streaming bool) (OutputFormat, error) {
	if name == "" {
		name = "mp3_44100_128"
	}
	parts := strings.Split(name, "_")
	if len(parts) < 2 || len(parts) > 3 {
		return OutputFormat{}, errors.New("unsupported_output_format")
	}
	f := OutputFormat{Kind: parts[0]}
	f.Rate, _ = strconv.Atoi(parts[1])
	if len(parts) == 3 {
		f.Bitrate, _ = strconv.Atoi(parts[2])
	}
	valid := false
	switch f.Kind {
	case "pcm", "wav":
		valid = len(parts) == 2 && (f.Rate == 8000 || f.Rate == 16000 || f.Rate == 22050 || f.Rate == 24000 || f.Rate == 32000 || f.Rate == 44100 || f.Rate == 48000)
		f.MIME = "audio/pcm"
		if f.Kind == "wav" {
			f.MIME = "audio/wav"
			valid = valid && !streaming
		}
	case "mp3":
		valid = len(parts) == 3 && ((f.Rate == 22050 && f.Bitrate == 32) || (f.Rate == 24000 && f.Bitrate == 48) || (f.Rate == 44100 && (f.Bitrate == 32 || f.Bitrate == 64 || f.Bitrate == 96 || f.Bitrate == 128 || f.Bitrate == 192)))
		f.MIME = "audio/mpeg"
	case "opus":
		valid = len(parts) == 3 && f.Rate == 48000 && (f.Bitrate == 32 || f.Bitrate == 64 || f.Bitrate == 96 || f.Bitrate == 128 || f.Bitrate == 192)
		f.MIME = "audio/ogg"
	case "ulaw", "alaw":
		valid = len(parts) == 2 && f.Rate == 8000
		f.MIME = "audio/basic"
		if f.Kind == "alaw" {
			f.MIME = "audio/pcma"
		}
	}
	// Reject aliases such as leading zeros; the upstream enum is exact.
	canonical := f.Kind + "_" + strconv.Itoa(f.Rate)
	if len(parts) == 3 {
		canonical += "_" + strconv.Itoa(f.Bitrate)
	}
	if !valid || name != canonical {
		return OutputFormat{}, errors.New("unsupported_output_format")
	}
	return f, nil
}

type OutputStream struct {
	io.ReadCloser
	// Size is -1 for compressed streams with unknown encoded size.
	Size int64
}

// Encode owns a bounded pipe and process for compressed formats. The caller must
// close it on every path. PCM input must be a finite, nonblocking reader (the
// lazy Morse encoder); no network-backed reader or filesystem input is accepted.
func Encode(ctx context.Context, pcm io.Reader, samples int, f OutputFormat) (*OutputStream, error) {
	return encode(ctx, pcm, samples, f, nil)
}

func encode(ctx context.Context, pcm io.Reader, samples int, f OutputFormat, liveInput io.Closer) (*OutputStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.Kind == "pcm" {
		return &OutputStream{io.NopCloser(pcm), int64(samples) * 2}, nil
	}
	if f.Kind == "wav" {
		return &OutputStream{io.NopCloser(io.MultiReader(bytes.NewReader(wavHeader(samples, f.Rate)), pcm)), int64(samples)*2 + 44}, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	args := []string{"-nostdin", "-v", "error", "-protocol_whitelist", "pipe", "-f", "s16le", "-ar", strconv.Itoa(f.Rate), "-ac", "1"}
	if liveInput != nil {
		// Format/rate/channels are already known. Default stream probing waits
		// for seconds of PCM, which can deadlock interactive text generation.
		args = append(args, "-probesize", "32", "-analyzeduration", "0")
	}
	args = append(args, "-i", "pipe:0", "-map", "0:a:0", "-vn", "-threads", "1")
	switch f.Kind {
	case "mp3":
		args = append(args, "-c:a", "libmp3lame", "-b:a", strconv.Itoa(f.Bitrate)+"k", "-write_xing", "0", "-id3v2_version", "0", "-f", "mp3")
	case "opus":
		args = append(args, "-c:a", "libopus", "-b:a", strconv.Itoa(f.Bitrate)+"k", "-application", "audio", "-frame_duration", "20", "-page_duration", "20000", "-f", "opus")
	case "ulaw":
		args = append(args, "-c:a", "pcm_mulaw", "-f", "mulaw")
	case "alaw":
		args = append(args, "-c:a", "pcm_alaw", "-f", "alaw")
	default:
		cancel()
		return nil, errors.New("unsupported_output_format")
	}
	args = append(args, "-flush_packets", "1", "pipe:1")
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stdin = pcm
	// Never retain subprocess stderr: it can include media-derived diagnostics.
	cmd.Stderr = io.Discard
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		_ = stdout.Close()
		return nil, errors.New("codec_unavailable")
	}
	return &OutputStream{&processStream{stdout: stdout, cmd: cmd, cancel: cancel, input: liveInput}, -1}, nil
}

type processStream struct {
	stdout    io.ReadCloser
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	waitOnce  sync.Once
	waitErr   error
	closeOnce sync.Once
	readEnd   error
	input     io.Closer
}

func (s *processStream) wait() error {
	s.waitOnce.Do(func() {
		if s.cmd.Wait() != nil {
			s.waitErr = errors.New("audio_encoding_failed")
		}
	})
	return s.waitErr
}

func (s *processStream) Read(p []byte) (int, error) {
	if s.readEnd != nil {
		return 0, s.readEnd
	}
	n, err := s.stdout.Read(p)
	if err != nil && s.input != nil {
		// A child can exit before its input producer. Unblock the stdin copier
		// before Wait joins it; waiting first could hang on a starved live source.
		_ = s.input.Close()
	}
	if err == io.EOF {
		if waitErr := s.wait(); waitErr != nil {
			s.readEnd = waitErr
			return n, waitErr
		}
	}
	if err != nil {
		s.readEnd = err
	}
	return n, err
}

func (s *processStream) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		if s.input != nil {
			_ = s.input.Close()
		}
		_ = s.stdout.Close()
		_ = s.wait()
	})
	// Cancellation is an expected close outcome. Encoding errors surface in Read.
	return nil
}

func wavHeader(samples, rate int) []byte {
	h := make([]byte, 44)
	copy(h, "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(samples*2+36))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], 1)
	binary.LittleEndian.PutUint32(h[24:], uint32(rate))
	binary.LittleEndian.PutUint32(h[28:], uint32(rate*2))
	binary.LittleEndian.PutUint16(h[32:], 2)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(samples*2))
	return h
}
