//go:build !cgo

// The pure-Go FLAC decoder, the one builds without cgo run.

package audio

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
)

// TestFramePCMReuseNoAllocs checks that framePCM reuses its scratch buffer. It is
// measured in isolation so ParseNext's own allocations do not count.
func TestFramePCMReuseNoAllocs(t *testing.T) {
	const block = 4096
	left := make([]int32, block)
	right := make([]int32, block)
	for i := range left {
		left[i] = int32(int16(i * 7))
		right[i] = -left[i]
	}
	fr := &frame.Frame{
		Header: frame.Header{BlockSize: block, BitsPerSample: 16},
		Subframes: []*frame.Subframe{
			{Samples: left, NSamples: block},
			{Samples: right, NSamples: block},
		},
	}
	f := &goFLACStream{shift: 0}
	f.framePCM(fr) // warm up: first call allocates the reusable buffer

	allocs := testing.AllocsPerRun(1000, func() {
		f.framePCM(fr)
	})
	if allocs != 0 {
		t.Fatalf("framePCM allocated %.1f times/call after warmup; want 0 (buffer should be reused)", allocs)
	}
}

// Decoding with buffers reused across frames gives the same PCM as parsing
// every frame into new buffers, also after a seek, and allocates far less than
// the size of the FLAC.
func TestFLACDecodeExactWithReusedBuffers(t *testing.T) {
	const frames, block = 100, 4096
	data := makeFLAC(frames, block)
	s, err := flac.New(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var want []byte
	ref := &goFLACStream{}
	for {
		fr, err := s.Next()
		if err != nil {
			break
		}
		if err := fr.Parse(); err != nil {
			t.Fatal(err)
		}
		ref.framePCM(fr)
		want = append(want, ref.pcm...)
	}
	if len(want) != frames*block*4 {
		t.Fatalf("reference decode: %d PCM bytes, want %d", len(want), frames*block*4)
	}
	check := func(got []byte, first int) {
		t.Helper()
		if !bytes.Equal(got, want[first*4:]) {
			t.Fatalf("decode from sample %d differs from the reference (%d bytes, want %d)", first, len(got), len(want)-first*4)
		}
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f, err := newGoFLACStream(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16<<10)
	for {
		if _, err := f.Read(buf); err != nil {
			break
		}
	}
	runtime.ReadMemStats(&after)
	if got := after.TotalAlloc - before.TotalAlloc; got > uint64(len(data))/4 {
		t.Errorf("decoding %d FLAC bytes allocated %d bytes, want under a quarter", len(data), got)
	}

	f, err = newGoFLACStream(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	check(decodeAll(t, f), 0)
	const at = 37*block + 5
	if _, err := f.Seek(at*4, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	check(decodeAll(t, f), 37*block)
}

// testdata/tone.flac is 3 s of generated stereo tones encoded by libFLAC 1.5.0, with
// fixed and LPC subframes and correlated-stereo frames. The patched decoder must give
// the same samples as fresh buffers and the MD5 libFLAC recorded.
func TestFLACLibFLACFileDecodesExactly(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "tone.flac"))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := flac.New(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	patched, err := flac.New(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	preds := map[frame.Pred]int{}
	stereo := map[frame.Channels]int{}
	sum := md5.New()
	frames := 0
	for {
		want, err := ref.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := want.Parse(); err != nil {
			t.Fatal(err)
		}
		got, err := patched.ParseNext()
		if err != nil {
			t.Fatalf("frame %d: %v", frames, err)
		}
		stereo[want.Channels]++
		for c, sf := range want.Subframes {
			preds[sf.Pred]++
			if !slices.Equal(got.Subframes[c].Samples, sf.Samples[:sf.NSamples]) {
				t.Fatalf("frame %d channel %d: the patched decoder's samples differ", frames, c)
			}
		}
		for i := range int(want.BlockSize) {
			for _, sf := range want.Subframes {
				_ = binary.Write(sum, binary.LittleEndian, int16(sf.Samples[i]))
			}
		}
		frames++
	}
	if _, err := patched.ParseNext(); err != io.EOF {
		t.Fatalf("patched decoder after the last frame: %v, want io.EOF", err)
	}
	if got := sum.Sum(nil); !bytes.Equal(got, ref.Info.MD5sum[:]) {
		t.Fatalf("decoded MD5 %x, libFLAC recorded %x", got, ref.Info.MD5sum)
	}
	t.Logf("%d frames; subframes fixed %d, LPC %d, constant %d, verbatim %d; frames L/R %d, L/S %d, S/R %d, M/S %d",
		frames, preds[frame.PredFixed], preds[frame.PredFIR], preds[frame.PredConstant], preds[frame.PredVerbatim],
		stereo[frame.ChannelsLR], stereo[frame.ChannelsLeftSide], stereo[frame.ChannelsSideRight], stereo[frame.ChannelsMidSide])
	if preds[frame.PredFixed] == 0 || preds[frame.PredFIR] == 0 {
		t.Fatal("the file lacks fixed or LPC subframes")
	}
	if stereo[frame.ChannelsLeftSide]+stereo[frame.ChannelsSideRight]+stereo[frame.ChannelsMidSide] == 0 {
		t.Fatal("the file lacks correlated-stereo frames")
	}
}
