package ui

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/control"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/history"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/listens"
	tea "github.com/charmbracelet/bubbletea"
)

// controlQueueEditMsg is a control API queue edit. The hook blocks on reply until the
// update loop applied it, so the handler can answer an error with 409 Conflict.
type controlQueueEditMsg struct {
	kind  string       // add | jump | remove | move
	track deezer.Track // for add (already fetched off-loop)
	next  bool         // for add: insert after current instead of appending
	i, j  int          // indices (move uses both)
	reply chan<- error // buffered(1); the update loop never blocks sending
}

// controlReplyTimeout bounds how long a control HTTP handler waits for the
// update loop to apply a queue edit (the loop normally answers within a tick).
const controlReplyTimeout = 5 * time.Second

// buildControlCommands builds the control callbacks for the control and web-remote
// servers. Commands and queue edits run on the update loop via send; fetches run on the
// HTTP goroutine; HistoryRecent reads the locked history store.
func buildControlCommands(send func(tea.Msg), client *deezer.Client, hist *history.Store) control.Commands {
	queueEdit := func(msg controlQueueEditMsg, reply chan error) error {
		send(msg)
		select {
		case err := <-reply:
			return err
		case <-time.After(controlReplyTimeout):
			return errors.New("timed out waiting for the player")
		}
	}
	edit := func(kind string, i, j int) error {
		reply := make(chan error, 1)
		return queueEdit(controlQueueEditMsg{kind: kind, i: i, j: j, reply: reply}, reply)
	}
	playNow := func(ctx listens.Context, fetch func() ([]deezer.Track, error)) error {
		if client == nil {
			return errors.New("not available")
		}
		ts, err := fetch()
		if err != nil {
			return err
		}
		if len(ts) == 0 {
			return errors.New("no tracks found")
		}
		send(playNowMsg{tracks: ts, ctx: ctx})
		return nil
	}
	return control.Commands{
		PlayPause:     func() { send(controlCmdMsg{kind: "playpause"}) },
		Next:          func() { send(controlCmdMsg{kind: "next"}) },
		Prev:          func() { send(controlCmdMsg{kind: "prev"}) },
		Stop:          func() { send(controlCmdMsg{kind: "stop"}) },
		Restart:       func() { send(controlCmdMsg{kind: "restart"}) },
		CycleRepeat:   func() { send(controlCmdMsg{kind: "repeat"}) },
		ToggleShuffle: func() { send(controlCmdMsg{kind: "shuffle"}) },
		// SET variants: a GUI/web/SDK controller can command an absolute repeat
		// mode / shuffle state (not just a cycle/toggle). Applied on the update
		// loop like the Cycle/Toggle kinds so the TUI honors BOTH command forms.
		SetRepeat:    func(mode string) { send(controlCmdMsg{kind: "repeat-set", mode: mode}) },
		SetShuffle:   func(on bool) { send(controlCmdMsg{kind: "shuffle-set", on: on}) },
		Seek:         func(ms int64) { send(controlCmdMsg{kind: "seek", ms: ms}) },
		SetVolume:    func(v float64) { send(controlCmdMsg{kind: "volume", vol: v}) },
		PlayTrack:    func(id string) { send(controlCmdMsg{kind: "playtrack", id: id}) },
		PlayPlaylist: func(id string) { send(controlCmdMsg{kind: "playplaylist", id: id}) },
		SetSleepTimer: func(minutes int, eot bool) {
			send(controlCmdMsg{kind: "sleep", ms: int64(minutes), eot: eot})
		},
		CancelSleepTimer: func() { send(controlCmdMsg{kind: "sleepcancel"}) },

		QueueAdd: func(id string, next bool) error {
			if client == nil {
				return errors.New("not available")
			}
			t, err := client.Track(id)
			if err != nil {
				return err
			}
			reply := make(chan error, 1)
			return queueEdit(controlQueueEditMsg{kind: "add", track: t, next: next, reply: reply}, reply)
		},
		QueueJump:   func(i int) error { return edit("jump", i, 0) },
		QueueRemove: func(i int) error { return edit("remove", i, 0) },
		QueueMove:   func(from, to int) error { return edit("move", from, to) },
		PlayAlbum: func(id string) error {
			return playNow(listens.Context{Type: "album_page", ID: id}, func() ([]deezer.Track, error) { return client.AlbumTracks(id) })
		},
		PlayMixTrack: func(id string) error {
			return playNow(listens.Context{Type: "track_mix_contextual", ID: id}, func() ([]deezer.Track, error) { return client.TrackMix(id) })
		},
		PlayMixArtist: func(id string) error {
			return playNow(listens.Context{Type: "artist_smartradio", ID: id}, func() ([]deezer.Track, error) { return client.ArtistMix(id) })
		},
		HistoryRecent: func(n int) (json.RawMessage, error) {
			if hist == nil {
				return json.RawMessage(`[]`), nil
			}
			es, err := hist.Recent(n)
			if err != nil {
				return nil, err
			}
			return json.Marshal(es)
		},
	}
}

// handleControlQueueEdit applies a control API queue edit on the update loop like the
// queue-view keys (remove refuses the playing row) and reports the result on the reply
// channel.
func (m *Model) handleControlQueueEdit(msg controlQueueEditMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var err error
	switch msg.kind {
	case "add":
		switch {
		case msg.track.ID == "":
			err = errors.New("unknown track")
		case m.episodeMode && m.q.Len() > 0:
			err = errors.New("queue is playing podcast episodes")
		default:
			if m.q.Len() == 0 {
				m.episodeMode = false
			}
			if msg.next {
				m.q.InsertAfterCurrent(msg.track)
			} else {
				m.q.Append(msg.track)
			}
			m.invalidatePreload()
		}
	case "jump":
		if msg.i < 0 || msg.i >= m.q.Len() {
			err = errors.New("queue index out of range")
		} else {
			cmd = m.queueJump(msg.i)
		}
	case "remove":
		switch {
		case msg.i < 0 || msg.i >= m.q.Len():
			err = errors.New("queue index out of range")
		case msg.i == m.q.Index():
			err = errors.New("can't remove the playing track")
		default:
			cmd = m.queueRemove(msg.i)
		}
	case "move":
		n := m.q.Len()
		if msg.i < 0 || msg.i >= n || msg.j < 0 || msg.j >= n {
			err = errors.New("queue index out of range")
		} else if msg.i != msg.j {
			cmd = m.queueMove(msg.i, msg.j)
		}
	default:
		err = errors.New("unknown queue edit")
	}
	// Publish the refreshed snapshot before sending the reply, so the handler serializes
	// the edited queue. Neither line can block: publishControl is nil-guarded and the reply
	// channel is buffered.
	m.publishControl()
	if msg.reply != nil {
		msg.reply <- err // buffered(1) by the sender; never blocks the loop
	}
	return m, cmd
}
