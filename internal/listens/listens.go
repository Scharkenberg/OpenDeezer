// Package listens reports plays to Deezer's listen log as its players do: at each
// start gw log.listen carries the ended listen and the starting track. A listen that
// cannot be sent waits in listens.json for one log.batch_listen.
package listens

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/audio"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/config"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	pdlog "github.com/Scharkenberg/PurpleDisc/v4/internal/log"
)

// Context is the list a track was played from, as Deezer's players name it in
// the log (ctxt): the page type, such as "playlist_page", "album_page",
// "profile_loved" or "dynamic_page_user_radio", and the id of what it shows.
type Context struct {
	Type string `json:"t,omitempty"`
	ID   string `json:"id,omitempty"`
}

// radio reports a context Deezer logs as a radio (listen type 1): Flow, mixes
// and radios, as opposed to tracks picked on demand.
func (c Context) radio() bool {
	for _, s := range []string{"radio", "flow", "mix"} {
		if strings.Contains(c.Type, s) {
			return true
		}
	}
	return false
}

// Start is a track that starts playing.
type Start struct {
	ID      string
	Episode bool // a podcast episode, else a song
	Context Context
	Shuffle bool
	Repeat  int // 0 off, 1 all, 2 one
}

// Sender sends the log: a logged-in client, which adds the device (the web
// player's build) to each listen, and keeps Deezer's clock.
type Sender interface {
	LogListen(params json.RawMessage, next *deezer.ListenMedia) error
	LogListens(params []json.RawMessage) error
	ServerNow() time.Time
}

// Log follows the tracks a player plays and reports them.
type Log struct {
	sender func() Sender // the online client, nil while offline
	grace  time.Duration // how long a listen that ended waits for the next start

	mu    sync.Mutex
	open  *session        // the track playing
	ended json.RawMessage // the listen that ended, until the next start or the grace
	timer *time.Timer

	qMu      sync.Mutex
	queue    []job // sends waiting, in order
	draining bool  // a goroutine is sending them
	sends    sync.WaitGroup
}

type session struct {
	start  Start
	ts     int64 // when it started (unix s)
	stream string
}

// New makes a Log that sends through sender's client (nil while offline).
func New(sender func() Sender) *Log {
	return &Log{sender: sender, grace: 5 * time.Second}
}

// Started reports that s starts playing: the listen that ended goes with it.
func (l *Log) Started(s Start) {
	// On Deezer's clock, as the web player stamps it; offline, on ours.
	ts := time.Now()
	if snd := l.sender(); snd != nil {
		ts = snd.ServerNow()
	}
	l.mu.Lock()
	ended := l.takeEndedLocked()
	l.open = &session{start: s, ts: ts.Unix(), stream: deezer.NewUUID()}
	l.mu.Unlock()
	t := "song"
	if s.Episode {
		t = "episode"
	}
	l.send(ended, &deezer.ListenMedia{ID: s.ID, Type: t})
}

// Ended reports what was heard of a track that stopped playing. A track this
// log did not see start is not reported.
func (l *Log) Ended(li audio.Listen) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.open == nil || l.open.start.ID != li.TrackID {
		return
	}
	if prev := l.takeEndedLocked(); prev != nil {
		l.send(prev, nil)
	}
	l.ended = params(l.open, li)
	l.open = nil
	l.timer = time.AfterFunc(l.grace, func() {
		l.mu.Lock()
		ended := l.takeEndedLocked()
		l.mu.Unlock()
		if ended != nil {
			l.send(ended, nil)
		}
	})
}

// takeEndedLocked returns the listen that ended and forgets it.
func (l *Log) takeEndedLocked() json.RawMessage {
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
	p := l.ended
	l.ended = nil
	return p
}

// Flush sends the listens kept for later, when there is a client.
func (l *Log) Flush() { l.send(nil, nil) }

// send queues params and next (either may be nil) for the sender goroutine,
// which sends them after those before (send).
func (l *Log) send(params json.RawMessage, next *deezer.ListenMedia) {
	l.sends.Add(1)
	l.qMu.Lock()
	l.queue = append(l.queue, job{params, next})
	idle := !l.draining
	l.draining = true
	l.qMu.Unlock()
	if idle {
		go l.drain()
	}
}

// job is one send.
type job struct {
	params json.RawMessage
	next   *deezer.ListenMedia
}

// drain sends the queued jobs in order, then ends. With no client, or when a
// send fails for want of a network, its params are kept for later; one the gateway
// refuses is dropped. After a send that worked, the listens kept go in one batch,
// which is likewise kept only when the network failed.
func (l *Log) drain() {
	for {
		l.qMu.Lock()
		if len(l.queue) == 0 {
			l.draining = false
			l.qMu.Unlock()
			return
		}
		j := l.queue[0]
		l.queue = l.queue[1:]
		l.qMu.Unlock()
		s := l.sender()
		keep := s == nil
		if s != nil && (j.params != nil || j.next != nil) {
			if err := s.LogListen(j.params, j.next); err != nil {
				keep = deezer.IsNoNetwork(err)
				if !keep {
					pdlog.Warn("listen log: dropped: %v", err)
				}
				s = nil
			}
		}
		kept := config.LoadListens()
		switch {
		case keep && j.params != nil:
			if err := config.SaveListens(append(kept, j.params)); err != nil {
				pdlog.Warn("listens.json: %v", err)
			}
		case s != nil && len(kept) > 0:
			err := s.LogListens(kept)
			if err != nil && deezer.IsNoNetwork(err) {
				break
			}
			if err != nil {
				pdlog.Warn("listen log batch: dropped: %v", err)
			}
			if err := config.SaveListens(nil); err != nil {
				pdlog.Warn("listens.json: %v", err)
			}
		}
		l.sends.Done()
	}
}

// wait waits for the sends under way (tests).
func (l *Log) wait() { l.sends.Wait() }

// params is the log entry of the listen li of the track s started.
func params(s *session, li audio.Listen) json.RawMessage {
	media := "song"
	if s.start.Episode {
		media = "episode"
	}
	b2i := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	p := map[string]any{
		"media":      map[string]any{"id": s.start.ID, "type": media, "format": li.Format},
		"type":       b2i(s.start.Context.radio()),
		"stat":       map[string]any{"seek": li.Seeks, "pause": li.Pauses, "sync": b2i(li.Cached)},
		"lt":         li.HeardMS / 1000,
		"ctxt":       s.start.Context,
		"payload":    map[string]any{},
		"ls":         []any{},
		"ts_listen":  s.ts,
		"is_shuffle": s.start.Shuffle,
		"stream_id":  s.stream,
	}
	if li.Preview {
		p["l_30sec"] = 1
	}
	switch s.start.Repeat {
	case 1:
		p["repeat_type"] = "repeat_all"
	case 2:
		p["repeat_type"] = "repeat_one"
	}
	b, _ := json.Marshal(p)
	return b
}
