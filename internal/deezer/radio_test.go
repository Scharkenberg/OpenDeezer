package deezer

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// Flow asks for the account's tuner, read once from Pipe in lowercase, and a
// mood's Flow adds its config id.
func TestFlowTunerAndMood(t *testing.T) {
	var bodies []string
	f := &pipeFake{ttl: time.Hour,
		reply: func(string, map[string]any) string {
			return `{"data":{"me":{"id":"7","flowTuner":{"discoveryTuner":"DISCOVERY"}}}}`
		},
		gwReply: func(method, body string) string {
			bodies = append(bodies, method+" "+body)
			return string(gwTracksPage(1, 3))
		}}
	c := f.client()
	if ts, err := c.Flow(""); err != nil || len(ts) != 3 {
		t.Fatalf("%d tracks, %v", len(ts), err)
	}
	if _, err := c.Flow("chill"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		`radio.getUserRadio {"tuner":"discovery","user_id":"7"}`,
		`radio.getUserRadio {"config_id":"chill","tuner":"discovery","user_id":"7"}`,
	}
	if strings.Join(bodies, "\n") != strings.Join(want, "\n") || len(f.ops) != 1 || f.ops[0] != "Tuner" {
		t.Fatalf("gw %q, pipe %v", bodies, f.ops)
	}
}

// Flow moods are read in one query, one flowConfig per mood, in the web
// player's order.
func TestFlowMoods(t *testing.T) {
	f := &pipeFake{ttl: time.Hour, reply: func(string, map[string]any) string {
		var b strings.Builder
		b.WriteString(`{"data":{`)
		for i, id := range flowMoodIDs {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `"m%d":{"id":%q,"title":"T-%s","visuals":{"dynamicPageIcon":{"id":"i","urls":["u-%s"]}}}`, i, id, id, id)
		}
		b.WriteString(`}}`)
		return b.String()
	}}
	got, err := f.client().FlowMoods()
	if err != nil || len(got) != len(flowMoodIDs) {
		t.Fatalf("%+v, %v", got, err)
	}
	for i, m := range got {
		id := flowMoodIDs[i]
		if m != (FlowMood{ID: id, Title: "T-" + id}) {
			t.Fatalf("mood %d: %+v", i, m)
		}
	}
	if len(f.ops) != 1 || f.ops[0] != "FlowMoods" {
		t.Fatalf("ops %v, want one FlowMoods", f.ops)
	}
}

// A tuner read that fails is not tried again on each Flow fetch.
func TestFlowTunerFailureRemembered(t *testing.T) {
	f := &pipeFake{ttl: time.Hour,
		reply:   func(string, map[string]any) string { return `{"errors":[{"message":"no"}]}` },
		gwReply: func(string, string) string { return string(gwTracksPage(1, 3)) }}
	c := f.client()
	for i := 0; i < 3; i++ {
		if _, err := c.Flow(""); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.ops) != 1 {
		t.Fatalf("%d tuner reads for 3 Flow fetches, want 1", len(f.ops))
	}
}

// A radio list is topped up when four tracks or fewer are left, with tracks
// it does not hold yet, and ends when a batch brings nothing new.
func TestRadioFill(t *testing.T) {
	batches := [][]int{{3, 4, 5, 6}, {5, 6}}
	var calls []string
	var mu sync.Mutex
	f := &pipeFake{ttl: time.Hour,
		reply: func(string, map[string]any) string {
			return `{"data":{"me":{"flowTuner":{"discoveryTuner":"DEFAULT"}}}}`
		},
		gwReply: func(method, body string) string {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, method)
			b := batches[0]
			if len(batches) > 1 {
				batches = batches[1:]
			}
			return string(gwTracksPage(b[0], len(b)))
		}}
	c := f.client()
	queued := func(ids ...int) func() []Track {
		return func() []Track {
			ts := make([]Track, len(ids))
			for i, id := range ids {
				ts[i] = Track{ID: fmt.Sprint(id)}
			}
			return ts
		}
	}
	var r RadioFill
	if got := r.More(c, "album_page", "1", 0, queued(1)); got != nil || len(calls) != 0 {
		t.Fatalf("an album was topped up: %v", got)
	}
	if got := r.More(c, "dynamic_page_user_radio", "7", 5, queued(1, 2, 3, 4, 5, 6)); got != nil || len(calls) != 0 {
		t.Fatalf("topped up with five left: %v", got)
	}
	got := r.More(c, "dynamic_page_user_radio", "7", 4, queued(1, 2, 3, 4))
	if len(got) != 2 || got[0].ID != "5" || got[1].ID != "6" {
		t.Fatalf("first top-up %v", got)
	}
	if got := r.More(c, "dynamic_page_user_radio", "7", 2, queued(1, 2, 3, 4, 5, 6)); got != nil {
		t.Fatalf("a batch of known tracks added %v", got)
	}
	if got := r.More(c, "dynamic_page_user_radio", "7", 1, queued(1, 2, 3, 4, 5, 6)); got != nil || len(calls) != 2 {
		t.Fatalf("an ended radio fetched again: %v, %d calls", got, len(calls))
	}
	// Another radio starts over.
	batches = [][]int{{9}}
	if got := r.More(c, "track_mix_contextual", "8", 0, queued(8)); len(got) != 1 || got[0].ID != "9" || calls[2] != "song.getSearchTrackMix" {
		t.Fatalf("a new radio: %v, calls %v", got, calls)
	}
}
