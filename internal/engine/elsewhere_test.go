package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/bridge"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
)

type elsewherePayload struct {
	Track bridge.Track `json:"track"`
	Since int64        `json:"sinceMs"`
	Until int64        `json:"untilMs"`
}

func readElsewhere(t *testing.T) elsewherePayload {
	t.Helper()
	var p elsewherePayload
	if err := json.Unmarshal([]byte(ElsewhereJSON()), &p); err != nil {
		t.Fatalf("ElsewhereJSON: %v", err)
	}
	return p
}

func TestElsewhereFollowsAnnouncedStarts(t *testing.T) {
	offlineEngine(t, "1001")
	t.Cleanup(stopElsewhere)
	c := Client()

	v := ElsewhereVersion()
	noteElsewhere(context.Background(), c, deezer.StreamStart{TrackID: "77", Session: "web"})
	if ElsewhereVersion() == v {
		t.Fatal("version did not move for a start")
	}
	p := readElsewhere(t)
	if p.Track.ID != "77" || p.Track.Name != "Song 77" {
		t.Fatalf("track = %+v", p.Track)
	}
	// Deezer announces no pause: a start counts until its track's length passed,
	// and the history's entry for its end had time to arrive.
	if want := 200_000 + elseGrace.Milliseconds(); p.Since == 0 || p.Until-p.Since != want {
		t.Errorf("since %d until %d, want %d ms apart", p.Since, p.Until, want)
	}

	// The web player announces a start twice; the repeat is the same start.
	v = ElsewhereVersion()
	noteElsewhere(context.Background(), c, deezer.StreamStart{TrackID: "77", Session: "web"})
	if ElsewhereVersion() != v || readElsewhere(t).Since != p.Since {
		t.Error("a repeated announcement counted as a new start")
	}

	noteElsewhere(context.Background(), c, deezer.StreamStart{TrackID: "78", Session: "web"})
	if ElsewhereVersion() == v || readElsewhere(t).Track.ID != "78" {
		t.Error("the next track did not replace the first")
	}

	v = ElsewhereVersion()
	stopElsewhere()
	if ElsewhereJSON() != "{}" || ElsewhereVersion() == v {
		t.Errorf("after stop: %s, version moved %v", ElsewhereJSON(), ElsewhereVersion() != v)
	}
}

// played is a listening-history entry: a track that started at startMS.
func played(id string, startMS int64) map[string]any {
	return map[string]any{"SNG_ID": id, "TS": startMS / 1000}
}

// Deezer's players announce no automatic advance; history gets each track as it ends.
// After the announced track's entry, the other player plays an unnamed track, moved on
// by each later entry until none comes for elseUnknownFor.
func TestElsewhereFollowsUnannouncedAdvances(t *testing.T) {
	fd := offlineEngine(t, "1001")
	t.Cleanup(stopElsewhere)
	c := Client()
	setHistory := func(h ...map[string]any) {
		fd.mu.Lock()
		fd.history = h
		fd.mu.Unlock()
	}
	ctx := context.Background()

	noteElsewhere(ctx, c, deezer.StreamStart{TrackID: "77", Session: "web"})
	since := readElsewhere(t).Since
	start77 := since - 2000 // the announcement arrives after the start
	older := played("12", start77-300_000)
	setHistory(older)

	v := ElsewhereVersion()
	followElsewhere(ctx, c, since+199_000)
	if ElsewhereVersion() != v || readElsewhere(t).Track.ID != "77" {
		t.Fatalf("an older entry moved the follow on: %s", ElsewhereJSON())
	}

	// The entry is written when the track ends, so the read that first sees
	// it dates the end; it showed up to 41 s late (measured 2026-11-23).
	setHistory(played("77", start77), older)
	end77 := since + 200_000 + 41_000
	followElsewhere(ctx, c, end77)
	p := readElsewhere(t)
	if ElsewhereVersion() == v || p.Track.ID != "" || p.Since != end77 || p.Until != end77+elseUnknownFor.Milliseconds() {
		t.Fatalf("after the announced track's entry: %s, want no track from %d", ElsewhereJSON(), end77)
	}

	v = ElsewhereVersion()
	followElsewhere(ctx, c, end77+60_000)
	if ElsewhereVersion() != v {
		t.Fatal("the same entry moved the follow on twice")
	}

	// The entry of a track that stalled two minutes keeps its start and
	// length (measured on Android, 2026-11-23); its end is still when it shows.
	setHistory(played("90", end77), played("77", start77), older)
	end90 := end77 + 300_000
	followElsewhere(ctx, c, end90)
	if p := readElsewhere(t); ElsewhereVersion() == v || p.Since != end90 || p.Until != end90+elseUnknownFor.Milliseconds() {
		t.Fatalf("after the next entry: %s, want from %d", ElsewhereJSON(), end90)
	}

	// No entry until elseUnknownFor passed: the follow ends, and a late entry
	// does not bring it back.
	v = ElsewhereVersion()
	late := end90 + elseUnknownFor.Milliseconds()
	setHistory(played("91", end90), played("90", end77))
	followElsewhere(ctx, c, late+1)
	if ElsewhereVersion() != v || readElsewhere(t).Until != late {
		t.Errorf("an entry after the follow ended counted: %s", ElsewhereJSON())
	}

	// An announced start replaces the untitled track.
	noteElsewhere(ctx, c, deezer.StreamStart{TrackID: "78", Session: "web"})
	if readElsewhere(t).Track.ID != "78" {
		t.Errorf("an announced start after an untitled one: %s", ElsewhereJSON())
	}
}

func TestElsewhereIgnoresUnknownTracks(t *testing.T) {
	fd := offlineEngine(t, "1001")
	t.Cleanup(stopElsewhere)
	fd.mu.Lock()
	fd.gone["66"] = true
	fd.mu.Unlock()
	v := ElsewhereVersion()
	noteElsewhere(context.Background(), Client(), deezer.StreamStart{TrackID: "66", Session: "web"})
	if ElsewhereVersion() != v || ElsewhereJSON() != "{}" {
		t.Errorf("a track Deezer does not serve showed: %s", ElsewhereJSON())
	}
}

// announcing records what the engine announces, as the watch's client would send it.
func announcing(t *testing.T) chan string {
	t.Helper()
	sent := make(chan string, 4)
	elseMu.Lock()
	elseAnnounce = func(id string) bool { sent <- id; return true }
	elseMu.Unlock()
	t.Cleanup(func() {
		elseMu.Lock()
		elseAnnounce = nil
		elseMu.Unlock()
		playAlongside.Store(false)
	})
	return sent
}

func expectAnnounced(t *testing.T, sent chan string, want string) {
	t.Helper()
	select {
	case id := <-sent:
		if id != want {
			t.Fatalf("announced %q, want %q", id, want)
		}
	case <-time.After(2 * time.Second):
		if want != "" {
			t.Fatalf("%q not announced", want)
		}
	}
}

// A song the player moves on to is announced, so Deezer's players pause, and
// what another session played is forgotten (it pauses on it). Episodes and
// playback alongside other players announce nothing.
func TestLocalStartsAreAnnounced(t *testing.T) {
	offlineEngine(t, "1001")
	t.Cleanup(stopElsewhere)
	t.Cleanup(func() { setCurrentTrack(deezer.Track{}) })
	sent := announcing(t)
	noteElsewhere(context.Background(), Client(), deezer.StreamStart{TrackID: "77", Session: "web"})

	v := ElsewhereVersion()
	setCurrentTrack(deezer.Track{ID: "s1"})
	announceLocal()
	expectAnnounced(t, sent, "s1")
	if ElsewhereJSON() != "{}" || ElsewhereVersion() == v {
		t.Errorf("the other session's track still shows: %s", ElsewhereJSON())
	}

	setCurrentEpisode(deezer.Track{ID: "ep1"})
	announceLocal()
	expectAnnounced(t, sent, "")

	SetSingleDevice(false)
	setCurrentTrack(deezer.Track{ID: "s2"})
	announceLocal()
	expectAnnounced(t, sent, "")
}

// A gapless promote starts the next song without a play: it is announced too.
func TestGaplessPromoteIsAnnounced(t *testing.T) {
	offlineEngine(t, "1001")
	t.Cleanup(func() { setCurrentTrack(deezer.Track{}) })
	sent := announcing(t)
	queueMu.Lock()
	engineQ.Set([]deezer.Track{{ID: "a"}, {ID: "b"}}, 0)
	queueMu.Unlock()
	t.Cleanup(func() {
		queueMu.Lock()
		engineQ.Set(nil, -1)
		queueMu.Unlock()
	})
	setCurrentTrack(deezer.Track{ID: "a"})
	if !engineSyncOnGaplessPromote() {
		t.Fatal("the engine queue did not own the promote")
	}
	expectAnnounced(t, sent, "b")
}

// Another session's start pauses local playback, once per start; with
// playback alongside other players it plays on.
func TestOtherSessionsStartPausesLocalPlayback(t *testing.T) {
	offlineEngine(t, "1001")
	t.Cleanup(stopElsewhere)
	announcing(t)
	paused := 0
	prev := pauseForOther
	pauseForOther = func() { paused++ }
	t.Cleanup(func() { pauseForOther = prev })

	noteElsewhere(context.Background(), Client(), deezer.StreamStart{TrackID: "77", Session: "web"})
	noteElsewhere(context.Background(), Client(), deezer.StreamStart{TrackID: "77", Session: "web"})
	if paused != 1 {
		t.Fatalf("paused %d times for one start, want 1", paused)
	}
	SetSingleDevice(false)
	noteElsewhere(context.Background(), Client(), deezer.StreamStart{TrackID: "78", Session: "web"})
	if paused != 1 || readElsewhere(t).Track.ID != "78" {
		t.Errorf("paused %d times alongside other players; showing %s", paused, ElsewhereJSON())
	}
}

// A start whose track arrives after the watch ended (a logout) is dropped.
func TestElsewhereDropsStartsAfterStop(t *testing.T) {
	offlineEngine(t, "1001")
	t.Cleanup(stopElsewhere)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	v := ElsewhereVersion()
	noteElsewhere(ctx, Client(), deezer.StreamStart{TrackID: "77", Session: "web"})
	if ElsewhereVersion() != v || ElsewhereJSON() != "{}" {
		t.Errorf("a start after stop showed: %s", ElsewhereJSON())
	}
}

// Offline mode ends the live watch (socket, history poll, announcements);
// leaving it watches again with the signed-in client.
func TestOfflineModeStopsElsewhere(t *testing.T) {
	offlineEngine(t, "1001")
	watchElsewhere(Client())
	t.Cleanup(stopElsewhere)
	t.Cleanup(func() { SetOfflineMode(false) })
	SetOfflineMode(true)
	if elsewhereWatching() {
		t.Fatal("offline mode left the live watch running")
	}
	SetOfflineMode(false)
	if !elsewhereWatching() {
		t.Fatal("leaving offline mode did not watch again")
	}
}
