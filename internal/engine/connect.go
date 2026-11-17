package engine

// OpenDeezer Connect: discover other OpenDeezer instances on the LAN and route
// this client's playback to a chosen one (like Spotify Connect). When a device
// is connected, the playback calls forward to its control API and read its
// status, so the host's existing transport UI drives the remote device with
// only a small picker added.

import (
	"encoding/json"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/audio"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/bridge"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/control"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/discovery"
	odlog "github.com/Cycl0o0/OpenDeezer/v3/internal/log"
)

// selfControlPort is this instance's control API port (0 if disabled), used to
// filter our own responder out of discovery results.
func selfControlPort() int {
	// Capture the shared server pointer under mu (every other reader/writer of
	// ctrlSrv does): reading it unlocked races the settings-toggle writers.
	mu.Lock()
	srv := ctrlSrv
	mu.Unlock()
	if srv == nil {
		return 0
	}
	_, port, err := net.SplitHostPort(srv.Addr())
	if err != nil {
		return 0
	}
	p, _ := strconv.Atoi(port)
	return p
}

var (
	remoteCli  *control.Client // non-nil => playback routed to a remote device
	remoteSt   control.State
	remoteAddr string
	remoteStop chan struct{}
)

// routedRemote returns the connected device's client (nil if playing locally).
func routedRemote() *control.Client {
	mu.Lock()
	defer mu.Unlock()
	return remoteCli
}

func remoteSnapshot() control.State {
	mu.Lock()
	defer mu.Unlock()
	return remoteSt
}

// NowPlaying returns the track actually playing right now: when routed to a
// device the remote's current track (so the controller's now-playing stays in
// sync), otherwise the local current track (which also reflects tracks
// started through the control API). ok is false when nothing is playing.
func NowPlaying() (t bridge.Track, ok bool) {
	if routedRemote() != nil {
		if rt := remoteSnapshot().Track; rt != nil {
			return bridge.Track{
				ID: rt.ID, Name: rt.Title, ArtistLine: rt.Artist, ArtistID: rt.ArtistID,
				AlbumName: rt.Album, Explicit: rt.Explicit, DurationMS: rt.DurationMS, ArtworkURL: rt.ArtworkURL,
			}, true
		}
		return bridge.Track{}, false
	}
	if cur := currentTrack(); cur.ID != "" {
		return bridge.FromTrack(cur), true
	}
	return bridge.Track{}, false
}

// NowPlayingJSON is NowPlaying as a wire track, "{}" when nothing plays.
func NowPlayingJSON() string {
	t, ok := NowPlaying()
	if !ok {
		return "{}"
	}
	b, _ := json.Marshal(t)
	return string(b)
}

var (
	npMu      sync.Mutex
	npLast    string // the payload npVersion stands for
	npVersion atomic.Int64
)

// NowPlayingVersion returns a counter that moves exactly when NowPlayingJSON's
// payload changes, so a host fetches it only then.
func NowPlayingVersion() int64 { return npVersion.Load() }

// nowPlayingChanged moves NowPlayingVersion when the payload differs from the
// last one. Every change of the current track or of the routed device's state
// calls it.
func nowPlayingChanged() {
	js := NowPlayingJSON()
	npMu.Lock()
	if js != npLast {
		npLast = js
		npVersion.Add(1)
	}
	npMu.Unlock()
}

// DiscoverDevices broadcasts a LAN probe and returns the OpenDeezer devices
// found and the configured peers (discovery.ProbePeers); timeout bounds the
// whole wait. A peer listed without metadata can still be connected:
// ConnectDevice makes its own authenticated status call before routing.
func DiscoverDevices(timeout time.Duration) []discovery.Device {
	uid := ""
	if c := Client(); c != nil {
		uid = c.UserID()
	}
	devs, err := discovery.ProbePeers(timeout, selfControlPort(), uid)
	if err != nil {
		// A discovery error is expected/partial (e.g. no multicast on a VPN); the
		// configured unicast peers still fill the picker.
		odlog.Debug("discover: %v", err)
	}
	return devs
}

// remoteTrack is the now-playing track of a remote status.
func remoteTrack(t *control.Track) deezer.Track {
	return deezer.Track{
		ID: t.ID, Name: t.Title, DurationMS: t.DurationMS,
		Artists:   []deezer.Artist{{ID: t.ArtistID, Name: t.Artist}},
		AlbumName: t.Album, Explicit: t.Explicit, ArtworkURL: t.ArtworkURL,
	}
}

// ConnectDevice routes playback to the device at addr ("host", "host:port" or
// a URL; the port defaults to 7654). Local playback is stopped (audio moves to
// the device). It reports whether the device is now routed.
func ConnectDevice(addr string) bool {
	base, hp := config.NormalizePeer(addr)
	c := Client()
	if c == nil || base == "" {
		return false
	}
	// Authenticate to discovered devices with the (non-secret) account id only:
	// never the control token: a discovery reply is unauthenticated and spoofable,
	// so sending the shared token would leak it to an attacker's fake device.
	rc := control.NewClient(base, "", c.UserID())
	// /whoami is served unauthenticated by design, so it can't prove the peer is
	// controllable. Require an AUTHENTICATED /status to succeed BEFORE stopping
	// local playback: a token-protected or different-account peer would
	// otherwise 401 every command, leaving the user with dead local audio and a
	// "connected" UI that drives nothing.
	st, err := rc.Status()
	if err != nil {
		odlog.Warn("connect %s: %v", hp, err)
		return false
	}
	// Normalize the host's repeat: this controller keeps repeat locally and never
	// forwards it (a Repeat-All would trap the host looping the single-item queue
	// we stream it). Send SetRepeat("off") once so a previously-set host repeat
	// can't strand playback. Best-effort.
	if _, e := rc.SetRepeat("off"); e != nil {
		odlog.Debug("connect %s: normalize repeat: %v", hp, e)
	}
	// Audio moves to the device: stop local playback (only now that the peer is
	// proven controllable).
	withPlayer(func(p *audio.Player) { p.Stop() })

	// Sync the engine's current-track with what's actually playing on the remote,
	// so now-playing / Discord RP / lyrics reflect the remote immediately.
	if st.Track != nil {
		setCurrentTrack(remoteTrack(st.Track))
	}

	mu.Lock()
	oldRc, oldAddr := remoteCli, remoteAddr // switching A->B: capture A to stop it below
	if remoteStop != nil {
		close(remoteStop)
	}
	remoteStop = make(chan struct{})
	stop := remoteStop
	remoteCli = rc
	remoteSt = st
	remoteAddr = hp
	mu.Unlock()

	// Switching devices A->B: stop the OLD device so it doesn't keep playing
	// unattended. Fire-and-forget on its own goroutine (rc.Stop is a network
	// round-trip bounded by the control client's own HTTP timeout) so the connect
	// returns immediately. Re-selecting the same device keeps it playing.
	if oldRc != nil && oldAddr != hp {
		go func() { _, _ = oldRc.Stop() }()
	}

	nowPlayingChanged()
	go remotePoller(rc, stop)
	odlog.Info("connected to device %s", hp)
	return true
}

// DisconnectDevice returns control to local playback. It stops the remote
// device (so it doesn't keep playing unattended) before clearing the
// connection.
func DisconnectDevice() {
	mu.Lock()
	rc, stop := clearRemoteLocked() // rc.Stop is a network call: done outside the lock
	mu.Unlock()
	nowPlayingChanged()
	if stop != nil {
		close(stop)
	}
	if rc != nil {
		// Halt the remote off the caller's thread: Stop can wait out the control
		// client's 15 s timeout on a dead device.
		go func() { _, _ = rc.Stop() }()
	}
}

// clearRemoteLocked forgets the Connect route and returns its client and its
// poller's stop channel for the caller to close. Caller holds mu.
func clearRemoteLocked() (rc *control.Client, stop chan struct{}) {
	rc, stop = remoteCli, remoteStop
	remoteCli, remoteStop, remoteSt, remoteAddr = nil, nil, control.State{}, ""
	return rc, stop
}

// ConnectedDevice returns the connected device's host:port ("" if local).
func ConnectedDevice() string {
	mu.Lock()
	defer mu.Unlock()
	return remoteAddr
}

// remotePoller refreshes the cached remote status once a second until stopped.
// Each poll also syncs the engine's current-track so now-playing / Discord RP /
// lyrics reflect what the remote is actually playing as it changes.
func remotePoller(rc *control.Client, stop chan struct{}) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			st, err := rc.Status()
			if err != nil {
				continue
			}
			mu.Lock()
			active := remoteCli == rc // still the active device?
			if active {
				// The remote renders single tracks we send it; it has no queue of
				// its own: THIS device drives the queue. When the remote's track
				// ends it goes playing -> stopped near the end, so bump the
				// finished counter to fire this device's auto-advance, which then
				// sends the next track (honoring repeat/shuffle). The position
				// guard keeps a user-initiated mid-track stop from auto-advancing.
				if remoteSt.State == "playing" && st.State == "stopped" &&
					remoteSt.DurationMS > 0 && remoteSt.PositionMS >= remoteSt.DurationMS-2000 {
					finished++
				}
				remoteSt = st
			}
			mu.Unlock()
			// Sync current-track outside the lock (setCurrentTrack uses its own mutex).
			if active && st.Track != nil {
				setCurrentTrack(remoteTrack(st.Track))
			} else if active {
				nowPlayingChanged()
			}
		}
	}
}

// setRemoteState caches a status returned by a command issued to rc (so the UI
// updates without waiting for the next poll). It applies ONLY when rc is still
// the CURRENT remote client: a late in-flight response from a device we've
// since disconnected from (or switched away from: A while B is now active)
// must not resurrect stale state or bump the finished counter for the wrong
// device.
func setRemoteState(rc *control.Client, st control.State) {
	mu.Lock()
	if remoteCli != nil && remoteCli == rc {
		// A command's status response can be the first observer of the remote's
		// track end (playing -> stopped near the end), so detect it here too (not
		// only in remotePoller) and bump finished to fire this device's
		// auto-advance. Otherwise the next poll sees stopped -> stopped and never
		// advances. The position guard keeps a user-initiated stop from advancing.
		if remoteSt.State == "playing" && st.State == "stopped" &&
			remoteSt.DurationMS > 0 && remoteSt.PositionMS >= remoteSt.DurationMS-2000 {
			finished++
		}
		remoteSt = st
	}
	mu.Unlock()
	nowPlayingChanged()
}

// remoteStateInt maps a control State string to the audio.State int the hosts use.
func remoteStateInt(s string) int {
	switch s {
	case "playing":
		return int(audio.Playing)
	case "paused":
		return int(audio.Paused)
	case "loading":
		return int(audio.Loading)
	case "error":
		return int(audio.Errored)
	default:
		return int(audio.Stopped)
	}
}
