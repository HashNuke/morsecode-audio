package audio

import (
	"context"
	"errors"
	"io"
	"sync"
)

// EncodeLive owns a cancellable source whose Close unblocks Read. It accepts
// unknown-length PCM for one continuous encoded stream; callers must bound input
// queues and duration. Close joins the codec and its input copier. Unlike Encode,
// a starved source is allowed, and the source is closed on every terminal path.
func EncodeLive(ctx context.Context, pcm io.ReadCloser, f OutputFormat) (*OutputStream, error) {
	input := &liveInput{source: pcm, ctx: ctx}
	input.stop = context.AfterFunc(ctx, input.closeSource)
	if err := ctx.Err(); err != nil {
		input.Close()
		return nil, err
	}
	if f.Kind == "wav" {
		input.Close()
		return nil, errors.New("unsupported_output_format")
	}
	if f.Kind == "pcm" {
		return &OutputStream{ReadCloser: input, Size: -1}, nil
	}
	stream, err := encode(ctx, input, 0, f, input)
	if err != nil {
		input.Close()
	}
	return stream, err
}

type liveInput struct {
	source  io.ReadCloser
	ctx     context.Context
	once    sync.Once
	stop    func() bool
	readEnd error
}

func (s *liveInput) closeSource() { s.once.Do(func() { _ = s.source.Close() }) }

func (s *liveInput) Read(p []byte) (int, error) {
	if s.readEnd != nil {
		return 0, s.readEnd
	}
	if err := s.ctx.Err(); err != nil {
		_ = s.Close()
		return 0, err
	}
	n, err := s.source.Read(p)
	if err != nil && s.ctx.Err() != nil {
		err = s.ctx.Err()
	}
	if err != nil {
		s.readEnd = err
		_ = s.Close()
	}
	return n, err
}

func (s *liveInput) Close() error {
	s.stop()
	s.closeSource()
	return nil
}
