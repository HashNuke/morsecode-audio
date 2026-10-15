package api

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"

	"github.com/HashNuke/morsecode-audio/internal/morse"
	"github.com/coder/websocket"
)

const maxRealtimeAudioBytes = 256 << 10

type audioChunk struct {
	Type     string  `json:"message_type"`
	Audio    *string `json:"audio_base_64"`
	Commit   *bool   `json:"commit"`
	Rate     *int    `json:"sample_rate"`
	Previous string  `json:"previous_text"`
}

func (s *Server) realtimeSTT(w http.ResponseWriter, r *http.Request) {
	options, err := parseRealtimeOptions(r.URL.Query())
	if err != nil {
		writeError(w, 422, err.Error(), "Invalid realtime transcription options")
		return
	}
	decoder, err := morse.NewDecoder(options.morseConfig())
	if err != nil {
		writeError(w, 422, "invalid_configuration", "Invalid Morse profile")
		return
	}
	socket := s.sockets.accept(w, r)
	if socket == nil {
		return
	}
	defer socket.close()
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		socket.failure("error", "Cannot initialize session")
		return
	}
	if err := socket.write(map[string]any{"message_type": "session_started", "session_id": hex.EncodeToString(id), "config": options}); err != nil {
		return
	}
	lastText := ""
	lastEmission := 0
	for {
		kind, data, err := socket.read()
		if err != nil {
			return
		}
		var chunk audioChunk
		jsonInput := json.NewDecoder(bytes.NewReader(data))
		jsonInput.DisallowUnknownFields()
		if kind != websocket.MessageText || jsonInput.Decode(&chunk) != nil || jsonInput.Decode(new(any)) != io.EOF || chunk.Type != "input_audio_chunk" || chunk.Audio == nil || chunk.Commit == nil || chunk.Rate == nil || *chunk.Rate != options.Rate || chunk.Previous != "" {
			socket.failure("input_error", "Invalid audio chunk")
			return
		}
		if len(*chunk.Audio) > base64.StdEncoding.EncodedLen(maxRealtimeAudioBytes) {
			socket.failure("chunk_size_exceeded", "Audio chunk exceeds the byte limit")
			return
		}
		pcm, err := base64.StdEncoding.Strict().DecodeString(*chunk.Audio)
		if err != nil || len(pcm) > maxRealtimeAudioBytes {
			socket.failure("input_error", "Invalid base64 audio")
			return
		}
		events, err := decoder.Push(pcm)
		if err != nil {
			realtimeSignalError(socket, err)
			return
		}
		if *chunk.Commit {
			final, flushErr := decoder.Flush()
			if flushErr != nil {
				realtimeSignalError(socket, flushErr)
				return
			}
			events = append(events, final...)
		}
		for _, event := range events {
			typeName := "partial_transcript"
			lastText = event.Text
			if event.Kind == "final" {
				typeName = "committed_transcript"
				lastText = ""
			}
			if err := socket.write(map[string]string{"message_type": typeName, "text": event.Text}); err != nil {
				return
			}
			if event.Kind == "final" && options.Timestamps {
				if err := socket.write(map[string]any{"message_type": "committed_transcript_with_timestamps", "text": event.Text, "language_code": options.Language, "words": transcriptWords(event.Characters, false)}); err != nil {
					return
				}
			}
			lastEmission = decoder.Samples()
		}
		if options.Keepalive > 0 && decoder.SilentSamples() > 0 && decoder.Samples()-lastEmission >= options.Rate*options.Keepalive/1000 {
			if err := socket.write(map[string]string{"message_type": "partial_transcript", "text": lastText}); err != nil {
				return
			}
			lastEmission = decoder.Samples()
		}
	}
}

func realtimeSignalError(socket *speechSocket, err error) {
	kind := "input_error"
	if err.Error() == "audio_too_long" {
		kind = "session_time_limit_exceeded"
	}
	socket.failure(kind, "Audio is not supported Morse or exceeds the session limit")
}
