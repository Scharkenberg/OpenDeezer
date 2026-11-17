package audio

import (
	"sync/atomic"
	"testing"
	"time"
)

// nearEndPlayer is a managed, Playing player on cur with a near-end hook of
// the given lead that counts its runs.
func nearEndPlayer(t *testing.T, cur *source, lead time.Duration) (*Player, *atomic.Int32) {
	t.Helper()
	p := newPlayer(&mockOutput{})
	var runs atomic.Int32
	p.OnNearEnd(func() time.Duration { return lead }, func() { runs.Add(1) })
	p.cur.Store(cur)
	p.state.Store(int32(Playing))
	go p.manage()
	t.Cleanup(func() { p.Close(); cur.kill() })
	return p, &runs
}

// settle waits a few manager ticks.
func settle() { time.Sleep(150 * time.Millisecond) }

func atMS(ms int64) int64 { return ms * bytesPerSec / 1000 }

// The hook runs once when the remaining time reaches the lead, not before; a
// seek back out of the span arms it again; RearmNearEnd lets it run again
// inside the span.
func TestNearEndRunsOncePerSpan(t *testing.T) {
	p, runs := nearEndPlayer(t, testSource("MP3", 100000), 30*time.Second)
	p.played.Store(atMS(60000)) // 40 s left
	settle()
	if n := runs.Load(); n != 0 {
		t.Fatalf("hook ran %d times with 40 s left, lead 30 s", n)
	}
	p.played.Store(atMS(71000)) // 29 s left
	waitFor(t, "the hook", func() bool { return runs.Load() == 1 })
	p.played.Store(atMS(80000))
	settle()
	if n := runs.Load(); n != 1 {
		t.Fatalf("hook ran %d times inside one span, want 1", n)
	}
	p.played.Store(atMS(10000)) // seek back out
	settle()
	p.played.Store(atMS(90000)) // and in again
	waitFor(t, "the hook after a seek back", func() bool { return runs.Load() == 2 })
	p.RearmNearEnd()
	waitFor(t, "the hook after RearmNearEnd", func() bool { return runs.Load() == 3 })
	settle()
	if n := runs.Load(); n != 3 {
		t.Fatalf("hook ran %d times, want 3", n)
	}
}

// A change of gapless or crossfade runs the hook again inside the span, so a
// preload request it skipped while both were off is looked at again; setting
// the same value does not.
func TestNearEndRerunsOnTransitionChange(t *testing.T) {
	p, runs := nearEndPlayer(t, testSource("MP3", 100000), 30*time.Second)
	p.SetGapless(false) // on by default; off before the track enters its span
	p.played.Store(atMS(90000))
	waitFor(t, "the hook", func() bool { return runs.Load() == 1 })
	p.SetGapless(true)
	waitFor(t, "the hook after gapless on", func() bool { return runs.Load() == 2 })
	p.SetCrossfadeMS(3000)
	waitFor(t, "the hook after a crossfade change", func() bool { return runs.Load() == 3 })
	p.SetGapless(true)
	p.SetCrossfadeMS(3000)
	settle()
	if n := runs.Load(); n != 3 {
		t.Fatalf("hook ran %d times after unchanged settings, want 3", n)
	}
}

// A track shorter than the lead, or of unknown length, runs the hook at once.
func TestNearEndShortOrUnknownRunsAtOnce(t *testing.T) {
	for name, dur := range map[string]int64{"short": 10000, "unknown": 0} {
		t.Run(name, func(t *testing.T) {
			_, runs := nearEndPlayer(t, testSource("MP3", dur), 30*time.Second)
			waitFor(t, "the hook", func() bool { return runs.Load() == 1 })
		})
	}
}

// The decoder's length wins over the one passed in: a FLAC of 7.4 s passed as
// unknown does not run a 1 s hook at its start, while an MP3 (no length in the
// decoder) does.
func TestNearEndUsesDecoderLength(t *testing.T) {
	for _, tc := range []struct {
		format string
		data   []byte
		runs   int32
	}{
		{"MP3", makeMP3(250), 1},
		{"FLAC", makeFLAC(80, 4096), 0},
	} {
		t.Run(tc.format, func(t *testing.T) {
			s := testSource(tc.format, 0)
			if err := s.sb.write(tc.data); err != nil {
				t.Fatal(err)
			}
			s.sb.finish(nil)
			go s.decode()
			waitFor(t, "the decoder", func() bool { return s.ring.buffered() > 0 })
			_, runs := nearEndPlayer(t, s, time.Second)
			settle()
			if n := runs.Load(); n != tc.runs {
				t.Fatalf("hook ran %d times at the start, want %d", n, tc.runs)
			}
		})
	}
}
