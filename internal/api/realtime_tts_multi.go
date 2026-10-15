package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/HashNuke/morsecode-audio/internal/morse"
	"github.com/coder/websocket"
)

// maxMultiContexts mirrors the ElevenLabs multi-stream limit: one socket
// carries a bounded set of sequential utterance contexts, never an
// unbounded session fan-out.
const maxMultiContexts = 5

// multiJobQueue bounds the reader-to-writer handoff for one socket. Each job
// holds at most one client message worth of audio, so queued work stays
// proportional to bounded input rather than minutes of PCM.
const multiJobQueue = 64

// multiFrameBytes matches the single-stream frame bound: every audio event
// carries at most 4,096 decoded bytes.
const multiFrameBytes = 4096

type multiTTSInput struct {
	Text         *string `json:"text"`
	Flush        bool    `json:"flush"`
	CloseContext bool    `json:"close_context"`
	CloseSocket  bool    `json:"close_socket"`
	ContextID    *string `json:"context_id"`
	Trigger      bool    `json:"try_trigger_generation"`
	Voice        *struct {
		Stability  *float64 `json:"stability"`
		Similarity *float64 `json:"similarity_boost"`
		Style      *float64 `json:"style"`
		Boost      *bool    `json:"use_speaker_boost"`
		Speed      *float64 `json:"speed"`
	} `json:"voice_settings"`
	Generation *struct {
		Schedule []int `json:"chunk_length_schedule"`
	} `json:"generation_config"`
	Key           string `json:"xi_api_key"`
	HeaderKey     string `json:"xi-api-key"`
	Authorization string `json:"authorization"`
}

// multiTTSContext is reader-owned state for one utterance context. The writer
// never touches it; cancellation crosses the boundary through the job's own
// atomic flag, so a reused context ID can never cancel or revive another
// generation's queued audio.
type multiTTSContext struct {
	encoder *morse.StreamEncoder
	active  time.Time
	flushed bool
	cancel  *atomic.Bool
}

// multiJob is one unit of writer work. Audio jobs stream their reader into
// bounded frames; terminal jobs send exactly one audio-free isFinal. A nil
// cancel marks flush audio as immortal: an already-requested flush finishes
// even when its context closes mid-write, while ordinary outstanding audio
// is suppressed.
type multiJob struct {
	id     string
	audio  io.Reader
	final  bool
	cancel *atomic.Bool
}

// realtimeTTSMulti is the bounded multi-context analogue of realtimeTTS. Each
// context is one utterance: initialize with a single space or with initial
// text, append text, flush sentence tails, then close_context for the terminal
// without closing the socket. Flush never ends the context, so later text
// with the same ID continues the same context; only close retires it, and the
// same ID may initialize again afterwards. Closing an unflushed context
// cancels its outstanding audio: already-written frames stand, the rest is
// suppressed, and the terminal still arrives. Each flush seals its
// generation with a fresh cancellation token, so only post-flush text is
// ever suppressed while exactly the promised flushed PCM drains, across any
// number of flushes in waveform order. An empty text is a keepalive
// and never produces audio or completion. Silent fragments such as lone
// spaces carry no PCM and never occupy the bounded queue, so benign bursts
// keep the reader responsive to close_context; every other enqueue (audio,
// direct-initialization audio, and terminals including unknown-context
// terminals) is bounded nonblocking under the same 64-job bound, so a full
// queue of meaningful work or a burst of terminals is explicit backpressure
// (a deterministic error that closes the socket), never a silent stall that
// hangs the reader behind audio. Unknown close_context is an
// idempotent terminal so a cancelled-then-completed race cannot end the
// socket; it never fabricates a context, only the terminal under the same
// bound. Idle contexts retire separately with terminals and free their
// slots while busier contexts on the same socket keep working.
//
// The handler is a single reader. One writer goroutine owns all socket
// writes; the reader never writes while the writer runs. Every terminal
// error stops and joins the writer first (abandoning queued work within the
// existing write bound), so the error is strictly the last write and audio
// can never arrive after it. Jobs are bounded, contexts are bounded, and no
// other tasks or messages exist per socket. Only the graceful close_socket
// drain intentionally waits for already-accepted flushed work.
func (s *Server) realtimeTTSMulti(w http.ResponseWriter, r *http.Request) {
	if !knownVoice(w, r) {
		return
	}
	rate, idle, err := ttsSocketOptions(r.URL.Query())
	if err != nil {
		writeError(w, 422, err.Error(), "Unsupported realtime synthesis options")
		return
	}
	// A connected multi-context session reserves one synthesis slot, shared
	// with HTTP and single-context synthesis.
	select {
	case s.ttsSlots <- struct{}{}:
		defer func() { <-s.ttsSlots }()
	default:
		writeError(w, 429, "too_many_concurrent_requests", "Synthesis capacity is busy")
		return
	}
	socket := s.ttsSockets.accept(w, r)
	if socket == nil {
		return
	}
	defer socket.close()
	socket.conn.SetReadLimit(64 << 10)
	cfg := morse.DefaultConfig()
	cfg.SampleRate = rate
	contexts := make(map[string]*multiTTSContext)
	jobs := make(chan multiJob, multiJobQueue)
	writerDone := make(chan struct{})
	var abort atomic.Bool
	writer := &multiWriter{socket: socket, abort: &abort}
	go func() {
		defer close(writerDone)
		writer.run(jobs)
	}()
	jobsOpen := true
	closeJobs := func() {
		if jobsOpen {
			jobsOpen = false
			close(jobs)
		}
	}
	defer func() {
		closeJobs()
		<-writerDone
	}()
	// fail is the only reader-side terminal-error path. It stops the writer
	// first (abandoning queued work so a pinned writer cannot keep the reader
	// behind minutes of audio) and joins it, so the error below is strictly
	// the last write on the socket: audio can never arrive after it. The join
	// is bounded by the existing 5-second write bound: the writer checks abort
	// before every job and frame, so at most one in-flight frame finishes.
	// Explicit overload may therefore truncate accepted flushed audio, but the
	// truncation is always followed by the error, never by completion.
	fail := func(code, message string) {
		abort.Store(true)
		closeJobs()
		<-writerDone
		ttsFailure(socket, code, message)
	}
	socketDead := func() bool {
		select {
		case <-writerDone:
			return true
		case <-socket.ctx.Done():
			return true
		default:
			return false
		}
	}
	// tryEnqueue is the bounded nonblocking policy for EVERY inbound-command
	// enqueue: audio and terminals alike never wait behind already-queued
	// work. A full queue is explicit backpressure (deterministic
	// too_many_queued_messages via fail) rather than a silent stall that
	// would delay close_context behind audio. A dead socket just ends the
	// handler with no further write. Unknown close_context stays an
	// idempotent terminal but never fabricates a context: only the terminal
	// is queued, under the same bound.
	tryEnqueue := func(job multiJob) bool {
		select {
		case jobs <- job:
			if socketDead() {
				return false
			}
			return true
		default:
		}
		if socketDead() {
			return false
		}
		fail("too_many_queued_messages", "The synthesis queue is full; pace messages or close contexts")
		return false
	}
	terminal := func(id string) bool {
		return tryEnqueue(multiJob{id: id, final: true})
	}
	sendAudio := func(job multiJob) bool {
		return tryEnqueue(job)
	}
	for {
		// The read timeout is fatal by design in coder/websocket: an expired
		// read context closes the connection. The socket therefore only dies
		// when truly idle, exactly like the single-context endpoint. Idle
		// contexts are retired below whenever any message wakes the reader,
		// so activity on one context never pins the slots of abandoned ones.
		kind, data, err := socket.readWithin(idle)
		if err != nil {
			return
		}
		now := time.Now()
		retired := make(map[string]bool, len(contexts))
		for id, expired := range contexts {
			if now.Sub(expired.active) < idle {
				continue
			}
			if !expired.flushed {
				expired.cancel.Store(true)
			}
			delete(contexts, id)
			retired[id] = true
			if !terminal(id) {
				return
			}
		}
		var input multiTTSInput
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if kind != websocket.MessageText || decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
			fail("invalid_input", "Expected a JSON text message")
			return
		}
		if input.Voice != nil && input.Voice.Speed != nil && *input.Voice.Speed != 1 {
			fail("unsupported_voice_settings", "Morse speed is fixed at 20 WPM")
			return
		}
		if input.Generation != nil {
			for _, size := range input.Generation.Schedule {
				if size < 50 || size > 500 {
					fail("invalid_generation_config", "Invalid chunk schedule")
					return
				}
			}
		}
		if input.CloseSocket {
			if input.Text != nil || input.Flush || input.CloseContext || input.ContextID != nil {
				fail("invalid_input", "close_socket ends the whole connection")
				return
			}
			// Only already-requested flushes finish: unflushed contexts are
			// cancelled, never synthesized, then every context ends with its
			// terminal in sorted order before the normal close.
			for _, id := range sortedMultiContexts(contexts) {
				ctx := contexts[id]
				if !ctx.flushed {
					ctx.cancel.Store(true)
				}
				delete(contexts, id)
				if !terminal(id) {
					return
				}
			}
			closeJobs()
			<-writerDone
			_ = socket.conn.Close(websocket.StatusNormalClosure, "")
			return
		}
		id := "default"
		if input.ContextID != nil {
			id = *input.ContextID
		}
		if len(id) == 0 || len(id) > 128 {
			fail("invalid_context", "Context identity must be 1-128 characters")
			return
		}
		if input.CloseContext {
			if input.Text != nil || input.Flush {
				fail("invalid_input", "close_context carries no text")
				return
			}
			ctx, open := contexts[id]
			if open {
				// Cancelling an unflushed context suppresses its
				// outstanding frames; an already-flushed context only ends.
				if !ctx.flushed {
					ctx.cancel.Store(true)
				}
				delete(contexts, id)
			}
			// A context retired for idleness on this same wakeup already has
			// its terminal queued; never send it twice.
			if open || !retired[id] {
				if !terminal(id) {
					return
				}
			}
			continue
		}
		if input.Text == nil && !input.Flush {
			fail("invalid_input", "Expected a JSON text message")
			return
		}
		var text string
		if input.Text != nil {
			text = *input.Text
		}
		ctx, open := contexts[id]
		if text == "" && !input.Flush {
			// Keepalive: reset the clock without audio or completion.
			if !open {
				if len(contexts) >= maxMultiContexts {
					fail("too_many_contexts", "Too many concurrent synthesis contexts")
					return
				}
				encoder, _ := morse.NewStreamEncoder(cfg)
				contexts[id] = &multiTTSContext{encoder: encoder, active: now, cancel: new(atomic.Bool)}
			} else {
				ctx.active = now
			}
			continue
		}
		if !open {
			if input.Text == nil {
				fail("unknown_context", "Flush targets an open synthesis context")
				return
			}
			// InitialiseContext: a new context starts with a single space or
			// directly with its first text, which synthesizes immediately.
			if text != " " && strings.TrimSpace(text) == "" {
				fail("invalid_initialization", "Initialize with a single space or initial text")
				return
			}
			if len(contexts) >= maxMultiContexts {
				fail("too_many_contexts", "Too many concurrent synthesis contexts")
				return
			}
			encoder, _ := morse.NewStreamEncoder(cfg)
			fresh := &multiTTSContext{encoder: encoder, active: now, cancel: new(atomic.Bool)}
			if text != " " {
				audio, err := encoder.Push(text, input.Flush)
				if err != nil {
					fail(err.Error(), "Text is not supported Morse or exceeds the utterance limit")
					return
				}
				fresh.flushed = input.Flush
				contexts[id] = fresh
				if !sendAudio(multiJob{id: id, audio: audio, cancel: multiCancel(fresh, input.Flush)}) {
					return
				}
				continue
			}
			if input.Flush {
				fresh.flushed = true
			}
			contexts[id] = fresh
			continue
		}
		ctx.active = now
		audio, err := ctx.encoder.Push(text, input.Flush)
		if err != nil {
			fail(err.Error(), "Text is not supported Morse or exceeds the utterance limit")
			return
		}
		coalesced := coalesceAudio(audio)
		if input.Flush {
			// Flush completes the sentence tail but leaves the context open
			// for later text with the same ID. It also seals the generation:
			// every job accepted so far keeps the previous token, which is
			// never cancelled afterwards, so a later close suppresses only
			// post-flush text while exactly the promised flushed PCM drains.
			// The swap runs in the single reader and the writer only loads
			// the token its own job captured, so sealing and cancellation
			// cannot race.
			ctx.flushed = true
			ctx.cancel = new(atomic.Bool)
		} else if coalesced != nil {
			ctx.flushed = false
		}
		if coalesced == nil {
			// A silent fragment (word-gap bookkeeping such as a lone space)
			// carries no PCM: the clock is updated above, but no job is
			// queued, so benign space bursts can never wedge the bounded
			// queue and starve control messages. Skipping the empty job is
			// wire-identical: it would have written zero frames.
			continue
		}
		if !sendAudio(multiJob{id: id, audio: coalesced, cancel: multiCancel(ctx, input.Flush)}) {
			return
		}
	}
}

// multiCancel binds an audio job to its generation: flush audio is immortal
// so an already-requested flush finishes across a close, while ordinary
// audio carries the generation flag the writer checks between frames. A
// flush also rotates the context token, so earlier jobs keep a token that is
// never cancelled afterwards and later text starts a fresh generation.
func multiCancel(ctx *multiTTSContext, flush bool) *atomic.Bool {
	if flush {
		return nil
	}
	return ctx.cancel
}

// coalesceAudio drops no-op synthesis without queueing: silent fragments
// produce zero PCM bytes, and one job per silent fragment would let benign
// bursts wedge the bounded queue and delay control behind nothing. A
// non-empty reader is returned with its probed bytes prepended, so the
// waveform is byte-identical.
func coalesceAudio(audio io.Reader) io.Reader {
	var probe [2]byte
	n, _ := audio.Read(probe[:])
	if n == 0 {
		return nil
	}
	return io.MultiReader(bytes.NewReader(probe[:n]), audio)
}

type multiWriter struct {
	socket *speechSocket
	abort  *atomic.Bool
	buffer [multiFrameBytes]byte
}

func (w *multiWriter) run(jobs <-chan multiJob) {
	for job := range jobs {
		if w.abort.Load() {
			return
		}
		if job.final {
			if w.socket.write(map[string]any{"audio": nil, "isFinal": true, "contextId": job.id}) != nil {
				return
			}
			continue
		}
		if job.cancel != nil && job.cancel.Load() {
			continue
		}
		if !w.audio(job) {
			return
		}
	}
}

func (w *multiWriter) audio(job multiJob) bool {
	for {
		if w.abort.Load() {
			return true
		}
		if job.cancel != nil && job.cancel.Load() {
			return true
		}
		n, err := job.audio.Read(w.buffer[:])
		if n > 0 {
			if writeErr := w.socket.write(map[string]any{"audio": base64.StdEncoding.EncodeToString(w.buffer[:n]), "isFinal": false, "contextId": job.id}); writeErr != nil {
				return false
			}
		}
		if err == io.EOF {
			return true
		}
		if err != nil {
			return false
		}
	}
}

func sortedMultiContexts(contexts map[string]*multiTTSContext) []string {
	ids := make([]string, 0, len(contexts))
	for id := range contexts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
