package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/coder/websocket"
)

func (s *Server) ttsWebSocket(w http.ResponseWriter, r *http.Request) {
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
	kind, data, err := socket.readWithin(s.ttsInitTimeout)
	if err != nil {
		return
	}
	initial, err := parseTTSSocketMessage(data)
	if err != nil || kind != websocket.MessageText || *initial.Text != " " || initial.Flush {
		ttsSocketFailure(socket, "invalid_request")
		return
	}
	if !authenticated && !initial.authenticated() {
		ttsSocketFailure(socket, "needs_authorization")
		return
	}
	// Never retain initialization credentials alongside the active text state.
	initial.Key = nil
	initial.HyphenKey = nil
	initial.Authorization = nil
	workCtx, cancel := context.WithCancel(socket.ctx)
	defer cancel()
	jobs := make(chan *ttsGeneration, 4)
	done := make(chan struct{})
	go func() { defer close(done); s.writeTTSJobs(workCtx, cancel, socket, options, jobs) }()
	input := ttsTextInput{ctx: workCtx, jobs: jobs, options: options, schedule: []int{120, 160, 250, 290}}
	if initial.Generation != nil {
		input.schedule = initial.Generation.Schedule
	}
	complete := false
	var inputErr error
	for {
		kind, data, err = socket.readWithin(options.idle)
		if err != nil {
			break
		}
		message, parseErr := parseTTSSocketMessage(data)
		if parseErr != nil {
			inputErr = parseErr
			break
		}
		if kind != websocket.MessageText || message.Key != nil || message.HyphenKey != nil || message.Authorization != nil ||
			(message.Voice != nil && !reflect.DeepEqual(message.Voice, initial.Voice)) ||
			(message.Generation != nil && !reflect.DeepEqual(message.Generation, initial.Generation)) {
			inputErr = errors.New("invalid_request")
			break
		}
		if *message.Text == "" {
			inputErr = input.finish()
			complete = inputErr == nil
			break
		}
		if inputErr = input.accept(message); inputErr != nil {
			break
		}
	}
	if !complete {
		cancel()
	}
	close(jobs)
	<-done // Join output and codecs before releasing the hijacked socket.
	if inputErr != nil && socket.ctx.Err() == nil {
		ttsSocketFailure(socket, inputErr.Error())
	}
}

func ttsSocketFailure(socket *speechSocket, code string) {
	_ = socket.write(map[string]any{"error": code, "message": "Cannot process this synthesis request"})
	_ = socket.conn.Close(websocket.StatusPolicyViolation, "Invalid synthesis request")
}

type ttsTextInput struct {
	ctx           context.Context
	jobs          chan<- *ttsGeneration
	options       ttsSocketOptions
	schedule      []int
	scheduleIndex int
	active        *ttsGeneration
}

func (s *ttsTextInput) accept(message ttsSocketMessage) error {
	text := *message.Text
	if s.active == nil && strings.TrimSpace(text) == "" {
		return nil
	}
	if s.active == nil {
		s.active = newTTSGeneration(s.ctx, s.options.format.Rate)
	}
	if err := s.active.buffer(text); err != nil {
		return err
	}
	if message.Flush {
		return s.finish()
	}
	threshold := s.schedule[min(s.scheduleIndex, len(s.schedule)-1)]
	if message.Trigger {
		threshold = min(threshold, 50)
	}
	if s.options.auto || utf8.RuneCountInString(s.active.pending) >= threshold {
		if err := s.active.release(); err != nil {
			return err
		}
		s.scheduleIndex++
		return s.start()
	}
	return nil
}

func (s *ttsTextInput) start() error {
	if s.active.started || len(s.active.source.queue) == 0 {
		return nil
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	select {
	case s.jobs <- s.active:
		s.active.started = true
		return nil
	default:
		return errors.New("generation_queue_full")
	}
}

func (s *ttsTextInput) finish() error {
	if s.active == nil {
		return nil
	}
	if err := s.active.finish(); err != nil {
		return err
	}
	if err := s.start(); err != nil {
		return err
	}
	if !s.active.started {
		_ = s.active.source.Close()
	}
	s.active = nil
	s.scheduleIndex = 0
	return nil
}
