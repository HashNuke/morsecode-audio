// Smoke checks the hosted Morse speech boundary at an explicitly selected origin.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func main() {
	origin := flag.String("base-url", "", "Required HTTP(S) origin of the Morse server")
	flag.Parse()
	if err := run(*origin); err != nil {
		fmt.Fprintln(os.Stderr, "Morse smoke failed:", err)
		os.Exit(1)
	}
	fmt.Println("Morse smoke passed: readiness, public discovery, HTTP and realtime TTS, batch and realtime STT.")
}

func run(origin string) error {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("base-url must be an explicit HTTP(S) origin")
	}
	origin = strings.TrimRight(origin, "/")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request := func(method, path, contentType string, body io.Reader, status int) ([]byte, error) {
		r, err := http.NewRequestWithContext(ctx, method, origin+path, body)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid request", path)
		}
		r.Header.Set("Content-Type", contentType)
		response, err := client.Do(r)
		if err != nil {
			return nil, fmt.Errorf("%s: connection failed", path)
		}
		defer response.Body.Close()
		if response.StatusCode != status {
			return nil, fmt.Errorf("%s: expected %d, received %d", path, status, response.StatusCode)
		}
		if strings.Contains(path, "/stream?") && (response.Header.Get("Content-Type") != "audio/pcm" || response.ContentLength != -1) {
			return nil, errors.New("TTS did not return streaming PCM")
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if err != nil || len(data) == 1<<20 {
			return nil, fmt.Errorf("%s: invalid or oversized response", path)
		}
		return data, nil
	}
	for _, check := range []struct {
		path   string
		status int
	}{{"/readyz", 200}, {"/v1/models", 200}, {"/v1/voices", 200}} {
		if _, err := request("GET", check.path, "", nil, check.status); err != nil {
			return err
		}
	}
	pcm, err := request("POST", "/v1/text-to-speech/morse/stream?output_format=pcm_16000", "application/json",
		strings.NewReader(`{"text":"E T","model_id":"morse_v1"}`), 200)
	if err != nil {
		return err
	}
	if len(pcm) != 16000*60/1000*(1+7+3+14)*2 {
		return errors.New("TTS returned incorrect Morse duration")
	}
	if err := checkRealtimeTTS(ctx, client, *u, pcm); err != nil {
		return err
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, _ := form.CreateFormFile("file", "morse.pcm")
	_, _ = file.Write(pcm)
	_ = form.WriteField("model_id", "morse_stt_v1")
	_ = form.WriteField("file_format", "pcm_s16le_16")
	_ = form.Close()
	transcript, err := request("POST", "/v1/speech-to-text", form.FormDataContentType(), &body, 200)
	if err != nil {
		return err
	}
	var batch struct{ Text string }
	if json.Unmarshal(transcript, &batch) != nil || batch.Text != "E T" {
		return errors.New("batch recognition did not reproduce Morse text")
	}
	u.Scheme = map[string]string{"http": "ws", "https": "wss"}[u.Scheme]
	u.Path = "/v1/speech-to-text/realtime"
	u.RawQuery = "model_id=morse_stt_v1&audio_format=pcm_16000&commit_strategy=vad&vad_silence_threshold_secs=0.84"
	conn, _, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		return errors.New("realtime STT connection failed")
	}
	defer conn.CloseNow()
	var event struct {
		Type string `json:"message_type"`
		Text string `json:"text"`
	}
	if wsjson.Read(ctx, conn, &event) != nil || event.Type != "session_started" {
		return errors.New("realtime STT did not acknowledge readiness")
	}
	for offset := 0; offset < len(pcm); offset += 640 {
		chunk := pcm[offset:min(offset+640, len(pcm))]
		if err := wsjson.Write(ctx, conn, map[string]any{"message_type": "input_audio_chunk", "audio_base_64": base64.StdEncoding.EncodeToString(chunk), "sample_rate": 16000, "commit": false}); err != nil {
			return errors.New("realtime STT input failed")
		}
	}
	partial := false
	for {
		if wsjson.Read(ctx, conn, &event) != nil {
			return errors.New("realtime STT transcript missing")
		}
		switch event.Type {
		case "partial_transcript":
			partial = partial || event.Text != ""
		case "committed_transcript":
			if event.Text != "E T" || !partial {
				return errors.New("realtime STT partial/final mismatch")
			}
			return nil
		default:
			return errors.New("unexpected realtime STT event")
		}
	}
}

func checkRealtimeTTS(ctx context.Context, client *http.Client, u url.URL, expected []byte) error {
	u.Scheme = map[string]string{"http": "ws", "https": "wss"}[u.Scheme]
	u.Path = "/v1/text-to-speech/morse/stream-input"
	u.RawQuery = "model_id=morse_v1&output_format=pcm_16000&auto_mode=true"
	conn, _, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		return errors.New("realtime TTS connection failed")
	}
	defer conn.CloseNow()
	for _, text := range []string{" ", "E "} {
		if wsjson.Write(ctx, conn, map[string]string{"text": text}) != nil {
			return errors.New("realtime TTS input failed")
		}
	}
	var actual []byte
	sentRest := false
	for {
		var event struct {
			Audio string `json:"audio"`
			Final bool   `json:"isFinal"`
		}
		if wsjson.Read(ctx, conn, &event) != nil {
			return errors.New("realtime TTS output missing")
		}
		if event.Final {
			if !bytes.Equal(actual, expected) {
				return errors.New("realtime TTS PCM mismatch")
			}
			return nil
		}
		pcm, err := base64.StdEncoding.DecodeString(event.Audio)
		if err != nil || len(pcm) == 0 || len(actual)+len(pcm) > len(expected) {
			return errors.New("invalid realtime TTS audio")
		}
		actual = append(actual, pcm...)
		if !sentRest {
			for _, text := range []string{"T", ""} {
				if wsjson.Write(ctx, conn, map[string]string{"text": text}) != nil {
					return errors.New("realtime TTS continuation failed")
				}
			}
			sentRest = true
		}
	}
}
