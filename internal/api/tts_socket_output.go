package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"math"

	"github.com/HashNuke/morsecode-audio/internal/audio"
	"github.com/coder/websocket"
)

func (s *Server) writeTTSJobs(ctx context.Context, cancel context.CancelFunc, socket *speechSocket, options ttsSocketOptions, jobs <-chan *ttsGeneration) {
	for {
		select {
		case <-ctx.Done():
			return
		case g, ok := <-jobs:
			if !ok {
				if ctx.Err() == nil {
					if socket.write(map[string]any{"isFinal": true}) == nil {
						_ = socket.conn.Close(websocket.StatusNormalClosure, "")
					}
				}
				return
			}
			err := s.writeTTSGeneration(ctx, socket, options, g)
			if err != nil {
				if ctx.Err() == nil {
					cancel()
					ttsSocketFailure(socket, err.Error())
				}
				return
			}
		}
	}
}

func (s *Server) writeTTSGeneration(ctx context.Context, socket *speechSocket, options ttsSocketOptions, g *ttsGeneration) error {
	defer g.source.Close()
	select {
	case s.ttsSlots <- struct{}{}:
		defer func() { <-s.ttsSlots }()
	default:
		return errors.New("too_many_concurrent_requests")
	}
	output, err := audio.EncodeLive(ctx, g.source, options.format)
	if err != nil {
		return errors.New("codec_unavailable")
	}
	defer output.Close()
	packets := audio.NewLivePackets(output, options.format)
	var pending bytes.Buffer
	lastEnd := 0.0
	for {
		packet, end, err := packets.Next()
		if err != nil && err != io.EOF {
			return errors.New("audio_encoding_failed")
		}
		pending.Write(packet)
		if pending.Len() > 512<<10 {
			return errors.New("output_chunk_too_large")
		}
		final := err == io.EOF
		through := end
		if final {
			through = math.Inf(1)
		}
		original, normalized, ready := g.takeAlignment(through, lastEnd, final)
		if ready && (pending.Len() > 0 || original != nil || normalized != nil) {
			message := map[string]any{"audio": base64.StdEncoding.EncodeToString(pending.Bytes())}
			if original != nil {
				message["alignment"] = original
			}
			if normalized != nil {
				message["normalizedAlignment"] = normalized
			}
			if err := socket.write(message); err != nil {
				return errors.New("output_disconnected")
			}
			pending.Reset()
			lastEnd = end
		}
		if final {
			return nil
		}
	}
}
