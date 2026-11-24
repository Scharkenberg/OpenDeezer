package audio

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
)

// listens records the Listens a player reports.
type listens struct {
	mu  sync.Mutex
	got []Listen
}

func (l *listens) add(v Listen) {
	l.mu.Lock()
	l.got = append(l.got, v)
	l.mu.Unlock()
}

func (l *listens) all() []Listen {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Listen(nil), l.got...)
}

func playingSource(p *Player, id string, durMS int64) *source {
	s := newSource(&deezer.StreamPlan{TrackID: id, Format: "MP3_320"}, durMS)
	s.ring.write(fullScale(ringMax), s.ring.seq())
	s.decoded.Store(ringMax)
	p.cur.Store(s)
	p.totalMS.Store(durMS)
	p.state.Store(int32(Playing))
	return s
}

// A stopped track reports what was heard of it: the audio played, not its
// position (a seek forward skips what was not heard), with its pauses and
// seeks.
func TestStopReportsListen(t *testing.T) {
	p := newPlayer(&mockOutput{})
	defer p.Close()
	var l listens
	p.SetOnListenEnd(l.add)
	s := playingSource(p, "7", 200000)
	buf := make([]byte, bytesPerSec/2)
	p.readPCM(buf) // 0.5 s
	p.Pause()
	p.Resume()
	p.readPCM(buf) // 1 s
	p.SeekMS(150000)
	s.ring.write(fullScale(bytesPerSec), s.ring.seq()) // as the decoder refills it from the seek
	p.readPCM(buf)                                     // 1.5 s heard, at 150.5 s
	p.Pause()
	p.Stop()
	got := l.all()
	if len(got) != 1 {
		t.Fatalf("%d listens, want 1", len(got))
	}
	if g := got[0]; g.TrackID != "7" || g.Format != "MP3_320" || g.Pauses != 2 || g.Seeks != 1 || g.Cached || g.Preview ||
		g.HeardMS < 1400 || g.HeardMS > 1600 {
		t.Fatalf("listen %+v, want track 7, MP3_320, 2 pauses, 1 seek, about 1500 ms heard", g)
	}
}

// A play replacing the current track reports the replaced one.
func TestPlayReportsReplacedListen(t *testing.T) {
	p := newPlayer(&mockOutput{})
	defer p.Close()
	var l listens
	p.SetOnListenEnd(l.add)
	playingSource(p, "7", 200000)
	p.readPCM(make([]byte, bytesPerSec))
	p.state.Store(int32(Paused)) // skip the fade-out wait
	if err := p.Play(&deezer.StreamPlan{TrackID: "8", Format: "MP3_128"}, 100000); err != nil {
		t.Fatal(err)
	}
	if got := l.all(); len(got) != 1 || got[0].TrackID != "7" || got[0].HeardMS != 1000 {
		t.Fatalf("listens %+v, want track 7 with 1000 ms heard", got)
	}
}

// A gapless hand-over reports the finished track before onFinish, and the
// next one counts what the hand-over already played of it.
func TestHandOverReportsListen(t *testing.T) {
	p := newPlayer(&mockOutput{})
	defer p.Close()
	var l listens
	var atFinish atomic.Int32
	p.SetOnListenEnd(l.add)
	p.SetOnFinish(func() { atFinish.Store(int32(len(l.all()))) })
	cur := playingSource(p, "7", 100000)
	cur.ring.flush()
	cur.ring.write(fullScale(1000), cur.ring.seq())
	cur.eof.Store(true)
	next := newSource(&deezer.StreamPlan{TrackID: "8", Format: "MP3_128"}, 100000)
	next.ring.write(fullScale(ringMax), next.ring.seq())
	p.next.Store(next)
	buf := make([]byte, 1764)
	p.readPCM(buf)
	go p.manage()
	waitFor(t, "the swap to next", func() bool { return p.cur.Load() == next })
	waitFor(t, "onFinish", func() bool { return atFinish.Load() > 0 })
	got := l.all()
	if len(got) != 1 || got[0].TrackID != "7" || atFinish.Load() != 1 {
		t.Fatalf("listens %+v (%d at onFinish), want track 7 reported before onFinish", got, atFinish.Load())
	}
	if h := next.heard.Load(); h != int64(len(buf)-1000) {
		t.Fatalf("next heard %d bytes, want the %d the hand-over played", h, len(buf)-1000)
	}
}

// A track that ends with nothing after it, or stops at an end-of-track sleep
// timer, is reported too.
func TestEndReportsListen(t *testing.T) {
	for _, eot := range []bool{false, true} {
		p := newPlayer(&mockOutput{})
		var l listens
		p.SetOnListenEnd(l.add)
		cur := playingSource(p, "7", 100000)
		cur.ring.flush()
		cur.ring.write(fullScale(bytesPerSec), cur.ring.seq())
		cur.eof.Store(true)
		if eot {
			p.SetSleepTimer(0, true)
		}
		p.readPCM(make([]byte, bytesPerSec))
		go p.manage()
		waitFor(t, "the end", func() bool { return len(l.all()) == 1 })
		if got := l.all()[0]; got.TrackID != "7" || got.HeardMS != 1000 {
			t.Errorf("end-of-track sleep %v: listen %+v, want track 7 with 1000 ms heard", eot, got)
		}
		p.Close()
	}
}
