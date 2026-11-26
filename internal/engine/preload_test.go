package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/audio"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
)

// preloadEngine logs a client in against a fake gateway and installs it with
// an idle player; it returns the number of stream resolves (song.getData)
// the gateway answered. Streams resolve to the gateway's /stream.
func preloadEngine(t *testing.T) *atomic.Int32 {
	t.Helper()
	var resolves atomic.Int32
	var gw *httptest.Server
	gw = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/get_url") {
			_, _ = w.Write([]byte(`{"data":[{"media":[{"format":"MP3_128","sources":[{"url":"` + gw.URL + `/stream"}]}]}]}`))
			return
		}
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			_, _ = w.Write([]byte(`{"error":{},"results":{"checkForm":"tok","USER":{"USER_ID":"1","BLOG_NAME":"t","OPTIONS":{"license_token":"lic"}},"OFFERS":[{"title":"P"}]}}`))
		case "song.getData":
			var body struct {
				ID json.Number `json:"sng_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			resolves.Add(1)
			_, _ = w.Write([]byte(`{"error":{},"results":{"SNG_ID":"` + body.ID.String() + `","SNG_TITLE":"S","DURATION":"200","TRACK_TOKEN":"tt","GAIN":"0"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(gw.Close)
	target, _ := url.Parse(gw.URL)
	orig := deezer.Transport()
	deezer.SetTransport(rtFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
		return orig.RoundTrip(r)
	}))
	t.Cleanup(func() { deezer.SetTransport(orig) })

	c := deezer.New("arl")
	if err := c.Login(); err != nil {
		t.Fatal(err)
	}
	p := &audio.Player{}
	p.SetGapless(true)
	mu.Lock()
	prevClient, prevPlayer := client, player
	client, player = c, p
	mu.Unlock()
	t.Cleanup(func() {
		preloads.Clear()
		mu.Lock()
		client, player = prevClient, prevPlayer
		mu.Unlock()
	})
	preloads.Clear()
	if _, err := preparePlan(c, "9"); err != nil {
		t.Fatalf("the fake gateway does not resolve a stream: %v", err)
	}
	resolves.Store(0)
	return &resolves
}

// Preload only records the request: nothing is resolved until the player's
// near-end hook runs, which resolves each requested track once.
func TestPreloadResolvesAtNearEndOncePerID(t *testing.T) {
	resolves := preloadEngine(t)

	preloads.Request("2", 200000)
	preloads.Request("2", 200000) // a queue edit asks again for the same track
	if n := resolves.Load(); n != 0 {
		t.Fatalf("%d resolves before the near-end hook, want 0", n)
	}
	preloads.Resolve()
	if n := resolves.Load(); n != 1 {
		t.Fatalf("%d resolves after the near-end hook, want 1", n)
	}
	preloads.Request("2", 200000) // already preloaded
	preloads.Resolve()            // a seek back and forth runs the hook again
	if n := resolves.Load(); n != 1 {
		t.Fatalf("%d resolves for one id, want 1", n)
	}

	preloads.Request("3", 200000) // the next track changed
	preloads.Resolve()
	if n := resolves.Load(); n != 2 {
		t.Fatalf("%d resolves after a new request, want 2", n)
	}
	if armed, ok := preloads.Take(); !ok || armed.ID != "3" || armed.DurationMS != 200000 {
		t.Fatalf("armed preload %+v, want 3 with its length", armed)
	}
}

// With gapless and crossfade both off nothing is resolved (the player would
// drop the preload), and the request stays recorded: turning gapless on
// resolves it.
func TestPreloadWaitsForGaplessOrCrossfade(t *testing.T) {
	resolves := preloadEngine(t)
	p := Player()
	p.SetGapless(false)

	preloads.Request("2", 200000)
	preloads.Resolve()
	if n := resolves.Load(); n != 0 {
		t.Fatalf("%d resolves with gapless and crossfade off, want 0", n)
	}
	p.SetGapless(true)
	preloads.Resolve() // the hook runs again on the change
	if n := resolves.Load(); n != 1 {
		t.Fatalf("%d resolves after gapless went on, want 1", n)
	}
	if armed, _ := preloads.Take(); armed.ID != "2" {
		t.Fatalf("armed preload %q, want 2", armed.ID)
	}
}

// A request without a length (the mobile API passes none) resolves once per
// id as well, and the armed track has its length.
func TestPreloadWithoutLengthResolvesOncePerID(t *testing.T) {
	resolves := preloadEngine(t)
	t.Cleanup(resetEngineQueue)
	engineQueueSet([]deezer.Track{{ID: "2", DurationMS: 200000}, {ID: "3", DurationMS: 200000}})

	Preload("2", 0)
	Preload("2", 0) // a queue edit asks again for the same track
	if n := resolves.Load(); n != 0 {
		t.Fatalf("%d resolves before the near-end hook, want 0", n)
	}
	preloads.Resolve()
	if n := resolves.Load(); n != 1 {
		t.Fatalf("%d resolves after the near-end hook, want 1", n)
	}
	Preload("2", 0)    // already preloaded
	preloads.Resolve() // a seek back and forth runs the hook again
	if n := resolves.Load(); n != 1 {
		t.Fatalf("%d resolves for one id, want 1", n)
	}

	Preload("3", 0) // the next track changed
	preloads.Resolve()
	if n := resolves.Load(); n != 2 {
		t.Fatalf("%d resolves after a new request, want 2", n)
	}
	if armed, _ := preloads.Take(); armed.ID != "3" || armed.DurationMS != 200000 {
		t.Fatalf("armed preload %+v, want 3 with its length", armed)
	}
}

// ClearPreload drops the recorded request as well.
func TestClearPreloadDropsRequest(t *testing.T) {
	resolves := preloadEngine(t)

	preloads.Request("2", 200000)
	preloads.Clear()
	preloads.Resolve()
	if n := resolves.Load(); n != 0 {
		t.Fatalf("%d resolves after the request was cleared, want 0", n)
	}
	if armed, ok := preloads.Take(); ok {
		t.Fatalf("armed preload %q after a clear", armed.ID)
	}
}
