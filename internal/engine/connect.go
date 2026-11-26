package engine

// Purple Disc Connect: discover Purple Disc instances on the LAN and route playback to
// one. While a device is connected, playback calls forward to its control API and read
// its status.

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/audio"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/bridge"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/config"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/control"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/discovery"
	pdlog "github.com/Scharkenberg/PurpleDisc/v4/internal/log"
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

// NowPlaying returns the playing track: the remote's when routed to a device, else the
// local one (control API starts included). ok is false when nothing plays.
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

// DiscoverDevices returns the LAN devices and the configured peers
// (discovery.ProbePeers) within timeout. A peer without metadata can still be
// connected: ConnectDevice checks it with an authenticated status call.
func DiscoverDevices(timeout time.Duration) []discovery.Device {
	devs, err := discovery.ProbePeers(timeout, selfControlPort())
	if err != nil {
		// A discovery error is expected/partial (e.g. no multicast on a VPN); the
		// configured unicast peers still fill the picker.
		pdlog.Debug("discover: %v", err)
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

// TakeOverJSON finds the Purple Disc host of the account on the LAN that plays trackID
// and returns its queue, for playing it on here where it stands:
// {"tracks":[...],"index":i,"positionMs":n,"shuffle":b,"repeat":"off|all|one"}.
// "" when no host plays it (another Deezer app, or one out of reach).
func TakeOverJSON(trackID string) string {
	c := Client()
	if c == nil || trackID == "" {
		return ""
	}
	var bases []string
	for _, d := range DiscoverDevices(700 * time.Millisecond) {
		if base, _ := config.NormalizePeer(d.Addr); base != "" {
			bases = append(bases, base)
		}
	}
	return takeOver(trackID, c.UserID(), bases)
}

// takeOverWait bounds each host's status answer: a host on the LAN answers at once.
const takeOverWait = 2 * time.Second

// takeOver asks each host at bases, with the account id as Connect does, until one plays
// trackID.
func takeOver(trackID, userID string, bases []string) string {
	for _, base := range bases {
		rc := control.NewClient(base, "", userID)
		rc.HTTP = &http.Client{Timeout: takeOverWait}
		st, err := rc.Status()
		if err != nil || st.Track == nil || st.Track.ID != trackID {
			continue
		}
		tracks := make([]deezer.Track, 0, len(st.Queue))
		index := -1
		for i := range st.Queue {
			if index < 0 && st.Queue[i].ID == trackID {
				index = i
			}
			tracks = append(tracks, remoteTrack(&st.Queue[i]))
		}
		if index < 0 {
			tracks, index = []deezer.Track{remoteTrack(st.Track)}, 0
		}
		b, _ := json.Marshal(map[string]any{
			"tracks": bridge.FromTracks(tracks), "index": index, "positionMs": st.PositionMS,
			"shuffle": st.Shuffle, "repeat": st.Repeat,
		})
		return string(b)
	}
	return ""
}

// ConnectDevice routes playback to the device at addr ("host", "host:port" or a URL;
// port 7654 by default), stopping local playback, and reports whether it is routed.
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
	// /whoami is unauthenticated, so require an authenticated /status before stopping
	// local playback: a peer that rejects our auth would leave dead audio and a UI that
	// drives nothing.
	st, err := rc.Status()
	if err != nil {
		pdlog.Warn("connect %s: %v", hp, err)
		return false
	}
	// Set the host's repeat to "off" once: repeat stays local here (a forwarded Repeat-All
	// would loop the host's single-item queue). A failure is ignored.
	if _, e := rc.SetRepeat("off"); e != nil {
		pdlog.Debug("connect %s: normalize repeat: %v", hp, e)
	}
	// Audio moves to the device: stop local playback, the peer being proven
	// controllable.
	withPlayer(func(p *audio.Player) { p.Stop() })

	// Set the engine's current track to the remote's, so now-playing, Discord RP and
	// lyrics follow the remote at once.
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

	// Switching devices A->B: stop the old device on its own goroutine (a network round
	// trip) so the connect returns at once. Re-selecting the same device keeps it playing.
	if oldRc != nil && oldAddr != hp {
		go func() { _, _ = oldRc.Stop() }()
	}

	nowPlayingChanged()
	go remotePoller(rc, stop)
	pdlog.Info("connected to device %s", hp)
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

// remotePoller refreshes the cached remote status once a second until stopped, also
// syncing the engine's current track with the remote's.
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
				// The remote plays single tracks we send and this device owns the queue. A remote
				// going playing -> stopped near the end bumps the finished counter to auto-advance;
				// the position guard ignores a mid-track stop.
				if remoteSt.State == "playing" && st.State == "stopped" &&
					remoteSt.DurationMS > 0 && remoteSt.PositionMS >= remoteSt.DurationMS-2000 {
					finished++
				}
				remoteSt = st
			}
			mu.Unlock()
			// A new track becomes the current one outside the lock (setCurrentTrack takes its own);
			// the same track again only refreshes now-playing.
			cur, _ := currentTrackSnapshot()
			if active && st.Track != nil && st.Track.ID != cur.ID {
				setCurrentTrack(remoteTrack(st.Track))
			} else if active {
				nowPlayingChanged()
			}
		}
	}
}

// setRemoteState caches a status returned by a command to rc, so the UI updates
// before the next poll, only while rc is the current remote client.
func setRemoteState(rc *control.Client, st control.State) {
	mu.Lock()
	if remoteCli != nil && remoteCli == rc {
		// A command's response may be the first to see the remote's track end (playing ->
		// stopped near the end), so bump finished here too; the next poll would only see
		// stopped -> stopped. The position guard ignores a user stop.
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
