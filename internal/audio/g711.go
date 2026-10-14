package audio

import "encoding/binary"

// DecodeMuLaw expands bounded G.711 mu-law bytes into signed PCM16 LE.
// Callers enforce input limits before the exactly twofold allocation.
func DecodeMuLaw(data []byte) []byte {
	pcm := make([]byte, len(data)*2)
	for i, encoded := range data {
		u := ^encoded
		magnitude := ((int(u&15) << 3) + 132) << ((u >> 4) & 7)
		value := magnitude - 132
		if u&128 != 0 {
			value = -value
		}
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(int16(value)))
	}
	return pcm
}
