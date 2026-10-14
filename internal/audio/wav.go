// Package audio validates and converts the service's supported audio formats.
package audio

import (
	"encoding/binary"
	"errors"
)

// PCMFromWAV returns a view into a bounded, validated mono PCM16 RIFF upload.
func PCMFromWAV(data []byte) ([]byte, int, error) {
	invalid := errors.New("invalid_wav")
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, 0, invalid
	}
	size := int64(binary.LittleEndian.Uint32(data[4:8])) + 8
	if size < 12 || size > int64(len(data)) {
		return nil, 0, invalid
	}
	data = data[12:size]
	rate := 0
	var pcm []byte
	for len(data) > 0 {
		if len(data) < 8 {
			return nil, 0, invalid
		}
		n := int64(binary.LittleEndian.Uint32(data[4:8]))
		id := string(data[:4])
		data = data[8:]
		padded := n + n%2
		if padded > int64(len(data)) {
			return nil, 0, invalid
		}
		chunk := data[:n]
		switch id {
		case "fmt ":
			if rate != 0 || len(chunk) < 16 || binary.LittleEndian.Uint16(chunk) != 1 || binary.LittleEndian.Uint16(chunk[2:]) != 1 || binary.LittleEndian.Uint16(chunk[12:]) != 2 || binary.LittleEndian.Uint16(chunk[14:]) != 16 {
				return nil, 0, invalid
			}
			rate = int(binary.LittleEndian.Uint32(chunk[4:]))
			if rate < 8000 || rate > 48000 || binary.LittleEndian.Uint32(chunk[8:]) != uint32(rate*2) {
				return nil, 0, invalid
			}
		case "data":
			if pcm != nil || n%2 != 0 {
				return nil, 0, invalid
			}
			pcm = chunk
		}
		data = data[padded:]
	}
	if rate == 0 || pcm == nil {
		return nil, 0, invalid
	}
	return pcm, rate, nil
}
