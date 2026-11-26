package engine

// Podcast progress for local playback, as Deezer's Android app keeps it: the bookmark
// is written every 30 s of media time, on pause or stop and when another track starts;
// episodes start at their bookmark. Connect devices record their own.

import (
	"maps"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/audio"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/history"
	pdlog "github.com/Scharkenberg/PurpleDisc/v4/internal/log"
)

// episodeBookmark is a bookmark to write: an episode, where it stopped and
// its length (seconds), and whether that counts as heard.
type episodeBookmark struct {
	id         string
	posS, durS int
	heard      bool
}

// episodeMarker follows the playing episode for its bookmark.
type episodeMarker struct {
	cur      episodeBookmark // the episode followed ("" none) and its last position
	writtenS int             // the position last written
	playing  bool
}

// step takes the playing state and returns the bookmarks to write.
func (m *episodeMarker) step(id string, episode, playing bool, posS, durS int) []episodeBookmark {
	var out []episodeBookmark
	write := func(b episodeBookmark) {
		if b.posS >= 10 { // under 10 s the episode was not started
			b.heard = deezer.EpisodeHeard(b.posS, b.durS)
			out = append(out, b)
			m.writtenS = b.posS
		}
	}
	if m.cur.id != "" && (!episode || id != m.cur.id) {
		if m.cur.posS != m.writtenS {
			write(m.cur) // where the episode was left
		}
		m.cur = episodeBookmark{}
	}
	if !episode {
		return out
	}
	if m.cur.id == "" {
		m.cur, m.writtenS, m.playing = episodeBookmark{id: id}, posS, playing
	}
	m.cur.posS, m.cur.durS = posS, durS
	paused := m.playing && !playing
	m.playing = playing
	if d := posS - m.writtenS; paused || d >= 30 || d <= -30 {
		write(m.cur)
	}
	return out
}

var (
	// episodes is the local player's episode marker; episodeLoop steps it.
	episodes   episodeMarker
	episodesMu sync.Mutex
	// episodeWake asks episodeLoop for a step: every state change (pause, stop, a new track).
	episodeWake = make(chan struct{}, 1)
	episodeOnce sync.Once
	// episodeSpeed is the speed episodes play at, as float64 bits (0: normal).
	episodeSpeed atomic.Uint64
)

// SetPlaybackSpeed sets the speed episodes play at, from 0.5 to 2 (1 is
// normal; Deezer's apps step it by 0.25), pitch not kept; at once when an
// episode plays. Songs always play at normal speed.
func SetPlaybackSpeed(speed float64) {
	speed = min(max(speed, 0.5), 2)
	episodeSpeed.Store(math.Float64bits(speed))
	if _, kind := currentTrackSnapshot(); kind == history.KindEpisode {
		withPlayerNotify(func(p *audio.Player) { p.SetSpeed(speed) })
	}
}

// PlaybackSpeed returns the speed episodes play at.
func PlaybackSpeed() float64 {
	if b := episodeSpeed.Load(); b != 0 {
		return math.Float64frombits(b)
	}
	return 1
}

var (
	// marks are the account's episode bookmarks as last read (EpisodeBookmarks)
	// and written here since; marksRead says they were read.
	marksMu   sync.Mutex
	marks     map[string]deezer.EpisodeBookmark
	marksRead bool
)

// EpisodeBookmarks reads the account's episode bookmarks and keeps them for
// the episode plays that follow.
func EpisodeBookmarks() (map[string]deezer.EpisodeBookmark, error) {
	c, err := Online()
	if err != nil {
		return nil, err
	}
	got, err := c.EpisodeBookmarks()
	if err != nil {
		return nil, err
	}
	marksMu.Lock()
	marks, marksRead = got, true
	out := maps.Clone(got)
	marksMu.Unlock()
	return out, nil
}

// MarkEpisodePlayed marks an episode as played or not, and keeps it so.
func MarkEpisodePlayed(id string, played bool) error {
	c, err := Online()
	if err != nil {
		return err
	}
	if err := c.MarkEpisodePlayed(id, played); err != nil {
		return err
	}
	keepMark(id, deezer.EpisodeBookmark{Heard: played})
	return nil
}

// keepMark records a bookmark among those read.
func keepMark(id string, b deezer.EpisodeBookmark) {
	marksMu.Lock()
	if marks == nil {
		marks = map[string]deezer.EpisodeBookmark{}
	}
	marks[id] = b
	marksMu.Unlock()
}

// forgetMarks drops the bookmarks of the account signing out or replaced.
func forgetMarks() {
	marksMu.Lock()
	marks, marksRead = nil, false
	marksMu.Unlock()
}

// episodeMark is the bookmark of an episode, read once per account.
func episodeMark(id string) deezer.EpisodeBookmark {
	marksMu.Lock()
	read, b := marksRead, marks[id]
	marksMu.Unlock()
	if read {
		return b
	}
	got, _ := EpisodeBookmarks()
	return got[id]
}

// episodeProgress steps the episode marker with the local player's state and
// writes what it returns.
func episodeProgress(p *audio.Player) {
	if routedRemote() != nil {
		return
	}
	t, kind := currentTrackSnapshot()
	dur := p.DurationMS()
	if dur <= 0 {
		dur = t.DurationMS
	}
	episodesMu.Lock()
	st := p.State()
	marks := episodes.step(t.ID, kind == history.KindEpisode, st == audio.Playing || st == audio.Loading, int(p.PositionMS()/1000), int(dur/1000))
	episodesMu.Unlock()
	c := netClient()
	if len(marks) == 0 || c == nil {
		return
	}
	go func() {
		for _, b := range marks {
			if err := c.SetEpisodeBookmark(b.id, b.posS, b.durS, b.heard); err != nil {
				pdlog.Warn("episode bookmark %s: %v", b.id, err)
				continue
			}
			keepMark(b.id, deezer.EpisodeBookmark{OffsetS: b.posS, Heard: b.heard})
		}
	}()
}

// episodeTick is how often a playing episode's position is looked at between state changes.
const episodeTick = 30 * time.Second

// ensureEpisodeLoop starts episodeLoop once.
func ensureEpisodeLoop() { episodeOnce.Do(func() { go episodeLoop() }) }

// episodeLoop steps the episode marker at each state change, and every episodeTick while an
// episode plays or loads here; nothing wakes it otherwise.
func episodeLoop() {
	tick := time.NewTimer(episodeTick)
	tick.Stop()
	for {
		select {
		case <-episodeWake:
		case <-tick.C:
		}
		p := Player()
		if p == nil {
			continue
		}
		episodeProgress(p)
		_, kind := currentTrackSnapshot()
		if st := p.State(); kind == history.KindEpisode && (st == audio.Playing || st == audio.Loading) && routedRemote() == nil {
			tick.Reset(episodeTick)
		} else {
			tick.Stop()
		}
	}
}
