package audio

import (
	"fmt"
	"io"
	"math"
	"testing"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
)

type mockOutput struct {
	lostHandler func(string)
}

func (m *mockOutput) start(read func(out []byte) int) error { return nil }
func (m *mockOutput) devices() ([]Device, error)            { return nil, nil }
func (m *mockOutput) setDevice(id string) error             { return nil }
func (m *mockOutput) currentDevice() string                 { return "" }
func (m *mockOutput) suspend(on bool) error                 { return nil }
func (m *mockOutput) setLostHandler(fn func(string))        { m.lostHandler = fn }
func (m *mockOutput) close()                                {}
func (m *mockOutput) deviceDown() bool                      { return false }
func (m *mockOutput) latencyFrames() int                    { return 0 }

type latencyMockOutput struct {
	mockOutput
	latency int
}

func (l *latencyMockOutput) latencyFrames() int { return l.latency }

// testSource is a plain source without the download pipeline: a test writes
// the stream itself.
func testSource(format string, durMS int64) *source {
	return newSource(&deezer.StreamPlan{Format: format}, durMS)
}

func fullScale(n int) []byte {
	b := make([]byte, n)
	for i := 0; i+1 < len(b); i += 2 {
		b[i] = 0xFF
		b[i+1] = 0x7F
	}
	return b
}

func maxAbs(b []byte) int {
	m := 0
	for i := 0; i+1 < len(b); i += 2 {
		v := int(int16(uint16(b[i]) | uint16(b[i+1])<<8))
		if v < 0 {
			v = -v
		}
		if v > m {
			m = v
		}
	}
	return m
}

// waitFor polls cond until it holds or two seconds pass.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestErroredFinishFailsAndFiresOnFinish(t *testing.T) {
	p := newPlayer(&mockOutput{})
	go p.manage()
	defer p.Close()

	src := newSource(&deezer.StreamPlan{Format: "MP3"}, 1000)
	src.setErr(fmt.Errorf("test decode error"))
	src.eof.Store(true)

	finished := make(chan bool, 1)
	p.SetOnFinish(func() { finished <- true })
	p.cur.Store(src)
	p.state.Store(int32(Playing))

	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("onFinish was not called on errored finish")
	}
	if p.State() != Errored {
		t.Errorf("player state = %v, want Errored", p.State())
	}
	if p.LastError() != "test decode error" {
		t.Errorf("player lastErr = %q, want %q", p.LastError(), "test decode error")
	}
}

func TestSetOnFinishWhilePlayingIsRaceFree(t *testing.T) {
	p := newPlayer(&mockOutput{})
	go p.manage()
	defer p.Close()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			p.SetOnFinish(func() {})
			p.SetOnFinish(nil)
		}
		close(done)
	}()
	p.cur.Store(newSource(&deezer.StreamPlan{Format: "MP3"}, 1000))
	p.state.Store(int32(Playing))
	<-done
}

func TestLostDeviceFailsPlayer(t *testing.T) {
	out := &mockOutput{}
	p := newPlayer(out)
	out.lostHandler("device gone")
	if p.State() != Errored || p.LastError() != "device gone" {
		t.Fatalf("state %v, lastErr %q after a lost device", p.State(), p.LastError())
	}
}

func TestPositionMSLatencyCompensation(t *testing.T) {
	// 10000 frames of output latency = 40000 bytes.
	p := newPlayer(&latencyMockOutput{latency: 10000})
	p.played.Store(20000)
	if pos := p.PositionMS(); pos != 0 {
		t.Errorf("PositionMS() = %d, want 0 (clamped)", pos)
	}
	p.played.Store(80000) // (80000 - 40000) * 1000 / 176400 = 226 ms
	if pos := p.PositionMS(); pos != 226 {
		t.Errorf("PositionMS() = %d, want 226", pos)
	}
}

// TestReadPCMShortReadTailSilent: the tail after a short read (end of track,
// underrun) is silence, not stale ring audio at full level.
func TestReadPCMShortReadTailSilent(t *testing.T) {
	p := newPlayer(&mockOutput{})
	p.volume.Store(math.Float64bits(0.3))
	s := testSource("MP3", 100000)
	defer s.kill()
	s.ring.write(fullScale(ringMax), s.ring.seq())
	p.cur.Store(s)
	p.state.Store(int32(Playing))
	buf := make([]byte, 1764)
	for s.ring.buffered() > 0 {
		p.readPCM(buf)
	}
	s.ring.write(fullScale(1000), s.ring.seq())
	n := p.readPCM(buf)
	if m := maxAbs(buf[n:]); m != 0 {
		t.Fatalf("tail after n=%d has peak %d, want silence", n, m)
	}
}

// TestSeekPastDecodableEndFinishesTrack: the duration (whole seconds) exceeds
// the decoded length; a seek into the difference while the decoder is still
// mid-track ends the track instead of decoding on from the old position. The
// decoder of either format may panic on such a seek; that is a seek error too.
func TestSeekPastDecodableEndFinishesTrack(t *testing.T) {
	for _, tc := range []struct {
		format string
		data   []byte
		seekMS int64
	}{
		{"MP3", makeMP3(250), 7500},        // 6.53s
		{"FLAC", makeFLAC(80, 4096), 7800}, // 7.43s
	} {
		t.Run(tc.format, func(t *testing.T) {
			p := newPlayer(&mockOutput{})
			s := testSource(tc.format, 8000)
			if err := s.sb.write(tc.data); err != nil {
				t.Fatal(err)
			}
			s.sb.finish(nil)
			go s.decode()
			defer s.kill()
			p.cur.Store(s)
			p.totalMS.Store(8000)
			p.state.Store(int32(Paused))
			waitFor(t, "a full ring", func() bool { return s.ring.buffered() == ringMax })
			if s.eof.Load() {
				t.Fatal("decoder reached the end before the seek")
			}
			p.SeekMS(tc.seekMS)
			waitFor(t, "the track to end", func() bool { return s.eof.Load() && s.ring.buffered() == 0 })
		})
	}
}

// TestManageIdlesWhileStoppedOrPaused: the manager does not tick while nothing
// plays, wakes when a track loads, and keeps ticking while paused with a sleep
// timer armed.
func TestManageIdlesWhileStoppedOrPaused(t *testing.T) {
	p := newPlayer(&mockOutput{})
	go p.manage()
	defer p.Close()
	time.Sleep(200 * time.Millisecond)
	if n := p.ticks.Load(); n != 0 {
		t.Fatalf("%d ticks while stopped", n)
	}
	s := testSource("MP3", 100000)
	defer s.kill()
	p.cur.Store(s)
	p.state.Store(int32(Loading))
	s.ring.write(make([]byte, prebufferB), s.ring.seq())
	waitFor(t, "Playing", func() bool { return p.State() == Playing })
	p.state.Store(int32(Paused))
	time.Sleep(100 * time.Millisecond)
	idle := p.ticks.Load()
	time.Sleep(200 * time.Millisecond)
	if n := p.ticks.Load() - idle; n > 0 {
		t.Fatalf("%d ticks while paused", n)
	}
	// An end-of-track timer acts only when a track ends: none ends while
	// paused, so the manager still waits.
	p.SetSleepTimer(0, true)
	time.Sleep(100 * time.Millisecond)
	idle = p.ticks.Load()
	time.Sleep(200 * time.Millisecond)
	if n := p.ticks.Load() - idle; n > 0 {
		t.Fatalf("%d ticks while paused with an end-of-track timer", n)
	}
	p.SetSleepTimer(50*time.Millisecond, false)
	waitFor(t, "the sleep timer to expire while paused", func() bool { return !p.SleepActive() })
}

// TestResumeFromErroredWithoutSource: Resume after a failed track (no source)
// must not report Playing.
func TestResumeFromErroredWithoutSource(t *testing.T) {
	p := newPlayer(&mockOutput{})
	go p.manage()
	defer p.Close()
	s := testSource("MP3", 1000)
	s.setErr(io.ErrUnexpectedEOF)
	s.eof.Store(true)
	p.cur.Store(s)
	p.state.Store(int32(Playing))
	waitFor(t, "the failed track to finish", func() bool { return p.cur.Load() == nil })
	p.Resume()
	if p.State() == Playing {
		t.Fatalf("Resume with no source: state=%v", p.State())
	}
}

// TestPauseDuringFinishKeepsStopped: a Pause whose fade outlasts the finish
// must not turn the finished track into Paused.
func TestPauseDuringFinishKeepsStopped(t *testing.T) {
	p := newPlayer(&mockOutput{})
	go p.manage()
	defer p.Close()
	s := testSource("MP3", 100000)
	s.eof.Store(true) // decoded and drained: finishes on the next tick
	p.cur.Store(s)
	p.state.Store(int32(Playing))
	p.Pause()
	if p.State() != Stopped {
		t.Fatalf("state after Pause during finish = %v, want Stopped", p.State())
	}
}

// loadingPlayer is a managed player Loading a source with nothing buffered.
func loadingPlayer(t *testing.T) (*Player, *source) {
	p := newPlayer(&mockOutput{})
	go p.manage()
	t.Cleanup(p.Close)
	s := testSource("MP3", 100000)
	p.cur.Store(s)
	p.state.Store(int32(Loading))
	return p, s
}

// TestPauseWhileLoading: a pause asked for while Loading holds once the track
// is buffered.
func TestPauseWhileLoading(t *testing.T) {
	p, s := loadingPlayer(t)
	p.Pause()
	s.ring.write(make([]byte, prebufferB), s.ring.seq())
	waitFor(t, "Paused", func() bool { return p.State() == Paused })
}

// TestTogglePauseWhileLoadingToggles: two toggles while Loading cancel out.
func TestTogglePauseWhileLoadingToggles(t *testing.T) {
	p, s := loadingPlayer(t)
	p.TogglePause()
	p.TogglePause()
	s.ring.write(make([]byte, prebufferB), s.ring.seq())
	waitFor(t, "Playing", func() bool { return p.State() == Playing })
}

// TestSleepTimerExpiresWhileLoading: a sleep deadline that passes while the
// track is loading pauses it once buffered.
func TestSleepTimerExpiresWhileLoading(t *testing.T) {
	p, s := loadingPlayer(t)
	p.SetSleepTimer(50*time.Millisecond, false)
	waitFor(t, "the sleep timer to fire", func() bool { return !p.SleepActive() })
	s.ring.write(make([]byte, prebufferB), s.ring.seq())
	waitFor(t, "Paused", func() bool { return p.State() == Paused })
}

// A seek rewinds a next track already mixed in when the current one's length
// is unknown, and with only the decoder's length it rewinds out of the
// crossfade window but not inside it.
func TestSeekRewindsNextWithoutPassedLength(t *testing.T) {
	for _, tc := range []struct {
		name   string
		decDur int64
		seekMS int64
		rewind bool
	}{
		{"unknown length", 0, 30000, true},
		{"decoder length, out of the window", 100000, 30000, true},
		{"decoder length, inside the window", 100000, 97000, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newPlayer(&mockOutput{})
			cur := testSource("MP3", 0)
			next := testSource("MP3", 100000)
			defer cur.kill()
			defer next.kill()
			cur.decDurMS.Store(tc.decDur)
			p.cur.Store(cur)
			p.next.Store(next)
			p.crossfadeMS.Store(5000)
			next.xfadeConsumed.Store(1000)
			p.state.Store(int32(Paused)) // skip the fade-out wait
			p.SeekMS(tc.seekMS)
			if rewound := next.xfadeConsumed.Load() == 0; rewound != tc.rewind {
				t.Fatalf("next rewound %v, want %v", rewound, tc.rewind)
			}
		})
	}
}

// TestCrossfadeSeekBackRewindsNext: seeking back out of the crossfade window
// rewinds next, whose intro was already mixed in.
func TestCrossfadeSeekBackRewindsNext(t *testing.T) {
	p := newPlayer(&mockOutput{})
	cur := testSource("MP3", 100000)
	next := testSource("MP3", 100000)
	defer cur.kill()
	defer next.kill()
	cur.ring.write(fullScale(ringMax), cur.ring.seq())
	next.ring.write(fullScale(ringMax), next.ring.seq())
	p.cur.Store(cur)
	p.next.Store(next)
	p.totalMS.Store(100000)
	p.crossfadeMS.Store(5000)
	p.played.Store(96 * bytesPerSec) // 1s into the 5s window
	p.state.Store(int32(Playing))
	buf := make([]byte, bytesPerSec)
	p.readPCM(buf)
	p.readPCM(buf)
	if next.xfadeConsumed.Load() == 0 {
		t.Fatal("crossfade did not read from next")
	}
	p.state.Store(int32(Paused)) // skip the fade-out wait
	p.SeekMS(30000)
	if c := next.xfadeConsumed.Load(); c != 0 {
		t.Fatalf("next.xfadeConsumed after seeking back = %d, want 0", c)
	}
}

// TestGaplessNoGap: the callback that drains cur continues straight into next.
func TestGaplessNoGap(t *testing.T) {
	p := newPlayer(&mockOutput{})
	cur := testSource("MP3", 100000)
	next := testSource("MP3", 100000)
	cur.ring.write(fullScale(1000), cur.ring.seq())
	cur.decoded.Store(1000)
	cur.eof.Store(true)
	next.ring.write(fullScale(ringMax), next.ring.seq())
	p.cur.Store(cur)
	p.next.Store(next)
	p.state.Store(int32(Playing))
	defer p.Close()
	buf := make([]byte, 1764)
	if n := p.readPCM(buf); n != len(buf) {
		t.Fatalf("last callback of cur: n=%d of %d with next ready", n, len(buf))
	}
	if got := p.played.Load(); got != 1000 {
		t.Fatalf("played = %d, want cur's 1000 bytes only", got)
	}
	go p.manage()
	waitFor(t, "the swap to next", func() bool { return p.cur.Load() == next })
	if got := p.played.Load(); got != int64(len(buf)-1000) {
		t.Fatalf("played after swap = %d, want %d (next's bytes already heard)", got, len(buf)-1000)
	}
}

// TestGaplessFillHoldsNextWhenItMustNotPlay: an armed end-of-track sleep timer,
// or a current track that failed before decoding anything, must not let the
// callback fill the buffer from next.
func TestGaplessFillHoldsNextWhenItMustNotPlay(t *testing.T) {
	for name, setup := range map[string]func(p *Player, cur *source){
		"end-of-track sleep": func(p *Player, cur *source) {
			cur.ring.write(fullScale(1000), cur.ring.seq())
			cur.decoded.Store(1000)
			p.SetSleepTimer(0, true)
		},
		"failed before decoding": func(p *Player, cur *source) {
			cur.setErr(io.ErrUnexpectedEOF)
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := newPlayer(&mockOutput{})
			cur := testSource("MP3", 100000)
			next := testSource("MP3", 100000)
			defer cur.kill()
			defer next.kill()
			setup(p, cur)
			cur.eof.Store(true)
			next.ring.write(fullScale(ringMax), next.ring.seq())
			p.cur.Store(cur)
			p.next.Store(next)
			p.state.Store(int32(Playing))
			p.readPCM(make([]byte, 1764))
			if c := next.xfadeConsumed.Load(); c != 0 {
				t.Fatalf("callback played %d bytes of next", c)
			}
		})
	}
}

// TestConcurrentPlayKeepsMetadata: overlapping Play calls leave cur with its
// own duration and format published.
func TestConcurrentPlayKeepsMetadata(t *testing.T) {
	for i := 0; i < 300; i++ {
		p := newPlayer(&mockOutput{})
		// No CDN URL and no cache: download ends at once with ErrOffline.
		a := &deezer.StreamPlan{Format: "MP3_A", Encrypted: true, TrackID: "1"}
		b := &deezer.StreamPlan{Format: "MP3_B", Encrypted: true, TrackID: "2"}
		done := make(chan struct{}, 2)
		go func() { _ = p.Play(a, 111000); done <- struct{}{} }()
		go func() { _ = p.Play(b, 222000); done <- struct{}{} }()
		<-done
		<-done
		cur := p.cur.Load()
		if cur != nil && (cur.durMS != p.totalMS.Load() || cur.plan().Format != p.Format()) {
			t.Fatalf("iteration %d: cur %s/%d, published %s/%d", i, cur.plan().Format, cur.durMS, p.Format(), p.totalMS.Load())
		}
		p.Stop()
	}
}
