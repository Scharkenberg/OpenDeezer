// Package queue is the playback queue model shared by the TUI and the C API
// (via corelib), so shuffle/repeat/prev-history behavior is defined once.
package queue

import (
	"math/rand"
	"slices"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
)

// Repeat is the loop mode.
type Repeat int

const (
	RepeatOff Repeat = iota
	RepeatAll
	RepeatOne
)

func (r Repeat) String() string {
	switch r {
	case RepeatAll:
		return "all"
	case RepeatOne:
		return "one"
	default:
		return "off"
	}
}

// maxHistory bounds the visited-index history so an endless shuffle+RepeatAll
// session can't grow it without limit.
const maxHistory = 500

// Queue holds an ordered track list plus a cursor, shuffle/repeat state and a
// visited-index history (so Prev under shuffle retraces the real path). The zero
// value is a valid empty queue. Not safe for concurrent use; callers serialize.
type Queue struct {
	tracks  []deezer.Track
	index   int
	repeat  Repeat
	shuffle bool
	history []int

	// version increments on every change of the track list (Set, Append, Remove, Move,
	// InsertAfterCurrent) but not on cursor moves, so callers rebuild derived snapshots
	// only when it changes.
	version uint64

	// order is a shuffled permutation of track indices and pos the current track's place
	// in it, so shuffle plays every track once per cycle. Kept only while shuffle is on;
	// order[pos] == index.
	order []int
	pos   int

	// intn is rand.Intn by default; tests override for determinism.
	intn func(n int) int
}

// New returns an empty queue.
func New() *Queue { return &Queue{index: -1, intn: rand.Intn} }

func (q *Queue) rnd(n int) int {
	if q.intn == nil {
		q.intn = rand.Intn
	}
	return q.intn(n)
}

// pushHistory records a visited index, keeping history bounded by maxHistory.
// -1 (no current track) is not a place Prev can return to and is skipped.
func (q *Queue) pushHistory(i int) {
	if i < 0 {
		return
	}
	q.history = append(q.history, i)
	if len(q.history) > maxHistory {
		// Drop the oldest entries into a fresh backing array so memory is bounded.
		q.history = append(q.history[:0:0], q.history[len(q.history)-maxHistory:]...)
	}
}

// shuffleOrder rebuilds order as a random permutation with `first` at position 0 (when
// in range, else fully random) and resets pos to 0.
func (q *Queue) shuffleOrder(first int) {
	n := len(q.tracks)
	q.order = make([]int, 0, n)
	pinned := first >= 0 && first < n
	if pinned {
		q.order = append(q.order, first)
	}
	for i := 0; i < n; i++ {
		if pinned && i == first {
			continue
		}
		q.order = append(q.order, i)
	}
	// Fisher–Yates over the tail, leaving a pinned first element in place.
	k := 0
	if pinned {
		k = 1
	}
	for i := len(q.order) - 1; i > k; i-- {
		j := k + q.rnd(i-k+1)
		q.order[i], q.order[j] = q.order[j], q.order[i]
	}
	q.pos = 0
}

// ensureOrder rebuilds the shuffle permutation if it has drifted out of sync
// with the track list (e.g. after InsertAfterCurrent with no current track).
func (q *Queue) ensureOrder() {
	if len(q.order) != len(q.tracks) {
		q.shuffleOrder(q.index)
	}
}

// syncPos repositions pos onto the current index within order after the cursor
// moved outside of Next (Prev history retrace, manual pick).
func (q *Queue) syncPos() {
	if !q.shuffle {
		return
	}
	for i, idx := range q.order {
		if idx == q.index {
			q.pos = i
			return
		}
	}
}

// Set replaces the queue contents with a copy of tracks and positions the
// cursor at start (clamped). History is cleared.
func (q *Queue) Set(tracks []deezer.Track, start int) {
	q.tracks = slices.Clone(tracks)
	q.version++
	q.history = nil
	q.order = nil
	if len(tracks) == 0 {
		q.index = -1
		return
	}
	if start < 0 {
		start = 0
	} else if start >= len(tracks) {
		start = len(tracks) - 1
	}
	q.index = start
	if q.shuffle {
		q.shuffleOrder(q.index)
	}
}

// Append adds tracks to the end without moving the cursor. Under shuffle the
// new tracks go to random positions after the current one, so the part of the
// cycle already played is not replayed.
func (q *Queue) Append(tracks ...deezer.Track) {
	if len(tracks) == 0 {
		return
	}
	n := len(q.tracks)
	q.tracks = append(q.tracks, tracks...)
	q.version++
	if q.shuffle && len(q.order) == n {
		lo := q.pos + 1
		if q.index < 0 {
			lo = 0
		}
		for i := n; i < len(q.tracks); i++ {
			q.order = slices.Insert(q.order, lo+q.rnd(len(q.order)-lo+1), i)
		}
	}
}

// InsertAfterCurrent inserts tracks right after the current one (at the head when there
// is none) without moving the cursor and returns the position. Under shuffle they are
// placed right after the current position in the cycle too.
func (q *Queue) InsertAfterCurrent(tracks ...deezer.Track) int {
	if len(tracks) == 0 {
		return -1
	}
	at := q.index + 1 // index == -1 (empty/no current) → insert at 0
	k := len(tracks)
	n := len(q.tracks)
	q.tracks = append(q.tracks, tracks...) // grow, then shift the tail right
	copy(q.tracks[at+k:], q.tracks[at:n])
	copy(q.tracks[at:at+k], tracks)
	q.version++
	// History entries at/after the insertion point name shifted tracks.
	for hi, h := range q.history {
		if h >= at {
			q.history[hi] = h + k
		}
	}
	if q.shuffle {
		if len(q.order) == n && q.index >= 0 {
			// Renumber the existing permutation, then splice the new indices in
			// right after the current position so they play next.
			for oi, idx := range q.order {
				if idx >= at {
					q.order[oi] = idx + k
				}
			}
			ins := make([]int, k)
			for i := range ins {
				ins[i] = at + i
			}
			q.order = append(q.order[:q.pos+1], append(ins, q.order[q.pos+1:]...)...)
		} else {
			q.order = nil // drifted; rebuilt lazily by ensureOrder
		}
	}
	return at
}

// Remove deletes the track at i and reports whether it did. The cursor follows its
// track; removing the current one keeps the cursor in place (clamped). History is
// remapped and the cycle loses the index.
func (q *Queue) Remove(i int) bool {
	if i < 0 || i >= len(q.tracks) {
		return false
	}
	q.tracks = append(q.tracks[:i], q.tracks[i+1:]...)
	q.version++
	hist := q.history[:0]
	for _, h := range q.history {
		if h == i {
			continue
		}
		if h > i {
			h--
		}
		hist = append(hist, h)
	}
	q.history = hist
	if len(q.tracks) == 0 {
		q.index = -1
		q.order = nil
		q.pos = 0
		return true
	}
	if q.index > i {
		q.index--
	} else if q.index >= len(q.tracks) {
		q.index = len(q.tracks) - 1
	}
	if q.shuffle {
		order := q.order[:0]
		for _, idx := range q.order {
			if idx == i {
				continue
			}
			if idx > i {
				idx--
			}
			order = append(order, idx)
		}
		q.order = order
		q.syncPos() // restore the order[pos] == index invariant
	}
	return true
}

// Move relocates the track at i to position j, reporting whether it did. The
// cursor, history and shuffle cycle all follow the tracks they referenced, so
// reordering never changes what's playing or what Prev retraces to.
func (q *Queue) Move(i, j int) bool {
	n := len(q.tracks)
	if i < 0 || i >= n || j < 0 || j >= n || i == j {
		return false
	}
	t := q.tracks[i]
	q.tracks = append(q.tracks[:i], q.tracks[i+1:]...)
	q.tracks = append(q.tracks, deezer.Track{})
	copy(q.tracks[j+1:], q.tracks[j:])
	q.tracks[j] = t
	q.version++
	// Remap an index through the remove-at-i + insert-at-j shuffle of positions.
	remap := func(x int) int {
		if x == i {
			return j
		}
		if x > i {
			x--
		}
		if x >= j {
			x++
		}
		return x
	}
	q.index = remap(q.index)
	for hi, h := range q.history {
		q.history[hi] = remap(h)
	}
	for oi, idx := range q.order {
		q.order[oi] = remap(idx) // order[pos]==index is preserved by the remap
	}
	return true
}

// Version returns the mutation counter for the track-list contents. See the
// field doc: bumped by Set/Append/Remove/Move/InsertAfterCurrent only.
func (q *Queue) Version() uint64 { return q.version }

// Tracks returns the underlying slice (read-only; do not mutate).
func (q *Queue) Tracks() []deezer.Track { return q.tracks }

// Len reports the number of queued tracks.
func (q *Queue) Len() int { return len(q.tracks) }

// Index returns the current cursor (−1 if empty).
func (q *Queue) Index() int { return q.index }

// Current returns the current track and whether one exists.
func (q *Queue) Current() (deezer.Track, bool) {
	if q.index < 0 || q.index >= len(q.tracks) {
		return deezer.Track{}, false
	}
	return q.tracks[q.index], true
}

// SetIndex moves the cursor (clamped) for a user pick, recording the previous position
// so Prev retraces. To align with a row already playing, use AlignIndex.
func (q *Queue) SetIndex(i int) {
	if i < 0 || i >= len(q.tracks) || i == q.index {
		return
	}
	if q.index >= 0 {
		q.pushHistory(q.index)
	}
	q.index = i
	q.syncPos()
}

// AlignIndex moves the cursor (clamped) without recording history, for a client that
// owns its queue: Set(tracks, 0) then AlignIndex(N). Real jumps use SetIndex.
func (q *Queue) AlignIndex(i int) {
	if i < 0 || i >= len(q.tracks) || i == q.index {
		return
	}
	q.index = i
	q.syncPos()
}

// Mark is the cursor and history at one moment, for Restore.
type Mark struct {
	index   int
	history []int
}

// Mark records the cursor and history.
func (q *Queue) Mark() Mark { return Mark{q.index, slices.Clone(q.history)} }

// Restore puts the cursor and history back as m recorded them.
func (q *Queue) Restore(m Mark) {
	q.index, q.history = m.index, m.history
	q.syncPos()
}

// Shuffle / Repeat accessors.
func (q *Queue) Shuffle() bool { return q.shuffle }
func (q *Queue) SetShuffle(on bool) {
	q.shuffle = on
	if on {
		q.shuffleOrder(q.index)
	} else {
		q.order = nil
	}
}
func (q *Queue) ToggleShuffle() bool { q.SetShuffle(!q.shuffle); return q.shuffle }
func (q *Queue) Repeat() Repeat      { return q.repeat }
func (q *Queue) SetRepeat(r Repeat)  { q.repeat = r }
func (q *Queue) CycleRepeat() Repeat { q.repeat = (q.repeat + 1) % 3; return q.repeat }

// Next advances the cursor by the shuffle and repeat rules and reports whether it
// moved to a playable track. RepeatOne advances normally here (see AdvanceAuto).
func (q *Queue) Next() bool {
	if len(q.tracks) == 0 {
		return false
	}
	if q.shuffle && len(q.tracks) > 1 {
		q.ensureOrder()
		if q.index < 0 { // nothing current yet: the cycle starts at its head
			q.pos = 0
			q.index = q.order[0]
			return true
		}
		if q.pos+1 < len(q.order) {
			q.pushHistory(q.index)
			q.pos++
			q.index = q.order[q.pos]
			return true
		}
		// End of the shuffled cycle: stop under RepeatOff (matching linear
		// semantics), reshuffle and continue under RepeatAll.
		if q.repeat != RepeatAll {
			return false
		}
		q.pushHistory(q.index)
		q.reshuffleWrap()
		q.index = q.order[q.pos]
		return true
	}
	ni := q.index + 1
	if ni >= len(q.tracks) {
		if q.repeat != RepeatAll {
			return false
		}
		ni = 0
	}
	q.pushHistory(q.index)
	q.index = ni
	return true
}

// reshuffleWrap builds a fresh shuffled cycle for RepeatAll, avoiding an
// immediate repeat of the track that just finished.
func (q *Queue) reshuffleWrap() {
	prev := q.index
	q.shuffleOrder(-1)
	if len(q.order) > 1 && q.order[0] == prev {
		q.order[0], q.order[1] = q.order[1], q.order[0]
	}
}

// Prev steps back, retracing shuffle history when present.
func (q *Queue) Prev() bool {
	if len(q.tracks) == 0 {
		return false
	}
	if n := len(q.history); n > 0 {
		q.index = q.history[n-1]
		q.history = q.history[:n-1]
		q.syncPos()
		return true
	}
	if q.index > 0 {
		q.index--
		q.syncPos()
		return true
	}
	return false
}

// PeekNext returns the track Next would advance to without changing the queue, only
// in the deterministic cases (linear, RepeatAll wrap); ok is false under shuffle or
// RepeatOne. Callers use it to decide on a gapless preload.
func (q *Queue) PeekNext() (deezer.Track, bool) {
	if len(q.tracks) == 0 || q.shuffle || q.repeat == RepeatOne {
		return deezer.Track{}, false
	}
	ni := q.nextLinear()
	if ni < 0 {
		return deezer.Track{}, false
	}
	return q.tracks[ni], true
}

// nextLinear is the index after the cursor, or the first after the last under RepeatAll;
// -1 when there is none.
func (q *Queue) nextLinear() int {
	if q.index+1 < len(q.tracks) {
		return q.index + 1
	}
	if q.repeat == RepeatAll {
		return 0
	}
	return -1
}

// AdvanceLinear moves the cursor to the track PeekNext returned (linear +1 or a
// RepeatAll wrap), ignoring shuffle and RepeatOne, to follow a gapless swap. It reports
// whether it moved.
func (q *Queue) AdvanceLinear() bool {
	if len(q.tracks) == 0 {
		return false
	}
	ni := q.nextLinear()
	if ni < 0 {
		return false
	}
	q.pushHistory(q.index)
	q.index = ni
	q.syncPos()
	return true
}

// AdvanceAuto is called when a track ends naturally: RepeatOne replays the
// current track (reports true, cursor unchanged); otherwise it behaves like
// Next. Returns whether playback should continue.
func (q *Queue) AdvanceAuto() bool {
	if q.repeat == RepeatOne {
		_, ok := q.Current()
		return ok
	}
	return q.Next()
}
