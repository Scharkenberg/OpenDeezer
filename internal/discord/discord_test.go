package discord

import (
	"bytes"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte(`{"hello":"world"}`)
	if err := writeFrame(&buf, opFrame, payload); err != nil {
		t.Fatal(err)
	}
	op, got, err := readFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if op != opFrame || !bytes.Equal(got, payload) {
		t.Fatalf("round-trip mismatch: op=%d got=%s", op, got)
	}
}

func TestActivityFramePlaying(t *testing.T) {
	r := &richPresence{appID: "x", pid: 42}
	b := r.activityFrame(State{
		Status: "playing", Title: "Song", Artist: "Artist", Album: "Album",
		PositionMS: 5000, DurationMS: 200000,
	})
	var p struct {
		Cmd  string `json:"cmd"`
		Args struct {
			PID      int `json:"pid"`
			Activity struct {
				Type       int            `json:"type"`
				Details    string         `json:"details"`
				State      string         `json:"state"`
				Timestamps map[string]any `json:"timestamps"`
			} `json:"activity"`
		} `json:"args"`
		Nonce string `json:"nonce"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if p.Cmd != "SET_ACTIVITY" || p.Args.PID != 42 {
		t.Fatalf("cmd/pid wrong: %+v", p)
	}
	a := p.Args.Activity
	if a.Type != 2 || a.Details != "Song" || a.State != "by Artist" {
		t.Fatalf("activity wrong: %+v", a)
	}
	if a.Timestamps["start"] == nil || a.Timestamps["end"] == nil {
		t.Fatalf("expected timestamps when playing: %+v", a.Timestamps)
	}
	if p.Nonce == "" {
		t.Fatal("nonce must be set")
	}
}

func TestClearFrameHasNullActivity(t *testing.T) {
	r := &richPresence{appID: "x", pid: 1}
	b := r.clearFrame()
	var p struct {
		Args struct {
			Activity json.RawMessage `json:"activity"`
		} `json:"args"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if string(p.Args.Activity) != "null" {
		t.Fatalf("clear activity = %s, want null", p.Args.Activity)
	}
}

func TestNewEmptyIsNoop(t *testing.T) {
	if _, ok := New("").(noop); !ok {
		t.Fatal("New(\"\") should be a no-op")
	}
	// A no-op Update/Close must not panic.
	p := New("")
	p.Update(State{Status: "playing", Title: "x"})
	p.Close()
}

func TestCloseBounded(t *testing.T) {
	origTimeout := closeTimeout
	closeTimeout = 10 * time.Millisecond
	defer func() { closeTimeout = origTimeout }()

	r := &richPresence{
		wake: make(chan struct{}, 1),
		done: make(chan struct{}),
	}

	start := time.Now()
	r.Close()
	elapsed := time.Since(start)

	// Since r.done is never closed, Close must timeout on closeTimeout (10ms)
	if elapsed < 10*time.Millisecond {
		t.Errorf("Close returned too early: %v", elapsed)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("Close took too long: %v, want close to %v", elapsed, closeTimeout)
	}
}

// trim and pad2 count characters, not bytes.
func TestTrimAndPadCountRunes(t *testing.T) {
	long := strings.Repeat("é", 200)
	got := trim(long, 128)
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != 128 {
		t.Fatalf("trim: valid %v, %d runes", utf8.ValidString(got), utf8.RuneCountInString(got))
	}
	if pad2("é") != "é " {
		t.Fatalf("pad2(é) = %q", pad2("é"))
	}
}

// fakeDiscord answers the handshake on s and forwards every later frame.
func fakeDiscord(s net.Conn, got chan<- []byte) {
	if _, _, err := readFrame(s); err != nil {
		return
	}
	if writeFrame(s, opFrame, []byte(`{"evt":"READY"}`)) != nil {
		return
	}
	for {
		_, b, err := readFrame(s)
		if err != nil {
			return
		}
		got <- b
	}
}

// when Discord closes the connection (a restart), the next push
// reconnects and sends the unchanged state again.
func TestPushReconnectsAfterDiscordRestart(t *testing.T) {
	got := make(chan []byte, 4)
	var servers []net.Conn
	orig := dial
	defer func() { dial = orig }()
	dial = func() (net.Conn, error) {
		c, s := net.Pipe()
		servers = append(servers, s)
		go fakeDiscord(s, got)
		return c, nil
	}
	recv := func() {
		t.Helper()
		select {
		case <-got:
		case <-time.After(2 * time.Second):
			t.Fatal("no activity sent")
		}
	}

	r := &richPresence{appID: "x", pid: 1}
	st := State{Status: "paused", Title: "Song"}
	r.push(st)
	recv()
	_ = servers[0].Close() // Discord goes away
	<-r.dead
	r.push(st)
	recv()
	if len(servers) != 2 {
		t.Fatalf("dialled %d times, want 2", len(servers))
	}
	r.drop()
}

// large_text gets the same limits as details.
func TestLargeTextTrimmedAndPadded(t *testing.T) {
	r := &richPresence{appID: "x", pid: 1}
	for album, want := range map[string]string{
		"é":                      "é ",
		strings.Repeat("ü", 200): strings.Repeat("ü", 128),
	} {
		var p struct {
			Args struct {
				Activity struct {
					Assets struct {
						LargeText string `json:"large_text"`
					} `json:"assets"`
				} `json:"activity"`
			} `json:"args"`
		}
		if err := json.Unmarshal(r.activityFrame(State{Status: "playing", Title: "Song", Album: album}), &p); err != nil {
			t.Fatal(err)
		}
		if got := p.Args.Activity.Assets.LargeText; got != want {
			t.Fatalf("large_text for %q = %q", album[:2], got)
		}
	}
}
