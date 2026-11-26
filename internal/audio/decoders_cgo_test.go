//go:build cgo

package audio

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"
	"testing/iotest"

	"github.com/mewkiz/flac"
)

func readAllPCM(t testing.TB, r io.Reader) []byte {
	t.Helper()
	var out []byte
	buf := make([]byte, decodeChunk)
	for {
		n, err := r.Read(buf)
		out = append(out, buf[:n]...)
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

// dr_flac and the pure-Go decoder (mewkiz, linked into the test only) give the same samples (FLAC is lossless), and
// dr_flac seeks to the exact sample.
func TestDrFLACMatchesGoDecoder(t *testing.T) {
	tone, err := os.ReadFile("testdata/tone.flac")
	if err != nil {
		t.Fatal(err)
	}
	// The synthetic file's right channel is -left, which overflows 16 bits in its side
	// channel at -32768: the pure-Go decoder clips there, so it is checked against the
	// samples it was made of.
	const frames, block = 20, 4096
	synth := make([]byte, 0, frames*block*frameBytes)
	for n := 0; n < frames*block; n++ {
		l := int16(n * 251)
		r := int16(-int32(l))
		synth = append(synth, byte(l), byte(l>>8), byte(r), byte(r>>8))
	}
	for name, data := range map[string][]byte{"tone": tone, "synthetic": makeFLAC(frames, block)} {
		t.Run(name, func(t *testing.T) {
			want := synth
			if name == "tone" {
				want = mewkizPCM(t, data)
			}
			c, err := newFLACStream(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if n, rate := c.info(); n == 0 || rate != 44100 {
				t.Fatalf("info: %d samples at %d Hz", n, rate)
			}
			if got := readAllPCM(t, c); !bytes.Equal(got, want) {
				t.Fatalf("dr_flac decode differs: %d PCM bytes, want %d", len(got), len(want))
			}
			at := int64(len(want)/frameBytes) / 3
			at += 7 // inside a frame
			if _, err := c.Seek(at*frameBytes, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			if got := readAllPCM(t, c); !bytes.Equal(got, want[at*frameBytes:]) {
				t.Fatalf("after a seek to sample %d: %d PCM bytes, want %d", at, len(got), len(want)-int(at*frameBytes))
			}
		})
	}
}

// minimp3 emits one frame of samples per frame of the stream, from a stream and
// after a seek of the indexed decoder.
func TestMiniMP3FrameAligned(t *testing.T) {
	const frames = 300
	data := makeMP3(frames)
	d, err := newMP3Decoder(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if d.SampleRate() != 44100 {
		t.Fatalf("rate %d", d.SampleRate())
	}
	if got := len(readAllPCM(t, d)); got != frames*1152*frameBytes {
		t.Fatalf("stream decode: %d PCM bytes, want %d", got, frames*1152*frameBytes)
	}
	s, err := newSeekableMP3Decoder(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	const at = 1152*170 + 33
	if _, err := s.Seek(at*frameBytes, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if got := len(readAllPCM(t, s)); got != (frames*1152-at)*frameBytes {
		t.Fatalf("after a seek: %d PCM bytes, want %d", got, (frames*1152-at)*frameBytes)
	}
	if _, err := s.Seek(int64(frames+1)*1152*frameBytes, io.SeekStart); err == nil {
		t.Fatal("a seek past the end succeeded")
	}
}

type failAfter struct {
	r   io.Reader
	err error
}

func (f failAfter) Read(p []byte) (int, error) {
	n, err := f.r.Read(p)
	if err == io.EOF {
		return n, f.err
	}
	return n, err
}

// A stream that fails after N whole frames still plays them (one may go to the bit
// reservoir) and then returns the error.
func TestMiniMP3PlaysUpToAReadError(t *testing.T) {
	const frames = 40
	cut := errors.New("connection reset")
	d, err := newMP3Decoder(failAfter{bytes.NewReader(makeMP3(frames)), cut})
	if err != nil {
		t.Fatal(err)
	}
	var got int
	buf := make([]byte, decodeChunk)
	for {
		n, err := d.Read(buf)
		got += n
		if err != nil {
			if err != cut {
				t.Fatalf("ended with %v, want the read error", err)
			}
			break
		}
	}
	if got < (frames-1)*1152*frameBytes {
		t.Fatalf("%d PCM bytes before the error, want at least %d", got, (frames-1)*1152*frameBytes)
	}
}

// A stream that arrives a byte at a time decodes to as many samples as a whole one.
func TestMiniMP3ByteAtATime(t *testing.T) {
	data := makeMP3(120)
	whole, err := newMP3Decoder(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	slow, err := newMP3Decoder(iotest.OneByteReader(bytes.NewReader(data)))
	if err != nil {
		t.Fatal(err)
	}
	if a, b := len(readAllPCM(t, whole)), len(readAllPCM(t, slow)); a != b {
		t.Fatalf("byte at a time: %d PCM bytes, whole: %d", b, a)
	}
}

// mewkizPCM decodes a 16-bit stereo FLAC with the pure-Go decoder, as interleaved s16.
func mewkizPCM(t *testing.T, data []byte) []byte {
	t.Helper()
	s, err := flac.New(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var out []byte
	for {
		fr, err := s.ParseNext()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		for i := range int(fr.BlockSize) {
			for _, sf := range fr.Subframes[:2] {
				v := int16(sf.Samples[i])
				out = append(out, byte(v), byte(v>>8))
			}
		}
	}
}
