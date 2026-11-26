package preload

import (
	"errors"
	"testing"
	"time"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/audio"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
)

// counting is a Preloader on a gapless player whose resolve counts its runs
// and answers the request as the track.
func counting() (*Preloader, *audio.Player, *int) {
	p := &audio.Player{}
	p.SetGapless(true)
	n := 0
	l := New(func() *audio.Player { return p }, func(req deezer.Track) (*deezer.StreamPlan, deezer.Track, error) {
		n++
		return &deezer.StreamPlan{TrackID: req.ID}, req, nil
	})
	return l, p, &n
}

// A request is resolved by the hook, once per track; a new track is resolved
// again; the armed track is taken once.
func TestRequestResolvesOncePerTrack(t *testing.T) {
	l, _, n := counting()
	l.Request("2", 1000)
	l.Request("2", 1000)
	if *n != 0 {
		t.Fatalf("%d resolves before the hook, want 0", *n)
	}
	l.Resolve()
	l.Request("2", 1000) // already armed
	l.Resolve()
	if *n != 1 {
		t.Fatalf("%d resolves for one track, want 1", *n)
	}
	l.Request("3", 2000)
	l.Resolve()
	if *n != 2 {
		t.Fatalf("%d resolves after a new request, want 2", *n)
	}
	if got, ok := l.Take(); !ok || got.ID != "3" || got.DurationMS != 2000 {
		t.Fatalf("Take = %+v %v, want 3 of 2000 ms", got, ok)
	}
	if _, ok := l.Take(); ok {
		t.Fatal("the armed track was taken twice")
	}
}

// Clear drops the request and the armed track.
func TestClearDropsRequestAndArmed(t *testing.T) {
	l, _, n := counting()
	l.Request("2", 0)
	l.Resolve()
	l.Request("3", 0)
	l.Clear()
	l.Resolve()
	if *n != 1 {
		t.Fatalf("%d resolves after a clear, want 1", *n)
	}
	if _, ok := l.Take(); ok {
		t.Fatal("an armed track survived Clear")
	}
}

// A cached plan whose entry is gone is planned again outside the lock Clear
// takes: a re-plan that hangs does not hold up Clear, and the preload it was
// for is not armed.
func TestBlockingReplanDoesNotHoldClear(t *testing.T) {
	p := &audio.Player{}
	p.SetGapless(true)
	entered, release := make(chan struct{}), make(chan struct{})
	l := New(func() *audio.Player { return p }, func(req deezer.Track) (*deezer.StreamPlan, deezer.Track, error) {
		return &deezer.StreamPlan{TrackID: req.ID, Format: "MP3_128", Refresh: func() (*deezer.StreamPlan, error) {
			close(entered)
			<-release
			return nil, errors.New("offline")
		}}, req, nil
	})
	l.Request("2", 0)
	resolved := make(chan struct{})
	go func() { l.Resolve(); close(resolved) }()
	<-entered
	cleared := make(chan struct{})
	go func() { l.Clear(); close(cleared) }()
	select {
	case <-cleared:
	case <-time.After(5 * time.Second):
		t.Fatal("Clear waited for a re-plan")
	}
	close(release)
	<-resolved
	if _, ok := l.Take(); ok {
		t.Fatal("a preload cleared during its re-plan was armed")
	}
}

// With gapless and crossfade off nothing is resolved and the request stays;
// a failed resolve arms nothing and keeps the request too.
func TestResolveWaitsForTransitionAndKeepsFailed(t *testing.T) {
	l, p, n := counting()
	p.SetGapless(false)
	l.Request("2", 0)
	l.Resolve()
	if *n != 0 {
		t.Fatalf("%d resolves with gapless and crossfade off, want 0", *n)
	}
	p.SetCrossfadeMS(2000)
	l.Resolve()
	if *n != 1 {
		t.Fatalf("%d resolves with a crossfade, want 1", *n)
	}

	fail := New(func() *audio.Player { return p }, func(deezer.Track) (*deezer.StreamPlan, deezer.Track, error) {
		return nil, deezer.Track{}, errors.New("offline")
	})
	fail.Request("4", 0)
	fail.Resolve()
	if _, ok := fail.Take(); ok {
		t.Fatal("a failed resolve armed a track")
	}
}
