package engine

// Single-device playback with the account's other sessions, over the starts announced
// on the live socket: local playback pauses on another start, local starts are
// announced, and another session's start shows until its track length has passed.

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/audio"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/bridge"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
	odlog "github.com/Cycl0o0/OpenDeezer/v3/internal/log"
)

const (
	// elsePoll is how often the listening history is read while another
	// session plays.
	elsePoll = 10 * time.Second
	// elseGrace keeps an announced track past its length, for the history's
	// entry of its end to arrive (up to 41 s after it, measured 2026-11-23).
	elseGrace = time.Minute
	// elseUnknownFor is how long another session counts as playing a track it
	// did not name, from that track's start, without a history entry.
	elseUnknownFor = 6 * time.Minute
	// elseAnnounceLag is how long before its announcement arrived a start may
	// lie in the history.
	elseAnnounceLag = time.Minute
	// elseHistoryN is how many history entries one read takes.
	elseHistoryN = 5
)

// elsewhere is what another session plays: the last start it announced, or,
// once the history shows that track ended, a track it did not name (no Track).
type elsewhere struct {
	Track   *bridge.Track `json:"track,omitempty"`
	SinceMS int64         `json:"sinceMs"` // when the track started (unix ms)
	UntilMS int64         `json:"untilMs"` // when it counts as ended
	session string
	dueMS   int64 // when the history is read for the track's end
	lastMS  int64 // the start of the history entry that moved the follow on last
}

var (
	elseMu       sync.Mutex
	elseCur      elsewhere
	elseStop     context.CancelFunc
	elseAnnounce func(trackID string) bool // the watched client's AnnouncePlay; nil while no watch
	elseVersion  atomic.Int64

	// playAlongside turns single-device playback off (SetSingleDevice).
	playAlongside atomic.Bool
)

// pauseForOther pauses local playback for another session's start (tests
// replace it: their player has no output to pause).
var pauseForOther = func() { withPlayerNotify(func(p *audio.Player) { p.Pause() }) }

// watchElsewhere follows the starts c's account announces and announces local
// ones through c, replacing the watch of a previous login.
func watchElsewhere(c *deezer.Client) {
	stopElsewhere()
	ctx, cancel := context.WithCancel(context.Background())
	elseMu.Lock()
	elseStop = cancel
	elseAnnounce = c.AnnouncePlay
	elseMu.Unlock()
	go c.WatchStreams(ctx, func(s deezer.StreamStart) { noteElsewhere(ctx, c, s) })
	go func() {
		tick := time.NewTicker(elsePoll)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				followElsewhere(ctx, c, time.Now().UnixMilli())
			}
		}
	}()
}

// stopElsewhere ends the watch and forgets the last start. The watch ends
// under elseMu, so a start noteElsewhere records after this sees it ended.
func stopElsewhere() {
	elseMu.Lock()
	if elseStop != nil {
		elseStop()
		elseStop = nil
	}
	elseAnnounce = nil
	had := elseCur != elsewhere{}
	elseCur = elsewhere{}
	elseMu.Unlock()
	if had {
		elseVersion.Add(1)
	}
}

// noteElsewhere pauses local playback for another session's start and records it with
// its metadata. A repeated announcement of the shown start is ignored, as is a start
// arriving after ctx ended.
func noteElsewhere(ctx context.Context, c *deezer.Client, s deezer.StreamStart) {
	now := time.Now().UnixMilli()
	elseMu.Lock()
	same := elseCur.Track != nil && elseCur.Track.ID == s.TrackID && elseCur.session == s.Session && now < elseCur.UntilMS
	elseMu.Unlock()
	if same || ctx.Err() != nil {
		return
	}
	odlog.Info("live: another player started track %s", s.TrackID)
	if !playAlongside.Load() {
		pauseForOther()
	}
	t, err := c.Track(s.TrackID)
	if err != nil {
		odlog.Warn("live: track %s: %v", s.TrackID, err)
		return
	}
	elseMu.Lock()
	if ctx.Err() != nil {
		elseMu.Unlock()
		return
	}
	bt := bridge.FromTrack(t)
	elseCur = elsewhere{Track: &bt, SinceMS: now, UntilMS: now + t.DurationMS + elseGrace.Milliseconds(),
		session: s.Session, dueMS: now + t.DurationMS - elsePoll.Milliseconds()}
	elseMu.Unlock()
	elseVersion.Add(1)
}

// followElsewhere reads the listening history while another session plays and
// its track's end is due. An entry for the shown track means it ended and the session
// moved to an unnamed track; that read marks the end.
func followElsewhere(ctx context.Context, c *deezer.Client, now int64) {
	elseMu.Lock()
	cur := elseCur
	elseMu.Unlock()
	if now >= cur.UntilMS || now < cur.dueMS {
		return
	}
	plays, err := c.RecentPlays(elseHistoryN)
	if err != nil {
		odlog.Warn("live: history: %v", err)
		return
	}
	for _, p := range plays {
		ended := p.StartMS > cur.lastMS
		if cur.Track != nil {
			ended = p.TrackID == cur.Track.ID && p.StartMS >= cur.SinceMS-elseAnnounceLag.Milliseconds()
		}
		if !ended {
			continue
		}
		elseMu.Lock()
		if ctx.Err() != nil || elseCur != cur {
			elseMu.Unlock()
			return
		}
		elseCur = elsewhere{SinceMS: now, UntilMS: now + elseUnknownFor.Milliseconds(), session: cur.session, dueMS: now, lastMS: p.StartMS}
		elseMu.Unlock()
		elseVersion.Add(1)
		odlog.Info("live: another player went on past track %s", p.TrackID)
		return
	}
}

// ElsewhereJSON returns what another session of the account plays, as
// {"track", "sinceMs", "untilMs"}, without "track" for a track it did not
// name; "{}" when nothing was announced since login.
func ElsewhereJSON() string {
	elseMu.Lock()
	cur := elseCur
	elseMu.Unlock()
	if cur == (elsewhere{}) {
		return "{}"
	}
	b, _ := json.Marshal(cur)
	return string(b)
}

// ElsewhereVersion returns a counter that moves exactly when ElsewhereJSON's
// payload changes, so a host fetches it only then.
func ElsewhereVersion() int64 { return elseVersion.Load() }

// announceLocal announces the current song to the account's other sessions, which
// pause on it, and forgets another session's shown start. Episodes are not announced.
// The send runs outside the player and queue locks.
func announceLocal() {
	t, kind := currentTrackSnapshot()
	if t.ID == "" || kind != "" || playAlongside.Load() {
		return
	}
	elseMu.Lock()
	send := elseAnnounce
	had := elseCur != elsewhere{}
	elseCur = elsewhere{}
	elseMu.Unlock()
	if had {
		elseVersion.Add(1)
	}
	if send != nil {
		go send(t.ID)
	}
}

// announceIfPlaying announces the current song once p plays (or loads) again.
func announceIfPlaying(p *audio.Player) {
	if s := p.State(); s == audio.Playing || s == audio.Loading {
		announceLocal()
	}
}

// SetSingleDevice turns single-device playback on (default) or off and saves it. On,
// another session's start pauses local playback and local song starts are announced;
// off, other sessions' starts are only shown.
func SetSingleDevice(on bool) {
	playAlongside.Store(!on)
	savePlayback(func(pb *config.Playback) { pb.PlayAlongside = !on })
}

// SingleDevice reports whether single-device playback is on.
func SingleDevice() bool { return !playAlongside.Load() }
