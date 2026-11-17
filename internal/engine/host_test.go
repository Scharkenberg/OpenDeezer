package engine

import (
	"net"
	"path/filepath"
	"testing"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/audio"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/control"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/history"
)

// closeControlServer closes and forgets the control server a test started.
func closeControlServer() {
	ctrlMu.Lock()
	rebuildControlServer(control.Config{}, nil)
	webRemotePrev = nil
	connectHost = false
	ctrlMu.Unlock()
}

// TestCycleRepeatAndToggleShuffleWired proves the legacy verbs
// (CycleRepeat/ToggleShuffle) are wired alongside the SET variants and drive
// the engine queue (off->all->one->off; shuffle flip), visible through
// engineState.
func TestCycleRepeatAndToggleShuffleWired(t *testing.T) {
	t.Cleanup(resetEngineQueue)
	resetEngineQueue()

	cmds := engineCommands()
	if cmds.CycleRepeat == nil || cmds.ToggleShuffle == nil {
		t.Fatal("CycleRepeat/ToggleShuffle hooks must be wired")
	}
	if cmds.SetRepeat == nil || cmds.SetShuffle == nil {
		t.Fatal("SetRepeat/SetShuffle hooks must stay wired")
	}

	if got := engineState().Repeat; got != "off" {
		t.Fatalf("baseline repeat = %q, want off", got)
	}
	for _, want := range []string{"all", "one", "off"} {
		cmds.CycleRepeat()
		if got := engineState().Repeat; got != want {
			t.Fatalf("after cycle repeat = %q, want %q", got, want)
		}
	}
	cmds.ToggleShuffle()
	if !engineState().Shuffle {
		t.Fatal("ToggleShuffle must turn shuffle on")
	}
	cmds.ToggleShuffle()
	if engineState().Shuffle {
		t.Fatal("ToggleShuffle must turn shuffle back off")
	}
}

// TestLogoutClearsRemoteRouting: Logout tears down the Connect controller
// routing (remoteCli/remoteSt/remoteAddr/remoteStop) before wiping the
// session, so commands can't keep routing to the old device under a new
// account.
func TestLogoutClearsRemoteRouting(t *testing.T) {
	t.Cleanup(clearRoutedRemote)
	t.Cleanup(closeControlServer)

	srv := control.New(control.Config{Addr: "127.0.0.1:0"}, engineState, engineAccount, engineCommands(), nil)
	if err := srv.Start(); err != nil {
		t.Fatalf("start test control server: %v", err)
	}
	t.Cleanup(srv.Close)
	rc := control.NewClient("http://"+srv.Addr(), "", "")

	mu.Lock()
	remoteCli = rc
	remoteSt = control.State{State: "playing"}
	remoteAddr = srv.Addr()
	remoteStop = make(chan struct{})
	mu.Unlock()

	if routedRemote() == nil {
		t.Fatal("precondition: a device must be routed")
	}

	Logout()

	if routedRemote() != nil {
		t.Fatal("Logout must clear remoteCli (else commands route to the stale device)")
	}
	if got := ConnectedDevice(); got != "" {
		t.Fatalf("Logout must clear remoteAddr, got %q", got)
	}
	mu.Lock()
	stopCleared := remoteStop == nil
	stateCleared := remoteSt.State == ""
	mu.Unlock()
	if !stopCleared {
		t.Fatal("Logout must clear remoteStop")
	}
	if !stateCleared {
		t.Fatal("Logout must clear the cached remote state")
	}
}

// The LAN rebind of the phone remote carries the configured token, so a
// token-protected control API is NOT downgraded to token-less (same-account
// only) auth when bound for the LAN. lanHost is loopback here.
func TestLANServerKeepsToken(t *testing.T) {
	t.Setenv("OPENDEEZER_CONTROL_TOKEN", "s3cr3t")
	prevHost := lanHost
	lanHost = "127.0.0.1"
	t.Cleanup(func() { lanHost = prevHost })
	t.Cleanup(closeControlServer)

	ctrlMu.Lock()
	first := rebuildControlServer(control.Config{}, nil, "127.0.0.1:0")
	ctrlMu.Unlock()
	if first == nil {
		t.Fatal("no loopback server")
	}
	waitServing(t, first.Addr()) // so the rebind's Close reclaims the port

	WebRemoteSetEnabled(true)
	mu.Lock()
	srv := ctrlSrv
	mu.Unlock()
	if srv == nil || srv == first {
		t.Fatal("the phone remote did not rebind the server")
	}
	base := "http://" + srv.Addr()
	waitServing(t, srv.Addr())

	// No token -> rejected (the rebind must not downgrade to token-less auth).
	if _, err := control.NewClient(base, "", "").Status(); err == nil {
		t.Fatal("token-less request accepted: the LAN rebind downgraded auth")
	}
	// A same-account header alone must NOT unlock a token-protected server
	// (token takes priority over same-account in the server's auth switch).
	if _, err := control.NewClient(base, "", "someacct").Status(); err == nil {
		t.Fatal("same-account request accepted despite a configured token")
	}
	// The correct token is accepted.
	if _, err := control.NewClient(base, "s3cr3t", "").Status(); err != nil {
		t.Fatalf("correct-token request rejected: %v", err)
	}
}

// With a Connect host switch and the Connect host on, turning the phone
// remote off keeps the LAN server for the Connect host, with the remote off
// in it and in the config a later rebuild uses. lanHost is loopback here.
func TestWebRemoteOffKeepsConnectHostServer(t *testing.T) {
	prevHost := lanHost
	lanHost = "127.0.0.1"
	t.Cleanup(func() { lanHost = prevHost })
	SetConnectHostSwitch(true)
	t.Cleanup(func() { SetConnectHostSwitch(false) })
	t.Cleanup(closeControlServer)

	// A server on a port the OS picked first: a rebind keeps the port.
	ctrlMu.Lock()
	first := rebuildControlServer(control.Config{}, nil, "127.0.0.1:0")
	ctrlMu.Unlock()
	if first == nil {
		t.Fatal("no loopback server")
	}
	waitServing(t, first.Addr())
	ConnectHostSetEnabled(true)
	mu.Lock()
	host := ctrlSrv
	mu.Unlock()
	if host == nil || host == first {
		t.Fatal("the Connect host did not rebind the server")
	}
	waitServing(t, host.Addr())
	WebRemoteSetEnabled(true)
	mu.Lock()
	srv := ctrlSrv
	mu.Unlock()
	if srv == nil || !srv.WebRemoteEnabled() {
		t.Fatal("the phone remote is not on")
	}

	WebRemoteSetEnabled(false)
	mu.Lock()
	kept, cfg := ctrlSrv, ctrlCfg
	mu.Unlock()
	if kept != srv || srv.WebRemoteEnabled() || cfg.WebRemote || webRemotePrev != nil {
		t.Fatalf("server kept %v, remote on %v, config %+v, prev %v; want the server kept with the remote off",
			kept == srv, srv.WebRemoteEnabled(), cfg, webRemotePrev)
	}
}

// When Start fails (port already bound) the rebuild returns nil and publishes
// nothing, so ctrlSrv never holds a listener-less server.
func TestRebuildControlServerNoDeadPublish(t *testing.T) {
	t.Cleanup(closeControlServer)

	// Occupy a loopback port so the control server's net.Listen fails.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy port: %v", err)
	}
	defer func() { _ = ln.Close() }()

	ctrlMu.Lock()
	s := rebuildControlServer(control.Config{}, nil, ln.Addr().String())
	ctrlMu.Unlock()
	if s != nil {
		t.Fatal("the rebuild must return nil when Start fails")
	}
	mu.Lock()
	published := ctrlSrv
	mu.Unlock()
	if published != nil {
		t.Fatal("a server that failed to Start must NOT be published to ctrlSrv")
	}
}

// TestIsErroredFinish proves the gate of the onFinish callback and
// noteTrackFinished: an errored finish (Errored state, or a stored LastError)
// with ~0 played is skipped (no history, no advance), while a clean finish, a
// gapless promote (Playing) and an error after real playback are genuine
// finishes.
func TestIsErroredFinish(t *testing.T) {
	cases := []struct {
		name    string
		state   audio.State
		lastErr string
		posMS   int64
		want    bool
	}{
		{"clean stopped finish", audio.Stopped, "", 0, false},
		{"gapless promote (playing, no error)", audio.Playing, "", 0, false},
		{"errored finish, nothing played", audio.Stopped, "decode failed", 0, true},
		{"device-loss Errored, nothing played", audio.Errored, "device lost", 0, true},
		{"errored state, nothing played, empty msg", audio.Errored, "", 0, true},
		{"error after real playback", audio.Stopped, "late decode error", 65000, false},
		{"errored state but played a lot", audio.Errored, "", 65000, false},
	}
	for _, c := range cases {
		if got := isErroredFinish(c.state, c.lastErr, c.posMS); got != c.want {
			t.Errorf("%s: isErroredFinish(%v,%q,%d) = %v, want %v",
				c.name, c.state, c.lastErr, c.posMS, got, c.want)
		}
	}
	// Without a player (before Init) a finish is never errored.
	mu.Lock()
	prev := player
	player = nil
	mu.Unlock()
	defer func() {
		mu.Lock()
		player = prev
		mu.Unlock()
	}()
	if erroredFinish() {
		t.Fatal("erroredFinish without a player must be false")
	}
}

// A host that advances its own queue gets every natural finish counted, also
// for a synced queue whose cursor is on the finished track, and the engine
// neither moves that cursor nor launches a play.
func TestHostAdvancesBumpsEveryFinish(t *testing.T) {
	t.Cleanup(resetEngineQueue)
	t.Cleanup(func() { setCurrentTrack(deezer.Track{}) })
	setHistoryStore(history.New(filepath.Join(t.TempDir(), "history.jsonl")))
	clearRoutedRemote()
	SetHostAdvances(true)
	t.Cleanup(func() { SetHostAdvances(false) })
	mu.Lock()
	prev := player
	player = &audio.Player{} // Stopped, no error: a natural finish
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		player = prev
		mu.Unlock()
	})

	engineQueueSet([]deezer.Track{{ID: "a"}, {ID: "b"}})
	setCurrentTrack(deezer.Track{ID: "a", DurationMS: 120000})
	before, seq := FinishedCount(), plays.seq.Load()
	onNaturalFinish()
	if got := FinishedCount(); got != before+1 {
		t.Fatalf("finished count %d -> %d, want +1", before, got)
	}
	if i := QueueIndex(); i != 0 || plays.seq.Load() != seq {
		t.Fatalf("cursor %d, play seq %d (was %d): the engine advanced a host-advanced queue", i, plays.seq.Load(), seq)
	}
	if got := waitHistory(t, 1); got[0].TrackID != "a" {
		t.Fatalf("recorded %q, want the finished track a", got[0].TrackID)
	}
}
