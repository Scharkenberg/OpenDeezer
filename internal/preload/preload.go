// Package preload is the next-track preload the engine hosts share (the
// desktop library, the mobile binding and the terminal UI): a host records
// which track follows the playing one (Request), and the player's near-end
// hook (Attach) resolves and arms it once the playing track nears its end.
package preload

import (
	"sync"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/audio"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
)

// Resolver turns a request (id, and length when known) into the plan to arm
// and the track it plays, with its length when known.
type Resolver func(req deezer.Track) (*deezer.StreamPlan, deezer.Track, error)

// Preloader holds the request not yet armed and the track armed on the
// player.
type Preloader struct {
	player  func() *audio.Player // the player to arm, nil before there is one
	resolve Resolver

	mu    sync.Mutex
	req   deezer.Track // the request not yet armed
	gen   uint64       // counts requests, arms and drops: a stale resolve arms nothing
	armed deezer.Track // the track armed on the player
	// armMu makes a resolve's last check and its arming one step against
	// Clear.
	armMu sync.Mutex
}

// New returns a Preloader that arms on player() what resolve answers.
func New(player func() *audio.Player, resolve Resolver) *Preloader {
	return &Preloader{player: player, resolve: resolve}
}

// Attach makes pl run Resolve once its playing track has max(30 s,
// crossfade + 20 s) left (at once for a shorter track or one of unknown
// length).
func (l *Preloader) Attach(pl *audio.Player) {
	pl.OnNearEnd(func() time.Duration {
		return max(30*time.Second, time.Duration(pl.CrossfadeMS())*time.Millisecond+20*time.Second)
	}, l.Resolve)
}

// Request records id as the track to preload after the playing one; the
// near-end hook resolves it. A request for the track already recorded or
// armed changes nothing.
func (l *Preloader) Request(id string, durationMS int64) {
	l.mu.Lock()
	same := id == l.req.ID || id == l.armed.ID
	if !same {
		l.req = deezer.Track{ID: id, DurationMS: durationMS}
		l.gen++
	}
	l.mu.Unlock()
	if p := l.player(); !same && p != nil {
		p.RearmNearEnd()
	}
}

// Resolve is the near-end hook: it resolves the recorded request and arms it
// on the player, unless the request was replaced or dropped meanwhile. With
// gapless and crossfade both off it resolves nothing (the player would drop
// the preload) and the request stays: a change of either setting runs the
// hook again. A resolve that fails leaves the track to the host's advance,
// as without a preload.
func (l *Preloader) Resolve() {
	l.mu.Lock()
	req, gen := l.req, l.gen
	l.mu.Unlock()
	p := l.player()
	if req.ID == "" || p == nil || (!p.Gapless() && p.CrossfadeMS() == 0) {
		return
	}
	plan, t, err := l.resolve(req)
	if err != nil {
		return
	}
	plan = p.ReplanIfGone(plan) // can block: before armMu, so Clear never waits for it
	l.armMu.Lock()
	defer l.armMu.Unlock()
	l.mu.Lock()
	current := gen == l.gen
	if current {
		l.req = deezer.Track{}
		l.gen++
		l.armed = t
	}
	l.mu.Unlock()
	if current {
		p.Preload(plan, t.DurationMS)
	}
}

// Clear drops the request, the player's armed preload and its identity
// together, and makes a resolve in flight arm nothing. For every path where
// the upcoming track is no longer determined (queue edits, repeat or shuffle
// changes, a new play).
func (l *Preloader) Clear() {
	l.armMu.Lock()
	defer l.armMu.Unlock()
	l.mu.Lock()
	l.req = deezer.Track{}
	l.armed = deezer.Track{}
	l.gen++
	l.mu.Unlock()
	if p := l.player(); p != nil {
		p.ClearPreload()
	}
}

// Take returns and forgets the armed track. The player consumes its preload
// on every promote, so the identity is single-use.
func (l *Preloader) Take() (deezer.Track, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.armed
	l.armed = deezer.Track{}
	return t, t.ID != ""
}
