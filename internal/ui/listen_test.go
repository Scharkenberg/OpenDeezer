package ui

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/audio"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/listens"
)

type sentListens struct {
	mu   sync.Mutex
	logs []string
}

func (s *sentListens) LogListen(params json.RawMessage, next *deezer.ListenMedia) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := ""
	if next != nil {
		n = next.ID
	}
	s.logs = append(s.logs, string(params)+"|"+n)
	return nil
}

func (s *sentListens) LogListens([]json.RawMessage) error { return nil }

func (s *sentListens) ServerNow() time.Time { return time.Now() }

func (s *sentListens) wait(n int) []string {
	deadline := time.Now().Add(3 * time.Second)
	for {
		s.mu.Lock()
		got := append([]string(nil), s.logs...)
		s.mu.Unlock()
		if len(got) >= n || time.Now().After(deadline) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A track played from a browsed list reports that list to the listen log.
func TestPlayedListReachesListenLog(t *testing.T) {
	useTempConfig(t)
	sent := &sentListens{}
	m := interactionModel(screenList)
	m.listens = listens.New(func() listens.Sender { return sent })
	ts := testTracks(2)
	m.update(tracksMsg{title: "Pl", tracks: ts, ctx: listens.Context{Type: "playlist_page", ID: "42"}})
	m.playBrowsed(ts[0], false)
	m.onTrackChanged(ts[0])
	m.listens.Ended(audio.Listen{TrackID: ts[0].ID, HeardMS: 3000})
	m.onTrackChanged(ts[1])
	got := sent.wait(2)
	if len(got) != 2 || got[0] != "|"+ts[0].ID || !strings.Contains(got[1], `"ctxt":{"t":"playlist_page","id":"42"}`) ||
		!strings.HasSuffix(got[1], "|"+ts[1].ID) {
		t.Fatalf("listen log %q", got)
	}
}
