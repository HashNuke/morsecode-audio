package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// socketSet owns admitted, upgrading and active speech sockets. HTTP shutdown
// does not track hijacked connections, so drain and completion are explicit.
type socketSet struct {
	mu                           sync.Mutex
	active                       map[*speechSocket]struct{}
	draining                     bool
	done                         chan struct{}
	closed                       bool
	capacity                     int
	idle, lifetime, writeTimeout time.Duration
}

func newSocketSet() *socketSet {
	return &socketSet{active: make(map[*speechSocket]struct{}), done: make(chan struct{}), capacity: 64, idle: 30 * time.Second, lifetime: time.Hour, writeTimeout: 5 * time.Second}
}

type speechSocket struct {
	set    *socketSet
	conn   *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
}

func (s *socketSet) accept(w http.ResponseWriter, r *http.Request) *speechSocket {
	s.mu.Lock()
	if s.draining || len(s.active) >= s.capacity {
		draining := s.draining
		s.mu.Unlock()
		if draining {
			writeError(w, 503, "not_ready", "Server is draining")
		} else {
			writeError(w, 429, "too_many_concurrent_requests", "Speech connection capacity is busy")
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.lifetime)
	socket := &speechSocket{set: s, ctx: ctx, cancel: cancel}
	s.active[socket] = struct{}{}
	s.mu.Unlock()
	// All origins may use this public API; cookies confer no authority. Each
	// admitted operation still requires an explicit compatibility key.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled, InsecureSkipVerify: true})
	if err != nil {
		socket.close()
		return nil
	}
	socket.conn = c
	c.SetReadLimit(512 << 10)
	if ctx.Err() != nil {
		socket.close()
		return nil
	}
	return socket
}

func (s *speechSocket) close() {
	s.cancel()
	if s.conn != nil {
		_ = s.conn.CloseNow()
	}
	s.set.mu.Lock()
	delete(s.set.active, s)
	s.set.finishDrain()
	s.set.mu.Unlock()
}

func (s *socketSet) finishDrain() {
	if s.draining && len(s.active) == 0 && !s.closed {
		s.closed = true
		close(s.done)
	}
}

func (s *socketSet) drain() {
	s.mu.Lock()
	s.draining = true
	for socket := range s.active {
		socket.cancel()
	}
	s.finishDrain()
	s.mu.Unlock()
}

func (s *speechSocket) read() (websocket.MessageType, []byte, error) {
	return s.readWithin(s.set.idle)
}

func (s *speechSocket) readWithin(timeout time.Duration) (websocket.MessageType, []byte, error) {
	ctx, cancel := context.WithTimeout(s.ctx, timeout)
	defer cancel()
	return s.conn.Read(ctx)
}

func (s *speechSocket) write(body any) error {
	ctx, cancel := context.WithTimeout(s.ctx, s.set.writeTimeout)
	defer cancel()
	return wsjson.Write(ctx, s.conn, body)
}

func (s *speechSocket) failure(kind, message string) {
	_ = s.write(map[string]string{"message_type": kind, "error": message})
}
