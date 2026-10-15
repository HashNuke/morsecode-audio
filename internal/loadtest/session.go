package loadtest

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func (t *target) openRealtime(ctx context.Context, name string) (*websocket.Conn, error) {
	u, _ := url.Parse(t.socketURL)
	if name == "realtime-tts" {
		u.Path = "/v1/text-to-speech/morse/stream-input"
		u.RawQuery = "model_id=morse_v1&output_format=pcm_16000&auto_mode=true"
	}
	conn, response, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{HTTPClient: t.client})
	if err != nil {
		if response != nil && response.StatusCode == 429 {
			return nil, errCapacity
		}
		return nil, errors.New("WebSocket handshake failed")
	}
	if name == "realtime-tts" {
		err = wsjson.Write(ctx, conn, map[string]string{"text": " "})
	} else {
		var event struct {
			Type string `json:"message_type"`
		}
		err = wsjson.Read(ctx, conn, &event)
		if err == nil && event.Type != "session_started" {
			err = errors.New("missing realtime session acknowledgement")
		}
	}
	if err != nil {
		_ = conn.CloseNow()
		return nil, errors.New("realtime initialization failed")
	}
	return conn, nil
}

// Each worker opens exactly one session. Failure ends that worker, so replacing
// failed connections cannot disguise an inability to sustain concurrency.
func (t *target) holdSession(ctx context.Context, name string, duration, timeout time.Duration) (out sample) {
	began := time.Now()
	defer func() { out.elapsed = time.Since(began) }()
	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	conn, err := t.openRealtime(connectCtx, name)
	cancel()
	if err != nil {
		out.err = err
		return
	}
	defer conn.CloseNow()
	release := t.trackSession()
	defer release()
	opened := time.Now()
	defer func() { out.held = time.Since(opened) }()
	deadline := opened.Add(duration)
	for {
		turnCtx, stop := context.WithTimeout(ctx, timeout)
		turnStart := time.Now()
		var first time.Duration
		if name == "realtime-tts" {
			first, err = t.ttsTurn(turnCtx, conn, false)
		} else {
			first, err = t.sttTurn(turnCtx, conn)
		}
		stop()
		if out.first == 0 && first > 0 {
			out.first = turnStart.Sub(began) + first
		}
		if err != nil {
			out.err = err
			return
		}
		t.turns.Add(1)
		if !time.Now().Before(deadline) {
			break
		}
	}
	if name == "realtime-tts" {
		finalCtx, stop := context.WithTimeout(ctx, timeout)
		defer stop()
		var final struct {
			Final bool    `json:"isFinal"`
			Audio *string `json:"audio"`
		}
		if wsjson.Write(finalCtx, conn, map[string]string{"text": ""}) != nil || wsjson.Read(finalCtx, conn, &final) != nil || !final.Final || (final.Audio != nil && *final.Audio != "") {
			out.err = errors.New("missing realtime TTS final completion")
		}
	}
	return
}
