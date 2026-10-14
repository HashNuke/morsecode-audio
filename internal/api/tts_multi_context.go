package api

import (
	"context"
	"time"
)

type multiOutput struct {
	generation *ttsGeneration
	body       map[string]any
	err        error
	stopped    bool
}

// Input and lifecycle fields belong to the connection coordinator. The worker
// owns output and codecs; terminalOnClose is read only after receiving jobs EOF.
type ttsMultiContext struct {
	id              string
	ctx             context.Context
	cancel          context.CancelFunc
	input           ttsTextInput
	initial         ttsSocketMessage
	jobs            chan *ttsGeneration
	output          chan multiOutput
	done            chan struct{}
	touched         time.Time
	closing         bool
	terminalOnClose bool
	hadGeneration   bool
}

func (s *Server) newMultiContext(parent context.Context, id string, options ttsSocketOptions, initial ttsSocketMessage, wake chan<- struct{}) *ttsMultiContext {
	ctx, cancel := context.WithCancel(parent)
	jobs := make(chan *ttsGeneration, 4)
	c := &ttsMultiContext{id: id, ctx: ctx, cancel: cancel, jobs: jobs, output: make(chan multiOutput, 1), done: make(chan struct{}), touched: time.Now(), initial: initial}
	c.initial.Key = nil
	c.initial.HyphenKey = nil
	c.initial.Authorization = nil
	c.initial.Text = nil
	c.input = ttsTextInput{ctx: ctx, jobs: jobs, options: options, schedule: []int{120, 160, 250, 290}}
	if initial.Generation != nil {
		c.input.schedule = initial.Generation.Schedule
	}
	go func() { defer close(c.done); s.runMultiContext(c, options, wake) }()
	return c
}

func (s *Server) runMultiContext(c *ttsMultiContext, options ttsSocketOptions, wake chan<- struct{}) {
	publish := func(event multiOutput) error {
		select {
		case <-c.ctx.Done():
			return c.ctx.Err()
		case c.output <- event:
			select {
			case wake <- struct{}{}:
			default:
			}
			return nil
		}
	}
	final := func() error { return publish(multiOutput{body: map[string]any{"isFinal": true}}) }
	for {
		select {
		case <-c.ctx.Done():
			return
		case g, ok := <-c.jobs:
			if !ok {
				if c.terminalOnClose {
					if final() != nil {
						return
					}
				}
				_ = publish(multiOutput{stopped: true})
				return
			}
			err := s.writeTTSGeneration(c.ctx, options, g, func(body map[string]any) error {
				// Close-context may cancel an unfinished generation while retaining earlier
				// flushed jobs. Do not publish any more of the canceled source's output.
				if g.aborted.Load() {
					return context.Canceled
				}
				return publish(multiOutput{body: body, generation: g})
			})
			if c.ctx.Err() != nil {
				return
			}
			if err != nil && !g.aborted.Load() {
				_ = publish(multiOutput{err: err})
				return
			}
			if final() != nil {
				return
			}
		}
	}
}

func (c *ttsMultiContext) closeInput(flush bool) error {
	if c.closing {
		return nil
	}
	if flush {
		if c.input.active != nil {
			c.hadGeneration = true
		}
		if err := c.input.finish(); err != nil {
			return err
		}
	} else if g := c.input.active; g != nil {
		if !g.started {
			c.terminalOnClose = true
		}
		g.aborted.Store(true)
		_ = g.source.Close()
		c.input.active = nil
	}
	if !c.hadGeneration {
		c.terminalOnClose = true
	}
	c.closing = true
	close(c.jobs)
	return nil
}
