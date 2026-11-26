package engine

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/audio"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/queue"
)

// netEngine logs a client in against a fake gateway, installs it with an idle player and
// returns a switch that makes every later request fail as a lost network would.
func netEngine(t *testing.T, gwError bool) func(down bool) {
	t.Helper()
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			_, _ = w.Write([]byte(`{"error":{},"results":{"checkForm":"tok","USER":{"USER_ID":"1","BLOG_NAME":"t","OPTIONS":{"license_token":"lic"}},"OFFERS":[{"title":"P"}]}}`))
		default:
			if gwError {
				_, _ = w.Write([]byte(`{"error":{"DATA_ERROR":"no such track"},"results":{}}`))
				return
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(gw.Close)
	target, _ := url.Parse(gw.URL)
	orig := deezer.Transport()
	var down bool
	deezer.SetTransport(rtFunc(func(r *http.Request) (*http.Response, error) {
		if down {
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no route", Name: r.URL.Host}}
		}
		r = r.Clone(r.Context())
		r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
		return orig.RoundTrip(r)
	}))
	t.Cleanup(func() { deezer.SetTransport(orig) })
	c := deezer.New("arl")
	if err := c.Login(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	prevClient, prevPlayer := client, player
	client, player = c, &audio.Player{}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		client, player = prevClient, prevPlayer
		mu.Unlock()
	})
	return func(d bool) { down = d }
}

func lastError(t *testing.T) (string, bool) {
	t.Helper()
	var r struct {
		Error   string `json:"error"`
		Network bool   `json:"network"`
	}
	if err := json.Unmarshal(LastErrorJSON(), &r); err != nil {
		t.Fatal(err)
	}
	return r.Error, r.Network
}

// A play that fails for want of a network says so; a gateway's refusal does not.
func TestLastErrorMarksNetwork(t *testing.T) {
	setDown := netEngine(t, false)
	setDown(true)
	if Play("901", 1000) != PlayFailed {
		t.Fatal("a play without a network started")
	}
	if msg, network := lastError(t); msg == "" || !network {
		t.Fatalf("network loss: %q, network %v", msg, network)
	}
}

func TestLastErrorGatewayIsNotNetwork(t *testing.T) {
	netEngine(t, true)
	if Play("902", 1000) != PlayFailed {
		t.Fatal("a refused track started")
	}
	if msg, network := lastError(t); msg == "" || network || strings.Contains(msg, "network") {
		t.Fatalf("gateway refusal: %q, network %v", msg, network)
	}
}

// An auto-advance that fails for want of a network stays on the track that could not start.
func TestAutoAdvanceStaysOnNetworkLoss(t *testing.T) {
	t.Cleanup(resetEngineQueue)
	t.Cleanup(resetOfflineGlobals)
	clearRoutedRemote()
	setDown := netEngine(t, false)
	setDown(true)
	queueMu.Lock()
	engineQ.Set([]deezer.Track{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}, 0)
	engineQ.SetRepeat(queue.RepeatOne)
	queueMu.Unlock()
	engineAutoAdvance(deezer.Track{ID: "a"}, plays.claim())
	if i := QueueIndex(); i != 0 {
		t.Fatalf("cursor moved to %d on a lost network", i)
	}
	if st := Player().State(); st != audio.Errored {
		t.Fatalf("state %v, want Errored", st)
	}
	if _, network := lastError(t); !network {
		t.Fatal("the error is not marked as a lost network")
	}
}
