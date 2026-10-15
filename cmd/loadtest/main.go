// Loadtest measures real speech requests against an explicitly selected origin.
package main

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
}

func main() {
	concurrency := flag.Int("concurrency", 4, "Concurrent clients (1–64)")
	duration := flag.Duration("duration", 10*time.Second, "Request launch window per scenario (1s–60s)")
	timeout := flag.Duration("timeout", 10*time.Second, "Timeout per operation (1s–30s)")
	scenario := flag.String("scenario", "all", "all, tts, batch-stt, or realtime-stt")
	text := flag.String("text", "SOS", "Morse text for every request (maximum 30 seconds of audio)")
	flag.Parse()
	if flag.NArg() != 1 || *concurrency < 1 || *concurrency > 64 || *duration < time.Second || *duration > time.Minute || *timeout < time.Second || *timeout > 30*time.Second {
		fmt.Fprintln(os.Stderr, "Usage: loadtest [flags] http(s)://host; run -help for bounds")
		os.Exit(2)
	}
	scenarios := []string{"tts", "batch-stt", "realtime-stt"}
	if *scenario != "all" {
		if *scenario != "tts" && *scenario != "batch-stt" && *scenario != "realtime-stt" {
			fmt.Fprintln(os.Stderr, "Unknown scenario")
			os.Exit(2)
		}
		scenarios = []string{*scenario}
	}
	t, err := newTarget(flag.Arg(0), *text)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer t.client.CloseIdleConnections()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	output := struct {
		Origin         string    `json:"origin"`
		Started        time.Time `json:"started_at"`
		Concurrency    int       `json:"concurrency"`
		WindowSeconds  float64   `json:"launch_window_seconds"`
		TimeoutSeconds float64   `json:"timeout_seconds"`
		Text           string    `json:"text"`
		AudioSeconds   float64   `json:"audio_seconds"`
		Reports        []report  `json:"scenarios"`
	}{Origin: t.origin, Started: time.Now().UTC(), Concurrency: *concurrency, WindowSeconds: duration.Seconds(), TimeoutSeconds: timeout.Seconds(), Text: t.text, AudioSeconds: float64(len(t.pcm)) / 32000}
	failed := false
	for _, name := range scenarios {
		if ctx.Err() != nil {
			failed = true
			break
		}
		fmt.Fprintf(os.Stderr, "%s: %d clients for %s\n", name, *concurrency, *duration)
		r := t.phase(ctx, name, *concurrency, *duration, *timeout)
		output.Reports = append(output.Reports, r)
		failed = failed || r.Errors > 0 || r.Capacity > 0 || r.Success == 0
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		failed = true
	}
	if failed || ctx.Err() != nil {
		os.Exit(1)
	}
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
	transport.MaxIdleConnsPerHost = 64
	transport.MaxConnsPerHost = 64
	transport.DisableCompression = true
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	u.Scheme = map[string]string{"http": "ws", "https": "wss"}[u.Scheme]
	u.Path = "/v1/speech-to-text/realtime"
	u.RawQuery = "model_id=morse_stt_v1&audio_format=pcm_16000&commit_strategy=manual"
	return &target{origin: strings.TrimRight(origin, "/"), socketURL: u.String(), text: encoder.Text, client: client, pcm: pcm, form: body.Bytes(), formType: form.FormDataContentType(), tts: payload}, nil
}

func (t *target) exercise(ctx context.Context, scenario string) (time.Duration, error) {
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
	conn, response, err := websocket.Dial(ctx, t.socketURL, &websocket.DialOptions{HTTPClient: t.client})
	if err != nil {
		if response != nil && response.StatusCode == 429 {
			return 0, errCapacity
		}
		return 0, errors.New("WebSocket handshake failed")
	}
	defer conn.CloseNow()
	var event struct {
		Type string `json:"message_type"`
		Text string `json:"text"`
	}
	if wsjson.Read(ctx, conn, &event) != nil || event.Type != "session_started" {
		return 0, errors.New("missing realtime session acknowledgement")
	}
	sendCtx, cancel := context.WithCancel(ctx)
	sent := make(chan error, 1)
	go func() {
		for offset := 0; offset < len(t.pcm); offset += 640 {
			end := min(offset+640, len(t.pcm))
			if err := wsjson.Write(sendCtx, conn, map[string]any{"message_type": "input_audio_chunk", "audio_base_64": base64.StdEncoding.EncodeToString(t.pcm[offset:end]), "sample_rate": 16000, "commit": end == len(t.pcm)}); err != nil {
				sent <- err
				return
			}
		}
		sent <- nil
	}()
	defer func() { cancel(); <-sent }()
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

type sample struct {
	elapsed, first time.Duration
	err            error
}

type percentiles struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	Max float64 `json:"max"`
}

type report struct {
	Scenario         string         `json:"scenario"`
	ElapsedSeconds   float64        `json:"elapsed_seconds"`
	Attempts         int            `json:"attempts"`
	Success          int            `json:"success"`
	Capacity         int            `json:"capacity_429"`
	Errors           int            `json:"errors"`
	SuccessPerSecond float64        `json:"success_per_second"`
	Latency          percentiles    `json:"latency_ms"`
	FirstEvent       percentiles    `json:"first_byte_or_partial_ms"`
	ErrorKinds       map[string]int `json:"error_kinds,omitempty"`
}

func (t *target) phase(ctx context.Context, name string, concurrency int, duration, timeout time.Duration) report {
	start := time.Now()
	deadline := start.Add(duration)
	results := make(chan sample, concurrency)
	var workers sync.WaitGroup
	for range concurrency {
		workers.Go(func() {
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
	return summarize(name, time.Since(start), samples)
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
