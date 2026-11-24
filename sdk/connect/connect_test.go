package connect_test

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/sdk/connect"
)

func TestNewRemoteClient(t *testing.T) {
	rc := connect.NewRemoteClient("127.0.0.1:9999", "", "")
	if rc == nil {
		t.Fatal("NewRemoteClient returned nil")
	}
}

// TestHostStartStop checks the inbound side: a Host binds a control endpoint
// and starts advertising, then shuts down.
func TestHostStartStop(t *testing.T) {
	h := connect.NewHost(
		connect.HostConfig{
			// Loopback + ephemeral port keeps the test self-contained and passes
			// the control server's non-loopback security check.
			Control: connect.Config{Addr: "127.0.0.1:0"},
			Name:    "Test Device",
			Client:  "test",
			Version: "0.0.1",
		},
		func() connect.State { return connect.State{State: "stopped"} },
		func() connect.Account { return connect.Account{Name: "Tester"} },
		connect.Commands{},
		nil,
	)
	if err := h.Start(); err != nil {
		t.Fatal("Start:", err)
	}
	defer h.Close()
	if h.Addr() == "" {
		t.Error("Addr should be non-empty after Start")
	}
	if h.Server() == nil {
		t.Error("Server should be accessible")
	}
	t.Log("host control endpoint at", h.Addr())
}

// TestRemoteClientRoundTrip drives a Host through every RemoteClient method
// added for control-API parity: exact repeat/shuffle set, cycle/toggle, the
// sleep timer, and the equalizer bridge wired via Host.Server().SetEQ.
func TestRemoteClientRoundTrip(t *testing.T) {
	var (
		gotRepeat       string
		gotShuffle      bool
		cycled, toggled bool
		sleepMin        int
		sleepEOT        bool
		sleepCanceled   bool
	)
	eq := connect.EQState{Preset: "flat", GainsDB: make([]float64, 10),
		Presets: []string{"flat", "rock"}}
	h := connect.NewHost(
		connect.HostConfig{
			Control: connect.Config{Addr: "127.0.0.1:0"},
			Name:    "Test Device", Client: "test", Version: "0.0.1",
		},
		func() connect.State { return connect.State{State: "playing"} },
		func() connect.Account { return connect.Account{Name: "Tester"} },
		connect.Commands{
			SetRepeat:        func(mode string) { gotRepeat = mode },
			SetShuffle:       func(on bool) { gotShuffle = on },
			CycleRepeat:      func() { cycled = true },
			ToggleShuffle:    func() { toggled = true },
			SetSleepTimer:    func(min int, eot bool) { sleepMin, sleepEOT = min, eot },
			CancelSleepTimer: func() { sleepCanceled = true },
		},
		nil, // browse endpoints disabled → Search/Playlists must error
	)
	// The EQ bridge must be wired before Start or the device's /eq is 404.
	h.Server().SetEQ(&connect.EQ{
		State:      func() connect.EQState { return eq },
		SetEnabled: func(on bool) { eq.Enabled = on },
		SetPreset:  func(name string) error { eq.Preset = name; return nil },
	})
	// An advertising failure (no multicast on CI) is accepted: the control endpoint still
	// serves, and that is all this test drives.
	if err := h.Start(); err != nil && !errors.Is(err, connect.ErrAdvertise) {
		t.Fatal("Start:", err)
	}
	defer h.Close()

	rc := connect.NewRemoteClient(h.Addr(), "", "")

	if _, err := rc.SetRepeat("all"); err != nil || gotRepeat != "all" {
		t.Errorf("SetRepeat: err=%v repeat=%q", err, gotRepeat)
	}
	if _, err := rc.SetShuffle(true); err != nil || !gotShuffle {
		t.Errorf("SetShuffle: err=%v on=%v", err, gotShuffle)
	}
	if _, err := rc.CycleRepeat(); err != nil || !cycled {
		t.Errorf("CycleRepeat: err=%v cycled=%v", err, cycled)
	}
	if _, err := rc.ToggleShuffle(); err != nil || !toggled {
		t.Errorf("ToggleShuffle: err=%v toggled=%v", err, toggled)
	}
	if _, err := rc.SetSleepTimer(0, true); err != nil || !sleepEOT {
		t.Errorf("SetSleepTimer: err=%v min=%d eot=%v", err, sleepMin, sleepEOT)
	}
	if _, err := rc.CancelSleepTimer(); err != nil || !sleepCanceled {
		t.Errorf("CancelSleepTimer: err=%v canceled=%v", err, sleepCanceled)
	}

	st, err := rc.SetEQ(url.Values{"on": {"1"}, "preset": {"rock"}})
	if err != nil || !st.Enabled || st.Preset != "rock" {
		t.Errorf("SetEQ: err=%v state=%+v", err, st)
	}
	st, err = rc.EQ()
	if err != nil || st.Preset != "rock" {
		t.Errorf("EQ: err=%v state=%+v", err, st)
	}

	// Without a Deezer client the browse endpoints answer 503 → error.
	if _, err := rc.Search("daft punk"); err == nil {
		t.Error("Search should error when the device has no Deezer client")
	}
	if _, err := rc.Playlists(); err == nil {
		t.Error("Playlists should error when the device has no Deezer client")
	}
}

// TestRemoteClientQueueAndEvents drives a Host through the queue and album methods and
// checks that the SSE stream delivers an initial snapshot and closes when the context
// is canceled.
func TestRemoteClientQueueAndEvents(t *testing.T) {
	var (
		addID   string
		addNext bool
		jumped  = -1
		albumID string
	)
	h := connect.NewHost(
		connect.HostConfig{
			Control: connect.Config{Addr: "127.0.0.1:0"},
			Name:    "Test Device", Client: "test", Version: "0.0.1",
		},
		func() connect.State { return connect.State{State: "playing"} },
		func() connect.Account { return connect.Account{Name: "Tester"} },
		connect.Commands{
			QueueAdd:  func(id string, next bool) error { addID, addNext = id, next; return nil },
			QueueJump: func(i int) error { jumped = i; return nil },
			PlayAlbum: func(id string) error { albumID = id; return nil },
		},
		nil,
	)
	if err := h.Start(); err != nil && !errors.Is(err, connect.ErrAdvertise) {
		t.Fatal("Start:", err)
	}
	defer h.Close()

	rc := connect.NewRemoteClient(h.Addr(), "", "")

	if _, err := rc.QueueAdd("3135556", true); err != nil || addID != "3135556" || !addNext {
		t.Errorf("QueueAdd: err=%v id=%q next=%v", err, addID, addNext)
	}
	if _, err := rc.QueueJump(3); err != nil || jumped != 3 {
		t.Errorf("QueueJump: err=%v index=%d", err, jumped)
	}
	if _, err := rc.PlayAlbum("302127"); err != nil || albumID != "302127" {
		t.Errorf("PlayAlbum: err=%v id=%q", err, albumID)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	states, err := rc.Events(ctx)
	if err != nil {
		t.Fatal("Events:", err)
	}
	select {
	case st, ok := <-states:
		if !ok {
			t.Fatal("stream closed before the initial snapshot")
		}
		if st.State != "playing" {
			t.Fatalf("initial snapshot state = %q", st.State)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the initial snapshot")
	}

	cancel()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-states:
			if !ok {
				return // channel closed on cancel: done
			}
		case <-deadline:
			t.Fatal("channel not closed after context cancel")
		}
	}
}
