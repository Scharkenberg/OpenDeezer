//go:build cgo

package audio

/*
#cgo CFLAGS: -O2 -I${SRCDIR}/cdec
#define MINIMP3_IMPLEMENTATION
#include "minimp3.h"
*/
import "C"

import (
	"errors"
	"io"
	"unsafe"
)

const (
	mp3InBuf   = 32 << 10 // compressed bytes held for the decoder
	mp3InLow   = 16 << 10 // refilled below this: minimp3 syncs on up to ten frames ahead
	mp3InMin   = 4 << 10  // more than the largest frame and the next header
	mp3Preroll = 640      // bytes decoded before a seek target: the bit reservoir (511) and more
)

var errMP3NoFrame = errors.New("mp3: no frame found")

// miniMP3 decodes MP3 with minimp3 (public domain, CC0): several times faster than the
// pure-Go decoder, with SIMD where the CPU has it. It emits one frame of samples per
// frame of the stream, a silent frame for one it cannot decode (its bit reservoir lost),
// so the output stays sample-aligned with the stream's frames like go-mp3's.
type miniMP3 struct {
	dec  C.mp3dec_t
	pcm  [C.MINIMP3_MAX_SAMPLES_PER_FRAME]int16
	r    io.Reader
	rs   io.ReadSeeker // set for a seekable decoder
	in   []byte
	a, b int // in[a:b] is buffered
	eof  bool
	rerr error // the reader's error, returned after the bytes read before it

	out    []byte // the current frame's PCM still to read
	stereo []byte // mono duplicated, reused
	rate   int
	spf    int     // samples per frame
	frame  int64   // the index of the next frame to decode
	drop   int64   // stereo samples to drop from the output (a seek inside a frame)
	index  []int64 // each frame's byte offset (seekable only)
}

func newMiniMP3(r io.Reader) (*miniMP3, error) {
	m := &miniMP3{r: r, in: make([]byte, mp3InBuf)}
	C.mp3dec_init(&m.dec)
	// The rate is known from the first frame, which is kept for the first Read.
	if err := m.next(); err != nil {
		if err == io.EOF {
			err = errMP3NoFrame
		}
		return nil, err
	}
	return m, nil
}

// newSeekableMiniMP3 indexes the frames of a whole stream (headers only), then decodes
// from its start.
func newSeekableMiniMP3(rs io.ReadSeeker) (mp3Decoder, error) {
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	var scan C.mp3dec_t
	C.mp3dec_init(&scan)
	var info C.mp3dec_frame_info_t
	idx := make([]int64, 0, 8192)
	buf := make([]byte, mp3InBuf)
	var base int64 // the stream offset of buf[0]
	a, b, eof := 0, 0, false
	for {
		if !eof && b-a < mp3InLow {
			copy(buf, buf[a:b])
			base += int64(a)
			b -= a
			a = 0
			n, err := io.ReadFull(rs, buf[b:])
			b += n
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				eof = true
			} else if err != nil {
				return nil, err
			}
		}
		if a >= b {
			break
		}
		info = C.mp3dec_frame_info_t{}
		n := C.mp3dec_decode_frame(&scan, (*C.uint8_t)(unsafe.Pointer(&buf[a])), C.int(b-a), nil, &info)
		fb := int(info.frame_bytes)
		if fb == 0 {
			break // a partial frame at the end
		}
		if n > 0 {
			idx = append(idx, base+int64(a)+int64(info.frame_offset))
		}
		a += fb
	}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	m, err := newMiniMP3(rs)
	if err != nil {
		return nil, err
	}
	m.rs, m.index = rs, idx
	return m, nil
}

func (m *miniMP3) SampleRate() int { return m.rate }

// fill tops the input buffer up from the reader, to at least mp3InMin bytes unless the
// stream ends: minimp3 given less than a frame and the next header resets and drops them.
// A read error ends the stream after what was read; next returns it then.
func (m *miniMP3) fill() {
	copy(m.in, m.in[m.a:m.b])
	m.b -= m.a
	m.a = 0
	for m.b < len(m.in) && !m.eof {
		n, err := m.r.Read(m.in[m.b:])
		m.b += n
		if err != nil {
			m.eof = true
			if err != io.EOF {
				m.rerr = err
			}
		} else if m.b >= mp3InMin {
			break
		}
	}
}

// end is what next returns once the buffered bytes are decoded.
func (m *miniMP3) end() error {
	if m.rerr != nil {
		return m.rerr
	}
	return io.EOF
}

// next decodes the next frame into m.out.
func (m *miniMP3) next() error {
	for {
		if !m.eof && m.b-m.a < mp3InLow {
			m.fill()
		}
		if m.a >= m.b {
			return m.end()
		}
		var info C.mp3dec_frame_info_t
		n := int(C.mp3dec_decode_frame(&m.dec, (*C.uint8_t)(unsafe.Pointer(&m.in[m.a])), C.int(m.b-m.a), (*C.mp3d_sample_t)(unsafe.Pointer(&m.pcm[0])), &info))
		fb := int(info.frame_bytes)
		if fb == 0 {
			// Only at the end (fill keeps more than a frame buffered): a partial last frame.
			m.a = m.b
			return m.end()
		}
		m.a += fb
		if info.hz == 0 {
			continue // skipped data (a tag), no frame
		}
		if m.rate == 0 {
			m.rate = int(info.hz)
			switch {
			case info.layer == 1:
				m.spf = 384
			case info.layer == 3 && info.hz < 32000:
				m.spf = 576 // MPEG-2 and 2.5 Layer III
			default:
				m.spf = 1152
			}
		}
		m.frame++
		samples := n
		if samples == 0 {
			// A frame that cannot decode still takes its time, in silence.
			samples = m.spf
			clear(m.pcm[:samples*2])
			info.channels = 2
		}
		if info.channels == 1 {
			if cap(m.stereo) < samples*4 {
				m.stereo = make([]byte, samples*4)
			}
			m.stereo = m.stereo[:samples*4]
			for i, s := range m.pcm[:samples] {
				o := i * 4
				m.stereo[o], m.stereo[o+1] = byte(s), byte(s>>8)
				m.stereo[o+2], m.stereo[o+3] = byte(s), byte(s>>8)
			}
			m.out = m.stereo
		} else {
			// Little-endian on every target: the samples are the bytes.
			m.out = unsafe.Slice((*byte)(unsafe.Pointer(&m.pcm[0])), samples*4)
		}
		if m.drop > 0 {
			d := min(m.drop, int64(len(m.out)/4))
			m.out = m.out[d*4:]
			m.drop -= d
			if len(m.out) == 0 {
				continue
			}
		}
		return nil
	}
}

func (m *miniMP3) Read(p []byte) (int, error) {
	for len(m.out) == 0 {
		if err := m.next(); err != nil {
			return 0, err
		}
	}
	n := copy(p, m.out)
	m.out = m.out[n:]
	return n, nil
}

// Seek moves to a PCM byte offset from the start: it restarts the decoder a few frames
// before the target's frame (for the bit reservoir and the filters' state) and drops
// the samples before the target. Only a seekable decoder seeks.
func (m *miniMP3) Seek(off int64, whence int) (int64, error) {
	if m.rs == nil || whence != io.SeekStart || off < 0 {
		return 0, errors.New("mp3: unsupported seek")
	}
	t := off / frameBytes
	k := t / int64(m.spf)
	if k >= int64(len(m.index)) {
		return 0, io.EOF
	}
	start := k
	for start > 0 && m.index[k]-m.index[start] < mp3Preroll {
		start--
	}
	start = max(0, start-2)
	if _, err := m.rs.Seek(m.index[start], io.SeekStart); err != nil {
		return 0, err
	}
	C.mp3dec_init(&m.dec)
	m.a, m.b, m.eof, m.rerr, m.out = 0, 0, false, nil, nil
	m.frame = start
	m.drop = t - start*int64(m.spf)
	return off, nil
}
