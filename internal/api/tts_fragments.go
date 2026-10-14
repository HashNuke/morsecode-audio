package api

import (
	"context"
	"errors"
	"io"
)

// fragmentSource is a bounded queue of finite lazy readers. One producer offers
// and finishes; the codec owns Read/Close. Close unblocks Read without requiring
// any further client input and does not close the producer-owned channel.
type fragmentSource struct {
	ctx     context.Context
	cancel  context.CancelFunc
	queue   chan io.Reader
	current io.Reader
}

func newFragmentSource(parent context.Context) *fragmentSource {
	ctx, cancel := context.WithCancel(parent)
	return &fragmentSource{ctx: ctx, cancel: cancel, queue: make(chan io.Reader, 16)}
}
func (s *fragmentSource) offer(r io.Reader) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	select {
	case s.queue <- r:
		return nil
	default:
		return errors.New("input_queue_full")
	}
}
func (s *fragmentSource) finish()      { close(s.queue) }
func (s *fragmentSource) Close() error { s.cancel(); return nil }
func (s *fragmentSource) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for {
		if err := s.ctx.Err(); err != nil {
			return 0, err
		}
		if s.current != nil {
			n, err := s.current.Read(p)
			if err == io.EOF {
				s.current = nil
				if n > 0 {
					return n, nil
				}
				continue
			}
			return n, err
		}
		select {
		case <-s.ctx.Done():
			return 0, s.ctx.Err()
		case next, ok := <-s.queue:
			if !ok {
				return 0, io.EOF
			}
			s.current = next
		}
	}
}
