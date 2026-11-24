package audio

import (
	"io"
	"math"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// memPCM is an in-memory interleaved-s16 pcmStream for testing the resampler.
type memPCM struct {
	buf []byte
	pos int
}

func (m *memPCM) Read(p []byte) (int, error) {
	if m.pos >= len(m.buf) {
		return 0, io.EOF
	}
	n := copy(p, m.buf[m.pos:])
	m.pos += n
	return n, nil
}

func (m *memPCM) Seek(off int64, whence int) (int64, error) {
	if whence != io.SeekStart {
		return 0, io.ErrUnexpectedEOF
	}
	if off < 0 {
		off = 0
	}
	if off > int64(len(m.buf)) {
		off = int64(len(m.buf))
	}
	m.pos = int(off)
	return off, nil
}

// makeConst builds `frames` stereo s16 frames all equal to v.
func makeConst(frames int, v int16) []byte {
	b := make([]byte, frames*frameBytes)
	for i := 0; i < frames; i++ {
		o := i * frameBytes
		b[o] = byte(v)
		b[o+1] = byte(uint16(v) >> 8)
		b[o+2] = byte(v)
		b[o+3] = byte(uint16(v) >> 8)
	}
	return b
}

func TestResampleUpsampleFrameCount(t *testing.T) {
	// 22050 Hz -> 44100 Hz doubles the frame count. Feed 1s of a constant signal;
	// expect ~2s out, and (linear interp of a constant) every sample unchanged.
	const srcRate = 22050
	const srcFrames = srcRate // ~1s
	const v int16 = 10000
	rs := newResampleStream(&memPCM{buf: makeConst(srcFrames, v)}, srcRate, nil)

	out := make([]byte, 8192)
	total := 0
	for {
		n, err := rs.Read(out)
		for i := 0; i+1 < n; i += 2 {
			s := int16(uint16(out[i]) | uint16(out[i+1])<<8)
			if s != v {
				t.Fatalf("constant signal changed after resample: got %d want %d", s, v)
			}
		}
		total += n
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
	}
	gotFrames := total / frameBytes
	want := srcFrames * sampleRate / srcRate
	if diff := gotFrames - want; diff < -4 || diff > 4 {
		t.Fatalf("output frames = %d, want ~%d (srcFrames=%d)", gotFrames, want, srcFrames)
	}
}

func TestResampleDownsampleFrameCount(t *testing.T) {
	// 48000 Hz -> 44100 Hz shrinks the frame count proportionally.
	const srcRate = 48000
	const srcFrames = srcRate // ~1s
	rs := newResampleStream(&memPCM{buf: makeConst(srcFrames, -5000)}, srcRate, nil)

	out := make([]byte, 4096)
	total := 0
	for {
		n, err := rs.Read(out)
		total += n
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
	}
	gotFrames := total / frameBytes
	want := srcFrames * sampleRate / srcRate
	if diff := gotFrames - want; diff < -4 || diff > 4 {
		t.Fatalf("output frames = %d, want ~%d", gotFrames, want)
	}
}

func TestResampleSeek(t *testing.T) {
	// A ramp lets us verify Seek lands near the right source position: source
	// sample s has value s (mod wrap), so after seeking to output byte offset the
	// first decoded sample should reflect the corresponding source frame.
	const srcRate = 48000
	const srcFrames = 20000
	buf := make([]byte, srcFrames*frameBytes)
	for i := 0; i < srcFrames; i++ {
		v := int16(i % 1000)
		o := i * frameBytes
		buf[o] = byte(v)
		buf[o+1] = byte(uint16(v) >> 8)
		buf[o+2] = byte(v)
		buf[o+3] = byte(uint16(v) >> 8)
	}
	rs := newResampleStream(&memPCM{buf: buf}, srcRate, nil)

	// Seek to output frame 5000 -> source frame ~ 5000 * 48000/44100.
	dstFrame := int64(5000)
	if _, err := rs.Seek(dstFrame*frameBytes, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	out := make([]byte, frameBytes)
	n, err := rs.Read(out)
	if err != nil && err != io.EOF {
		t.Fatalf("Read after seek: %v", err)
	}
	if n < frameBytes {
		t.Fatalf("short read after seek: %d", n)
	}
	got := int16(uint16(out[0]) | uint16(out[1])<<8)
	wantSrc := int16((int64(float64(dstFrame)*rs.ratio) % 1000))
	if d := got - wantSrc; d < -2 || d > 2 {
		t.Fatalf("first sample after seek = %d, want ~%d", got, wantSrc)
	}
}

// At double speed the resampler steps two source frames per output frame, and
// a seek offset is media time (normal speed).
func TestResampleSpeed(t *testing.T) {
	const frames = 1000
	buf := make([]byte, frames*frameBytes)
	for i := 0; i < frames; i++ {
		o := i * frameBytes
		buf[o], buf[o+1] = byte(i), byte(uint16(i)>>8)
	}
	var speed atomic.Uint64
	speed.Store(math.Float64bits(2))
	rs := newResampleStream(&memPCM{buf: buf}, sampleRate, &speed)
	out := make([]byte, 4*frameBytes)
	if n, _ := rs.Read(out); n != len(out) {
		t.Fatalf("read %d", n)
	}
	for i := 0; i < 4; i++ {
		if v := int(uint16(out[i*frameBytes]) | uint16(out[i*frameBytes+1])<<8); v != 2*i {
			t.Fatalf("frame %d = %d, want %d", i, v, 2*i)
		}
	}
	if _, err := rs.Seek(100*frameBytes, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if n, _ := rs.Read(out[:frameBytes]); n != frameBytes || int(uint16(out[0])|uint16(out[1])<<8) != 100 {
		t.Fatalf("after a seek to frame 100: %d", int(uint16(out[0])|uint16(out[1])<<8))
	}
}

// A speed change does not seek (an MP3 seek may wait for the whole stream): it
// drops the PCM decoded at the old speed and counts that media time as played.
func TestSetSpeedDoesNotSeek(t *testing.T) {
	p := newPlayer(&mockOutput{})
	defer p.Close()
	cur := playingSource(p, "e", 3600000)
	buffered := cur.ring.buffered()
	p.SetSpeed(1.5)
	if cur.seeks.Load() != 0 || cur.seekTo.Load() >= 0 {
		t.Fatalf("a speed change sought (%d seeks, target %d)", cur.seeks.Load(), cur.seekTo.Load())
	}
	if speedOf(&cur.speed) != 1.5 || cur.ring.buffered() != 0 {
		t.Fatalf("speed %v, %d bytes still buffered", speedOf(&cur.speed), cur.ring.buffered())
	}
	if got := p.played.Load(); got != int64(buffered) {
		t.Fatalf("played %d, want the %d dropped bytes counted", got, buffered)
	}
}

// A forward seek on an MP3 still downloading decodes up to the target instead
// of waiting for the whole stream.
func TestMP3ForwardSeekDoesNotWaitForTheEnd(t *testing.T) {
	mp3 := makeMP3(1400)
	sb := newStreamFile(nil)
	defer sb.close()
	_ = sb.write(mp3[:len(mp3)/2])
	m, err := newMP3Stream(sb)
	if err != nil {
		t.Fatal(err)
	}
	off := int64(2 * bytesPerSec)
	done := make(chan error, 1)
	go func() {
		_, err := m.Seek(off, io.SeekStart)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a forward seek waited for the rest of the stream")
	}
	if n, err := m.Read(make([]byte, 4096)); n == 0 || err != nil {
		t.Fatalf("read after the seek: %d, %v", n, err)
	}
	if m.pos <= off {
		t.Fatalf("position %d after reading from %d", m.pos, off)
	}
}

// One second of audio through the resampler: 44.1 kHz at normal speed (an
// episode's usual case) and 48 kHz.
func BenchmarkResampleRead(b *testing.B) {
	for _, rate := range []int{sampleRate, 48000} {
		b.Run(strconv.Itoa(rate), func(b *testing.B) {
			src := makeConst(rate, 1000)
			out := make([]byte, decodeChunk)
			b.SetBytes(int64(len(src)))
			for i := 0; i < b.N; i++ {
				rs := newResampleStream(&memPCM{buf: src}, rate, nil)
				for {
					if _, err := rs.Read(out); err != nil {
						break
					}
				}
			}
		})
	}
}
