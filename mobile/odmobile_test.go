package odmobile

import (
	"encoding/json"
	"strings"
	"testing"
)

// resetEngineQueue restores the engine queue to its pristine (unsynced) state
// so tests can't leak queue contents / repeat / shuffle into each other.
func resetEngineQueue() {
	_ = SetQueueJSON("[]")
	SetRepeat(0)
	SetShuffle(0)
}

// queueIDs returns the ids of the engine queue, from QueueJSON.
func queueIDs(t *testing.T) []string {
	t.Helper()
	var q struct {
		Tracks []struct {
			ID string `json:"id"`
		} `json:"tracks"`
	}
	if err := json.Unmarshal([]byte(QueueJSON()), &q); err != nil {
		t.Fatalf("QueueJSON unmarshal: %v", err)
	}
	ids := make([]string, len(q.Tracks))
	for i, tr := range q.Tracks {
		ids[i] = tr.ID
	}
	return ids
}

// AppendQueueJSON adds to the end, keeps the cursor and bumps the version;
// a bad payload changes nothing.
func TestAppendQueueJSON(t *testing.T) {
	t.Cleanup(resetEngineQueue)
	if err := SetQueueJSON(`[{"id":"a"},{"id":"b"}]`); err != nil {
		t.Fatal(err)
	}
	SetQueueIndex(1)
	v := QueueVersion()
	if err := AppendQueueJSON(`[{"id":"c","durationMs":5000}]`); err != nil {
		t.Fatal(err)
	}
	if QueueIndex() != 1 || QueueVersion() != v+1 || !strings.Contains(QueueJSON(), `"c"`) {
		t.Fatalf("index %d, version %d (was %d), queue %s", QueueIndex(), QueueVersion(), v, QueueJSON())
	}
	if err := AppendQueueJSON(`{"id":`); err == nil || QueueVersion() != v+1 {
		t.Fatalf("bad payload: err %v, version %d", err, QueueVersion())
	}
}

// TestSetQueueJSONRejectsBadPayload proves malformed JSON errors instead of
// silently clearing the queue.
func TestSetQueueJSONRejectsBadPayload(t *testing.T) {
	t.Cleanup(resetEngineQueue)

	if err := SetQueueJSON(`[{"id":"1"}]`); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}
	if err := SetQueueJSON(`{"not":"an array"}`); err == nil {
		t.Fatal("non-array payload must be rejected")
	}
	if err := SetQueueJSON(`[{"id":`); err == nil {
		t.Fatal("truncated payload must be rejected")
	}
	// A rejected payload must leave the previously synced queue untouched.
	if got := QueueIndex(); got != 0 {
		t.Fatalf("bad payload disturbed the queue: index = %d, want 0", got)
	}
}

// TestPreloadRequiresEngine proves Preload surfaces "engine not ready" before
// Init instead of panicking, and ClearPreload is a safe no-op without a player.
func TestPreloadRequiresEngine(t *testing.T) {
	if err := Preload("123"); err == nil {
		t.Fatal("Preload before Init must return an error")
	}
	ClearPreload() // must not panic without a player
}

// TestQueueJSONExport proves the QueueJSON app-resync export carries version,
// cursor and the wire-shaped track list.
func TestQueueJSONExport(t *testing.T) {
	t.Cleanup(resetEngineQueue)

	if err := SetQueueJSON(`[{"id":"a","name":"Alpha"},{"id":"b"}]`); err != nil {
		t.Fatalf("SetQueueJSON: %v", err)
	}
	SetQueueIndex(1)

	var got struct {
		Version uint64 `json:"version"`
		Index   int    `json:"index"`
		Tracks  []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"tracks"`
	}
	if err := json.Unmarshal([]byte(QueueJSON()), &got); err != nil {
		t.Fatalf("QueueJSON unmarshal: %v", err)
	}
	if got.Index != 1 || len(got.Tracks) != 2 || got.Tracks[0].ID != "a" || got.Tracks[0].Name != "Alpha" {
		t.Fatalf("QueueJSON = %+v", got)
	}
	if got.Version == 0 {
		t.Fatal("QueueJSON version missing (Set must bump it)")
	}
}

// TestFavoriteIDsJSONNoClient proves FavoriteIDsJSON degrades to a valid empty
// JSON array (never null/error) when not logged in.
func TestFavoriteIDsJSONNoClient(t *testing.T) {
	if got := FavoriteIDsJSON(); got != "[]" {
		t.Fatalf("FavoriteIDsJSON with no client = %q, want []", got)
	}
}

// TestQueueEditExports proves the queue-edit exports for Android/iOS queue
// screens: they mutate the engine queue, bump the version, and are safe (no
// panic) on bad indices and payloads.
func TestQueueEditExports(t *testing.T) {
	t.Cleanup(resetEngineQueue)

	if err := SetQueueJSON(`[{"id":"a"},{"id":"b"},{"id":"c"}]`); err != nil {
		t.Fatalf("SetQueueJSON: %v", err)
	}
	v0 := QueueVersion()

	QueueInsertNext(`{"id":"X","name":"NextOne"}`)
	// X should be after current (0)
	if got := queueIDs(t); len(got) != 4 || got[1] != "X" {
		t.Fatalf("after InsertNext: %v", got)
	}
	if v1 := QueueVersion(); v1 <= v0 {
		t.Fatalf("version not bumped on insert: %d", v1)
	}

	QueueRemove(2) // 0a 1X 2b 3c -> 0a 1X 2c
	if got := queueIDs(t); len(got) != 3 || got[0] != "a" || got[1] != "X" || got[2] != "c" {
		t.Fatalf("after Remove: %v", got)
	}

	QueueMove(0, 2) // a X c -> X c a
	if got := queueIDs(t); got[0] != "X" || got[2] != "a" {
		t.Fatalf("after Move: %v", got)
	}

	// bad indices are no-ops, no panic
	QueueRemove(99)
	QueueMove(-1, 5)
	QueueInsertNext(`{"not":"valid"}`) // noop
	if got := QueueVersion(); got == 0 {
		t.Fatal("version zero after edits")
	}
}
