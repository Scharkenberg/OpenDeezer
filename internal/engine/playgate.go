package engine

import (
	"errors"
	"sync"
	"sync/atomic"
)

// playGate orders the plays: every play path claims a sequence number on entry and
// startPlay starts only the newest claim, so a slow resolve never overrides a later
// track.
type playGate struct {
	seq atomic.Uint64
	// startMu makes the superseded check and the start one step, so two
	// resolves finishing together cannot both start (newest request wins).
	startMu sync.Mutex
	// user counts user plays still resolving: a finish meanwhile must not
	// auto-advance.
	user atomic.Int32
}

// plays is the engine's play gate.
var plays playGate

// errSuperseded is what a play returns when a newer one claimed a seq before
// it could start.
var errSuperseded = errors.New("superseded by a newer play")

// claim takes a fresh sequence number.
func (g *playGate) claim() uint64 { return g.seq.Add(1) }

// superseded reports whether a newer play claimed a seq since seq, i.e. an
// in-flight resolve should drop its result.
func (g *playGate) superseded(seq uint64) bool { return g.seq.Load() != seq }

// beginUser claims a seq for a user play and holds off the engine's
// auto-advance until done is called.
func (g *playGate) beginUser() (seq uint64, done func()) {
	g.user.Add(1)
	return g.claim(), func() { g.user.Add(-1) }
}
