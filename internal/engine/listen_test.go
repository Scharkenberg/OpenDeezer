package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/audio"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/config"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
)

// logsOf waits for n listen log calls and returns them.
func logsOf(t *testing.T, fd *fakeDeezer, n int) []string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		fd.mu.Lock()
		got := append([]string(nil), fd.logs...)
		fd.mu.Unlock()
		if len(got) >= n || time.Now().After(deadline) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Each local start reports the track to Deezer's listen log, with the listen
// of the track before it: what the player heard of it, the list the host named
// (SetListenContext), and the shuffle and repeat in force.
func TestListenLogFollowsStarts(t *testing.T) {
	fd := offlineEngine(t, "1001")
	t.Cleanup(Logout)
	if !Init("1001") {
		t.Fatal("login failed")
	}
	SetListenContext("playlist_page", "42")
	SetShuffle(true)
	SetRepeat(1)
	t.Cleanup(func() { SetShuffle(false); SetRepeat(0); SetListenContext("", "") })
	setCurrentTrack(deezer.Track{ID: "101"})
	listenStarted()
	listenLog.Ended(audio.Listen{TrackID: "101", Format: "MP3_128", HeardMS: 95000, Pauses: 1})
	setCurrentEpisode(deezer.Track{ID: "900"})
	listenStarted()
	got := logsOf(t, fd, 2)
	if len(got) != 2 || got[0] != `log.listen {"next_media":{"media":{"id":"101","type":"song"}}}` {
		t.Fatalf("listen log %q", got)
	}
	var body struct {
		Params map[string]any `json:"params"`
		Next   map[string]any `json:"next_media"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(got[1], "log.listen ")), &body); err != nil {
		t.Fatalf("%s: %v", got[1], err)
	}
	p, ctxt := body.Params, body.Params["ctxt"].(map[string]any)
	if p["lt"] != 95.0 || ctxt["t"] != "playlist_page" || ctxt["id"] != "42" || p["is_shuffle"] != true || p["repeat_type"] != "repeat_all" ||
		body.Next["media"].(map[string]any)["type"] != "episode" {
		t.Fatalf("second log %s", got[1])
	}
}

// After a login the engine learns the web player's build and keeps it for the
// next start, which reports it before any login.
func TestLoginKeepsWebRelease(t *testing.T) {
	offlineEngine(t, "1001")
	t.Cleanup(func() { deezer.SetWebRelease("", "") })
	deezer.SetWebRelease("", "")
	deezer.KeepWebRelease(Client())
	if bundle, release := config.LoadWebRelease(); release != "10020261124120000" || bundle != "https://cdn-files.dzcdn.net/cache/js/app-web.cafe.js" {
		t.Fatalf("web-release.json %q %q", bundle, release)
	}
}

// SessionExpired reports a signed-in client whose ARL Deezer refused since.
func TestSessionExpired(t *testing.T) {
	offlineEngine(t, "1001")
	if SessionExpired() {
		t.Fatal("expired right after a login")
	}
	c := deezer.New("0") // the fake's anonymous answer: the ARL refused
	if err := c.Login(); !deezer.IsARLExpired(err) {
		t.Fatalf("login %v", err)
	}
	mu.Lock()
	client = c
	mu.Unlock()
	if !SessionExpired() {
		t.Fatal("not expired after Deezer refused the ARL")
	}
}
