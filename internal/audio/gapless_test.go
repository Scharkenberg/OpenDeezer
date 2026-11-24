package audio

import "testing"

// lameFrame builds an MPEG-1 Layer III stereo Info frame as Deezer's MP3s
// start (measured 2026-11-23): Info at 36 with all four fields, then the
// LAME tag with delay and padding and its CRC.
func lameFrame(frames, delay, padding int, goodCRC bool) []byte {
	b := make([]byte, 418)
	b[0], b[1], b[2], b[3] = 0xFF, 0xFB, 0x90, 0x00
	copy(b[36:], "Info")
	b[43] = 0x0F // frames, bytes, TOC, quality
	b[44], b[45], b[46], b[47] = byte(frames>>24), byte(frames>>16), byte(frames>>8), byte(frames)
	lame := 36 + 8 + 4 + 4 + 100 + 4
	copy(b[lame:], "Lame3.100")
	b[lame+21] = byte(delay >> 4)
	b[lame+22] = byte(delay<<4) | byte(padding>>8&0x0F)
	b[lame+23] = byte(padding)
	crc := uint16(0)
	for _, x := range b[:lame+34] {
		crc ^= uint16(x)
		for k := 0; k < 8; k++ {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	if !goodCRC {
		crc++
	}
	b[lame+34], b[lame+35] = byte(crc>>8), byte(crc)
	return b
}

// The LAME tag gives the PCM to skip (the Info frame, the encoder's delay and
// the decoder's 529 samples) and the music's length; a tag whose CRC fails,
// or no tag, trims nothing.
func TestLAMEGapless(t *testing.T) {
	skip, music := lameGapless(lameFrame(11112, 576, 1452, true))
	if skip != (1152+576+529)*frameBytes || music != (11112*1152-576-1452)*frameBytes {
		t.Fatalf("skip %d, music %d", skip, music)
	}
	if skip, music := lameGapless(lameFrame(11112, 576, 1452, false)); skip != 0 || music != 0 {
		t.Fatalf("a bad CRC trimmed: %d, %d", skip, music)
	}
	plain := lameFrame(10, 576, 1452, true)
	copy(plain[36:], "abcd")
	if skip, music := lameGapless(plain); skip != 0 || music != 0 {
		t.Fatalf("no tag trimmed: %d, %d", skip, music)
	}
}
