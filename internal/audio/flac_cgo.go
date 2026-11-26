//go:build cgo

package audio

/*
#cgo CFLAGS: -I${SRCDIR}/cdec
#include <stdint.h>
#include "dr_flac.h"

drflac* pd_flac_open(uintptr_t h);
*/
import "C"

import (
	"errors"
	"io"
	"runtime"
	"runtime/cgo"
	"unsafe"
)

// flacSource is the reader dr_flac's callbacks read, and the first error it met. A seek
// from the end, which dr_flac makes once to check metadata sizes against the file's, is
// answered from the declared length without moving (the bytes may not have arrived): at
// is that position until the next seek.
type flacSource struct {
	r   io.ReadSeeker
	err error
	at  int64 // -1: the reader's own position
}

// size is the stream's length when known, else 0.
func (s *flacSource) size() int64 {
	if z, ok := s.r.(interface{ Size() int64 }); ok {
		return z.Size()
	}
	return 0
}

func (s *flacSource) pos() (int64, error) {
	if s.at >= 0 {
		return s.at, nil
	}
	return s.r.Seek(0, io.SeekCurrent)
}

//export pdFlacRead
func pdFlacRead(h C.uintptr_t, buf unsafe.Pointer, n C.size_t) C.size_t {
	s := cgo.Handle(h).Value().(*flacSource)
	if s.at >= 0 {
		return 0 // at the end
	}
	p := unsafe.Slice((*byte)(buf), int(n))
	got, err := io.ReadFull(s.r, p)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF && s.err == nil {
		s.err = err
	}
	return C.size_t(got)
}

//export pdFlacSeek
func pdFlacSeek(h C.uintptr_t, offset C.int, origin C.int) C.int {
	s := cgo.Handle(h).Value().(*flacSource)
	want := int64(offset)
	switch origin {
	case C.DRFLAC_SEEK_SET:
	case C.DRFLAC_SEEK_CUR:
		cur, err := s.pos()
		if err != nil {
			return 0
		}
		want += cur
	default:
		size := s.size()
		if size <= 0 {
			return 0
		}
		s.at = size + want
		return 1
	}
	s.at = -1
	// The stream reader stops at its end rather than failing past it; dr_flac wants to know.
	got, err := s.r.Seek(want, io.SeekStart)
	if err != nil || got != want {
		return 0
	}
	return 1
}

//export pdFlacTell
func pdFlacTell(h C.uintptr_t, cursor *C.int64_t) C.int {
	s := cgo.Handle(h).Value().(*flacSource)
	cur, err := s.pos()
	if err != nil {
		return 0
	}
	*cursor = C.int64_t(cur)
	return 1
}

// drFLAC decodes FLAC with dr_flac: several times faster than the pure-Go decoder. It
// yields interleaved s16 stereo (mono duplicated, extra channels dropped, deeper samples
// shifted to 16 bits) and seeks to the exact sample.
type drFLAC struct {
	f        *C.drflac
	src      *flacSource
	channels int
	samples  uint64
	rate     uint32
	buf      []int16 // decoded frames when they are not stereo
	pending  []byte  // converted PCM still to read
	conv     []byte
}

type flacStream = drFLAC

type drFLACHandle struct {
	f *C.drflac
	h cgo.Handle
}

func newFLACStream(r io.ReadSeeker) (*flacStream, error) {
	src := &flacSource{r: r, at: -1}
	h := cgo.NewHandle(src)
	f := C.pd_flac_open(C.uintptr_t(h))
	if f == nil {
		h.Delete()
		if src.err != nil {
			return nil, src.err
		}
		return nil, errors.New("flac: not a FLAC stream")
	}
	d := &drFLAC{f: f, src: src, channels: int(f.channels), samples: uint64(f.totalPCMFrameCount), rate: uint32(f.sampleRate)}
	// The decoder's C memory goes with the stream (pcmStream has no Close).
	runtime.AddCleanup(d, func(c drFLACHandle) {
		C.drflac_close(c.f)
		c.h.Delete()
	}, drFLACHandle{f, h})
	return d, nil
}

// info is the stream's length in samples (0: unknown) and its sample rate.
func (d *drFLAC) info() (samples uint64, rate uint32) { return d.samples, d.rate }

func (d *drFLAC) Read(p []byte) (int, error) {
	if len(d.pending) > 0 {
		n := copy(p, d.pending)
		d.pending = d.pending[n:]
		return n, nil
	}
	frames := len(p) / frameBytes
	if d.channels == 2 && frames > 0 && uintptr(unsafe.Pointer(&p[0]))%2 == 0 {
		// Stereo straight into the caller's buffer.
		n := uint64(C.drflac_read_pcm_frames_s16(d.f, C.drflac_uint64(frames), (*C.drflac_int16)(unsafe.Pointer(&p[0]))))
		runtime.KeepAlive(d)
		if n == 0 {
			return 0, d.end()
		}
		return int(n) * frameBytes, nil
	}
	frames = max(frames, 1024)
	if need := frames * d.channels; cap(d.buf) < need {
		d.buf = make([]int16, need)
	}
	buf := d.buf[:frames*d.channels]
	n := int(C.drflac_read_pcm_frames_s16(d.f, C.drflac_uint64(frames), (*C.drflac_int16)(unsafe.Pointer(&buf[0]))))
	runtime.KeepAlive(d)
	if n == 0 {
		return 0, d.end()
	}
	if cap(d.conv) < n*frameBytes {
		d.conv = make([]byte, n*frameBytes)
	}
	out := d.conv[:n*frameBytes]
	for i := 0; i < n; i++ {
		l := buf[i*d.channels]
		r := l
		if d.channels > 1 {
			r = buf[i*d.channels+1]
		}
		o := i * frameBytes
		out[o], out[o+1], out[o+2], out[o+3] = byte(l), byte(l>>8), byte(r), byte(r>>8)
	}
	c := copy(p, out)
	d.pending = out[c:]
	return c, nil
}

func (d *drFLAC) end() error {
	if d.src.err != nil {
		return d.src.err
	}
	return io.EOF
}

// Seek moves to a PCM byte offset from the start, to the exact sample.
func (d *drFLAC) Seek(off int64, whence int) (int64, error) {
	if whence != io.SeekStart || off < 0 {
		return 0, io.ErrUnexpectedEOF
	}
	d.pending = nil
	ok := C.drflac_seek_to_pcm_frame(d.f, C.drflac_uint64(off/frameBytes))
	runtime.KeepAlive(d)
	if ok == 0 {
		if d.src.err != nil {
			return 0, d.src.err
		}
		return 0, errors.New("flac: seek past the end")
	}
	return off, nil
}
