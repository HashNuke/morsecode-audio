package api_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HashNuke/morsecode-audio/internal/api"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func dialSTT(t *testing.T, server *httptest.Server, query string) (*websocket.Conn, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	c, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/speech-to-text/realtime?"+query, &websocket.DialOptions{HTTPHeader: http.Header{"Xi-Api-Key": []string{"synthetic-realtime"}}})
	if err != nil {
		t.Fatalf("upgrade: %v (response %v)", err, response)
	}
	t.Cleanup(func() { c.CloseNow() })
	message := readSTT(t, ctx, c)
	if message["message_type"] != "session_started" || message["session_id"] == "" {
		t.Fatalf("session: %+v", message)
	}
	return c, ctx
}

func readSTT(t *testing.T, ctx context.Context, c *websocket.Conn) map[string]any {
	t.Helper()
	var message map[string]any
	if err := wsjson.Read(ctx, c, &message); err != nil {
		t.Fatal(err)
	}
	return message
}

func TestRealtimeSTTElevenLabsEventContract(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(s.URL, "http")+"/v1/speech-to-text/realtime?commit_strategy=vad&include_timestamps=true&keepalive_interval_ms=1000", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	started := readSTT(t, ctx, c)
	if started["message_type"] != "session_started" {
		t.Fatal("missing session_started")
	}
	config := started["config"].(map[string]any)
	if config["keepalive_interval_ms"] != float64(1000) {
		t.Fatal("session config must echo keepalive interval")
	}
	sendSTT(t, ctx, c, independentMorse("E E", 16000), 16000, false)
	for _, expected := range []struct{ kind, text string }{{"partial_transcript", "E"}, {"partial_transcript", "E E"}, {"committed_transcript", "E E"}, {"committed_transcript_with_timestamps", "E E"}} {
		event := readSTT(t, ctx, c)
		if event["message_type"] != expected.kind || event["text"] != expected.text {
			t.Fatalf("unexpected transcript/turn event: %v", event)
		}
		if expected.kind == "committed_transcript_with_timestamps" && (event["words"] == nil || event["language_code"] == nil) {
			t.Fatal("missing timestamp event fields")
		}
	}
}

func sendSTT(t *testing.T, ctx context.Context, c *websocket.Conn, data []byte, rate int, commit bool) {
	t.Helper()
	if err := wsjson.Write(ctx, c, map[string]any{"message_type": "input_audio_chunk", "audio_base_64": base64.StdEncoding.EncodeToString(data), "sample_rate": rate, "commit": commit}); err != nil {
		t.Fatal(err)
	}
}

func readFinal(t *testing.T, ctx context.Context, c *websocket.Conn, want string) {
	t.Helper()
	for {
		message := readSTT(t, ctx, c)
		switch message["message_type"] {
		case "partial_transcript":
			if !strings.HasPrefix(want, message["text"].(string)) {
				t.Fatalf("unstable prefix: %+v", message)
			}
		case "committed_transcript":
			if message["text"] != want {
				t.Fatalf("final: %+v", message)
			}
			return
		default:
			t.Fatalf("unexpected message: %+v", message)
		}
	}
}

func TestRealtimeManualRepeatedTurnsAndTimestampOrigin(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	c, ctx := dialSTT(t, s, "model_id=scribe_v2_realtime&commit_strategy=manual&include_timestamps=true")
	// Empty commits never fabricate a final or contaminate the following turn.
	sendSTT(t, ctx, c, nil, 16000, true)
	var audioOffset float64
	for _, text := range []string{"E", "T"} {
		pcm := independentMorse(text, 16000)
		for off := 0; off < len(pcm); {
			end := min(off+317, len(pcm))
			sendSTT(t, ctx, c, pcm[off:end], 16000, false)
			off = end
		}
		sendSTT(t, ctx, c, nil, 16000, true)
		readFinal(t, ctx, c, text)
		timed := readSTT(t, ctx, c)
		if timed["message_type"] != "committed_transcript_with_timestamps" || timed["text"] != text {
			t.Fatalf("timed: %+v", timed)
		}
		words := timed["words"].([]any)
		word := words[0].(map[string]any)
		if math.Abs(word["start"].(float64)-audioOffset) > .011 || word["end"].(float64) <= word["start"].(float64) {
			t.Fatalf("timestamp reset: %+v", word)
		}
		audioOffset += float64(len(pcm)) / 32000
		sendSTT(t, ctx, c, nil, 16000, true)
	}
}

func TestRealtimeLongSegmentUsesIncrementalChunks(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	c, ctx := dialSTT(t, s, "commit_strategy=manual&audio_format=pcm_8000")
	text := strings.Repeat("E", 500)
	pcm := independentMorse(text, 8000)
	written := make(chan error, 1)
	go func() {
		for off := 0; off < len(pcm); {
			end := min(off+8191, len(pcm))
			err := wsjson.Write(ctx, c, map[string]any{"message_type": "input_audio_chunk", "audio_base_64": base64.StdEncoding.EncodeToString(pcm[off:end]), "sample_rate": 8000, "commit": end == len(pcm)})
			if err != nil {
				written <- err
				return
			}
			off = end
		}
		written <- nil
	}()
	readFinal(t, ctx, c, text)
	if err := <-written; err != nil {
		t.Fatal(err)
	}
}

func TestRealtimeAutomaticCommitAcrossAllInputFormats(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	for _, format := range []string{"pcm_8000", "pcm_16000", "pcm_24000", "pcm_48000"} {
		t.Run(format, func(t *testing.T) {
			var rate int
			fmt.Sscanf(strings.SplitN(format, "_", 2)[1], "%d", &rate)
			c, ctx := dialSTT(t, s, "model_id=morse_stt_v1&commit_strategy=vad&audio_format="+format)
			for range 2 {
				pcm := independentMorse("E T", rate)
				for off := 0; off < len(pcm); {
					end := min(off+991, len(pcm))
					sendSTT(t, ctx, c, pcm[off:end], rate, false)
					off = end
				}
				readFinal(t, ctx, c, "E T")
			}
		})
	}
}

func TestRealtimeIgnoresAPIKey(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, key := range []string{"", "arbitrary-key"} {
		headers := http.Header{"Origin": []string{"https://example.com"}}
		if key != "" {
			headers.Set("xi-api-key", key)
		}
		c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(s.URL, "http")+"/v1/speech-to-text/realtime", &websocket.DialOptions{HTTPHeader: headers})
		if err != nil {
			t.Fatal("optional key rejected", err)
		}
		t.Cleanup(func() { c.CloseNow() })
		if reply := readSTT(t, ctx, c); reply["message_type"] != "session_started" {
			t.Fatal(reply)
		}
		sendSTT(t, ctx, c, independentMorse("E", 16000), 16000, true)
		readFinal(t, ctx, c, "E")
		c.CloseNow()
	}
}

func TestRealtimeMalformedInput(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	for _, message := range []string{
		`{"message_type":"input_audio_chunk","audio_base_64":"!!!","commit":false,"sample_rate":16000}`,
		`{"message_type":"input_audio_chunk","audio_base_64":"AQ==","commit":true,"sample_rate":16000}`,
		`{"message_type":"input_audio_chunk","audio_base_64":"","commit":true,"sample_rate":48000}`,
		`{"message_type":"input_audio_chunk"}`,
		`{"message_type":"wrong"}`,
		`not-json`,
	} {
		c, ctx := dialSTT(t, s, "commit_strategy=manual")
		if err := c.Write(ctx, websocket.MessageText, []byte(message)); err != nil {
			t.Fatal(err)
		}
		reply := readSTT(t, ctx, c)
		if reply["message_type"] != "input_error" || reply["error"] == "" {
			t.Fatalf("bad input: %+v", reply)
		}
	}
}

func TestRealtimeSilenceKeepaliveHasNoFinal(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	c, ctx := dialSTT(t, s, "commit_strategy=vad&keepalive_interval_ms=500")
	sendSTT(t, ctx, c, make([]byte, 16000), 16000, false)
	reply := readSTT(t, ctx, c)
	if reply["message_type"] != "partial_transcript" || reply["text"] != "" {
		t.Fatalf("silent keepalive: %+v", reply)
	}
	sendSTT(t, ctx, c, nil, 16000, true)
	// A terminal error acts as an ordered barrier: no empty committed message.
	if err := c.Write(ctx, websocket.MessageText, []byte(`{"message_type":"bad"}`)); err != nil {
		t.Fatal(err)
	}
	if reply := readSTT(t, ctx, c); reply["message_type"] != "input_error" {
		t.Fatalf("fabricated commit: %+v", reply)
	}
}

func TestRealtimeSeparateSocketsAndCustomSilence(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	a, ctx := dialSTT(t, s, "commit_strategy=vad&vad_silence_threshold_secs=1.5")
	b, other := dialSTT(t, s, "commit_strategy=manual")
	sendSTT(t, ctx, a, independentMorse("E", 16000), 16000, false)
	reply := readSTT(t, ctx, a)
	if reply["message_type"] != "partial_transcript" || reply["text"] != "E" {
		t.Fatal(reply)
	}
	sendSTT(t, other, b, independentMorse("T", 16000), 16000, true)
	readFinal(t, other, b, "T")
	// New E begins before 1.5 seconds of silence: the earlier segment must
	// remain uncommitted; it becomes one transcript with an ordinary word gap.
	sendSTT(t, ctx, a, independentMorse("E", 16000), 16000, false)
	sendSTT(t, ctx, a, make([]byte, 16000*2), 16000, false)
	readFinal(t, ctx, a, "E E")
}

func TestRealtimeKeepalivePreservesUncommittedPartial(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	c, ctx := dialSTT(t, s, "commit_strategy=manual&keepalive_interval_ms=500")
	sendSTT(t, ctx, c, independentMorse("E", 16000), 16000, false)
	if reply := readSTT(t, ctx, c); reply["message_type"] != "partial_transcript" || reply["text"] != "E" {
		t.Fatal(reply)
	}
	sendSTT(t, ctx, c, make([]byte, 16000), 16000, false)
	if reply := readSTT(t, ctx, c); reply["message_type"] != "partial_transcript" || reply["text"] != "E" {
		t.Fatal(reply)
	}
	sendSTT(t, ctx, c, nil, 16000, true)
	readFinal(t, ctx, c, "E")
}

func TestRealtimeChunkAndFrameLimits(t *testing.T) {
	s := httptest.NewServer(api.New())
	defer s.Close()
	c, ctx := dialSTT(t, s, "")
	if err := wsjson.Write(ctx, c, map[string]any{"message_type": "input_audio_chunk", "audio_base_64": strings.Repeat("A", base64.StdEncoding.EncodedLen(256<<10)+4), "sample_rate": 16000, "commit": false}); err != nil {
		t.Fatal(err)
	}
	if reply := readSTT(t, ctx, c); reply["message_type"] != "chunk_size_exceeded" {
		t.Fatal(reply)
	}
	c, ctx = dialSTT(t, s, "")
	_ = c.Write(ctx, websocket.MessageText, bytes.Repeat([]byte("x"), 600<<10))
	_, _, err := c.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusMessageTooBig {
		t.Fatalf("oversized frame: %v", err)
	}
}

// Independent square-wave E/T fixtures; timing is declared here rather than
// generated by the production alphabet/encoder.
func independentMorse(text string, rate int) []byte {
	var pcm []byte
	appendRun := func(tone bool, units int) {
		for i := 0; i < rate*60/1000*units; i++ {
			var value int16
			if tone {
				value = 3000
				if (i*1400/rate)%2 != 0 {
					value = -3000
				}
			}
			pcm = binary.LittleEndian.AppendUint16(pcm, uint16(value))
		}
	}
	for i, char := range text {
		if char == ' ' {
			appendRun(false, 7)
			continue
		}
		if i > 0 && text[i-1] != ' ' {
			appendRun(false, 3)
		}
		units := 1
		if char == 'T' {
			units = 3
		}
		appendRun(true, units)
	}
	appendRun(false, 14)
	return pcm
}
