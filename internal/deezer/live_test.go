package deezer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

// liveServer is a fake live.deezer.com: each connection records the frames the
// client sends and receives the frames serve writes.
type liveServer struct {
	mu     sync.Mutex
	paths  []string
	frames []string
	conns  int
}

func (s *liveServer) start(t *testing.T, serve func(n int, ws *websocket.Conn)) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		s.mu.Lock()
		s.conns++
		n := s.conns
		s.paths = append(s.paths, ws.Request().URL.Path)
		s.mu.Unlock()
		go func() {
			for {
				var f string
				if websocket.Message.Receive(ws, &f) != nil {
					return
				}
				s.mu.Lock()
				s.frames = append(s.frames, f)
				s.mu.Unlock()
			}
		}()
		serve(n, ws)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func liveClient(ts *httptest.Server) *Client {
	c := New("arl")
	c.userID = "42"
	c.userToken = "tok"
	c.liveURLOverride = "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/"
	return c
}

func sendFrame(t *testing.T, ws *websocket.Conn, frame any) {
	t.Helper()
	b, _ := json.Marshal(frame)
	if err := websocket.Message.Send(ws, string(b)); err != nil {
		t.Errorf("send: %v", err)
	}
}

func playFrame(channel, track, session string) []any {
	return []any{"msg", channel, map[string]any{
		"ACTION": "PLAY", "APP": "LIMITATION",
		"VALUE": map[string]any{"SNG_ID": track, "UNIQID": session, "USER_ID": "42"},
	}}
}

func TestWatchStreams_ReportsPlaysOnTheStreamChannel(t *testing.T) {
	var s liveServer
	ts := s.start(t, func(_ int, ws *websocket.Conn) {
		time.Sleep(100 * time.Millisecond) // the subscription lands first
		sendFrame(t, ws, playFrame("42_42_REMOTEDISCOVER", "1", "a"))
		sendFrame(t, ws, []any{"msg", "42_42_STREAM", map[string]any{"ACTION": "STOP", "APP": "LIMITATION"}})
		sendFrame(t, ws, playFrame("42_42_STREAM", "3210645811", "web-1"))
		time.Sleep(time.Second)
	})
	c := liveClient(ts)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan StreamStart, 4)
	go c.WatchStreams(ctx, func(p StreamStart) { got <- p })

	select {
	case p := <-got:
		if p != (StreamStart{TrackID: "3210645811", Session: "web-1"}) {
			t.Fatalf("start = %+v", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no stream start reported")
	}
	select {
	case p := <-got:
		t.Fatalf("extra start %+v", p)
	case <-time.After(200 * time.Millisecond):
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.paths) == 0 || s.paths[0] != "/ws/tok" {
		t.Errorf("paths = %v, want /ws/tok", s.paths)
	}
	// Watching only subscribes: a published frame would make the other session pause.
	if len(s.frames) != 1 || s.frames[0] != `["sub","42_42_STREAM"]` {
		t.Errorf("frames sent = %q", s.frames)
	}
}

// watching starts WatchStreams on c and waits for its subscription.
func watching(t *testing.T, s *liveServer, c *Client, onStart func(StreamStart)) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go c.WatchStreams(ctx, onStart)
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		s.mu.Lock()
		n := len(s.frames)
		s.mu.Unlock()
		if n > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("no subscription")
		}
	}
}

func TestAnnouncePlay_PublishesOnTheStreamChannel(t *testing.T) {
	var s liveServer
	ts := s.start(t, func(int, *websocket.Conn) { time.Sleep(2 * time.Second) })
	c := liveClient(ts)
	watching(t, &s, c, func(StreamStart) {})

	if !c.AnnouncePlay("3210645811") {
		t.Fatal("AnnouncePlay = false with a connection")
	}
	want := `["send","42_42_STREAM",{"ACTION":"PLAY","APP":"LIMITATION","VALUE":{"USER_ID":"42","UNIQID":"` +
		c.liveSession + `","SNG_ID":"3210645811"}}]`
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		s.mu.Lock()
		frames := append([]string(nil), s.frames...)
		s.mu.Unlock()
		if len(frames) == 2 {
			if frames[1] != want {
				t.Fatalf("announcement %s, want %s", frames[1], want)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("frames = %q", frames)
		}
	}
	if len(c.liveSession) != 36 {
		t.Errorf("session %q is not a uuid", c.liveSession)
	}
}

func TestAnnouncePlay_FalseWithoutConnection(t *testing.T) {
	c := New("arl")
	c.userID = "42"
	if c.AnnouncePlay("1") {
		t.Error("AnnouncePlay = true with no connection")
	}
}

// The socket echoes a client's own announcement back; that is not another session.
func TestWatchStreams_SkipsOwnAnnouncements(t *testing.T) {
	var s liveServer
	c := New("arl")
	ts := s.start(t, func(_ int, ws *websocket.Conn) {
		time.Sleep(100 * time.Millisecond)
		sendFrame(t, ws, playFrame("42_42_STREAM", "1", c.liveSession))
		sendFrame(t, ws, playFrame("42_42_STREAM", "2", "web-1"))
		time.Sleep(time.Second)
	})
	c.userID, c.userToken = "42", "tok"
	c.liveURLOverride = "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/"
	got := make(chan StreamStart, 2)
	watching(t, &s, c, func(p StreamStart) { got <- p })
	select {
	case p := <-got:
		if p.TrackID != "2" {
			t.Fatalf("start = %+v, want only the other session's", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no start reported")
	}
}

func TestWatchStreams_ReconnectsAfterADrop(t *testing.T) {
	var s liveServer
	ts := s.start(t, func(n int, ws *websocket.Conn) {
		if n == 1 {
			return // the server drops the first connection
		}
		time.Sleep(100 * time.Millisecond)
		sendFrame(t, ws, playFrame("42_42_STREAM", "7", "phone"))
		time.Sleep(time.Second)
	})
	c := liveClient(ts)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan StreamStart, 1)
	go c.WatchStreams(ctx, func(p StreamStart) { got <- p })

	select {
	case p := <-got:
		if p.TrackID != "7" {
			t.Fatalf("start = %+v", p)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no start after the reconnect")
	}
}

func TestLoginKeepsLiveToken(t *testing.T) {
	c := New("dummy")
	c.http = &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse([]byte(`{"error":{},"results":{"checkForm":"api-t","USER_TOKEN":"live-t","USER":{"USER_ID":"1"}}}`)), nil
	})}
	if err := c.Login(); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if c.userToken != "live-t" {
		t.Errorf("userToken = %q", c.userToken)
	}
}

func TestWatchStreams_ReturnsWhenCanceled(t *testing.T) {
	var s liveServer
	ts := s.start(t, func(int, *websocket.Conn) { time.Sleep(5 * time.Second) })
	c := liveClient(ts)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.WatchStreams(ctx, func(StreamStart) {}); close(done) }()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("WatchStreams still running after cancel")
	}
}
