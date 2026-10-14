package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

// Packets frames only this application's encoder output. It is not a media
// decoder or an untrusted-upload parser. Packet boundaries let timed JSON carry
// incremental audio without retaining a complete encoded response.
type Packets struct {
	input   io.Reader
	format  OutputFormat
	samples int64
	preSkip int64
}

func NewPackets(input io.Reader, format OutputFormat) *Packets {
	return &Packets{input: input, format: format}
}

func (p *Packets) Next() ([]byte, float64, error) {
	switch p.format.Kind {
	case "mp3":
		return p.mp3()
	case "opus":
		return p.opus()
	default:
		data := make([]byte, 4096)
		n, err := io.ReadFull(p.input, data)
		if err != nil && err != io.ErrUnexpectedEOF {
			return nil, 0, err
		}
		width := 1
		if p.format.Kind == "pcm" {
			width = 2
		}
		p.samples += int64(n / width)
		return data[:n], float64(p.samples) / float64(p.format.Rate), nil
	}
}

func (p *Packets) mp3() ([]byte, float64, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(p.input, header); err != nil {
		return nil, 0, err
	}
	// The configured MP3 encoder emits MPEG-1/2 layer III CBR, without ID3/Xing.
	if header[0] != 0xff || header[1]&0xe6 != 0xe2 {
		return nil, 0, errors.New("invalid_encoded_frame")
	}
	samples, factor := 1152, 144000
	if p.format.Rate < 32000 {
		samples, factor = 576, 72000
	}
	size := factor*p.format.Bitrate/p.format.Rate + int(header[2]>>1&1)
	if size < 4 || size > 2048 {
		return nil, 0, errors.New("invalid_encoded_frame")
	}
	frame := make([]byte, size)
	copy(frame, header)
	if _, err := io.ReadFull(p.input, frame[4:]); err != nil {
		return nil, 0, err
	}
	p.samples += int64(samples)
	return frame, float64(p.samples) / float64(p.format.Rate), nil
}

func (p *Packets) opus() ([]byte, float64, error) {
	header := make([]byte, 27)
	if _, err := io.ReadFull(p.input, header); err != nil {
		return nil, 0, err
	}
	if !bytes.Equal(header[:4], []byte("OggS")) || header[4] != 0 {
		return nil, 0, errors.New("invalid_encoded_page")
	}
	lacing := make([]byte, int(header[26]))
	if _, err := io.ReadFull(p.input, lacing); err != nil {
		return nil, 0, err
	}
	size := 0
	for _, n := range lacing {
		size += int(n)
	}
	page := make([]byte, 27+len(lacing)+size)
	copy(page, header)
	copy(page[27:], lacing)
	body := page[27+len(lacing):]
	if _, err := io.ReadFull(p.input, body); err != nil {
		return nil, 0, err
	}
	if len(body) >= 19 && bytes.Equal(body[:8], []byte("OpusHead")) {
		p.preSkip = int64(binary.LittleEndian.Uint16(body[10:]))
	}
	granule := int64(binary.LittleEndian.Uint64(header[6:14]))
	if granule >= 0 {
		p.samples = max(0, granule-p.preSkip)
	}
	return page, float64(p.samples) / 48000, nil
}
