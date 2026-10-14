package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"

	"github.com/HashNuke/morsecode-audio/internal/audio"
	"github.com/HashNuke/morsecode-audio/internal/morse"
)

type timedChunk struct {
	Audio      string              `json:"audio_base64"`
	Alignment  *characterAlignment `json:"alignment"`
	Normalized *characterAlignment `json:"normalized_alignment"`
}

func writeTimed(w http.ResponseWriter, ctx context.Context, output io.Reader, original, normalized []morse.Character) {
	buffer := make([]byte, 4096)
	n, err := output.Read(buffer)
	if err != nil && err != io.EOF {
		writeError(w, 502, "audio_encoding_failed", "Audio encoding failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if _, err := io.WriteString(w, `{"audio_base64":"`); err != nil {
		return
	}
	encoded := base64.NewEncoder(base64.StdEncoding, w)
	for {
		if ctx.Err() != nil {
			panic(http.ErrAbortHandler)
		}
		if n > 0 {
			if _, err := encoded.Write(buffer[:n]); err != nil {
				return
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(http.ErrAbortHandler)
		}
		n, err = output.Read(buffer)
	}
	if err := encoded.Close(); err != nil {
		return
	}
	tail, err := json.Marshal(struct {
		Alignment  *characterAlignment `json:"alignment"`
		Normalized *characterAlignment `json:"normalized_alignment"`
	}{alignment(original), alignment(normalized)})
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	if _, err := io.WriteString(w, `",`); err != nil {
		return
	}
	_, _ = w.Write(append(tail[1:], '\n'))
}

func writeTimedStream(w http.ResponseWriter, ctx context.Context, output io.Reader, format audio.OutputFormat, original, normalized []morse.Character) {
	packets := audio.NewPackets(output, format)
	data, end, err := packets.Next()
	if err != nil {
		writeError(w, 502, "audio_encoding_failed", "Audio encoding failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	jsonOut := json.NewEncoder(w)
	controller := http.NewResponseController(w)
	for {
		if ctx.Err() != nil {
			panic(http.ErrAbortHandler)
		}
		chunk := timedChunk{base64.StdEncoding.EncodeToString(data), takeAlignment(&original, end), takeAlignment(&normalized, end)}
		if err := jsonOut.Encode(chunk); err != nil {
			return
		}
		if err := controller.Flush(); err != nil {
			return
		}
		data, end, err = packets.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			panic(http.ErrAbortHandler)
		}
	}
}
