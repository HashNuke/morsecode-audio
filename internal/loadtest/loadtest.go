// Loadtest measures real speech requests against an explicitly selected origin.
package loadtest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/HashNuke/morsecode-audio/internal/morse"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

var errCapacity = errors.New("HTTP 429 capacity rejection")

type target struct {
	origin, socketURL, text, formType string
	client                            *http.Client
	pcm, form, tts                    []byte
	paced                             bool
	reconnect                         bool
	turns                             atomic.Int64
	active, peak                      atomic.Int64
}

// Run executes a bounded load test and returns a process exit status.
func Run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("loadtest", flag.ContinueOnError)
	flags.SetOutput(stderr)
	selected := "all"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "tts", "stt", "batch-stt", "realtime-stt", "realtime-tts", "all":
			selected, args = args[0], args[1:]
		}
	}
	concurrency := flags.Int("concurrency", 4, "Concurrent clients (1–128)")
	duration := flags.Duration("duration", 10*time.Second, "Session hold duration / HTTP launch window (1s–10m)")
	timeout := flags.Duration("timeout", 10*time.Second, "Timeout per handshake or turn (1s–2m)")
	scenario := flags.String("scenario", selected, "tts, stt, realtime-stt, realtime-tts, or all")
	paced := flags.Bool("pace", true, "Pace realtime audio at audio speed and text at one character per 100 ms")
	reconnect := flags.Bool("reconnect", false, "Open fresh realtime connections per turn instead of holding sessions")
	text := flags.String("text", "SOS", "Morse text per turn (maximum 30 seconds of audio)")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: morsecode-audio loadtest <service> [flags] http(s)://host")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 1 || *concurrency < 1 || *concurrency > 128 || *duration < time.Second || *duration > 10*time.Minute || *timeout < time.Second || *timeout > 2*time.Minute {
		flags.Usage()
		return 2
	}
	if *scenario == "stt" {
		*scenario = "batch-stt"
	}
	scenarios := []string{"tts", "batch-stt", "realtime-stt", "realtime-tts"}
	if *scenario != "all" {
		if *scenario != "tts" && *scenario != "batch-stt" && *scenario != "realtime-stt" && *scenario != "realtime-tts" {
			fmt.Fprintln(stderr, "Unknown service")
			return 2
		}
		scenarios = []string{*scenario}
	}
	t, err := newTarget(flags.Arg(0), *text)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	defer t.client.CloseIdleConnections()
	t.paced, t.reconnect = *paced, *reconnect
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	output := struct {
		Origin          string    `json:"origin"`
		Started         time.Time `json:"started_at"`
		Concurrency     int       `json:"concurrency"`
		DurationSeconds float64   `json:"duration_seconds"`
		TimeoutSeconds  float64   `json:"timeout_seconds"`
		Text            string    `json:"text"`
		AudioSeconds    float64   `json:"audio_seconds"`
		Reports         []report  `json:"scenarios"`
		Paced           bool      `json:"paced"`
		Reconnect       bool      `json:"reconnect_per_turn"`
	}{Origin: t.origin, Started: time.Now().UTC(), Concurrency: *concurrency, DurationSeconds: duration.Seconds(), TimeoutSeconds: timeout.Seconds(), Text: t.text, AudioSeconds: float64(len(t.pcm)) / 32000, Paced: *paced, Reconnect: *reconnect}
	failed := false
	for _, name := range scenarios {
		if ctx.Err() != nil {
			failed = true
			break
		}
		fmt.Fprintf(stderr, "%s: %d clients for %s\n", name, *concurrency, *duration)
		r := t.phase(ctx, name, *concurrency, *duration, *timeout)
		output.Reports = append(output.Reports, r)
		failed = failed || r.Errors > 0 || r.Capacity > 0 || r.Success == 0
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(stderr, err)
		failed = true
	}
	if failed || ctx.Err() != nil {
		return 1
	}
	return 0
}

func newTarget(origin, text string) (*target, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("target must be an explicit HTTP(S) origin without credentials, path, query or fragment")
	}
	cfg := morse.DefaultConfig()
	cfg.MaxDurationSeconds = 30
	encoder, err := morse.NewEncoder(text, cfg)
	if err != nil {
		return nil, fmt.Errorf("invalid load-test text: %w", err)
	}
	pcm, err := io.ReadAll(encoder)
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, _ := form.CreateFormFile("file", "loadtest.pcm")
	_, _ = file.Write(pcm)
	_ = form.WriteField("model_id", "morse_stt_v1")
	_ = form.WriteField("file_format", "pcm_s16le_16")
	_ = form.Close()
	payload, _ := json.Marshal(map[string]string{"text": encoder.Text, "model_id": "morse_v1"})
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = 128
	transport.MaxConnsPerHost = 128
	transport.DisableCompression = true
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	u.Scheme = map[string]string{"http": "ws", "https": "wss"}[u.Scheme]
	u.Path = "/v1/speech-to-text/realtime"
	u.RawQuery = "model_id=morse_stt_v1&audio_format=pcm_16000&commit_strategy=manual"
	return &target{origin: strings.TrimRight(origin, "/"), socketURL: u.String(), text: encoder.Text, client: client, pcm: pcm, form: body.Bytes(), formType: form.FormDataContentType(), tts: payload}, nil
}

func (t *target) exercise(ctx context.Context, scenario string) (time.Duration, error) {
	if scenario == "realtime-tts" {
		return t.realtimeTTS(ctx)
	}
	if scenario == "realtime-stt" {
		return t.realtime(ctx)
	}
	path, contentType, payload := "/v1/text-to-speech/morse/stream?output_format=pcm_16000", "application/json", t.tts
	if scenario == "batch-stt" {
		path, contentType, payload = "/v1/speech-to-text", t.formType, t.form
	}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, "POST", t.origin+path, bytes.NewReader(payload))
	if err != nil {
		return 0, errors.New("invalid request")
	}
	req.Header.Set("Content-Type", contentType)
	response, err := t.client.Do(req)
	if err != nil {
		return 0, errors.New("HTTP transport failure or timeout")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		if response.StatusCode == 429 {
			return 0, errCapacity
		}
		return 0, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	if scenario == "batch-stt" {
		data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		var transcript struct{ Text string }
		if err != nil || json.Unmarshal(data, &transcript) != nil || transcript.Text != t.text {
			return 0, errors.New("incorrect batch transcript")
		}
		return 0, nil
	}
	if response.Header.Get("Content-Type") != "audio/pcm" {
		return 0, errors.New("incorrect TTS content type")
	}
	// Consume incrementally and verify all PCM without retaining response audio.
	var first [1]byte
	if _, err := io.ReadFull(response.Body, first[:]); err != nil {
		return 0, errors.New("missing TTS audio")
	}
	firstByte := time.Since(start)
	hash := sha256.New()
	_, _ = hash.Write(first[:])
	n, err := io.Copy(hash, io.LimitReader(response.Body, int64(len(t.pcm))))
	expected := sha256.Sum256(t.pcm)
	if err != nil || n+1 != int64(len(t.pcm)) || !bytes.Equal(hash.Sum(nil), expected[:]) {
		return firstByte, errors.New("incorrect or incomplete TTS PCM")
	}
	return firstByte, nil
}

func (t *target) realtime(ctx context.Context) (time.Duration, error) {
	start := time.Now()
	conn, err := t.openRealtime(ctx, "realtime-stt")
	if err != nil {
		return 0, err
	}
	defer conn.CloseNow()
	release := t.trackSession()
	defer release()
	setup := time.Since(start)
	first, err := t.sttTurn(ctx, conn)
	if err == nil {
		t.turns.Add(1)
	}
	return setup + first, err
}

func (t *target) sttTurn(ctx context.Context, conn *websocket.Conn) (firstTime time.Duration, resultErr error) {
	start := time.Now()
	var event struct {
		Type string `json:"message_type"`
		Text string `json:"text"`
	}
	sendCtx, cancel := context.WithCancel(ctx)
	sent := make(chan error, 1)
	go func() {
		began := time.Now()
		for offset := 0; offset < len(t.pcm); offset += 640 {
			end := min(offset+640, len(t.pcm))
			if t.paced {
				if err := waitUntil(sendCtx, began.Add(time.Duration(end)*time.Second/32000)); err != nil {
					sent <- err
					return
				}
			}
			if err := wsjson.Write(sendCtx, conn, map[string]any{"message_type": "input_audio_chunk", "audio_base_64": base64.StdEncoding.EncodeToString(t.pcm[offset:end]), "sample_rate": 16000, "commit": end == len(t.pcm)}); err != nil {
				sent <- err
				return
			}
		}
		sent <- nil
	}()
	defer func() {
		// A completed response can arrive before the writer goroutine returns.
		// Join it before cancellation so a successful turn does not close a reused socket.
		if resultErr == nil {
			if <-sent != nil {
				resultErr = errors.New("realtime input failed")
			}
			cancel()
		} else {
			cancel()
			<-sent
		}
	}()
	var firstPartial time.Duration
	for {
		if err := wsjson.Read(ctx, conn, &event); err != nil {
			return firstPartial, errors.New("realtime read failure or timeout")
		}
		switch event.Type {
		case "partial_transcript":
			if event.Text != "" && firstPartial == 0 {
				firstPartial = time.Since(start)
			}
			if !strings.HasPrefix(t.text, event.Text) {
				return firstPartial, errors.New("incorrect realtime partial")
			}
		case "committed_transcript":
			if event.Text != t.text || firstPartial == 0 {
				return firstPartial, errors.New("incorrect realtime final or missing partial")
			}
			return firstPartial, nil
		default:
			return firstPartial, errors.New("unexpected realtime event")
		}
	}
}

func (t *target) realtimeTTS(ctx context.Context) (time.Duration, error) {
	start := time.Now()
	conn, err := t.openRealtime(ctx, "realtime-tts")
	if err != nil {
		return 0, err
	}
	defer conn.CloseNow()
	release := t.trackSession()
	defer release()
	setup := time.Since(start)
	first, err := t.ttsTurn(ctx, conn, true)
	if err == nil {
		t.turns.Add(1)
	}
	return setup + first, err
}

func (t *target) ttsTurn(ctx context.Context, conn *websocket.Conn, final bool) (firstTime time.Duration, resultErr error) {
	start := time.Now()
	sendCtx, cancel := context.WithCancel(ctx)
	sent := make(chan error, 1)
	go func() {
		began := time.Now()
		for index, char := range t.text {
			if t.paced {
				if err := waitUntil(sendCtx, began.Add(time.Duration(index)*100*time.Millisecond)); err != nil {
					sent <- err
					return
				}
			}
			if err := wsjson.Write(sendCtx, conn, map[string]any{"text": string(char), "flush": !final && index == len(t.text)-1}); err != nil {
				sent <- err
				return
			}
		}
		if final {
			sent <- wsjson.Write(sendCtx, conn, map[string]string{"text": ""})
		} else {
			sent <- nil
		}
	}()
	defer func() {
		// A completed response can arrive before the writer goroutine returns.
		// Join it before cancellation so a successful turn does not close a reused socket.
		if resultErr == nil {
			if <-sent != nil {
				resultErr = errors.New("realtime input failed")
			}
			cancel()
		} else {
			cancel()
			<-sent
		}
	}()
	var first time.Duration
	hash := sha256.New()
	count := 0
	for {
		var event struct {
			Audio *string `json:"audio"`
			Final bool    `json:"isFinal"`
			Error string  `json:"error"`
		}
		if wsjson.Read(ctx, conn, &event) != nil {
			return first, errors.New("realtime TTS read failure or timeout")
		}
		if event.Error != "" {
			return first, errors.New("realtime TTS error event")
		}
		if event.Final {
			expected := sha256.Sum256(t.pcm)
			if count != len(t.pcm) || !bytes.Equal(hash.Sum(nil), expected[:]) {
				return first, errors.New("incorrect realtime TTS PCM")
			}
			return first, nil
		}
		if event.Audio == nil {
			return first, errors.New("missing realtime TTS audio")
		}
		pcm, err := base64.StdEncoding.Strict().DecodeString(*event.Audio)
		if err != nil || len(pcm) == 0 || len(pcm)%2 != 0 || count+len(pcm) > len(t.pcm) {
			return first, errors.New("invalid realtime TTS audio")
		}
		if first == 0 {
			first = time.Since(start)
		}
		_, _ = hash.Write(pcm)
		count += len(pcm)
		if !final && count == len(t.pcm) {
			expected := sha256.Sum256(t.pcm)
			if !bytes.Equal(hash.Sum(nil), expected[:]) {
				return first, errors.New("incorrect realtime TTS PCM")
			}
			return first, nil
		}
	}
}

func waitUntil(ctx context.Context, deadline time.Time) error {
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case <-timer.C:
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *target) trackSession() func() {
	active := t.active.Add(1)
	for peak := t.peak.Load(); active > peak; peak = t.peak.Load() {
		if t.peak.CompareAndSwap(peak, active) {
			break
		}
	}
	return func() { t.active.Add(-1) }
}

type sample struct {
	elapsed, first, held time.Duration
	err                  error
}

type percentiles struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	Max float64 `json:"max"`
}

type report struct {
	Scenario               string         `json:"scenario"`
	ElapsedSeconds         float64        `json:"elapsed_seconds"`
	Attempts               int            `json:"attempts"`
	Success                int            `json:"success"`
	Capacity               int            `json:"capacity_429"`
	Errors                 int            `json:"errors"`
	SuccessPerSecond       float64        `json:"success_per_second"`
	Latency                percentiles    `json:"latency_ms"`
	FirstEvent             percentiles    `json:"first_byte_or_partial_ms"`
	ErrorKinds             map[string]int `json:"error_kinds,omitempty"`
	PeakSessions           int64          `json:"peak_realtime_sessions"`
	CompletedTurns         int64          `json:"completed_turns"`
	ShortestSessionSeconds float64        `json:"shortest_successful_session_seconds,omitempty"`
}

func (t *target) phase(ctx context.Context, name string, concurrency int, duration, timeout time.Duration) report {
	t.peak.Store(0)
	t.turns.Store(0)
	start := time.Now()
	deadline := start.Add(duration)
	results := make(chan sample, concurrency)
	var workers sync.WaitGroup
	for range concurrency {
		workers.Go(func() {
			if !t.reconnect && (name == "realtime-stt" || name == "realtime-tts") {
				if ctx.Err() == nil {
					results <- t.holdSession(ctx, name, duration, timeout)
				}
				return
			}
			for ctx.Err() == nil && time.Now().Before(deadline) {
				operationCtx, cancel := context.WithTimeout(ctx, timeout)
				began := time.Now()
				first, err := t.exercise(operationCtx, name)
				cancel()
				results <- sample{elapsed: time.Since(began), first: first, err: err}
			}
		})
	}
	go func() { workers.Wait(); close(results) }()
	var samples []sample
	for result := range results {
		samples = append(samples, result)
	}
	r := summarize(name, time.Since(start), samples)
	r.PeakSessions = t.peak.Load()
	r.CompletedTurns = t.turns.Load()
	return r
}

func summarize(name string, elapsed time.Duration, samples []sample) report {
	r := report{Scenario: name, ElapsedSeconds: elapsed.Seconds(), Attempts: len(samples), ErrorKinds: make(map[string]int)}
	var latencies, first []time.Duration
	for _, s := range samples {
		if errors.Is(s.err, errCapacity) {
			r.Capacity++
			continue
		}
		if s.err != nil {
			r.Errors++
			r.ErrorKinds[s.err.Error()]++
			continue
		}
		r.Success++
		if s.held > 0 && (r.ShortestSessionSeconds == 0 || s.held.Seconds() < r.ShortestSessionSeconds) {
			r.ShortestSessionSeconds = s.held.Seconds()
		}
		latencies = append(latencies, s.elapsed)
		if s.first > 0 {
			first = append(first, s.first)
		}
	}
	if elapsed > 0 {
		r.SuccessPerSecond = float64(r.Success) / elapsed.Seconds()
	}
	r.Latency, r.FirstEvent = distribution(latencies), distribution(first)
	return r
}

func distribution(values []time.Duration) percentiles {
	if len(values) == 0 {
		return percentiles{}
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	value := func(p float64) float64 {
		return float64(values[int(math.Ceil(p*float64(len(values))))-1]) / float64(time.Millisecond)
	}
	return percentiles{P50: value(.5), P95: value(.95), P99: value(.99), Max: value(1)}
}
