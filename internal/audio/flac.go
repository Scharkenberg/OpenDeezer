package audio

import (
	"io"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
)

// flacStream decodes a decrypted FLAC buffer to interleaved s16 stereo PCM with the
// same Read+Seek contract as go-mp3's decoder. Other bit depths are shifted to 16-bit;
// the sample rate is assumed to be 44100.
type flacStream struct {
	s     *flac.Stream
	pcm   []byte // one frame's rendered interleaved PCM, reused across frames
	off   int    // read offset into pcm (bytes already returned)
	shift int    // bitsPerSample - 16
}

func newFLACStream(r io.ReadSeeker) (*flacStream, error) {
	s, err := flac.NewSeek(r)
	if err != nil {
		return nil, err
	}
	return &flacStream{s: s, shift: int(s.Info.BitsPerSample) - 16}, nil
}

func (f *flacStream) conv(s int32) int16 {
	if f.shift > 0 {
		s >>= f.shift
	} else if f.shift < 0 {
		s <<= -f.shift
	}
	if s > 32767 {
		s = 32767
	} else if s < -32768 {
		s = -32768
	}
	return int16(s)
}

// framePCM renders one FLAC frame into f.pcm as interleaved s16 stereo (mono
// duplicated, extra channels dropped) and resets the read offset. The scratch buffer
// is reused, so only the first frame allocates.
func (f *flacStream) framePCM(fr *frame.Frame) {
	n := int(fr.BlockSize)
	need := n * 4 // n * 2ch * 2 bytes
	if cap(f.pcm) < need {
		f.pcm = make([]byte, need)
	} else {
		f.pcm = f.pcm[:need]
	}
	left := fr.Subframes[0].Samples
	right := left
	if len(fr.Subframes) > 1 {
		right = fr.Subframes[1].Samples
	}
	for i := 0; i < n; i++ {
		l := f.conv(left[i])
		r := f.conv(right[i])
		o := i * 4
		f.pcm[o] = byte(l)
		f.pcm[o+1] = byte(l >> 8)
		f.pcm[o+2] = byte(r)
		f.pcm[o+3] = byte(r >> 8)
	}
	f.off = 0
}

func (f *flacStream) Read(p []byte) (int, error) {
	for f.off >= len(f.pcm) {
		fr, err := f.s.ParseNext()
		if err != nil {
			return 0, err // includes io.EOF
		}
		f.framePCM(fr)
	}
	n := copy(p, f.pcm[f.off:])
	f.off += n
	return n, nil
}

// Seek moves to a PCM byte offset from the start (SeekStart only, like the
// player uses). Converts the byte offset to a sample number and seeks the FLAC
// stream to the frame containing it.
func (f *flacStream) Seek(off int64, whence int) (int64, error) {
	if whence != io.SeekStart {
		return 0, io.ErrUnexpectedEOF
	}
	sample := uint64(off) / uint64(channels*2)
	if _, err := f.s.Seek(sample); err != nil {
		return 0, err
	}
	// Mark the scratch buffer empty (off >= len) so the next Read parses a fresh
	// frame; keep its capacity to avoid a reallocation.
	f.pcm = f.pcm[:0]
	f.off = 0
	return off, nil
}
