package listens

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/audio"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
)

// fakeSender records what a client would send.
type fakeSender struct {
	mu      sync.Mutex
	singles []string // params|next
	batches [][]string
	fail    bool // a lost network
	refuse  bool // the gateway's refusal
}

func (f *fakeSender) LogListen(params json.RawMessage, next *deezer.ListenMedia) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return fmt.Errorf("%w: dial tcp", deezer.ErrNoNetwork)
	}
	if f.refuse {
		return errors.New("deezer gw log.listen: invalid listen")
	}
	n := ""
	if next != nil {
		n = next.Type + ":" + next.ID
	}
	f.singles = append(f.singles, string(params)+"|"+n)
	return nil
}

// ServerNow is Deezer's clock: an hour ahead of the test's.
func (f *fakeSender) ServerNow() time.Time { return time.Unix(1795443600, 0) }

func (f *fakeSender) LogListens(params []json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var b []string
	for _, p := range params {
		b = append(b, string(p))
	}
	f.batches = append(f.batches, b)
	return nil
}

func useTempConfig(t *testing.T) {
	t.Helper()
	config.SetBaseDir(t.TempDir())
	t.Cleanup(func() { config.SetBaseDir("") })
}

func newLog(s *fakeSender) *Log {
	l := New(func() Sender {
		if s == nil {
			return nil
		}
		return s
	})
	l.grace = 20 * time.Millisecond
	return l
}

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("params %s: %v", s, err)
	}
	return m
}

// A start sends the track as next_media; the next start sends the listen that
// ended with it, in the fields Deezer's web player sends: its start on Deezer's
// clock, l_30sec only for a preview (the client adds the device).
func TestStartSendsEndedListenWithNext(t *testing.T) {
	useTempConfig(t)
	s := &fakeSender{}
	l := newLog(s)
	l.Started(Start{ID: "7", Context: Context{Type: "playlist_page", ID: "908622995"}, Shuffle: true, Repeat: 1})
	l.Ended(audio.Listen{TrackID: "7", Format: "MP3_320", HeardMS: 61500, Pauses: 2, Seeks: 1, Cached: true})
	l.Started(Start{ID: "8", Episode: true})
	l.wait()
	if len(s.singles) != 2 || s.singles[0] != "|song:7" {
		t.Fatalf("sent %q", s.singles)
	}
	params, next, _ := cut(s.singles[1])
	if next != "episode:8" {
		t.Fatalf("next_media %q, want episode:8", next)
	}
	p := decode(t, params)
	media := p["media"].(map[string]any)
	stat := p["stat"].(map[string]any)
	ctxt := p["ctxt"].(map[string]any)
	if media["id"] != "7" || media["type"] != "song" || media["format"] != "MP3_320" ||
		p["lt"] != 61.0 || p["type"] != 0.0 || p["l_30sec"] != nil || p["ts_listen"] != 1795443600.0 || p["dev"] != nil ||
		stat["pause"] != 2.0 || stat["seek"] != 1.0 || stat["sync"] != 1.0 ||
		ctxt["t"] != "playlist_page" || ctxt["id"] != "908622995" ||
		p["is_shuffle"] != true || p["repeat_type"] != "repeat_all" ||
		p["stream_id"] == "" || p["payload"] == nil || p["ls"] == nil {
		t.Fatalf("params %s", params)
	}
}

// A listen that no start follows is sent alone, once the grace has passed.
func TestEndWithoutStartSendsAlone(t *testing.T) {
	useTempConfig(t)
	s := &fakeSender{}
	l := newLog(s)
	l.Started(Start{ID: "7", Context: Context{Type: "dynamic_page_user_radio", ID: "1"}})
	l.Ended(audio.Listen{TrackID: "7", Format: "MP3_128", HeardMS: 1000, Preview: true})
	time.Sleep(5 * l.grace)
	l.wait()
	if len(s.singles) != 2 {
		t.Fatalf("sent %q", s.singles)
	}
	params, next, _ := cut(s.singles[1])
	p := decode(t, params)
	if next != "" || p["type"] != 1.0 || p["l_30sec"] != 1.0 || p["repeat_type"] != nil || p["is_shuffle"] != false {
		t.Fatalf("sent %s with next %q; want a Flow listen alone (type 1, a preview)", params, next)
	}
}

// A listen that cannot be sent (no client, or the request failed) is kept and
// goes in one log.batch_listen once a send works again.
func TestUnsentListensBatched(t *testing.T) {
	useTempConfig(t)
	s := &fakeSender{fail: true}
	l := newLog(s)
	l.Started(Start{ID: "7"})
	l.Ended(audio.Listen{TrackID: "7", HeardMS: 30000})
	l.Started(Start{ID: "8"})
	l.wait()
	off := newLog(nil) // offline: no client
	off.Started(Start{ID: "9"})
	off.Ended(audio.Listen{TrackID: "9", HeardMS: 40000})
	time.Sleep(5 * off.grace)
	off.wait()
	if got := config.LoadListens(); len(got) != 2 {
		t.Fatalf("%d listens kept, want 2", len(got))
	}
	s.fail = false
	l.Flush()
	l.wait()
	if len(s.batches) != 1 || len(s.batches[0]) != 2 {
		t.Fatalf("batches %q, want both kept listens in one", s.batches)
	}
	if got := config.LoadListens(); len(got) != 0 {
		t.Fatalf("%d listens still kept after the batch", len(got))
	}
}

// An end of a track the log did not see start (another host's play) is not
// reported.
func TestEndOfUnknownTrackIgnored(t *testing.T) {
	useTempConfig(t)
	s := &fakeSender{}
	l := newLog(s)
	l.Started(Start{ID: "7"})
	l.Ended(audio.Listen{TrackID: "5", HeardMS: 1000})
	time.Sleep(5 * l.grace)
	l.wait()
	if len(s.singles) != 1 {
		t.Fatalf("sent %q, want only the start", s.singles)
	}
}

func cut(s string) (params, next string, ok bool) {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '|' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

// A listen the gateway refuses is dropped, not kept to be sent again after every track.
func TestRefusedListenDropped(t *testing.T) {
	useTempConfig(t)
	s := &fakeSender{refuse: true}
	l := newLog(s)
	l.Started(Start{ID: "7"})
	l.Ended(audio.Listen{TrackID: "7", HeardMS: 30000})
	l.Started(Start{ID: "8"})
	l.wait()
	if got := config.LoadListens(); len(got) != 0 {
		t.Fatalf("%d refused listens kept", len(got))
	}
	s.refuse = false
	l.Ended(audio.Listen{TrackID: "8", HeardMS: 30000})
	l.Started(Start{ID: "9"})
	l.wait()
	if len(s.batches) != 0 {
		t.Fatalf("batches %q after a refusal", s.batches)
	}
}
