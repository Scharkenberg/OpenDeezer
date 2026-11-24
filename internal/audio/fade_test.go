package audio

import (
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
)

func sampleAt(b []byte, frameIdx int) int16 {
	o := frameIdx * frameBytes // left channel of the frame
	return int16(uint16(b[o]) | uint16(b[o+1])<<8)
}

// TestApplyFadeOutRamp checks applyFadeOut produces a monotone down-ramp from
// near full scale to near zero across fadeOutFrames, not a hard cut.
func TestApplyFadeOutRamp(t *testing.T) {
	n := int(fadeOutFrames)
	b := make([]byte, n*frameBytes)
	for i := 0; i+1 < len(b); i += 2 {
		b[i] = 0xFF
		b[i+1] = 0x7F // 0x7FFF = 32767 full scale
	}
	rem := applyFadeOut(b, fadeOutFrames)
	if rem != 0 {
		t.Fatalf("ramp should be spent over a full-length buffer, %d frames left", rem)
	}
	first := sampleAt(b, 0)
	last := sampleAt(b, n-1)
	if first < 30000 {
		t.Fatalf("first frame should stay near full scale, got %d", first)
	}
	if last > 2000 {
		t.Fatalf("last frame should ramp near zero, got %d", last)
	}
	if last >= first {
		t.Fatalf("expected a downward ramp (first %d > last %d)", first, last)
	}
}

// filledSource returns a source whose ring is pre-loaded with nbytes of
// full-scale (0x7FFF) PCM, for driving readPCM without the download/decode
// pipeline.
func filledSource(t *testing.T, nbytes int) *source {
	t.Helper()
	s := newSource(&deezer.StreamPlan{Format: "MP3"}, 100000)
	data := make([]byte, nbytes)
	for i := 0; i+1 < len(data); i += 2 {
		data[i] = 0xFF
		data[i+1] = 0x7F
	}
	if !s.ring.write(data, s.ring.seq()) {
		t.Fatal("ring.write failed to preload PCM")
	}
	return s
}

// TestReadPCMFadeOut drives the realtime callback directly: an armed fade-out ramps
// the PCM down instead of cutting it, and output stays muted after the ramp until the
// deferred state flip.
func TestReadPCMFadeOut(t *testing.T) {
	p := newPlayer(&mockOutput{})
	frames := int(fadeOutFrames)
	src := filledSource(t, 4*frames*frameBytes)
	p.cur.Store(src)
	p.state.Store(int32(Playing))

	buf := make([]byte, frames*frameBytes)

	// Baseline: normal playback keeps full-scale samples (no fade armed).
	if n := p.readPCM(buf); n != len(buf) {
		t.Fatalf("baseline read short: %d of %d", n, len(buf))
	}
	if got := sampleAt(buf, 0); got < 30000 {
		t.Fatalf("baseline first frame should be full scale, got %d", got)
	}

	// Arm the fade-out as Pause/Stop/skip/seek do: frame count first, then armed, the
	// order fadeOutAndWait uses.
	p.fadeOut.Store(fadeOutFrames)
	p.fadeOutArmed.Store(true)
	p.readPCM(buf)
	first := sampleAt(buf, 0)
	last := sampleAt(buf, frames-1)
	if first < 30000 {
		t.Fatalf("fade-out first frame should start near full scale, got %d", first)
	}
	if last > 2000 {
		t.Fatalf("fade-out last frame should be near zero, got %d (looks like a hard cut/no ramp)", last)
	}
	if p.fadeOut.Load() != 0 {
		t.Fatalf("ramp should be spent after a full-length callback, %d frames left", p.fadeOut.Load())
	}

	// After the ramp completes, while still armed (awaiting the control op's flip),
	// output must be muted rather than resurging to full volume.
	p.readPCM(buf)
	for i := 0; i < frames; i++ {
		if v := sampleAt(buf, i); v != 0 {
			t.Fatalf("post-ramp output must be muted; frame %d = %d", i, v)
		}
	}
}

// TestFadeOutArmPublishesFramesFirst checks fadeOutAndWait's publication order: an
// observer reading in the callback's order never sees armed with fadeOut == 0, which
// would hard-mute a full-volume buffer before the ramp.
func TestFadeOutArmPublishesFramesFirst(t *testing.T) {
	for iter := 0; iter < 200; iter++ {
		p := newPlayer(&mockOutput{})
		p.state.Store(int32(Playing))
		var violated atomic.Bool
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				if p.fadeOutArmed.Load() {
					// Callback's view at arm time: armed must come with frames.
					if p.fadeOut.Load() == 0 {
						violated.Store(true)
					}
					// Play the ramp out so fadeOutAndWait returns promptly.
					p.fadeOut.Store(0)
					return
				}
				runtime.Gosched()
			}
		}()
		p.fadeOutAndWait()
		<-done
		if violated.Load() {
			t.Fatalf("iteration %d: callback observed fadeOutArmed with fadeOut==0 at arm time (pre-ramp hard mute)", iter)
		}
	}
}
