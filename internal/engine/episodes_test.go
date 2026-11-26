package engine

import (
	"reflect"
	"testing"
	"time"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/audio"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
)

// The bookmark follows the playing episode: written every 30 s of media
// time, on a pause, and when another track starts; never under 10 s.
func TestEpisodeMarker(t *testing.T) {
	var m episodeMarker
	type step struct {
		id       string
		episode  bool
		playing  bool
		pos, dur int
		want     []episodeBookmark
	}
	for i, s := range []step{
		{"e1", true, true, 0, 3600, nil},
		{"e1", true, true, 29, 3600, nil},
		{"e1", true, true, 30, 3600, []episodeBookmark{{"e1", 30, 3600, false}}},
		{"e1", true, true, 59, 3600, nil},
		{"e1", true, false, 61, 3600, []episodeBookmark{{"e1", 61, 3600, false}}},   // paused
		{"e1", true, false, 61, 3600, nil},                                          // still paused
		{"e1", true, true, 3500, 3600, []episodeBookmark{{"e1", 3500, 3600, true}}}, // a seek
		{"e1", true, true, 3510, 3600, nil},
		{"s1", false, true, 3, 200, []episodeBookmark{{"e1", 3510, 3600, true}}}, // a song starts
		{"s1", false, true, 40, 200, nil},
		{"e2", true, true, 0, 600, nil},
		{"e2", true, false, 9, 600, nil}, // paused under 10 s
		{"e3", true, true, 0, 600, nil},  // e2 ends under 10 s
	} {
		got := m.step(s.id, s.episode, s.playing, s.pos, s.dur)
		if !reflect.DeepEqual(got, s.want) {
			t.Fatalf("step %d: %+v, want %+v", i, got, s.want)
		}
	}
}

// Without Discord's ticker, an episode start still reaches the bookmark marker.
func TestEpisodeBookmarkWithoutDiscord(t *testing.T) {
	mu.Lock()
	prev := player
	player = &audio.Player{}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		player = prev
		mu.Unlock()
		setCurrentTrack(deezer.Track{})
	})
	ensureEpisodeLoop()
	setCurrentEpisode(deezer.Track{ID: "e-nodiscord", DurationMS: 600000})
	deadline := time.Now().Add(5 * time.Second)
	for {
		episodesMu.Lock()
		id := episodes.cur.id
		episodesMu.Unlock()
		if id == "e-nodiscord" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the marker follows %q, want the episode that started", id)
		}
		time.Sleep(time.Millisecond)
	}
}

// An episode that cannot be resolved says why, and an episode play reads its
// bookmark from what the engine holds instead of fetching the user menu again.
func TestPlayEpisodeErrorAndBookmarkReads(t *testing.T) {
	fd := offlineEngine(t, "1001")
	if marks, err := EpisodeBookmarks(); err != nil || marks["77"].OffsetS != 120 {
		t.Fatalf("bookmarks %+v %v", marks, err)
	}
	menus := fd.menus.Load()
	for i := 0; i < 2; i++ {
		if PlayEpisode("77", 0) != PlayFailed {
			t.Fatal("an episode Deezer does not serve played")
		}
		if Player().LastError() == "" {
			t.Fatal("a failed episode play left no reason")
		}
	}
	if got := fd.menus.Load(); got != menus {
		t.Fatalf("two episode plays fetched the user menu %d times", got-menus)
	}
}
