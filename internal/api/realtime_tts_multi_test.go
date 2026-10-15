package api_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HashNuke/morsecode-audio/internal/api"
	"github.com/HashNuke/morsecode-audio/internal/morse"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func dialMultiTTS(t *testing.T, server *httptest.Server, rate int) (*websocket.Conn, context.Context) {
	return dialMultiTTSWith(t, server, fmt.Sprintf("model_id=morse_v1&output_format=pcm_%d&auto_mode=true", rate))
}

func dialMultiTTSWith(t *testing.T, server *httptest.Server, query string) (*websocket.Conn, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	c, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/text-to-speech/morse/multi-stream-input?"+query, nil)
	if err != nil {
		t.Fatalf("multi-stream upgrade: %v (%v)", err, response)
	}
	t.Cleanup(func() { c.CloseNow() })
	return c, ctx
}

// multiUtterance runs one complete context on an already-open socket and
// returns its audio: initialize, append text, flush the tail, then close the
// context for its terminal. Flush alone never ends the context, so the close
// is what delivers isFinal. The socket must stay open for the next utterance.
func multiUtterance(t *testing.T, ctx context.Context, c *websocket.Conn, contextID, text string, rate int) []byte {
	t.Helper()
	ttsMessage(t, ctx, c, map[string]any{"text": " ", "context_id": contextID})
	ttsMessage(t, ctx, c, map[string]any{"text": text, "context_id": contextID})
	ttsMessage(t, ctx, c, map[string]any{"context_id": contextID, "flush": true})
	ttsMessage(t, ctx, c, map[string]any{"context_id": contextID, "close_context": true})
	var audio []byte
	for {
		message := readSTT(t, ctx, c)
		if message["contextId"] != contextID {
			t.Fatalf("missing context correlation: %v", message)
		}
		if message["isFinal"] == true {
			if message["audio"] != nil {
				t.Fatalf("final must not carry audio: %v", message)
			}
			return audio
		}
		encoded, _ := message["audio"].(string)
		pcm, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(pcm) == 0 || len(pcm) > 4096 || len(pcm)%2 != 0 {
			t.Fatal("invalid bounded PCM frame")
		}
		audio = append(audio, pcm...)
	}
}

// multiAudio reads exactly size audio bytes for one context. Audio events
// must never carry isFinal; the terminal arrives separately on close.
func multiAudio(t *testing.T, ctx context.Context, c *websocket.Conn, contextID string, size int) []byte {
	t.Helper()
	var audio []byte
	for len(audio) < size {
		message := readSTT(t, ctx, c)
		if message["contextId"] != contextID {
			t.Fatalf("missing context correlation: %v", message)
		}
		if message["isFinal"] == true {
			t.Fatalf("flush must not end the context: %v", message)
		}
		encoded, _ := message["audio"].(string)
		pcm, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(pcm) == 0 || len(pcm) > 4096 || len(pcm)%2 != 0 {
			t.Fatal("invalid bounded PCM frame")
		}
		audio = append(audio, pcm...)
	}
	if len(audio) != size {
		t.Fatal("unexpected audio tail")
	}
	return audio
}

func multiWant(t *testing.T, text string, rate int) []byte {
	t.Helper()
	cfg := morse.DefaultConfig()
	cfg.SampleRate = rate
	encoder, err := morse.NewEncoder(text, cfg)
	if err != nil {
		t.Fatal(err)
	}
	want, err := io.ReadAll(encoder)
	if err != nil {
		t.Fatal(err)
	}
	return want
}

// multiBody returns exactly the unflushed PCM for text: a streaming encoder
// pushed once without flush. Cancellation tests must compare against this
// real body length, not the tailed full utterance: the 840-ms tail would
// otherwise admit draining the entire unflushed body while still passing.
func multiBody(t *testing.T, text string, rate int) []byte {
	t.Helper()
	cfg := morse.DefaultConfig()
	cfg.SampleRate = rate
	encoder, err := morse.NewStreamEncoder(cfg)
	if err != nil {
		t.Fatal(err)
	}
	audio, err := encoder.Push(text, false)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(audio)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestMultiStreamSequentialUtterancesOnOneSocket(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	c, ctx := dialMultiTTS(t, s, 16000)
	// Five sequential utterances on one live socket, including a context ID
	// reused after its close, prove bounded socket reuse without new dials.
	for _, utterance := range []struct{ id, text string }{
		{"ctx-1", "SOS"},
		{"ctx-2", "E T"},
		{"ctx-reused", "SOS"},
		{"ctx-reused", "T"},
		{"ctx-3", "HELLO"},
	} {
		if actual := multiUtterance(t, ctx, c, utterance.id, utterance.text, 16000); !bytes.Equal(actual, multiWant(t, utterance.text, 16000)) {
			t.Fatalf("utterance %q changed waveform or timing", utterance.text)
		}
	}
	ttsMessage(t, ctx, c, map[string]any{"close_socket": true})
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("expected normal close after close_socket: %v", err)
	}
}

// TestMultiStreamInitWithDirectText is the InitialiseContext regression: a new
// context accepts its first text immediately, without a single-space prologue.
func TestMultiStreamInitWithDirectText(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	c, ctx := dialMultiTTS(t, s, 16000)
	ttsMessage(t, ctx, c, map[string]any{"text": "SOS ", "context_id": "ctx-init"})
	ttsMessage(t, ctx, c, map[string]any{"context_id": "ctx-init", "flush": true})
	ttsMessage(t, ctx, c, map[string]any{"context_id": "ctx-init", "close_context": true})
	var audio []byte
	for {
		message := readSTT(t, ctx, c)
		if message["contextId"] != "ctx-init" {
			t.Fatalf("missing context correlation: %v", message)
		}
		if message["isFinal"] == true {
			if message["audio"] != nil {
				t.Fatalf("final must not carry audio: %v", message)
			}
			break
		}
		encoded, _ := message["audio"].(string)
		pcm, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(pcm) == 0 {
			t.Fatal("invalid init audio")
		}
		audio = append(audio, pcm...)
	}
	if !bytes.Equal(audio, multiWant(t, "SOS ", 16000)) {
		t.Fatal("direct-text initialization changed waveform or timing")
	}
}

// TestMultiStreamCancelSuppressesOutstanding sends minutes of unflushed audio,
// closes that context, then synthesizes the next one. Already-written frames
// may arrive, but the outstanding tail must be suppressed and the new context
// must complete with a strict waveform instead of draining the old audio.
func TestMultiStreamCancelSuppressesOutstanding(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	c, ctx := dialMultiTTS(t, s, 16000)
	long := strings.Repeat("T ", 480)
	full := multiWant(t, long, 16000)
	body := multiBody(t, long, 16000)
	if len(body) < 1<<20 {
		t.Fatalf("cancel fixture too short to prove suppression: %d bytes", len(body))
	}
	if len(full) != len(body)+int(0.84*16000*2) {
		t.Fatalf("body/tail split changed: full %d body %d", len(full), len(body))
	}
	ttsMessage(t, ctx, c, map[string]any{"text": " ", "context_id": "ctx-old"})
	// No reads between the long text and its close: the close must cancel the
	// still-queued generation instead of waiting for minutes of audio.
	ttsMessage(t, ctx, c, map[string]any{"text": long, "context_id": "ctx-old"})
	ttsMessage(t, ctx, c, map[string]any{"context_id": "ctx-old", "close_context": true})
	ttsMessage(t, ctx, c, map[string]any{"text": " ", "context_id": "ctx-new"})
	ttsMessage(t, ctx, c, map[string]any{"text": "SOS", "context_id": "ctx-new"})
	ttsMessage(t, ctx, c, map[string]any{"context_id": "ctx-new", "flush": true})
	ttsMessage(t, ctx, c, map[string]any{"context_id": "ctx-new", "close_context": true})
	var oldAudio, newAudio []byte
	var oldFinal, newFinal bool
	for !oldFinal || !newFinal {
		message := readSTT(t, ctx, c)
		id, _ := message["contextId"].(string)
		if message["isFinal"] == true {
			if message["audio"] != nil {
				t.Fatalf("final must not carry audio: %v", message)
			}
			switch id {
			case "ctx-old":
				oldFinal = true
			case "ctx-new":
				newFinal = true
			default:
				t.Fatalf("uncorrelated terminal: %v", message)
			}
			continue
		}
		encoded, _ := message["audio"].(string)
		pcm, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(pcm) == 0 || len(pcm) > 4096 {
			t.Fatal("invalid bounded PCM frame")
		}
		switch id {
		case "ctx-old":
			oldAudio = append(oldAudio, pcm...)
		case "ctx-new":
			newAudio = append(newAudio, pcm...)
		default:
			t.Fatalf("uncorrelated audio: %v", message)
		}
	}
	if len(oldAudio) >= len(body) {
		t.Fatalf("cancelled context drained the complete unflushed body instead of suppressing it: got %d bytes, body %d", len(oldAudio), len(body))
	}
	if !bytes.HasPrefix(body, oldAudio) {
		t.Fatal("cancelled audio is not a strict prefix of the unflushed body")
	}
	if !bytes.Equal(newAudio, multiWant(t, "SOS", 16000)) {
		t.Fatal("utterance after cancel changed waveform or timing")
	}
}

func TestMultiStreamKeepaliveCancelAndReuse(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	// A one-second idle budget proves empty-text keepalives reset the clock:
	// without them the socket below would die long before the utterance.
	c, ctx := dialMultiTTSWith(t, s, "model_id=morse_v1&output_format=pcm_16000&auto_mode=true&inactivity_timeout=1")
	ttsMessage(t, ctx, c, map[string]any{"text": " ", "context_id": "ctx-a"})
	for range 3 {
		time.Sleep(600 * time.Millisecond)
		// An empty text is a keepalive: it resets the clock without audio or
		// completion, so the first frame after it must still be utterance audio.
		ttsMessage(t, ctx, c, map[string]any{"text": "", "context_id": "ctx-a"})
	}
	// Cancelling an unflushed context ends it with a terminal; the unflushed
	// tail is cancelled, so only an already-written prefix (possibly empty)
	// may arrive before isFinal, never the complete waveform.
	long := strings.Repeat("SOS ", 30)
	body := multiBody(t, long, 16000)
	ttsMessage(t, ctx, c, map[string]any{"text": long, "context_id": "ctx-a"})
	ttsMessage(t, ctx, c, map[string]any{"context_id": "ctx-a", "close_context": true})
	var audio []byte
	for {
		message := readSTT(t, ctx, c)
		if message["contextId"] != "ctx-a" {
			t.Fatalf("missing cancel correlation: %v", message)
		}
		if message["isFinal"] == true {
			if message["audio"] != nil {
				t.Fatalf("final must not carry audio: %v", message)
			}
			break
		}
		encoded, _ := message["audio"].(string)
		pcm, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(pcm) == 0 {
			t.Fatal("invalid cancel audio")
		}
		audio = append(audio, pcm...)
	}
	if len(audio) >= len(body) {
		t.Fatalf("close_context drained the complete unflushed body instead of cancelling it: got %d bytes, body %d", len(audio), len(body))
	}
	if !bytes.HasPrefix(body, audio) {
		t.Fatal("cancelled audio is not a strict prefix of the unflushed body")
	}
	// Closing an unknown context is idempotent and must not end the socket.
	ttsMessage(t, ctx, c, map[string]any{"context_id": "missing", "close_context": true})
	if message := readSTT(t, ctx, c); message["isFinal"] != true || message["contextId"] != "missing" {
		t.Fatalf("unknown close not fenced: %v", message)
	}
	// A new utterance on the same socket still completes independently.
	if actual := multiUtterance(t, ctx, c, "ctx-b", "T", 16000); !bytes.Equal(actual, multiWant(t, "T", 16000)) {
		t.Fatal("utterance after cancel changed waveform or timing")
	}
}

// TestMultiStreamPerContextIdleExpiry keeps context A alive while B-E go idle.
// The abandoned contexts must retire separately with terminals and free their
// slots; activity on A must not pin them.
func TestMultiStreamPerContextIdleExpiry(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	c, ctx := dialMultiTTSWith(t, s, "model_id=morse_v1&output_format=pcm_16000&auto_mode=true&inactivity_timeout=1")
	ttsMessage(t, ctx, c, map[string]any{"text": " ", "context_id": "ctx-a"})
	for _, id := range []string{"ctx-b", "ctx-c", "ctx-d", "ctx-e"} {
		ttsMessage(t, ctx, c, map[string]any{"text": " ", "context_id": id})
	}
	// Keep only A alive past the one-second budget; B-E must expire on their
	// own clocks even though the socket itself stays busy. Keepalives go out
	// on their own writer while this goroutine blocks in reads: a client read
	// with a short timeout would break the connection, so expiry is awaited
	// with the bounding test context instead.
	keepalive := make(chan struct{})
	defer close(keepalive)
	go func() {
		ticker := time.NewTicker(400 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-keepalive:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = wsjson.Write(ctx, c, map[string]any{"text": "", "context_id": "ctx-a"})
			}
		}
	}()
	expired := map[string]bool{}
	for len(expired) < 4 {
		message := readSTT(t, ctx, c)
		if message["isFinal"] != true || message["audio"] != nil {
			t.Fatalf("expiry must be an audio-free terminal: %v", message)
		}
		id, _ := message["contextId"].(string)
		if id == "ctx-a" {
			t.Fatalf("kept-alive context expired: %v", message)
		}
		expired[id] = true
	}
	for _, id := range []string{"ctx-b", "ctx-c", "ctx-d", "ctx-e"} {
		if !expired[id] {
			t.Fatalf("abandoned context %s pinned by activity on ctx-a: %v", id, expired)
		}
	}
	// All four slots are free again: a new context completes, and A is still
	// usable on the same socket.
	if actual := multiUtterance(t, ctx, c, "ctx-f", "SOS", 16000); !bytes.Equal(actual, multiWant(t, "SOS", 16000)) {
		t.Fatal("freed slot did not accept a new context")
	}
	if actual := multiUtterance(t, ctx, c, "ctx-a", "E", 16000); !bytes.Equal(actual, multiWant(t, "E", 16000)) {
		t.Fatal("kept-alive context stopped working after peers expired")
	}
}

// TestMultiStreamFlushKeepsContextUsable flushes sentences without closing:
// the context accepts later text with the same ID, each utterance keeps strict
// waveform timing, and only the explicit close sends the single terminal.
func TestMultiStreamFlushKeepsContextUsable(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	c, ctx := dialMultiTTS(t, s, 16000)
	ttsMessage(t, ctx, c, map[string]any{"text": " ", "context_id": "ctx-s"})
	ttsMessage(t, ctx, c, map[string]any{"text": "SOS", "context_id": "ctx-s"})
	ttsMessage(t, ctx, c, map[string]any{"context_id": "ctx-s", "flush": true})
	if audio := multiAudio(t, ctx, c, "ctx-s", len(multiWant(t, "SOS", 16000))); !bytes.Equal(audio, multiWant(t, "SOS", 16000)) {
		t.Fatal("first flushed sentence changed waveform or timing")
	}
	// The flush completed the sentence tail but left the context open. This is
	// proven by ordering: a wrongly-sent terminal would arrive before the next
	// sentence's audio, and multiAudio rejects terminals.
	ttsMessage(t, ctx, c, map[string]any{"text": "E", "context_id": "ctx-s"})
	ttsMessage(t, ctx, c, map[string]any{"context_id": "ctx-s", "flush": true})
	if audio := multiAudio(t, ctx, c, "ctx-s", len(multiWant(t, "E", 16000))); !bytes.Equal(audio, multiWant(t, "E", 16000)) {
		t.Fatal("second sentence on the same context changed waveform or timing")
	}
	// Closing after flushes ends the context once, with no duplicate tail.
	ttsMessage(t, ctx, c, map[string]any{"context_id": "ctx-s", "close_context": true})
	message := readSTT(t, ctx, c)
	if message["isFinal"] != true || message["contextId"] != "ctx-s" || message["audio"] != nil {
		t.Fatalf("close after flush must send one audio-free terminal: %v", message)
	}
	// And the socket stays usable for the next context.
	if actual := multiUtterance(t, ctx, c, "ctx-next", "T", 16000); !bytes.Equal(actual, multiWant(t, "T", 16000)) {
		t.Fatal("socket stalled after close of a flushed context")
	}
}

// TestMultiStreamCloseSocketSettlesFlushedCancelsUnflushed proves close_socket
// finishes an already-flushed context while cancelling a never-flushed one
// instead of synthesizing it, then closes the transport normally.
func TestMultiStreamCloseSocketSettlesFlushedCancelsUnflushed(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	c, ctx := dialMultiTTS(t, s, 16000)
	long := strings.Repeat("T ", 480)
	fullOld := multiWant(t, long, 16000)
	bodyOld := multiBody(t, long, 16000)
	if len(fullOld) != len(bodyOld)+int(0.84*16000*2) {
		t.Fatalf("body/tail split changed: full %d body %d", len(fullOld), len(bodyOld))
	}
	ttsMessage(t, ctx, c, map[string]any{"text": " ", "context_id": "ctx-old"})
	ttsMessage(t, ctx, c, map[string]any{"text": long, "context_id": "ctx-old"})
	ttsMessage(t, ctx, c, map[string]any{"text": " ", "context_id": "ctx-done"})
	ttsMessage(t, ctx, c, map[string]any{"text": "SOS", "context_id": "ctx-done"})
	ttsMessage(t, ctx, c, map[string]any{"context_id": "ctx-done", "flush": true})
	ttsMessage(t, ctx, c, map[string]any{"close_socket": true})
	audioByID := map[string][]byte{}
	finals := map[string]bool{}
	for len(finals) < 2 {
		message := readSTT(t, ctx, c)
		id, _ := message["contextId"].(string)
		if message["isFinal"] == true {
			if message["audio"] != nil {
				t.Fatalf("final must not carry audio: %v", message)
			}
			finals[id] = true
			continue
		}
		encoded, _ := message["audio"].(string)
		pcm, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(pcm) == 0 || len(pcm) > 4096 {
			t.Fatal("invalid bounded PCM frame")
		}
		audioByID[id] = append(audioByID[id], pcm...)
	}
	if !bytes.Equal(audioByID["ctx-done"], multiWant(t, "SOS", 16000)) {
		t.Fatal("close_socket dropped an already-flushed context")
	}
	if len(audioByID["ctx-old"]) >= len(bodyOld) {
		t.Fatalf("close_socket drained the complete unflushed body instead of cancelling it: got %d bytes, body %d", len(audioByID["ctx-old"]), len(bodyOld))
	}
	if !bytes.HasPrefix(bodyOld, audioByID["ctx-old"]) {
		t.Fatal("cancelled audio is not a strict prefix of the unflushed body")
	}
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("expected normal close after close_socket: %v", err)
	}
}

// TestMultiStreamFlushSealsEarlierAudioAgainstLaterCancel is the flush
// generation regression: text, flush, more text, then close on one context
// must deliver exactly the promised flushed PCM (strict waveform) while
// suppressing only the post-flush text. A single cancellation token shared
// across flushes dropped the flushed body and delivered just the 840-ms
// tail.
func TestMultiStreamFlushSealsEarlierAudioAgainstLaterCancel(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	c, ctx := dialMultiTTS(t, s, 16000)
	text := strings.Repeat("T ", 480)
	want := multiWant(t, text, 16000)
	ttsMessage(t, ctx, c, map[string]any{"context_id": "same", "text": text})
	ttsMessage(t, ctx, c, map[string]any{"context_id": "same", "flush": true})
	ttsMessage(t, ctx, c, map[string]any{"context_id": "same", "text": "E"})
	ttsMessage(t, ctx, c, map[string]any{"context_id": "same", "close_context": true})
	var audio []byte
	finals := 0
	for finals == 0 {
		message := readSTT(t, ctx, c)
		if message["contextId"] != "same" {
			t.Fatalf("missing context correlation: %v", message)
		}
		if message["isFinal"] == true {
			if message["audio"] != nil {
				t.Fatalf("final must not carry audio: %v", message)
			}
			finals++
			continue
		}
		encoded, _ := message["audio"].(string)
		pcm, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(pcm) == 0 || len(pcm) > 4096 || len(pcm)%2 != 0 {
			t.Fatal("invalid bounded PCM frame")
		}
		audio = append(audio, pcm...)
	}
	if !bytes.Equal(audio, want) {
		t.Fatalf("already-flushed audio dropped: got %d bytes, expected exactly %d", len(audio), len(want))
	}
}

// TestMultiStreamFullQueueStillCancels is the control starvation regression:
// a long body followed by seventy single-space fragments must still cancel
// on close_context instead of draining the complete unflushed body. Silent
// fragments carry no PCM, so they never occupy the bounded queue; the close
// therefore reaches the reader immediately and the outstanding generation
// is suppressed to a strict prefix of its real body.
func TestMultiStreamFullQueueStillCancels(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	c, ctx := dialMultiTTS(t, s, 16000)
	text := strings.Repeat("T ", 480)
	body := multiBody(t, text, 16000)
	ttsMessage(t, ctx, c, map[string]any{"context_id": "same", "text": text})
	for range 70 {
		ttsMessage(t, ctx, c, map[string]any{"context_id": "same", "text": " "})
	}
	ttsMessage(t, ctx, c, map[string]any{"context_id": "same", "close_context": true})
	var audio []byte
	for {
		message := readSTT(t, ctx, c)
		if message["contextId"] != "same" {
			t.Fatalf("missing context correlation: %v", message)
		}
		if message["isFinal"] == true {
			if message["audio"] != nil {
				t.Fatalf("final must not carry audio: %v", message)
			}
			break
		}
		encoded, _ := message["audio"].(string)
		pcm, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(pcm) == 0 || len(pcm) > 4096 || len(pcm)%2 != 0 {
			t.Fatal("invalid bounded PCM frame")
		}
		audio = append(audio, pcm...)
	}
	if len(audio) >= len(body) {
		t.Fatalf("cancel drained complete unflushed body: got %d bytes, full body %d", len(audio), len(body))
	}
	if !bytes.HasPrefix(body, audio) {
		t.Fatal("cancelled audio is not a strict prefix of the unflushed body")
	}
}

// TestMultiStreamMultipleFlushesPreserveOrder seals each generation at its
// flush: two flushed sentences keep their exact waveforms in order while a
// trailing unflushed sentence is still suppressed to a strict body prefix.
func TestMultiStreamMultipleFlushesPreserveOrder(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	c, ctx := dialMultiTTS(t, s, 16000)
	trailing := strings.Repeat("T ", 480)
	first := strings.Repeat("T ", 480)
	wantA := multiWant(t, first, 16000)
	wantB := multiWant(t, "E", 16000)
	bodyC := multiBody(t, trailing, 16000)
	ttsMessage(t, ctx, c, map[string]any{"context_id": "story", "text": first})
	ttsMessage(t, ctx, c, map[string]any{"context_id": "story", "flush": true})
	ttsMessage(t, ctx, c, map[string]any{"context_id": "story", "text": "E"})
	ttsMessage(t, ctx, c, map[string]any{"context_id": "story", "flush": true})
	ttsMessage(t, ctx, c, map[string]any{"context_id": "story", "text": trailing})
	ttsMessage(t, ctx, c, map[string]any{"context_id": "story", "close_context": true})
	var audio []byte
	for {
		message := readSTT(t, ctx, c)
		if message["contextId"] != "story" {
			t.Fatalf("missing context correlation: %v", message)
		}
		if message["isFinal"] == true {
			if message["audio"] != nil {
				t.Fatalf("final must not carry audio: %v", message)
			}
			break
		}
		encoded, _ := message["audio"].(string)
		pcm, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(pcm) == 0 || len(pcm) > 4096 || len(pcm)%2 != 0 {
			t.Fatal("invalid bounded PCM frame")
		}
		audio = append(audio, pcm...)
	}
	sealed := append(append([]byte{}, wantA...), wantB...)
	if !bytes.HasPrefix(audio, sealed) {
		t.Fatalf("multiple flushes changed waveform or order: got %d bytes, sealed prefix %d", len(audio), len(sealed))
	}
	rest := audio[len(sealed):]
	if len(rest) >= len(bodyC) {
		t.Fatalf("trailing unflushed sentence drained instead of cancelling: got %d bytes, body %d", len(rest), len(bodyC))
	}
	if !bytes.HasPrefix(bodyC, rest) {
		t.Fatal("trailing audio is not a strict prefix of its sentence body")
	}
}

func TestMultiStreamBoundsContexts(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	c, ctx := dialMultiTTS(t, s, 16000)
	for i := 0; i < 5; i++ {
		ttsMessage(t, ctx, c, map[string]any{"text": " ", "context_id": fmt.Sprintf("parked-%d", i)})
	}
	ttsMessage(t, ctx, c, map[string]any{"text": " ", "context_id": "parked-overflow"})
	if message := readSTT(t, ctx, c); message["error"] == nil {
		t.Fatalf("context overflow accepted: %v", message)
	}
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) == -1 {
		t.Fatal("overflow must close the socket")
	}
}

// TestMultiStreamQueueOverloadRejectsDeterministically pins the writer with
// one long generation, then floods meaningful fragments faster than the
// bounded queue can drain. The overflow must surface as an explicit
// too_many_queued_messages error that closes the socket, never as a silent
// stall that delays control behind queued audio. The error is strictly last:
// no audio may arrive after it, so the next read must be the close.
func TestMultiStreamQueueOverloadRejectsDeterministically(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	c, ctx := dialMultiTTS(t, s, 16000)
	ttsMessage(t, ctx, c, map[string]any{"context_id": "busy", "text": strings.Repeat("T ", 240)})
	for range 300 {
		ttsMessage(t, ctx, c, map[string]any{"context_id": "busy", "text": "E"})
	}
	for {
		message := readSTT(t, ctx, c)
		if message["error"] == nil {
			continue
		}
		if message["error"] != "too_many_queued_messages" {
			t.Fatalf("unexpected overload error: %v", message)
		}
		break
	}
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("overload must close the socket with a policy violation: %v", err)
	}
}

// TestMultiStreamTerminalBurstFailsExplicitly is the queued-terminal
// regression: a long unflushed body followed by seventy unknown-context
// closes must never hang the reader behind audio. Every terminal enqueue is
// bounded nonblocking, so the burst fills the 64-job queue and surfaces an
// explicit too_many_queued_messages error instead of wedging the close of
// the real context behind the whole 9,202,560-byte body. Partial audio
// before the error must be a strict prefix of the real body.
func TestMultiStreamTerminalBurstFailsExplicitly(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	c, ctx := dialMultiTTS(t, s, 16000)
	text := strings.Repeat("T ", 480)
	body := multiBody(t, text, 16000)
	ttsMessage(t, ctx, c, map[string]any{"context_id": "same", "text": text})
	for range 70 {
		ttsMessage(t, ctx, c, map[string]any{"context_id": "missing", "close_context": true})
	}
	ttsMessage(t, ctx, c, map[string]any{"context_id": "same", "close_context": true})
	var audio []byte
	for {
		message := readSTT(t, ctx, c)
		if message["error"] != nil {
			if message["error"] != "too_many_queued_messages" {
				t.Fatalf("terminal burst must fail explicitly, got: %v", message)
			}
			break
		}
		if message["isFinal"] == true {
			t.Fatalf("terminal burst must not complete a context: %v", message)
		}
		encoded, _ := message["audio"].(string)
		pcm, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(pcm) == 0 || len(pcm) > 4096 || len(pcm)%2 != 0 {
			t.Fatal("invalid bounded PCM frame")
		}
		audio = append(audio, pcm...)
	}
	if len(audio) >= len(body) {
		t.Fatalf("terminal burst drained the complete unflushed body: got %d bytes, body %d", len(audio), len(body))
	}
	if !bytes.HasPrefix(body, audio) {
		t.Fatal("pre-error audio is not a strict prefix of the unflushed body")
	}
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("terminal-burst overload must close with a policy violation: %v", err)
	}
}

// TestMultiStreamMalformedAfterAudioHasNoTrailingAudio is the
// error-then-close ordering regression for malformed input: a long generation
// pins the writer, then an unknown field fails decoding. The reader must stop
// and join the writer before the terminal error, so already-written frames
// may precede the error but no audio may follow it.
func TestMultiStreamMalformedAfterAudioHasNoTrailingAudio(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	c, ctx := dialMultiTTS(t, s, 16000)
	text := strings.Repeat("T ", 480)
	body := multiBody(t, text, 16000)
	ttsMessage(t, ctx, c, map[string]any{"context_id": "same", "text": text})
	ttsMessage(t, ctx, c, map[string]any{"context_id": "same", "text": "E", "bogus": 1})
	var audio []byte
	for {
		message := readSTT(t, ctx, c)
		if message["error"] == nil {
			if message["isFinal"] == true {
				t.Fatalf("malformed input must not complete a context: %v", message)
			}
			encoded, _ := message["audio"].(string)
			pcm, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil || len(pcm) == 0 || len(pcm) > 4096 || len(pcm)%2 != 0 {
				t.Fatal("invalid bounded PCM frame")
			}
			audio = append(audio, pcm...)
			continue
		}
		if message["error"] != "invalid_input" {
			t.Fatalf("unexpected malformed error: %v", message)
		}
		break
	}
	if len(audio) >= len(body) {
		t.Fatalf("malformed error drained the complete unflushed body: got %d bytes, body %d", len(audio), len(body))
	}
	if !bytes.HasPrefix(body, audio) {
		t.Fatal("pre-error audio is not a strict prefix of the unflushed body")
	}
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("malformed error must close with a policy violation: %v", err)
	}
}
