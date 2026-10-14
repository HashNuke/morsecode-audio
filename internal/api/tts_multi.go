package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/coder/websocket"
)

type multiInput struct {
	message ttsMultiMessage
	err     error
}
type ttsMultiConnection struct {
	server    *Server
	socket    *speechSocket
	ctx       context.Context
	options   ttsSocketOptions
	contexts  []*ttsMultiContext
	wake      chan struct{}
	next      int
	closing   bool
	lastInput time.Time
}

func (s *Server) ttsMultiWebSocket(w http.ResponseWriter, r *http.Request) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, 422, "invalid_request", "Invalid query parameters")
		return
	}
	authenticated, ok := s.ttsSocketAuth(w, r, q)
	if !ok {
		return
	}
	if !knownVoice(w, r) {
		return
	}
	options, err := parseTTSSocketOptions(q)
	if err != nil {
		writeError(w, 422, err.Error(), "Invalid synthesis options")
		return
	}
	socket := s.sockets.accept(w, r)
	if socket == nil {
		return
	}
	defer socket.close()
	socket.conn.SetReadLimit(64 << 10)
	workCtx, cancel := context.WithCancel(socket.ctx)
	c := &ttsMultiConnection{server: s, socket: socket, ctx: workCtx, options: options, wake: make(chan struct{}, 1), lastInput: time.Now()}
	defer func() {
		cancel()
		for _, item := range c.contexts {
			item.cancel()
		}
		for _, item := range c.contexts {
			<-item.done
		}
	}()
	kind, data, err := socket.readWithin(s.ttsInitTimeout)
	if err != nil {
		return
	}
	first, err := parseTTSMultiMessage(data)
	if err != nil || kind != websocket.MessageText || first.CloseSocket || first.CloseContext != nil || first.Text == nil {
		ttsSocketFailure(socket, "invalid_request")
		return
	}
	if !authenticated && !first.authenticated() {
		ttsSocketFailure(socket, "needs_authorization")
		return
	}
	// The authenticated connection authorizes subsequent contexts, not their keys.
	first.Key = nil
	first.HyphenKey = nil
	first.Authorization = nil
	if err := c.accept(first); err != nil {
		ttsSocketFailure(socket, err.Error())
		return
	}
	incoming := make(chan multiInput, 1)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			kind, data, err := socket.conn.Read(workCtx)
			var m ttsMultiMessage
			if err != nil {
				err = errors.New("input_disconnected")
			}
			if err == nil {
				m, err = parseTTSMultiMessage(data)
				if kind != websocket.MessageText {
					err = errors.New("invalid_request")
				}
			}
			select {
			case incoming <- multiInput{m, err}:
			case <-workCtx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	defer func() { cancel(); <-readerDone }()
	if err := c.run(incoming); err != nil && socket.ctx.Err() == nil {
		ttsSocketFailure(socket, err.Error())
	}
}

func (c *ttsMultiConnection) accept(m ttsMultiMessage) error {
	if c.closing {
		return nil
	}
	c.lastInput = time.Now()
	if m.CloseSocket {
		for _, item := range c.contexts {
			if err := item.closeInput(true); err != nil {
				return err
			}
		}
		c.closing = true
		return nil
	}
	id := m.id()
	var item *ttsMultiContext
	for _, candidate := range c.contexts {
		if candidate.id == id && !candidate.closing {
			item = candidate
		}
	}
	if m.CloseContext != nil {
		if item != nil {
			item.touched = time.Now()
			if *m.CloseContext {
				return item.closeInput(false)
			}
		}
		return nil
	}
	if m.Key != nil || m.HyphenKey != nil || m.Authorization != nil {
		if item != nil {
			return errors.New("invalid_request")
		}
		if !m.authenticated() {
			return errors.New("needs_authorization")
		}
		m.Key = nil
		m.HyphenKey = nil
		m.Authorization = nil
	}
	if item == nil {
		if len(c.contexts) >= 5 {
			return errors.New("too_many_contexts")
		}
		if m.Text == nil {
			return errors.New("context_not_found")
		}
		item = c.server.newMultiContext(c.ctx, id, c.options, m.ttsSocketMessage, c.wake)
		c.contexts = append(c.contexts, item)
	} else if (m.Voice != nil && !reflect.DeepEqual(m.Voice, item.initial.Voice)) || (m.Generation != nil && !reflect.DeepEqual(m.Generation, item.initial.Generation)) {
		return errors.New("invalid_request")
	}
	item.touched = time.Now()
	if m.Text != nil && *m.Text != "" {
		if item.input.active != nil || strings.TrimSpace(*m.Text) != "" {
			item.hadGeneration = true
		}
		if err := item.input.accept(m.ttsSocketMessage); err != nil {
			return err
		}
	} else if m.Flush {
		if item.input.active != nil {
			item.hadGeneration = true
		}
		if err := item.input.finish(); err != nil {
			return err
		}
	}
	return nil
}

func (c *ttsMultiConnection) publishOne() (bool, error) {
	n := len(c.contexts)
	for offset := 0; offset < n; offset++ {
		index := (c.next + offset) % n
		item := c.contexts[index]
		blocked := false
		for i := 0; i < index; i++ {
			if c.contexts[i].id == item.id {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		} // Reused IDs never interleave with their retiring epoch.
		select {
		case event := <-item.output:
			c.next = (index + 1) % n
			if event.err != nil {
				return true, event.err
			}
			if event.stopped {
				item.cancel()
				<-item.done
				c.contexts = append(c.contexts[:index], c.contexts[index+1:]...)
				c.next = 0
				return true, nil
			}
			if event.generation != nil && event.generation.aborted.Load() {
				return true, nil
			}
			event.body["contextId"] = item.id
			return true, c.socket.write(event.body)
		default:
		}
	}
	return false, nil
}

func (c *ttsMultiConnection) run(incoming <-chan multiInput) error {
	timer := time.NewTimer(c.options.idle)
	defer timer.Stop()
	for {
		if err := c.ctx.Err(); err != nil {
			return err
		}
		// Process queued control input before another audio packet.
		select {
		case input := <-incoming:
			if input.err != nil {
				return input.err
			}
			if err := c.accept(input.message); err != nil {
				return err
			}
		default:
		}
		now := time.Now()
		nextDeadline, err := c.expireContexts(now)
		if err != nil {
			return err
		}
		if len(c.contexts) == 0 {
			if c.closing || !now.Before(c.lastInput.Add(c.options.idle)) {
				return c.socket.conn.Close(websocket.StatusNormalClosure, "")
			}
			nextDeadline = c.lastInput.Add(c.options.idle)
		}
		ready, err := c.publishOne()
		if err != nil {
			return err
		}
		if ready {
			continue
		}
		timer.Reset(time.Until(nextDeadline))
		select {
		case <-c.ctx.Done():
			return c.ctx.Err()
		case input := <-incoming:
			if input.err != nil {
				return input.err
			}
			if err := c.accept(input.message); err != nil {
				return err
			}
		case <-c.wake:
		case <-timer.C:
		}
	}
}

// Activity is per context: traffic in one context cannot keep another alive.
func (c *ttsMultiConnection) expireContexts(now time.Time) (time.Time, error) {
	nextDeadline := now.Add(c.options.idle)
	for _, item := range c.contexts {
		if item.closing {
			continue
		}
		deadline := item.touched.Add(c.options.idle)
		if !now.Before(deadline) {
			if err := item.closeInput(false); err != nil {
				return time.Time{}, err
			}
		} else if deadline.Before(nextDeadline) {
			nextDeadline = deadline
		}
	}
	return nextDeadline, nil
}
