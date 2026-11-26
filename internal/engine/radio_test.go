package engine

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/control"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/listens"
)

// A new queue (and each track change) asks the radio source for more with the
// play context, the tracks left after the cursor and the queue; what it
// returns is appended and bumps the queue version, so hosts pull it.
func TestRadioTopsQueueUp(t *testing.T) {
	t.Cleanup(resetEngineQueue)
	prev := radioMore
	t.Cleanup(func() { radioMore = prev; SetListenContext("", "") })
	type ask struct {
		ctx    listens.Context
		left   int
		queued int
	}
	asks := make(chan ask, 8)
	radioMore = func(ctx listens.Context, left int, queued func() []deezer.Track) []deezer.Track {
		asks <- ask{ctx, left, len(queued())}
		if left > 2 {
			return nil
		}
		return []deezer.Track{{ID: "x"}, {ID: "y"}}
	}
	SetListenContext("dynamic_page_user_radio", "7")
	engineQueueSet([]deezer.Track{{ID: "a"}, {ID: "b"}, {ID: "c"}})
	select {
	case a := <-asks:
		if a.ctx != (listens.Context{Type: "dynamic_page_user_radio", ID: "7"}) || a.left != 2 || a.queued != 3 {
			t.Fatalf("asked %+v", a)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the radio was not asked")
	}
	deadline := time.Now().Add(5 * time.Second)
	ts, _, _ := QueueSnapshot()
	for len(ts) != 5 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		ts, _, _ = QueueSnapshot()
	}
	if len(ts) != 5 || ts[3].ID != "x" || ts[4].ID != "y" {
		t.Fatalf("queue %v", ts)
	}
}

// A state change that is no track change (a pause, a seek, the volume) asks
// the radio nothing.
func TestStateChangeAsksNoRadio(t *testing.T) {
	t.Cleanup(resetEngineQueue)
	prev := radioMore
	t.Cleanup(func() { radioMore = prev; SetListenContext("", "") })
	var asked atomic.Int32
	radioMore = func(listens.Context, int, func() []deezer.Track) []deezer.Track {
		asked.Add(1)
		return nil
	}
	SetListenContext("dynamic_page_user_radio", "7")
	queueMu.Lock()
	engineQ.Set([]deezer.Track{{ID: "a"}, {ID: "b"}}, 0)
	queueMu.Unlock()
	notifyControlState()
	time.Sleep(200 * time.Millisecond)
	if n := asked.Load(); n != 0 {
		t.Fatalf("a state change asked the radio %d times", n)
	}
}

// While casting, the 1 s poll of an unchanged track asks the radio nothing.
func TestCastPollAsksRadioOncePerTrack(t *testing.T) {
	t.Cleanup(resetEngineQueue)
	t.Cleanup(clearRoutedRemote)
	t.Cleanup(func() { setCurrentTrack(deezer.Track{}) })
	prev := radioMore
	t.Cleanup(func() { radioMore = prev; SetListenContext("", "") })
	var asked atomic.Int32
	radioMore = func(listens.Context, int, func() []deezer.Track) []deezer.Track {
		asked.Add(1)
		return nil
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"playing","track":{"id":"a","title":"A"},"positionMs":1000,"durationMs":200000}`))
	}))
	t.Cleanup(srv.Close)
	SetListenContext("dynamic_page_user_radio", "7")
	queueMu.Lock()
	engineQ.Set([]deezer.Track{{ID: "a"}, {ID: "b"}}, 0)
	queueMu.Unlock()
	rc := control.NewClient(srv.URL, "", "1001")
	stop := make(chan struct{})
	mu.Lock()
	remoteCli = rc
	mu.Unlock()
	go remotePoller(rc, stop)
	time.Sleep(3200 * time.Millisecond)
	close(stop)
	time.Sleep(100 * time.Millisecond)
	if n := asked.Load(); n != 1 {
		t.Fatalf("3 s of polls asked the radio %d times, want 1", n)
	}
}
