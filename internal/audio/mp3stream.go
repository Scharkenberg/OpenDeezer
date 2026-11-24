package audio

import (
	"io"

	"github.com/hajimehoshi/go-mp3"
)

// readerOnly hides an io.ReadSeeker's Seek so go-mp3 treats the source as a stream
// and reads only the first frame, instead of scanning the whole stream to build its
// frame index. Decoding can then start before the download completes.
type readerOnly struct{ r io.Reader }

func (ro readerOnly) Read(p []byte) (int, error) { return ro.r.Read(p) }

// mp3Stream adapts go-mp3 to pcmStream. It decodes over a non-seeking view so audio
// starts at once; a forward seek decodes and drops, and only a backward one waits for
// the full download, builds a seekable decoder and seeks it (later seeks reuse it).
type mp3Stream struct {
	sb       *streamFile
	r        io.ReadSeeker // sb's reader the decoder reads
	dec      *mp3.Decoder
	rate     int
	upgraded bool // dec was rebuilt seekable (post full-download); Seek works
	// lead and music trim the stream to its music (lameGapless): the PCM
	// bytes before it and its length (0: to the end). pos is the position in
	// the music; lead is the bytes before it, skip what is left of them to read.
	lead, skip, music, pos int64
}

func newMP3Stream(sb *streamFile) (*mp3Stream, error) {
	head := make([]byte, 512)
	n, _ := io.ReadFull(sb.reader(), head)
	r := sb.reader()
	d, err := mp3.NewDecoder(readerOnly{r})
	if err != nil {
		return nil, err
	}
	skip, music := lameGapless(head[:n])
	return &mp3Stream{sb: sb, r: r, dec: d, rate: d.SampleRate(), lead: skip, skip: skip, music: music}, nil
}

// lameGapless reads the LAME tag of an MP3's first frame and returns the PCM bytes
// go-mp3 decodes before the music (that frame, encoder delay, 529 decoder samples) and
// the music's length (0 without a frame count). Zeros when no tag has a valid CRC.
func lameGapless(b []byte) (skip, music int64) {
	if len(b) < 4 || b[0] != 0xFF || b[1]&0xE0 != 0xE0 || b[1]>>1&3 != 1 {
		return 0, 0 // not a Layer III frame
	}
	mpeg1, mono := b[1]>>3&3 == 3, b[3]>>6 == 3
	side, spf := 17, 576 // MPEG-2/2.5 stereo, MPEG-1 mono: 17 bytes
	switch {
	case mpeg1 && !mono:
		side = 32
	case !mpeg1 && mono:
		side = 9
	}
	if mpeg1 {
		spf = 1152
	}
	x := 4 + side
	if b[1]&1 == 0 {
		x += 2 // the frame's CRC
	}
	if len(b) < x+8 || string(b[x:x+4]) != "Xing" && string(b[x:x+4]) != "Info" {
		return 0, 0
	}
	be32 := func(o int) int64 { return int64(b[o])<<24 | int64(b[o+1])<<16 | int64(b[o+2])<<8 | int64(b[o+3]) }
	flags, p := be32(x+4), x+8
	var frames int64
	if flags&1 != 0 {
		frames = be32(p)
		p += 4
	}
	for _, f := range []struct {
		bit int64
		n   int
	}{{2, 4}, {4, 100}, {8, 4}} {
		if flags&f.bit != 0 {
			p += f.n
		}
	}
	if len(b) < p+36 {
		return 0, 0
	}
	crc := uint16(0) // CRC-16/ARC of the frame up to the tag's CRC
	for _, c := range b[:p+34] {
		crc ^= uint16(c)
		for k := 0; k < 8; k++ {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	if crc != uint16(b[p+34])<<8|uint16(b[p+35]) {
		return 0, 0
	}
	delay := int64(b[p+21])<<4 | int64(b[p+22])>>4
	padding := int64(b[p+22]&0x0F)<<8 | int64(b[p+23])
	skip = (int64(spf) + delay + 529) * frameBytes
	if m := frames*int64(spf) - delay - padding; frames > 0 && m > 0 {
		music = m * frameBytes
	}
	return skip, music
}

// SampleRate reports the decoded stream's native sample rate (from the first
// frame). decode() wraps mp3Stream in a resampler when it differs from 44100.
func (m *mp3Stream) SampleRate() int { return m.rate }

func (m *mp3Stream) Read(p []byte) (int, error) {
	for m.skip > 0 {
		n, err := m.dec.Read(p[:min(int64(len(p)), m.skip)])
		m.skip -= int64(n)
		if err != nil {
			return 0, err
		}
	}
	if m.music > 0 {
		if m.pos >= m.music {
			return 0, io.EOF
		}
		p = p[:min(int64(len(p)), m.music-m.pos)]
	}
	n, err := m.dec.Read(p)
	m.pos += int64(n)
	return n, err
}

// Seek panics (go-mp3: index out of range) past the last frame; decode()
// recovers that as a seek error.
func (m *mp3Stream) Seek(off int64, whence int) (int64, error) {
	// Forward on the stream as it arrives: decode up to the target and drop it.
	if !m.upgraded && whence == io.SeekStart && off >= m.pos {
		m.skip += off - m.pos
		m.pos = off
		return off, nil
	}
	if !m.upgraded {
		// Building go-mp3's frame index needs the whole stream, so wait for the download.
		m.sb.waitDone()
		if _, err := m.r.Seek(0, io.SeekStart); err != nil {
			return 0, err
		}
		nd, err := mp3.NewDecoder(m.r) // seekable: scans + indexes frames
		if err != nil {
			return 0, err
		}
		m.dec = nd
		m.upgraded = true
	}
	if _, err := m.dec.Seek(off+m.lead, whence); err != nil {
		return 0, err
	}
	m.skip, m.pos = 0, off
	return off, nil
}
