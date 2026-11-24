package engine

import (
	"bufio"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/audio"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/control"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/history"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/mediacache"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/mediacache/mctest"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/preload"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/queue"
)

// clearRoutedRemote resets the Connect routing globals so a test that installs a
// fake remote can't leak it into another test's routedRemote() reads.
func clearRoutedRemote() {
	mu.Lock()
	remoteCli = nil
	remoteSt = control.State{}
	remoteAddr = ""
	mu.Unlock()
}

// waitServing blocks until the server at addr answers HTTP, proving Serve runs, so a
// later Close frees the port for the same-port rebind. A TCP dial is not enough: the
// kernel accepts before Serve registers the listener.
func waitServing(t *testing.T, addr string) {
	t.Helper()
	cl := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		// Any HTTP status (even 401/403/404) proves the handler chain ran.
		if resp, err := cl.Get("http://" + addr + "/whoami"); err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("control server at %s never started serving", addr)
}

// TestEngineQueueAutoAdvance checks that a loaded playlist walks every track on
// successive natural finishes and stops at the end under RepeatOff.
func TestEngineQueueAutoAdvance(t *testing.T) {
	ts := []deezer.Track{{ID: "1"}, {ID: "2"}, {ID: "3"}}

	queueMu.Lock()
	engineQ.Set(ts, 0)
	queueMu.Unlock()
	setCurrentTrack(ts[0]) // the player started on the first track

	var played []string
	for {
		// Simulate the player's onFinish for the current track.
		next, ok := engineQueueAdvance(currentTrack().ID)
		if !ok {
			break
		}
		played = append(played, next.ID)
		setCurrentTrack(next) // the player is on `next`
	}

	want := []string{"2", "3"}
	if len(played) != len(want) {
		t.Fatalf("auto-advance visited %v, want %v", played, want)
	}
	for i := range want {
		if played[i] != want[i] {
			t.Fatalf("auto-advance[%d]=%q want %q (full: %v)", i, played[i], want[i], played)
		}
	}

	// A further finish at the end (RepeatOff) must not advance.
	if _, ok := engineQueueAdvance(currentTrack().ID); ok {
		t.Fatal("auto-advance past the end should stop under RepeatOff")
	}
}

// TestEngineQueueRepeatAllWraps proves a natural finish at the end wraps back to
// the first track when the queue is in RepeatAll (honored for free via the
// shared queue.Queue).
func TestEngineQueueRepeatAllWraps(t *testing.T) {
	queueMu.Lock()
	engineQ.Set([]deezer.Track{{ID: "a"}, {ID: "b"}}, 1) // start on the last track
	engineQ.SetRepeat(queue.RepeatAll)
	queueMu.Unlock()
	setCurrentTrack(deezer.Track{ID: "b"})

	next, ok := engineQueueAdvance("b")
	if !ok || next.ID != "a" {
		t.Fatalf("RepeatAll finish at end -> %q ok=%v, want wrap to \"a\"", next.ID, ok)
	}

	// Restore default so other tests see RepeatOff.
	queueMu.Lock()
	engineQ.SetRepeat(queue.RepeatOff)
	queueMu.Unlock()
}

// TestEngineQueueAdvanceIgnoresAdHocTrack proves that finishing a track which is
// NOT the engine queue's current track (e.g. a direct DZPlay that bypassed the
// engine queue) does not trigger a stale auto-advance into a loaded playlist.
func TestEngineQueueAdvanceIgnoresAdHocTrack(t *testing.T) {
	queueMu.Lock()
	engineQ.Set([]deezer.Track{{ID: "10"}, {ID: "11"}}, 0)
	queueMu.Unlock()

	if _, ok := engineQueueAdvance("999"); ok {
		t.Fatal("finish of a non-queued track must not auto-advance the engine queue")
	}
}

// resetEngineQueue restores the engine queue to its pristine (unsynced) state
// so tests can't leak queue contents / repeat / shuffle into each other.
func resetEngineQueue() {
	queueMu.Lock()
	engineQ.Set(nil, 0)
	engineQ.SetRepeat(queue.RepeatOff)
	engineQ.SetShuffle(false)
	shuffleOn, shuffleHostOwned = false, false
	queueMu.Unlock()
}

// TestQueueSyncReflectsInEngineState checks that a queue synced via
// DZQueueSet/DZQueueSetIndex shows up on /status (State.Queue) with the GUI's cursor.
func TestQueueSyncReflectsInEngineState(t *testing.T) {
	t.Cleanup(resetEngineQueue)

	js := `[
		{"id":"1","name":"One","durationMs":180000,"artistLine":"Solo Artist","albumName":"Alpha"},
		{"id":"2","name":"Two","durationMs":200000,"artists":[{"id":"77","name":"Duo"}],"albumName":"Beta"},
		{"id":"","name":"dropped: no id"}
	]`
	ts, err := queueTracksFromJSON(js)
	if err != nil {
		t.Fatalf("queueTracksFromJSON: %v", err)
	}
	engineQueueSet(ts)
	QueueSetIndex(1)

	st := engineState()
	if len(st.Queue) != 2 {
		t.Fatalf("engineState queue len = %d, want 2 (id-less entries dropped)", len(st.Queue))
	}
	if st.Queue[0].ID != "1" || st.Queue[0].Title != "One" || st.Queue[0].DurationMS != 180000 {
		t.Fatalf("queue[0] = %+v, want id=1 title=One durationMs=180000", st.Queue[0])
	}
	// artistLine-only wire tracks keep their artist via the fallback conversion.
	if st.Queue[0].Artist != "Solo Artist" {
		t.Fatalf("queue[0].Artist = %q, want artistLine fallback \"Solo Artist\"", st.Queue[0].Artist)
	}
	if st.Queue[1].ID != "2" || st.Queue[1].Artist != "Duo" || st.Queue[1].ArtistID != "77" {
		t.Fatalf("queue[1] = %+v, want id=2 artist=Duo artistId=77", st.Queue[1])
	}
	if got := QueueIndex(); got != 1 {
		t.Fatalf("engineQueueIndex = %d, want 1", got)
	}
	// The synced queue also feeds Preload's length lookup.
	if got := queuedDurationMS("2"); got != 200000 {
		t.Fatalf("queuedDurationMS(2) = %d, want 200000", got)
	}
	if got := queuedDurationMS("missing"); got != 0 {
		t.Fatalf("queuedDurationMS(missing) = %d, want 0", got)
	}

	// Clearing (the "[]" payload) removes the queue from /status again.
	ts, err = queueTracksFromJSON(`[]`)
	if err != nil {
		t.Fatalf("queueTracksFromJSON(\"[]\"): %v", err)
	}
	engineQueueSet(ts)
	if st := engineState(); len(st.Queue) != 0 {
		t.Fatalf("cleared queue still reported: %+v", st.Queue)
	}
	if got := QueueIndex(); got != -1 {
		t.Fatalf("engineQueueIndex after clear = %d, want -1", got)
	}
}

// Appending keeps the cursor, bumps the version once, and drops a preload
// only when the upcoming track changed.
func TestEngineQueueAppend(t *testing.T) {
	t.Cleanup(resetEngineQueue)
	engineQueueSet([]deezer.Track{{ID: "a"}, {ID: "b"}, {ID: "c"}})
	QueueSetIndex(1)
	armPreload(t, deezer.Track{ID: "c"})
	v := QueueVersion()
	ts, err := queueTracksFromJSON(`[{"id":"d","durationMs":1000},{"id":"e"}]`)
	if err != nil {
		t.Fatal(err)
	}
	engineQueueAppend(ts)
	got, index, version := QueueSnapshot()
	if len(got) != 5 || got[3].ID != "d" || got[3].DurationMS != 1000 || index != 1 || version != v+1 {
		t.Fatalf("queue %v, index %d, version %d (was %d)", got, index, version, v)
	}
	if p, armed := preloads.Take(); !armed || p.ID != "c" {
		t.Fatalf("preload of the unchanged next dropped: %+v %v", p, armed)
	}

	QueueSetIndex(4) // at the end: nothing next
	armPreload(t, deezer.Track{ID: "stale"})
	engineQueueAppend([]deezer.Track{{ID: "f"}})
	if _, armed := preloads.Take(); armed {
		t.Fatal("preload kept although the upcoming track changed")
	}
	if engineQueueAppend(nil); QueueVersion() != v+2 {
		t.Fatalf("an empty append bumped the version to %d", QueueVersion())
	}
}

// The /events snapshot leaves out a queue the stream already carries.
func TestEngineStateSinceSkipsUnchangedQueue(t *testing.T) {
	t.Cleanup(resetEngineQueue)
	engineQueueSet([]deezer.Track{{ID: "1"}, {ID: "2"}})
	v := QueueVersion()
	if st := engineStateSince(v - 1); len(st.Queue) != 2 || st.QueueVersion != v {
		t.Fatalf("changed queue: %d tracks, version %d", len(st.Queue), st.QueueVersion)
	}
	if st := engineStateSince(v); st.Queue != nil || st.QueueVersion != v {
		t.Fatalf("unchanged queue: %d tracks, version %d", len(st.Queue), st.QueueVersion)
	}
}

// TestQueueTracksFromJSONRejectsBadPayload proves DZQueueSet's 0-return path:
// malformed JSON must error instead of silently clearing the queue.
func TestQueueTracksFromJSONRejectsBadPayload(t *testing.T) {
	if _, err := queueTracksFromJSON(`{"not":"an array"}`); err == nil {
		t.Fatal("non-array payload must be rejected")
	}
	if _, err := queueTracksFromJSON(`[{"id":`); err == nil {
		t.Fatal("truncated payload must be rejected")
	}
	// "" and "null" are the documented clear forms.
	if ts, err := queueTracksFromJSON(""); err != nil || len(ts) != 0 {
		t.Fatalf("empty payload = (%v, %v), want empty clear", ts, err)
	}
	if ts, err := queueTracksFromJSON("null"); err != nil || len(ts) != 0 {
		t.Fatalf("null payload = (%v, %v), want empty clear", ts, err)
	}
}

// TestRepeatShuffleReflectInEngineState proves the DZSetRepeat / DZSetShuffle
// persistence path: engineState reports the last set values instead of the old
// hard-coded Repeat:"off", Shuffle:false.
func TestRepeatShuffleReflectInEngineState(t *testing.T) {
	t.Cleanup(resetEngineQueue)

	engineSetRepeat("all")
	engineSetShuffle(true)
	st := engineState()
	if st.Repeat != "all" || !st.Shuffle {
		t.Fatalf("engineState repeat=%q shuffle=%v, want all/true", st.Repeat, st.Shuffle)
	}

	engineSetRepeat("one")
	if st := engineState(); st.Repeat != "one" {
		t.Fatalf("engineState repeat=%q, want one", st.Repeat)
	}

	engineSetRepeat("off")
	engineSetShuffle(false)
	st = engineState()
	if st.Repeat != "off" || st.Shuffle {
		t.Fatalf("engineState repeat=%q shuffle=%v, want off/false (default wire shape)", st.Repeat, st.Shuffle)
	}
}

// TestHostOwnedShuffleRecordsOnly proves DZSetShuffleHostOwned: the flag is
// reported (/status, DZGetShuffle) but the engine queue keeps the GUI's order.
func TestHostOwnedShuffleRecordsOnly(t *testing.T) {
	t.Cleanup(resetEngineQueue)

	SetShuffleHostOwned(true)
	engineSetShuffle(true)
	queueMu.Lock()
	applied := engineQ.Shuffle()
	queueMu.Unlock()
	if !engineState().Shuffle || !Shuffle() || applied {
		t.Fatalf("status=%v get=%v applied=%v, want true/true/false", engineState().Shuffle, Shuffle(), applied)
	}
}

// TestEngineNextPrevWalkSyncedQueue proves the control API's /next and /prev
// walk a queue synced through the DZQueueSet path (cursor movement is the queue
// deliverable; play itself needs a logged-in client + audio device).
func TestEngineNextPrevWalkSyncedQueue(t *testing.T) {
	t.Cleanup(resetEngineQueue)

	ts, err := queueTracksFromJSON(`[{"id":"a"},{"id":"b"},{"id":"c"}]`)
	if err != nil {
		t.Fatalf("queueTracksFromJSON: %v", err)
	}
	engineQueueSet(ts)

	engineNext()
	if got := QueueIndex(); got != 1 {
		t.Fatalf("after Next: index = %d, want 1", got)
	}
	engineNext()
	if got := QueueIndex(); got != 2 {
		t.Fatalf("after Next x2: index = %d, want 2", got)
	}
	engineNext() // end of queue under RepeatOff: stays put
	if got := QueueIndex(); got != 2 {
		t.Fatalf("Next past the end moved the cursor to %d, want 2", got)
	}
	enginePrev()
	if got := QueueIndex(); got != 1 {
		t.Fatalf("after Prev: index = %d, want 1", got)
	}
	enginePrev()
	if got := QueueIndex(); got != 0 {
		t.Fatalf("after Prev x2: index = %d, want 0", got)
	}
}

// TestRefreshControlServerSwapsStaleClient proves that an account switch rebuilds
// the control server around the new client (so it stops serving the old
// account's library) and is a no-op for the same client.
func TestRefreshControlServerSwapsStaleClient(t *testing.T) {
	old := deezer.New("old-arl")
	srv := control.New(
		control.Config{Addr: "127.0.0.1:0"},
		engineState, engineAccount, engineCommands(), old,
	)
	if err := srv.Start(); err != nil {
		t.Fatalf("start control server: %v", err)
	}
	waitServing(t, srv.Addr()) // so refreshControlServer's Close reclaims the port
	defer func() {
		mu.Lock()
		s := ctrlSrv
		ctrlSrv, ctrlSrvUserID = nil, ""
		mu.Unlock()
		if s != nil {
			s.Close()
		}
	}()

	mu.Lock()
	ctrlSrv = srv
	ctrlCfg = control.Config{Addr: "127.0.0.1:0"}
	ctrlSrvUserID = old.Account().UserID
	mu.Unlock()

	// Same account: no-op (no rebuild). Staleness keys on the account UserID, not the
	// *deezer.Client pointer, so a fresh client for the same account stays a no-op.
	refreshControlServer(old)
	mu.Lock()
	unchanged := ctrlSrv == srv && ctrlSrvUserID == old.Account().UserID
	mu.Unlock()
	if !unchanged {
		t.Fatal("refreshControlServer for the same account must be a no-op")
	}

	// Account switch: simulate the tracked account being a different one (in
	// production both clients are logged in with real, distinct UserIDs; the test
	// clients aren't logged in, so seed the tracked id to force the mismatch).
	mu.Lock()
	ctrlSrvUserID = "old-account-uid"
	mu.Unlock()
	fresh := deezer.New("new-arl")
	refreshControlServer(fresh)
	mu.Lock()
	swapped := ctrlSrv != nil && ctrlSrv != srv && ctrlSrvUserID == fresh.Account().UserID
	mu.Unlock()
	if !swapped {
		t.Fatal("refreshControlServer did not rebuild the control server for the new account")
	}
}

// TestEngineCommandsWiresExtendedHooks proves the extended-control hooks
// are all wired (a nil hook makes the control server answer 501/404 for its
// endpoint, silently disabling the feature for every remote).
func TestEngineCommandsWiresExtendedHooks(t *testing.T) {
	cmds := engineCommands()
	if cmds.SetQueue == nil {
		t.Fatal("SetQueue hook must be wired")
	}
	if cmds.QueueAdd == nil || cmds.QueueJump == nil || cmds.QueueRemove == nil || cmds.QueueMove == nil {
		t.Fatal("queue hooks (add/jump/remove/move) must be wired")
	}
	if cmds.PlayAlbum == nil || cmds.PlayMixTrack == nil || cmds.PlayMixArtist == nil {
		t.Fatal("play hooks (album/mix track/mix artist) must be wired")
	}
	if cmds.HistoryRecent == nil {
		t.Fatal("HistoryRecent hook must be wired")
	}
}

// TestEngineQueueInsertNextAndAppend checks that "play next" inserts after the cursor,
// add appends, and both show in engineState and bump the queue version.
func TestEngineQueueInsertNextAndAppend(t *testing.T) {
	t.Cleanup(resetEngineQueue)

	engineQueueSet([]deezer.Track{{ID: "1"}, {ID: "2"}, {ID: "3"}})
	v0 := QueueVersion()

	engineQueueInsert(deezer.Track{ID: "X"}, true) // play next -> after cursor (0)
	engineQueueInsert(deezer.Track{ID: "Y"}, false)

	st := engineState()
	got := make([]string, len(st.Queue))
	for i, tr := range st.Queue {
		got[i] = tr.ID
	}
	want := []string{"1", "X", "2", "3", "Y"}
	if len(got) != len(want) {
		t.Fatalf("queue after add = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("queue[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
	if QueueIndex() != 0 {
		t.Fatalf("adds moved the cursor to %d, want 0", QueueIndex())
	}
	if v1 := QueueVersion(); v1 <= v0 {
		t.Fatalf("queue version did not bump on adds: %d -> %d", v0, v1)
	}
}

// TestEngineQueueSelectMovesCursor checks that a valid index moves the cursor (with
// history) and returns the track, and that out-of-range indices error without changing
// the queue. Every jump records the outgoing row.
func TestEngineQueueSelectMovesCursor(t *testing.T) {
	t.Cleanup(resetEngineQueue)

	engineQueueSet([]deezer.Track{{ID: "a"}, {ID: "b"}, {ID: "c"}})

	if tr, err := engineQueueSelect(1); err != nil || tr.ID != "b" {
		t.Fatalf("engineQueueSelect(1) = (%q, %v), want (b, nil)", tr.ID, err)
	}
	tr, err := engineQueueSelect(2)
	if err != nil || tr.ID != "c" {
		t.Fatalf("engineQueueSelect(2) = (%q, %v), want (c, nil)", tr.ID, err)
	}
	if QueueIndex() != 2 {
		t.Fatalf("cursor = %d after select, want 2", QueueIndex())
	}
	// Prev retraces to the row that was playing before the jump.
	enginePrev()
	if QueueIndex() != 1 {
		t.Fatalf("Prev after jump retraced to %d, want 1", QueueIndex())
	}
	if _, err := engineQueueSelect(3); err == nil {
		t.Fatal("out-of-range select must error")
	}
	if _, err := engineQueueSelect(-1); err == nil {
		t.Fatal("negative select must error")
	}
}

// TestEngineQueueSetIndexAlignsWithoutHistory proves the GUI cursor-sync path
// (DZQueueSetIndex → engineQueueSetIndex → queue.AlignIndex) records no history,
// so a remote Prev after alignment never jumps to a never-played row 0.
func TestEngineQueueSetIndexAlignsWithoutHistory(t *testing.T) {
	t.Cleanup(resetEngineQueue)

	engineQueueSet([]deezer.Track{{ID: "a"}, {ID: "b"}, {ID: "c"}})
	QueueSetIndex(2) // GUI says "I'm playing row 2": pure alignment
	if QueueIndex() != 2 {
		t.Fatalf("cursor = %d after alignment, want 2", QueueIndex())
	}
	enginePrev()
	if QueueIndex() == 0 {
		t.Fatal("Prev after alignment jumped to the never-played row 0")
	}
	if QueueIndex() != 1 {
		t.Fatalf("Prev after alignment stepped to %d, want linear 1", QueueIndex())
	}
}

// TestEngineQueueJumpRequiresEngine proves QueueJump fails cleanly (and leaves
// the cursor alone) when no client/player is up, instead of half-jumping.
func TestEngineQueueJumpRequiresEngine(t *testing.T) {
	t.Cleanup(resetEngineQueue)

	engineQueueSet([]deezer.Track{{ID: "a"}, {ID: "b"}})
	if err := engineQueueJump(1); err == nil {
		t.Fatal("QueueJump without a logged-in engine must error")
	}
	if QueueIndex() != 0 {
		t.Fatalf("failed jump moved the cursor to %d, want 0", QueueIndex())
	}
}

// TestEngineQueueRemoveGuards proves QueueRemove rejects the playing row and
// out-of-range indices, and otherwise edits the queue /status reports.
func TestEngineQueueRemoveGuards(t *testing.T) {
	t.Cleanup(resetEngineQueue)

	engineQueueSet([]deezer.Track{{ID: "a"}, {ID: "b"}, {ID: "c"}})
	QueueSetIndex(1)

	if err := QueueRemove(1); err == nil {
		t.Fatal("removing the playing row must be rejected")
	}
	if err := QueueRemove(3); err == nil {
		t.Fatal("out-of-range remove must error")
	}
	if err := QueueRemove(2); err != nil {
		t.Fatalf("valid remove failed: %v", err)
	}
	st := engineState()
	if len(st.Queue) != 2 || st.Queue[0].ID != "a" || st.Queue[1].ID != "b" {
		t.Fatalf("queue after remove = %+v, want [a b]", st.Queue)
	}
	if QueueIndex() != 1 {
		t.Fatalf("cursor = %d after removing a later row, want 1", QueueIndex())
	}
}

// TestEngineQueueMoveFollowsCursor proves QueueMove reorders the queue while
// the cursor keeps following the playing track, and rejects bad indices.
func TestEngineQueueMoveFollowsCursor(t *testing.T) {
	t.Cleanup(resetEngineQueue)

	engineQueueSet([]deezer.Track{{ID: "a"}, {ID: "b"}, {ID: "c"}}) // playing "a" (cursor 0)

	if err := QueueMove(2, 0); err != nil {
		t.Fatalf("move failed: %v", err)
	}
	st := engineState()
	got := []string{st.Queue[0].ID, st.Queue[1].ID, st.Queue[2].ID}
	if got[0] != "c" || got[1] != "a" || got[2] != "b" {
		t.Fatalf("queue after move = %v, want [c a b]", got)
	}
	if QueueIndex() != 1 {
		t.Fatalf("cursor = %d, want 1 (still on the playing track)", QueueIndex())
	}
	if err := QueueMove(0, 0); err != nil {
		t.Fatalf("from == to must be a no-op success, got %v", err)
	}
	if err := QueueMove(0, 9); err == nil {
		t.Fatal("out-of-range move must error")
	}
}

// TestPlayCommandsRequireEngine proves the album/mix commands surface a clean
// error before login instead of panicking or silently doing nothing (the
// control server maps it to 409 for the remote).
func TestPlayCommandsRequireEngine(t *testing.T) {
	if err := enginePlayAlbum("123"); err == nil {
		t.Fatal("PlayAlbum without a client must error")
	}
	if err := enginePlayMixTrack("123"); err == nil {
		t.Fatal("PlayMixTrack without a client must error")
	}
	if err := enginePlayMixArtist("123"); err == nil {
		t.Fatal("PlayMixArtist without a client must error")
	}
	if err := engineQueueAdd("123", true); err == nil {
		t.Fatal("QueueAdd without a client must error")
	}
}

// TestEnginePlayFetchedRejectsEmpty proves an album/mix that resolves to zero
// tracks errors instead of clearing the queue and "succeeding" silently.
func TestEnginePlayFetchedRejectsEmpty(t *testing.T) {
	if err := enginePlayFetched("album", func(*deezer.Client) ([]deezer.Track, error) { return nil, nil }); err == nil {
		t.Fatal("empty track list must error")
	}
}

// TestEngineHistoryRecentReturnsJSON checks that /history/recent's hook returns valid
// JSON (newest first) from the history store, and [] (not null) for an empty log.
func TestEngineHistoryRecentReturnsJSON(t *testing.T) {
	setHistoryStore(history.New(filepath.Join(t.TempDir(), "history.jsonl")))

	raw, err := engineHistoryRecent(10)
	if err != nil {
		t.Fatalf("empty history errored: %v", err)
	}
	if string(raw) != "[]" {
		t.Fatalf("empty history = %s, want []", raw)
	}

	st := historyStore()
	if err := st.Record(history.Entry{TrackID: "1", Title: "First", Artist: "A", StartedAt: 100, DurationPlayedSec: 60}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := st.Record(history.Entry{TrackID: "2", Title: "Second", Artist: "B", StartedAt: 200, DurationPlayedSec: 30}); err != nil {
		t.Fatalf("record: %v", err)
	}

	raw, err = engineHistoryRecent(10)
	if err != nil {
		t.Fatalf("engineHistoryRecent: %v", err)
	}
	if !json.Valid(raw) {
		t.Fatalf("invalid JSON: %s", raw)
	}
	var entries []history.Entry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 2 || entries[0].TrackID != "2" || entries[1].TrackID != "1" {
		t.Fatalf("entries = %+v, want newest first [2 1]", entries)
	}
}

// TestEpisodeListenRecordsKind checks that an episode play records Kind="episode", a
// song Kind="", and that the kind reaches the /history/recent JSON.
func TestEpisodeListenRecordsKind(t *testing.T) {
	t.Cleanup(func() { setCurrentTrack(deezer.Track{}) })
	setHistoryStore(history.New(filepath.Join(t.TempDir(), "history.jsonl")))

	// Play an episode, then swap away: the transition (player still Playing)
	// records the outgoing episode via currentTrackSnapshot -> recordListen.
	setCurrentEpisode(deezer.Track{ID: "ep1", Name: "Episode", DurationMS: 600000})
	recordTransition(audio.Playing, 120000)
	entries := waitHistory(t, 1)
	if entries[0].TrackID != "ep1" || entries[0].Kind != history.KindEpisode {
		t.Fatalf("episode listen = %+v, want trackId=ep1 Kind=episode", entries[0])
	}

	// A song recorded next carries no kind (setCurrentTrack clears it).
	setCurrentTrack(deezer.Track{ID: "s1", Name: "Song", DurationMS: 200000})
	recordTransition(audio.Playing, 90000)
	entries = waitHistory(t, 2)
	if entries[0].TrackID != "s1" || entries[0].Kind != "" {
		t.Fatalf("song listen = %+v, want trackId=s1 empty Kind", entries[0])
	}

	// The kind reaches the /history/recent JSON wire that DZHistoryRecentJSON serves.
	raw, err := engineHistoryRecent(10)
	if err != nil {
		t.Fatalf("engineHistoryRecent: %v", err)
	}
	var wire []history.Entry
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal recent JSON: %v", err)
	}
	byID := map[string]history.Entry{}
	for _, e := range wire {
		byID[e.TrackID] = e
	}
	if byID["ep1"].Kind != history.KindEpisode {
		t.Fatalf("recent JSON episode kind = %q, want episode (raw=%s)", byID["ep1"].Kind, raw)
	}
	if byID["s1"].Kind != "" {
		t.Fatalf("recent JSON song kind = %q, want empty", byID["s1"].Kind)
	}
}

// TestListenEntry proves the history-record guards: no id and sub-second
// listens are dropped (start-skips, pause/resume can't produce records), the
// listened time is clamped to the track duration, and the fields map through.
func TestListenEntry(t *testing.T) {
	if _, ok := listenEntry(deezer.Track{}, "", 5000); ok {
		t.Fatal("track without an id must not be recorded")
	}
	if _, ok := listenEntry(deezer.Track{ID: "1"}, "", 999); ok {
		t.Fatal("sub-second listens must not be recorded")
	}
	e, ok := listenEntry(deezer.Track{
		ID: "42", Name: "Song", AlbumName: "Album", DurationMS: 3000,
		Artists: []deezer.Artist{{ID: "7", Name: "Artist"}},
	}, "", 10000)
	if !ok {
		t.Fatal("valid listen was dropped")
	}
	if e.TrackID != "42" || e.Title != "Song" || e.Artist != "Artist" || e.Album != "Album" {
		t.Fatalf("entry fields = %+v", e)
	}
	if e.Kind != "" {
		t.Fatalf("song entry Kind = %q, want empty", e.Kind)
	}
	if e.DurationPlayedSec != 3 {
		t.Fatalf("played sec = %d, want 3 (clamped to track duration)", e.DurationPlayedSec)
	}
	if e.StartedAt == 0 {
		t.Fatal("StartedAt must be derived, not left for Record's now()")
	}
	// An episode listen carries Kind="episode" so replay can route it.
	ep, ok := listenEntry(deezer.Track{ID: "9", Name: "Ep", DurationMS: 3000}, history.KindEpisode, 10000)
	if !ok || ep.Kind != history.KindEpisode {
		t.Fatalf("episode entry = %+v ok=%v, want Kind=episode", ep, ok)
	}
}

// waitHistory polls the history store until it holds at least n entries and
// returns them newest-first. recordListen appends on its own goroutine, so
// tests must wait rather than read immediately after the recording call.
func waitHistory(t *testing.T, n int) []history.Entry {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		entries, err := historyStore().Recent(50)
		if err == nil && len(entries) >= n {
			return entries
		}
		if time.Now().After(deadline) {
			t.Fatalf("history has %d entries (err=%v), want %d", len(entries), err, n)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestGaplessPromoteUnsyncedQueueUsesArmedPreload: DZPreload without DZQueueSet. After
// a gapless promote, now-playing moves to the preloaded track, the outgoing track is
// recorded once, and the promoted track on its own finish.
func TestGaplessPromoteUnsyncedQueueUsesArmedPreload(t *testing.T) {
	resetEngineQueue() // this test depends on an EMPTY (unsynced) engine queue
	t.Cleanup(resetEngineQueue)
	t.Cleanup(func() { setCurrentTrack(deezer.Track{}) })
	setHistoryStore(history.New(filepath.Join(t.TempDir(), "history.jsonl")))

	a := deezer.Track{ID: "A", Name: "First", DurationMS: 3000}
	b := deezer.Track{ID: "B", Name: "Second", DurationMS: 4000}
	setCurrentTrack(a) // playing A; the engine queue stays empty (unsynced)
	armPreload(t, b)   // the GUI armed a preload for B (DZPreload)

	// A drains; the player promotes B and keeps playing. The onFinish callback
	// records A (noteTrackFinished), then runs the promote bookkeeping.
	noteTrackFinished()
	if engineSyncOnGaplessPromote() {
		t.Fatal("an unsynced engine queue must not own the finish (the GUI's own queue advances)")
	}
	if got := currentTrack().ID; got != "B" {
		t.Fatalf("currentTrack after promote = %q, want B (the armed preload)", got)
	}
	if _, armed := preloads.Take(); armed {
		t.Fatal("the promote must consume the preload stash (single-use)")
	}
	// Wait for A's (async) history write before finishing B: in production the
	// two finishes are a whole track apart, and the file order is the assertion.
	if got := waitHistory(t, 1); got[0].TrackID != "A" {
		t.Fatalf("promote finish recorded %q, want the outgoing track A", got[0].TrackID)
	}

	// B finishes naturally later: its own listen is recorded, not A again.
	noteTrackFinished()

	entries := waitHistory(t, 2)
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.TrackID
	}
	if len(entries) != 2 || ids[0] != "B" || ids[1] != "A" {
		t.Fatalf("history = %v, want [B A] (newest first, A exactly once)", ids)
	}
}

// TestGaplessPromoteSyncedQueueStillWalks: with the engine queue synced and aligned, a
// gapless promote walks cursor and now-playing and reports the finish as engine-owned.
func TestGaplessPromoteSyncedQueueStillWalks(t *testing.T) {
	t.Cleanup(resetEngineQueue)
	t.Cleanup(func() { setCurrentTrack(deezer.Track{}) })

	a := deezer.Track{ID: "A", DurationMS: 3000}
	b := deezer.Track{ID: "B", DurationMS: 4000}
	queueMu.Lock()
	engineQ.Set([]deezer.Track{a, b}, 0)
	queueMu.Unlock()
	setCurrentTrack(a)
	armPreload(t, b)

	if !engineSyncOnGaplessPromote() {
		t.Fatal("a synced+aligned engine queue must own the promote")
	}
	if got := currentTrack().ID; got != "B" {
		t.Fatalf("currentTrack after owned promote = %q, want B", got)
	}
	if got := QueueIndex(); got != 1 {
		t.Fatalf("queue cursor after owned promote = %d, want 1", got)
	}
	if _, armed := preloads.Take(); armed {
		t.Fatal("an owned promote must also consume the preload stash")
	}
}

// TestRepeatShuffleToggleClearsArmedPreload checks that DZSetRepeat and DZSetShuffle
// invalidate an armed preload, so the stale next is not promoted.
func TestRepeatShuffleToggleClearsArmedPreload(t *testing.T) {
	resetEngineQueue() // this test depends on an EMPTY (unsynced) engine queue
	t.Cleanup(resetEngineQueue)
	t.Cleanup(func() { setCurrentTrack(deezer.Track{}) })

	setCurrentTrack(deezer.Track{ID: "A", DurationMS: 3000})

	armPreload(t, deezer.Track{ID: "stale-next", DurationMS: 3000})
	engineSetRepeat("one")
	if _, armed := preloads.Take(); armed {
		t.Fatal("engineSetRepeat must clear an armed preload")
	}

	armPreload(t, deezer.Track{ID: "stale-next", DurationMS: 3000})
	engineSetShuffle(true)
	if _, armed := preloads.Take(); armed {
		t.Fatal("engineSetShuffle must clear an armed preload")
	}

	// With the preload cleared, a later finish cannot promote the stale next:
	// now-playing stays put and the finish is not engine-owned.
	if engineSyncOnGaplessPromote() {
		t.Fatal("a cleared preload must not make the finish engine-owned")
	}
	if got := currentTrack().ID; got != "A" {
		t.Fatalf("cleared preload still moved now-playing to %q, want A", got)
	}
}

// TestStopRecordsOutgoingListenOnce checks that DZStop and the control Stop record the
// listen once: noteTrackTransition runs before p.Stop() while Playing, and a second
// Stop (already Stopped) records nothing.
func TestStopRecordsOutgoingListenOnce(t *testing.T) {
	t.Cleanup(func() { setCurrentTrack(deezer.Track{}) })
	setHistoryStore(history.New(filepath.Join(t.TempDir(), "history.jsonl")))

	setCurrentTrack(deezer.Track{ID: "77", Name: "Stopped Song", DurationMS: 240000})

	// First Stop: noteTrackTransition runs while the player is still Playing,
	// so the listen-so-far is recorded.
	recordTransition(audio.Playing, 65000)
	entries := waitHistory(t, 1)
	if entries[0].TrackID != "77" || entries[0].DurationPlayedSec != 65 {
		t.Fatalf("stop-listen entry = %+v, want track 77 played 65s", entries[0])
	}

	// Second Stop: the player is Stopped, so the transition records nothing. The guard is
	// synchronous (recordListen is never called), so this is asserted at once.
	recordTransition(audio.Stopped, 65000)
	if got, _ := historyStore().Recent(10); len(got) != 1 {
		t.Fatalf("second Stop double-recorded: history = %+v", got)
	}
}

// TestGetRepeatShuffleLocalVsRouted: while routed, shuffle reads the device's snapshot
// and repeat stays the engine queue's.
func TestGetRepeatShuffleLocalVsRouted(t *testing.T) {
	t.Cleanup(resetEngineQueue)
	t.Cleanup(clearRoutedRemote)

	engineSetRepeat("all")
	engineSetShuffle(true)
	if got := Repeat(); got != "all" {
		t.Fatalf("local repeat = %q, want all", got)
	}
	if !Shuffle() {
		t.Fatal("local shuffle = false, want true")
	}

	rc := control.NewClient("http://127.0.0.1:1", "", "")
	mu.Lock()
	remoteCli = rc
	remoteSt = control.State{Repeat: "off", Shuffle: false}
	mu.Unlock()
	if got := Repeat(); got != "all" {
		t.Fatalf("routed repeat = %q, want the local all", got)
	}
	if Shuffle() {
		t.Fatal("routed shuffle = true, want the remote snapshot's false")
	}
}

// TestRepeatNotForwardedToRoutedRemote: with a device connected,
// setting/cycling repeat records locally but is NOT forwarded to the host (which
// would trap it looping its single-item queue), while shuffle IS still forwarded.
func TestRepeatNotForwardedToRoutedRemote(t *testing.T) {
	t.Cleanup(resetEngineQueue)
	t.Cleanup(clearRoutedRemote)

	var repeatCalls, shuffleCalls int32
	host := control.New(
		control.Config{Addr: "127.0.0.1:0"},
		func() control.State { return control.State{} },
		func() control.Account { return control.Account{} },
		control.Commands{
			SetRepeat:  func(string) { atomic.AddInt32(&repeatCalls, 1) },
			SetShuffle: func(bool) { atomic.AddInt32(&shuffleCalls, 1) },
		},
		nil,
	)
	if err := host.Start(); err != nil {
		t.Fatalf("start fake host: %v", err)
	}
	defer host.Close()
	waitServing(t, host.Addr())

	rc := control.NewClient("http://"+host.Addr(), "", "")
	mu.Lock()
	remoteCli = rc
	remoteSt = control.State{}
	mu.Unlock()

	cmds := engineCommands()
	cmds.SetRepeat("all") // records engineQ=all, must NOT forward
	cmds.CycleRepeat()    // all -> one, must NOT forward
	if n := atomic.LoadInt32(&repeatCalls); n != 0 {
		t.Fatalf("repeat forwarded to the routed host %d times, want 0", n)
	}

	// The mode is still recorded on the engine queue (drives the engine's own
	// auto-advance, which honors repeat when it feeds the host).
	queueMu.Lock()
	local := engineQ.Repeat().String()
	queueMu.Unlock()
	if local != "one" {
		t.Fatalf("engine repeat = %q, want one (off->all->one)", local)
	}

	// Shuffle is still forwarded to the host.
	cmds.SetShuffle(true)
	if n := atomic.LoadInt32(&shuffleCalls); n != 1 {
		t.Fatalf("shuffle forwarded %d times, want 1", n)
	}
}

// TestAutoAdvanceBumpsPlaySeqAndGuardDropsStale: engineAdvanceOnFinish claims a play
// seq, and a later user play bumps past it, so the in-flight resolve is dropped.
func TestAutoAdvanceBumpsPlaySeqAndGuardDropsStale(t *testing.T) {
	t.Cleanup(resetEngineQueue)
	t.Cleanup(func() { setCurrentTrack(deezer.Track{}) })

	queueMu.Lock()
	engineQ.Set([]deezer.Track{{ID: "1"}, {ID: "2"}}, 0)
	queueMu.Unlock()
	setCurrentTrack(deezer.Track{ID: "1"})

	before := plays.seq.Load()
	// No live client/player in tests: the async launch's goroutine no-ops, but
	// engineAdvanceOnFinish still advances the queue and claims a seq.
	if !engineAdvanceOnFinish() {
		t.Fatal("auto-advance should own the finish of the queue's current track")
	}
	seqAtLaunch := plays.seq.Load()
	if seqAtLaunch == before {
		t.Fatal("engineAdvanceOnFinish must claim a seq for its async launch")
	}
	if plays.superseded(seqAtLaunch) {
		t.Fatal("a resolve must not be superseded before any newer play")
	}
	// A user play claims a seq: the in-flight resolve is stale.
	plays.claim()
	if !plays.superseded(seqAtLaunch) {
		t.Fatal("a newer play must supersede the stale auto-advance resolve")
	}
	// The stale resolve is dropped at the start, before it touches the player.
	if err := startPlay(&audio.Player{}, &deezer.StreamPlan{CDNURL: "http://127.0.0.1/a"}, 0, seqAtLaunch, func() { t.Fatal("stale play started") }); !errors.Is(err, errSuperseded) {
		t.Fatalf("startPlay(stale) = %v, want errSuperseded", err)
	}
}

// A cached plan whose entry is gone is planned again before the start step:
// a re-plan that hangs does not hold up a newer play's start, and the play it
// was for does not start once a newer one was claimed.
func TestBlockingReplanDoesNotHoldNewerPlay(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	plan := &deezer.StreamPlan{TrackID: "1", Format: "MP3_128", Refresh: func() (*deezer.StreamPlan, error) {
		close(entered)
		<-release
		return nil, errors.New("offline")
	}}
	first := plays.claim()
	result := make(chan error, 1)
	go func() {
		result <- startPlay(&audio.Player{}, plan, 0, first, func() { t.Error("a superseded play started") })
	}()
	<-entered
	plays.claim() // a newer play
	free := make(chan struct{})
	go func() {
		plays.startMu.Lock()
		plays.startMu.Unlock()
		close(free)
	}()
	select {
	case <-free:
	case <-time.After(5 * time.Second):
		t.Fatal("a newer play's start waited for a re-plan")
	}
	close(release)
	if err := <-result; !errors.Is(err, errSuperseded) {
		t.Fatalf("the first play returned %v, want errSuperseded", err)
	}
}

// A finish while a user play resolves does not auto-advance after it.
func TestFinishDuringUserPlayDoesNotAdvance(t *testing.T) {
	t.Cleanup(resetEngineQueue)
	t.Cleanup(func() { setCurrentTrack(deezer.Track{}) })
	queueMu.Lock()
	engineQ.Set([]deezer.Track{{ID: "1"}, {ID: "2"}}, 0)
	queueMu.Unlock()
	setCurrentTrack(deezer.Track{ID: "1"})

	_, done := plays.beginUser()
	owned := engineAdvanceOnFinish()
	done()
	queueMu.Lock()
	idx := engineQ.Index()
	queueMu.Unlock()
	if !owned || idx != 0 {
		t.Fatalf("finish during a user play: owned %v, cursor %d; want owned and still 0", owned, idx)
	}
}

// TestErroredFinishNotRecorded: a finish that carries a player error
// with ~0 played (a CDN/decode failure) is neither logged as a listen nor
// auto-advanced, while a clean finish (and an error deep into the track) records.
func TestErroredFinishNotRecorded(t *testing.T) {
	t.Cleanup(func() { setCurrentTrack(deezer.Track{}) })
	setHistoryStore(history.New(filepath.Join(t.TempDir(), "history.jsonl")))

	prev := deezer.Track{ID: "E", Name: "Failed", DurationMS: 200000}
	setCurrentTrack(prev)

	// An errored finish records nothing (recordFinished returns before spawning
	// the async write, so an immediate read is authoritative).
	recordFinished(prev, "", audio.Errored, 0, true)
	if got, _ := historyStore().Recent(10); len(got) != 0 {
		t.Fatalf("errored finish was recorded: %+v", got)
	}

	// A clean finish IS recorded.
	recordFinished(prev, "", audio.Stopped, 0, true)
	if entries := waitHistory(t, 1); entries[0].TrackID != "E" {
		t.Fatalf("clean finish recorded %q, want E", entries[0].TrackID)
	}
}

// TestSetRemoteStateIdentityChecked: setRemoteState applies only for the current remote
// client; a late response from a previous device changes neither state nor the
// finished counter.
func TestSetRemoteStateIdentityChecked(t *testing.T) {
	t.Cleanup(clearRoutedRemote)

	rcA := control.NewClient("http://127.0.0.1:1", "", "") // the OLD device
	rcB := control.NewClient("http://127.0.0.1:2", "", "") // the CURRENT device
	mu.Lock()
	remoteCli = rcB
	remoteSt = control.State{State: "playing", DurationMS: 1000, PositionMS: 900}
	startFinished := finished
	mu.Unlock()

	// Late response from A (not current): ignored, state and the finished counter are
	// unchanged.
	setRemoteState(rcA, control.State{State: "stopped", DurationMS: 1000, PositionMS: 1000})
	mu.Lock()
	stA, finA := remoteSt, finished
	mu.Unlock()
	if stA.State != "playing" {
		t.Fatalf("a late response from a non-current device overwrote B's state: %+v", stA)
	}
	if finA != startFinished {
		t.Fatalf("a non-current device bumped finished: %d -> %d", startFinished, finA)
	}

	// Current device B applies, and its playing->stopped-near-end bumps finished.
	setRemoteState(rcB, control.State{State: "stopped", DurationMS: 1000, PositionMS: 1000})
	mu.Lock()
	stB, finB := remoteSt, finished
	mu.Unlock()
	if stB.State != "stopped" {
		t.Fatalf("current-device response not applied: %+v", stB)
	}
	if finB != startFinished+1 {
		t.Fatalf("current device end-of-track did not bump finished: %d -> %d", startFinished, finB)
	}
}

// ---- Phase-4 corelib wiring: SetQueue hook, NotifyFinished, queue edits, offline ----

// armPreload arms tr as the preload, as the near-end hook does after a
// request; for the rest of the test the preloader resolves with no network,
// on a gapless player of its own.
func armPreload(t *testing.T, tr deezer.Track) {
	t.Helper()
	prev := preloads
	p := &audio.Player{}
	p.SetGapless(true)
	preloads = preload.New(func() *audio.Player { return p }, func(deezer.Track) (*deezer.StreamPlan, deezer.Track, error) {
		return &deezer.StreamPlan{TrackID: tr.ID}, tr, nil
	})
	t.Cleanup(func() { preloads = prev })
	preloads.Request(tr.ID, tr.DurationMS)
	preloads.Resolve()
}

// resetOfflineGlobals waits for a test's offline fetches and auto-advance launches,
// then starts a fresh media cache host, offline store and mode, so no cache or pin
// leaks into a later test.
func resetOfflineGlobals() {
	advancing.Wait()
	waitJobsDone(nil)
	jobMu.Lock()
	jobs = nil
	jobMu.Unlock()
	if c := caches.Current(); c != nil {
		c.Close()
	}
	caches = &mediacache.Host{Attach: caches.Attach, Pinned: caches.Pinned}
	offMu.Lock()
	offLoaded, off = false, offlineData{}
	offMu.Unlock()
	offlineOn.Store(false)
	offlineAuto.Store(false)
	fetchStream = audio.Fetch
	quality.Store(0)
}

// waitJobsDone waits until no offline fetch runs (failing t when one still
// runs after 10 s; t nil just stops waiting).
func waitJobsDone(t *testing.T) {
	deadline := time.Now().Add(10 * time.Second)
	for {
		jobMu.Lock()
		running := slices.ContainsFunc(jobs, func(j *offlineJob) bool { return j.running })
		jobMu.Unlock()
		if !running {
			return
		}
		if time.Now().After(deadline) {
			if t != nil {
				t.Fatal("offline fetches still running")
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestSetQueueCommandHookReplacesEngineQueue checks that SetQueue(tracksJSON, index)
// replaces engineQ with the cursor at index and shows in engineState, as the mobile
// binding's hook does.
func TestSetQueueCommandHookReplacesEngineQueue(t *testing.T) {
	t.Cleanup(resetEngineQueue)
	clearRoutedRemote()

	cmds := engineCommands()
	if cmds.SetQueue == nil {
		t.Fatal("SetQueue hook must be wired")
	}
	if err := cmds.SetQueue(`[{"id":"a"},{"id":"b"},{"id":"c"}]`, 1); err != nil {
		t.Fatalf("SetQueue: %v", err)
	}
	st := engineState()
	got := make([]string, len(st.Queue))
	for i, tr := range st.Queue {
		got[i] = tr.ID
	}
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("queue after SetQueue = %v, want [a b c]", got)
	}
	if QueueIndex() != 1 {
		t.Fatalf("cursor after SetQueue(index=1) = %d, want 1", QueueIndex())
	}
	// A bad payload is surfaced as an error and leaves the queue untouched.
	if err := cmds.SetQueue(`{"not":"an array"}`, 0); err == nil {
		t.Fatal("SetQueue with a non-array payload must error")
	}
	if QueueIndex() != 1 {
		t.Fatalf("a failed SetQueue disturbed the cursor: %d", QueueIndex())
	}
}

// TestOnNaturalFinishNotifiesControlServer checks that onFinish publishes the finished
// edge (NotifyFinished) with the track id and bumps the GUI counter when the engine
// queue did not own the finish.
func TestOnNaturalFinishNotifiesControlServer(t *testing.T) {
	t.Cleanup(resetEngineQueue)
	hist := history.New(filepath.Join(t.TempDir(), "history.jsonl"))
	setHistoryStore(hist)
	clearRoutedRemote()

	// Empty engine queue: engineAdvanceOnFinish returns false, so the GUI
	// finished-counter path runs and NotifyFinished still fires.
	queueMu.Lock()
	engineQ.Set(nil, 0)
	queueMu.Unlock()

	srv := control.New(control.Config{Addr: "127.0.0.1:0"}, engineState, engineAccount, engineCommands(), deezer.New("arl"))
	if err := srv.Start(); err != nil {
		t.Fatalf("start control server: %v", err)
	}
	waitServing(t, srv.Addr())
	mu.Lock()
	ctrlSrv = srv
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		s := ctrlSrv
		ctrlSrv, ctrlSrvUserID = nil, ""
		mu.Unlock()
		if s != nil {
			s.Close()
		}
	})

	ready := make(chan struct{})
	gotFinished := make(chan string, 1)
	go func() {
		resp, err := http.Get("http://" + srv.Addr() + "/events")
		if err != nil {
			close(ready)
			return
		}
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sawState, expectFinishedData := false, false
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: finished"):
				expectFinishedData = true
			case strings.HasPrefix(line, "event: state"):
				if !sawState {
					sawState = true
					close(ready) // subscriber is registered
				}
			case expectFinishedData && strings.HasPrefix(line, "data: "):
				var fe struct {
					TrackID string `json:"trackId"`
				}
				_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &fe)
				gotFinished <- fe.TrackID
				return
			}
		}
	}()

	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("SSE subscriber never received the initial state event")
	}

	mu.Lock()
	before := finished
	mu.Unlock()
	setCurrentTrack(deezer.Track{ID: "t1", DurationMS: 200000})

	onNaturalFinish()

	select {
	case id := <-gotFinished:
		if id != "t1" {
			t.Fatalf("finished SSE event trackId = %q, want t1", id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no finished SSE event after a natural finish")
	}

	mu.Lock()
	after := finished
	mu.Unlock()
	if after != before+1 {
		t.Fatalf("GUI finished-counter: %d -> %d, want +1 (unsynced queue)", before, after)
	}
	// The finish is recorded on its own goroutine: the test ends only once that
	// write is in, so it cannot land in the temp folder as it is removed.
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if es, _ := hist.Recent(1); len(es) == 1 && es[0].TrackID == "t1" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the finish was not recorded in the history")
		}
	}
}

// TestTracksFromInsertJSONAcceptsObjectOrArray proves DZQueueInsertNext's
// flexible decoder: a single wire object, an array of them, and rejects junk.
func TestTracksFromInsertJSONAcceptsObjectOrArray(t *testing.T) {
	if ts := tracksFromInsertJSON(`{"id":"solo","name":"S"}`); len(ts) != 1 || ts[0].ID != "solo" {
		t.Fatalf("single object decode = %+v, want one track id=solo", ts)
	}
	if ts := tracksFromInsertJSON(`[{"id":"a"},{"id":"b"}]`); len(ts) != 2 || ts[0].ID != "a" || ts[1].ID != "b" {
		t.Fatalf("array decode = %+v, want [a b]", ts)
	}
	if ts := tracksFromInsertJSON(``); len(ts) != 0 {
		t.Fatalf("empty payload = %+v, want none", ts)
	}
	if ts := tracksFromInsertJSON(`{"no":"id"}`); len(ts) != 0 {
		t.Fatalf("id-less object = %+v, want none", ts)
	}
	if ts := tracksFromInsertJSON(`not json`); len(ts) != 0 {
		t.Fatalf("junk payload = %+v, want none", ts)
	}
}

// TestQueueEditExportsMutateEngineQueue checks DZQueueInsertNext, DZQueueRemove and
// DZQueueMove: insert keeps order after the cursor, remove guards the playing row,
// move follows the cursor, and each bumps the version.
func TestQueueEditExportsMutateEngineQueue(t *testing.T) {
	t.Cleanup(resetEngineQueue)
	clearRoutedRemote()

	engineQueueSet([]deezer.Track{{ID: "1"}, {ID: "2"}, {ID: "3"}}) // cursor 0
	v0 := QueueVersion()

	// Insert-next (via the export's decoder): array order preserved after cursor.
	QueueInsertNext(`[{"id":"X"},{"id":"Y"}]`)
	ids := func() []string {
		ts, _, _ := QueueSnapshot()
		out := make([]string, len(ts))
		for i, tr := range ts {
			out[i] = tr.ID
		}
		return out
	}
	if got, want := ids(), []string{"1", "X", "Y", "2", "3"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("after insert-next = %v, want %v", got, want)
	}
	if QueueVersion() <= v0 {
		t.Fatal("insert-next did not bump the queue version")
	}

	// Remove guards the playing row (index 0), removes a non-playing row.
	if err := QueueRemove(0); err == nil {
		t.Fatal("removing the playing row must error")
	}
	if err := QueueRemove(1); err != nil { // drop "X"
		t.Fatalf("QueueRemove(1): %v", err)
	}
	if got, want := ids(), []string{"1", "Y", "2", "3"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("after remove = %v, want %v", got, want)
	}

	// Move reorders without changing what's audible (cursor stays on "1").
	if err := QueueMove(3, 0); err != nil {
		t.Fatalf("QueueMove(3,0): %v", err)
	}
	if got, want := ids(), []string{"3", "1", "Y", "2"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("after move = %v, want %v", got, want)
	}
	if cur, _ := func() (deezer.Track, bool) {
		queueMu.Lock()
		defer queueMu.Unlock()
		return engineQ.Current()
	}(); cur.ID != "1" {
		t.Fatalf("move changed the audible track to %q, want 1", cur.ID)
	}
}

// TestPreparePlanPrefersCache: with the track's meta in the media cache, preparePlan
// returns a cache plan (CDNURL ""); a miss goes to PrepareStream, which errors "not
// logged in" here.
func TestPreparePlanPrefersCache(t *testing.T) {
	cacheProfile(t)
	if err := caches.SetMB(8); err != nil {
		t.Fatal(err)
	}
	mc := caches.Current()
	mctest.PutBody(t, mc, "cached1.MP3_128")
	if err := mc.PutMeta("cached1.MP3_128", mediacache.StreamMeta{Format: "MP3_128", Encrypted: true, GainDB: -3}); err != nil {
		t.Fatalf("PutMeta: %v", err)
	}

	c := deezer.New("arl") // not logged in: a cache hit must not need the network

	plan, err := preparePlan(c, "cached1")
	if err != nil {
		t.Fatalf("preparePlan(cached) unexpected error: %v", err)
	}
	if plan.CDNURL != "" {
		t.Fatalf("cache-sourced plan must have empty CDNURL, got %q", plan.CDNURL)
	}
	if plan.Format != "MP3_128" || !plan.Encrypted {
		t.Fatalf("cache-sourced plan meta mismatch: %+v", plan)
	}
	// Cache miss on an un-logged-in client falls back to the network path.
	if _, err := preparePlan(c, "missing2"); err == nil {
		t.Fatal("preparePlan on a cache miss (not logged in) must error via PrepareStream")
	}
}

// Turning the phone remote off restores the server it replaced.
func TestWebRemoteOffRestoresPriorServer(t *testing.T) {
	t.Cleanup(func() {
		ctrlMu.Lock()
		rebuildControlServer(control.Config{}, nil)
		webRemotePrev = nil
		ctrlMu.Unlock()
	})
	ctrlMu.Lock()
	rebuildControlServer(control.Config{WebRemote: true}, nil, "127.0.0.1:0")
	webRemotePrev = &control.Config{Addr: "127.0.0.1:0"}
	ctrlMu.Unlock()

	WebRemoteSetEnabled(false)
	mu.Lock()
	srv := ctrlSrv
	mu.Unlock()
	if srv == nil || !config.IsLoopbackAddr(srv.Addr()) || srv.WebRemoteEnabled() || webRemotePrev != nil {
		t.Fatalf("web remote off: want the prior loopback server back, got %v", srv)
	}
}

// The configured control server started at login replaces a phone-remote
// server started before it, so turning the remote off later restores nothing.
func TestStartServicesForgetsPhoneRemotePrior(t *testing.T) {
	t.Setenv("OPENDEEZER_CONTROL", "127.0.0.1:0")
	t.Cleanup(func() {
		ctrlMu.Lock()
		rebuildControlServer(control.Config{}, nil)
		webRemotePrev = nil
		ctrlMu.Unlock()
	})
	servicesOnce = sync.Once{}
	ctrlMu.Lock()
	webRemotePrev = &control.Config{Addr: "127.0.0.1:1"}
	ctrlMu.Unlock()

	startServices(nil)
	ctrlMu.Lock()
	prev := webRemotePrev
	ctrlMu.Unlock()
	if prev != nil {
		t.Fatalf("webRemotePrev = %+v after startServices, want nil", prev)
	}
}

// An auto-advance skips tracks that cannot be resolved with Next, so repeat-one
// does not retry the same track, and stops after maxAdvanceFailures.
func TestAutoAdvanceSkipsUnresolvableWithNext(t *testing.T) {
	t.Cleanup(resetEngineQueue)
	t.Cleanup(resetOfflineGlobals)
	clearRoutedRemote()
	mu.Lock()
	prevClient := client
	client = deezer.New("arl") // not logged in: every resolve fails
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		client = prevClient
		mu.Unlock()
	})
	queueMu.Lock()
	engineQ.Set([]deezer.Track{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}, 0)
	engineQ.SetRepeat(queue.RepeatOne)
	queueMu.Unlock()

	engineAutoAdvance(deezer.Track{ID: "a"}, plays.claim())
	if i := QueueIndex(); i != maxAdvanceFailures-1 {
		t.Fatalf("cursor after %d failures = %d, want %d", maxAdvanceFailures, i, maxAdvanceFailures-1)
	}
}

// An auto-advance whose queue ends before maxAdvanceFailures unresolvable
// tracks still reports why it stopped.
func TestAutoAdvanceFailsWhenQueueEnds(t *testing.T) {
	t.Cleanup(resetEngineQueue)
	mu.Lock()
	prevClient, prevPlayer := client, player
	client = deezer.New("arl") // not logged in: every resolve fails
	player = &audio.Player{}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		client, player = prevClient, prevPlayer
		mu.Unlock()
	})
	queueMu.Lock()
	engineQ.Set([]deezer.Track{{ID: "a"}, {ID: "b"}}, 0)
	queueMu.Unlock()

	engineAutoAdvance(deezer.Track{ID: "a"}, plays.claim())
	if st, msg := Player().State(), Player().LastError(); st != audio.Errored || msg == "" {
		t.Fatalf("player %v %q after the queue ran out of resolvable tracks; want Errored with the reason", st, msg)
	}
}

// A recorded listen keeps the track's artwork and length.
func TestListenEntryKeepsArtworkAndLength(t *testing.T) {
	tr := deezer.Track{ID: "7", Name: "Song", DurationMS: 215000, ArtworkURL: "https://cdn/cover.jpg"}
	e, ok := listenEntry(tr, "", 60000)
	if !ok || e.ArtworkURL != tr.ArtworkURL || e.DurationMS != 215000 || e.DurationPlayedSec != 60 {
		t.Fatalf("entry = %+v, want artwork, durationMs 215000, 60 s played", e)
	}
	b, _ := json.Marshal(e)
	if !strings.Contains(string(b), `"artworkUrl":"https://cdn/cover.jpg"`) || !strings.Contains(string(b), `"durationMs":215000`) {
		t.Fatalf("wire entry %s lacks artworkUrl/durationMs", b)
	}
}

// Songs recorded without artwork or length get them from a track lookup, once
// per id, also when the lookup fails; episodes are not looked up.
func TestFillHistoryMetaLooksUpEachTrackOnce(t *testing.T) {
	var mu sync.Mutex
	lookups := map[string]int{}
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			_, _ = w.Write([]byte(`{"error":{},"results":{"checkForm":"tok","USER":{"USER_ID":"1","BLOG_NAME":"t","OPTIONS":{"license_token":"lic"}},"OFFERS":[{"title":"P"}]}}`))
		case "song.getData":
			var body struct {
				ID json.Number `json:"sng_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			lookups[body.ID.String()]++
			mu.Unlock()
			if body.ID.String() == "404" {
				_, _ = w.Write([]byte(`{"error":{"DATA_ERROR":"song not found"},"results":{}}`))
				return
			}
			_, _ = w.Write([]byte(`{"error":{},"results":{"SNG_ID":"` + body.ID.String() + `","SNG_TITLE":"S","DURATION":"200","ALB_PICTURE":"pic` + body.ID.String() + `"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer gw.Close()
	target, _ := url.Parse(gw.URL)
	orig := deezer.Transport()
	deezer.SetTransport(rtFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
		return orig.RoundTrip(r)
	}))
	defer deezer.SetTransport(orig)
	trackMetaMu.Lock()
	trackMeta = map[string]deezer.Track{}
	trackMetaMu.Unlock()

	c := deezer.New("arl")
	if err := c.Login(); err != nil {
		t.Fatal(err)
	}
	entries := func() []history.Entry {
		return []history.Entry{
			{TrackID: "1"},
			{TrackID: "1"},
			{TrackID: "2", ArtworkURL: "kept", DurationMS: 0},
			{TrackID: "3", ArtworkURL: "kept", DurationMS: 1000},
			{TrackID: "e", Kind: history.KindEpisode},
			{TrackID: "404"},
		}
	}
	got := entries()
	fillHistoryMeta(c, got)
	if !strings.Contains(got[0].ArtworkURL, "pic1") || got[0].DurationMS != 200000 || got[1] != got[0] {
		t.Fatalf("song 1 = %+v, want its artwork and 200000 ms", got[0])
	}
	if got[2].ArtworkURL != "kept" || got[2].DurationMS != 200000 {
		t.Fatalf("song 2 = %+v, want its own artwork kept and the length filled", got[2])
	}
	if got[4].ArtworkURL != "" || got[4].DurationMS != 0 {
		t.Fatalf("episode = %+v, want it untouched", got[4])
	}
	fillHistoryMeta(c, entries())
	mu.Lock()
	defer mu.Unlock()
	if lookups["1"] != 1 || lookups["2"] != 1 || lookups["3"] != 0 || lookups["e"] != 0 || lookups["404"] != 1 {
		t.Fatalf("lookups = %v, want 1, 2 and the unknown 404 once, 3 and e never", lookups)
	}
}

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Without a length from the player (a play started with none), the current
// track's length is reported once its metadata has arrived.
func TestDurationFallsBackToCurrentTrack(t *testing.T) {
	clearRoutedRemote()
	t.Cleanup(func() { setCurrentTrack(deezer.Track{}) })
	setCurrentTrack(deezer.Track{ID: "9", DurationMS: 215000})
	if got := DurationMS(); got != 215000 {
		t.Fatalf("DurationMS = %d, want 215000", got)
	}
}
