// Package engine is the OpenDeezer engine the native shells host: corelib
// (the C ABI the desktop apps call) and mobile (the gomobile API). It owns the
// logged-in client, the player, the now-playing track, the engine queue,
// preloads, the media cache, the listening history, the control server with
// its commands, the phone remote and the Connect routing; a shell converts
// arguments and results and nothing more.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/audio"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/control"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
	odlog "github.com/Cycl0o0/OpenDeezer/v3/internal/log"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/mediacache"
)

var errNotReady = errors.New("not logged in")

var (
	mu       sync.Mutex
	client   *deezer.Client
	player   *audio.Player
	finished int // bumped whenever a track ends naturally and the engine queue does not advance itself
)

// lastLoginKind records why the most recent Init failed (LoginErrorKind).
var lastLoginKind atomic.Int32

// loginNoAudio is the LoginErrorKind for a missing audio output device; Init
// sets it before any login.
const loginNoAudio = 4

// loginKind maps a Login error to the stable code the hosts branch on: 0 = ok,
// 1 = ARL expired/invalid (send to re-auth), 2 = no internet (show the
// No-Internet screen + Retry), 3 = other (generic failure).
func loginKind(err error) int32 {
	switch {
	case err == nil:
		return 0
	case deezer.IsNoNetwork(err):
		return 2
	case deezer.IsARLExpired(err):
		return 1
	default:
		return 3
	}
}

// Init logs in with arl and starts the engine-hosted services. The first call
// also clears the stream files a crash left behind and the caches of older
// versions, makes the player (with playback.json's settings, which the
// client of each login takes too) and opens the media cache (media.json, and kept
// open while anything is pinned). Another account than the logged-in one
// drops the Connect route and rebuilds the control server; the same account
// keeps both. It reports whether the login succeeded; LoginErrorKind tells
// why not. A login that fails for want of a network turns offline mode on,
// for the last account that signed in; a later successful one turns that
// mode off again (not one the host set).
func Init(arl string) bool {
	// One-time, non-network setup under the lock.
	mu.Lock()
	first := player == nil
	if first {
		audio.RemoveTempFiles() // stream files a crash left behind
		mediacache.RemoveOldCaches()
		p, err := audio.NewPlayer()
		if err != nil {
			mu.Unlock()
			lastLoginKind.Store(loginNoAudio)
			odlog.Warn("audio output: %v", err)
			return false
		}
		player = p
		player.SetOnFinish(onNaturalFinish)
		preloads.Attach(p)
		applyPlayback(p, nil)
	}
	mu.Unlock()
	if first {
		// The on-disk raw-stream cache (media.json: mediaCacheMB > 0, or
		// anything pinned). Best-effort: a cache failure only logs; playback
		// runs uncached.
		if err := caches.Open(); err != nil {
			odlog.Warn("media cache: %v", err)
		} else if caches.Current() != nil {
			odlog.Info("media cache on (%d MB)", config.LoadMedia().MediaCacheMB)
		}
	}

	// Login is a network round-trip (up to 30s): do it WITHOUT holding mu, so it
	// can't block every other engine call meanwhile.
	c := deezer.New(arl)
	if err := c.Login(); err != nil {
		kind := loginKind(err)
		lastLoginKind.Store(kind)
		odlog.Warn("login failed: %v", err)
		if kind == 2 && !offlineOn.Load() {
			offlineOn.Store(true)
			offlineAuto.Store(true)
			stopNetJobs()
		}
		return false
	}
	lastLoginKind.Store(0)
	if offlineAuto.Load() {
		offlineOn.Store(false)
		offlineAuto.Store(false)
	}
	rememberUser(c.UserID())
	c.SetAdsDisabled(config.LoadAdsDisabled()) // free-tier ads opt-out (persisted)
	applyPlayback(nil, c)
	mu.Lock()
	// Another account: drop the Connect route, whose client carries the old
	// account's id.
	var stopRemote chan struct{}
	if client != nil && client.UserID() != c.UserID() {
		_, stopRemote = clearRemoteLocked()
	}
	client = c
	mu.Unlock()
	// The other accounts' fetches, after the client changed: none starts again
	// with the old one.
	dropJobs(func(j *offlineJob) bool { return j.user != c.UserID() })
	if stopRemote != nil {
		nowPlayingChanged()
		close(stopRemote)
	}
	resumeJobs() // pins a quit or a failure left half fetched

	odlog.Info("logged in: %s (%s)", c.Account().Name, c.Account().Offer)
	// Start engine-hosted services (Discord RP + control API) once.
	startServices(c)
	// On a re-login, hand the control server the new client: a same-account
	// re-login swaps it into the running server, another account rebuilds the
	// server (startServices runs under a sync.Once and won't re-run).
	refreshControlServer(c)
	return true
}

// LoginErrorKind returns why the most recent Init failed: 0 = ok / logged in,
// 1 = ARL expired or invalid, 2 = no internet, 3 = other, 4 = no audio output
// device.
func LoginErrorKind() int { return int(lastLoginKind.Load()) }

// Logout ends the session: it disconnects a Connect device (stopping it),
// stops playback and the offline fetches, closes the control server (dropping
// the phone remote's sessions and the old account's same-account auth) and its
// advertiser, and forgets the client. The next Init starts the services again.
func Logout() {
	DisconnectDevice()
	withPlayer(func(p *audio.Player) { p.Stop() })
	ctrlMu.Lock()
	webRemotePrev = nil
	connectHost = false
	rebuildControlServer(control.Config{}, nil)
	ctrlMu.Unlock()
	mu.Lock()
	client = nil
	servicesOnce = sync.Once{} // startServices runs again on the next Init
	mu.Unlock()
	dropJobs(func(*offlineJob) bool { return true }) // after the client went: none starts again with it
}

// Client returns the logged-in client, nil before Init succeeded.
func Client() *deezer.Client {
	mu.Lock()
	defer mu.Unlock()
	return client
}

// Player returns the player, nil before Init made one.
func Player() *audio.Player {
	mu.Lock()
	defer mu.Unlock()
	return player
}

func withPlayer(fn func(*audio.Player)) {
	if p := Player(); p != nil {
		fn(p)
	}
}

// Caches is the media cache host (size, folder, maximum age, stats, clear).
func Caches() *mediacache.Host { return caches }

// ---- playback ----

// PlayResult is what Play and PlayEpisode report.
type PlayResult int

const (
	// PlayFailed: the play could not start; the player's LastError says why.
	PlayFailed PlayResult = 0
	// PlayStarted: the play started.
	PlayStarted PlayResult = 1
	// PlaySuperseded: a newer play claimed the player before this one started;
	// nothing failed and no error is set.
	PlaySuperseded PlayResult = 2
)

// startResult is the PlayResult of startPlay's err.
func startResult(err error) PlayResult {
	switch {
	case err == nil:
		return PlayStarted
	case errors.Is(err, errSuperseded):
		return PlaySuperseded
	default:
		return PlayFailed
	}
}

// Play resolves and plays a track, or plays it on the routed Connect device.
// durationMS is the length the host knows (0 when unknown). A newer play
// started meanwhile wins: this one then reports PlaySuperseded. With no client
// (offline mode, no login) only a pinned or cached track plays, and nothing is
// requested. When the track could not be planned the player's LastError says
// why.
func Play(id string, durationMS int64) PlayResult {
	// A fresh user play supersedes any in-flight resolve (newest wins) and holds
	// off the engine's auto-advance while it resolves.
	seq, done := plays.beginUser()
	defer done()
	// OpenDeezer Connect: when a device is selected, play there instead.
	if rc := routedRemote(); rc != nil {
		st, err := rc.PlayTrack(id)
		if err != nil {
			return PlayFailed
		}
		setRemoteState(rc, st)
		return PlayStarted
	}
	p := Player()
	if p == nil {
		return PlayFailed
	}
	c := netClient()
	plan, err := preparePlan(c, id)
	if err != nil {
		p.SetError(err.Error())
		return PlayFailed
	}
	// Track the now-playing for Discord RP + remote status. A cache-sourced plan
	// carries no metadata (unless pinned): its title/artist/album are filled in
	// asynchronously.
	t := plan.Track
	if t.ID == "" {
		t = deezer.Track{ID: id, DurationMS: durationMS}
	}
	var gen uint64
	if r := startResult(startPlay(p, plan, durationMS, seq, func() { gen = setCurrentTrack(t) })); r != PlayStarted {
		return r
	}
	if c == nil {
		return PlayStarted
	}
	if plan.Track.ID == "" {
		go fetchTrackMeta(c, id, gen)
	}
	// Report the play to Deezer (log.listen), like the web player: free-tier ad
	// accounting + artist play-counts depend on it. Best-effort, off the hot path.
	go func() { _ = c.NowPlaying(id) }()
	return PlayStarted
}

// PlayEpisode resolves and plays a podcast episode (a plain, unencrypted
// stream). The episode is the current track at once (id + duration), its
// title, podcast name and artwork follow asynchronously. It reports as Play
// does.
func PlayEpisode(id string, durationMS int64) PlayResult {
	seq, done := plays.beginUser() // as in Play
	defer done()
	p := Player()
	if p == nil {
		return PlayFailed
	}
	c, err := Online()
	if err != nil {
		p.SetError(err.Error())
		return PlayFailed
	}
	plan, err := c.PodcastEpisodeStream(id)
	if err != nil {
		odlog.Warn("episode %s: %v", id, err)
		return PlayFailed
	}
	// setCurrentEpisode (not setCurrentTrack) tags the media kind so this listen
	// lands in history as Kind="episode" and history screens replay it via
	// PlayEpisode.
	var gen uint64
	if r := startResult(startPlay(p, plan, durationMS, seq, func() { gen = setCurrentEpisode(deezer.Track{ID: id, DurationMS: durationMS}) })); r != PlayStarted {
		return r
	}
	go fetchEpisodeMeta(c, id, gen)
	return PlayStarted
}

// Pause pauses playback (on the routed device while it plays).
func Pause() {
	if rc := routedRemote(); rc != nil {
		if remoteSnapshot().State == "playing" {
			if st, err := rc.PlayPause(); err == nil {
				setRemoteState(rc, st)
			}
		}
		return
	}
	withPlayerNotify(func(p *audio.Player) { p.Pause() })
}

// Resume resumes playback (on the routed device while it is paused).
func Resume() {
	if rc := routedRemote(); rc != nil {
		if remoteSnapshot().State == "paused" {
			if st, err := rc.PlayPause(); err == nil {
				setRemoteState(rc, st)
			}
		}
		return
	}
	withPlayerNotify(func(p *audio.Player) { p.Resume() })
}

// TogglePause flips Playing and Paused only; from Stopped or Errored the host
// plays again.
func TogglePause() {
	if rc := routedRemote(); rc != nil {
		if st, err := rc.PlayPause(); err == nil {
			setRemoteState(rc, st)
		}
		return
	}
	withPlayerNotify(func(p *audio.Player) { p.TogglePause() })
}

// Stop stops playback, recording the listen so far.
func Stop() {
	if rc := routedRemote(); rc != nil {
		if st, err := rc.Stop(); err == nil {
			setRemoteState(rc, st)
		}
		return
	}
	// Record the in-progress listen before halting: the next Play sees the
	// player Stopped and skips recording, so this is the only chance.
	withPlayerNotify(func(p *audio.Player) { noteTrackTransition(p); p.Stop() })
}

// Seek jumps to ms in the current track.
func Seek(ms int64) {
	if rc := routedRemote(); rc != nil {
		if st, err := rc.SeekMS(ms); err == nil {
			setRemoteState(rc, st)
		}
		return
	}
	withPlayerNotify(func(p *audio.Player) { p.SeekMS(ms) })
}

// SetVolume sets the volume (0..1).
func SetVolume(v float64) {
	if rc := routedRemote(); rc != nil {
		if st, err := rc.SetVolume(v); err == nil {
			setRemoteState(rc, st)
		}
		return
	}
	withPlayerNotify(func(p *audio.Player) { p.SetVolume(v) })
}

// Volume returns the volume (0..1; 1 before Init).
func Volume() float64 {
	if routedRemote() != nil {
		return remoteSnapshot().Volume
	}
	v := 1.0
	withPlayer(func(p *audio.Player) { v = p.Volume() })
	return v
}

// State returns the playback state as an audio.State int.
func State() int {
	if routedRemote() != nil {
		return remoteStateInt(remoteSnapshot().State)
	}
	v := 0
	withPlayer(func(p *audio.Player) { v = int(p.State()) })
	return v
}

// PositionMS returns the playback position.
func PositionMS() int64 {
	if routedRemote() != nil {
		return remoteSnapshot().PositionMS
	}
	var v int64
	withPlayer(func(p *audio.Player) { v = p.PositionMS() })
	return v
}

// DurationMS returns the playing track's length: the player's, else (a play
// started without one) the current track's once its metadata has arrived.
func DurationMS() int64 {
	if routedRemote() != nil {
		return remoteSnapshot().DurationMS
	}
	var v int64
	withPlayer(func(p *audio.Player) { v = p.DurationMS() })
	if v == 0 {
		v = currentTrack().DurationMS
	}
	return v
}

// Format returns a label of the current stream's format ("FLAC · lossless",
// "MP3 · 320 kbps"), "" when nothing plays.
func Format() string {
	if routedRemote() != nil {
		return deezer.FormatLabel(remoteSnapshot().Format)
	}
	f := ""
	withPlayer(func(p *audio.Player) { f = p.Format() })
	return deezer.FormatLabel(f)
}

// SetSleepTimer arms the sleep timer: pause after minutes (with a fade-out),
// or when the current track ends with endOfTrack (minutes ignored). minutes
// <= 0 without endOfTrack cancels it.
func SetSleepTimer(minutes int, endOfTrack bool) {
	withPlayerNotify(func(p *audio.Player) {
		p.SetSleepTimer(time.Duration(minutes)*time.Minute, endOfTrack)
	})
}

// CancelSleepTimer disarms the sleep timer.
func CancelSleepTimer() { withPlayerNotify(func(p *audio.Player) { p.CancelSleepTimer() }) }

// FinishedCount returns a count, never decreasing, of the natural finishes the
// host advances its own queue on: every one with SetHostAdvances, else those
// of tracks the engine queue does not advance itself.
func FinishedCount() int {
	mu.Lock()
	defer mu.Unlock()
	return finished
}

// IsPreview reports whether the current track is Deezer's 30-second preview
// (the free-account fallback) rather than the full stream.
func IsPreview() bool {
	p := Player()
	return p != nil && p.IsPreview()
}

// Preload asks for a track to follow the current one gaplessly or with a
// crossfade. It only records the request: the engine resolves and downloads
// it once the current track has max(30 s, crossfade + 20 s) left (at once when
// it is shorter or already there). A new request replaces the recorded one; a
// request for the track already recorded or preloaded changes nothing.
// durationMS 0 takes the length from the engine queue. No-op when routed to a
// Connect device.
func Preload(id string, durationMS int64) {
	if routedRemote() != nil {
		return // remote playback: nothing to preload locally
	}
	if durationMS == 0 {
		durationMS = queuedDurationMS(id)
	}
	preloads.Request(id, durationMS)
}

// ClearPreload discards the recorded and the preloaded next track. Call when
// the upcoming track is no longer determined, so a stale preload can't be
// gaplessly swapped in.
func ClearPreload() { preloads.Clear() }

// queuedDurationMS looks a track's duration up in the engine queue (0 when
// absent).
func queuedDurationMS(id string) int64 {
	queueMu.Lock()
	defer queueMu.Unlock()
	for _, t := range engineQ.Tracks() {
		if t.ID == id {
			return t.DurationMS
		}
	}
	return 0
}

// ---- downloads (premium-only, full track to disk) ----

// DownloadTrack saves a track to a file in dir ("" = the download folder) and
// returns its path. Blocking; downloads are premium-only.
func DownloadTrack(id, dir string) (string, error) {
	c, err := Online()
	if err != nil {
		return "", err
	}
	if dir == "" {
		dir = config.LoadDownloadDir()
	}
	return c.SaveTrack(context.Background(), id, dir)
}

// DownloadAlbum saves every track of an album to the download folder, skipping
// files already there, and returns {"saved":N,"failed":N,"dir":"...",
// "error":""}: error is "" on full success and carries the batch error
// otherwise. Blocking; premium-only.
func DownloadAlbum(id string) (map[string]any, error) {
	return downloadBatch(func(c *deezer.Client, dir string, opts deezer.DownloadOptions) ([]string, error) {
		return c.SaveAlbum(context.Background(), id, dir, opts)
	})
}

// DownloadPlaylist is DownloadAlbum for a playlist.
func DownloadPlaylist(id string) (map[string]any, error) {
	return downloadBatch(func(c *deezer.Client, dir string, opts deezer.DownloadOptions) ([]string, error) {
		return c.SavePlaylist(context.Background(), id, dir, opts)
	})
}

func downloadBatch(save func(*deezer.Client, string, deezer.DownloadOptions) ([]string, error)) (map[string]any, error) {
	c, err := Online()
	if err != nil {
		return nil, err
	}
	dir := config.LoadDownloadDir()
	var failed int
	opts := deezer.DownloadOptions{SkipExisting: true, Progress: func(p deezer.DownloadProgress) {
		if p.Err != nil {
			failed++
		}
	}}
	saved, err := save(c, dir, opts)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return map[string]any{"saved": len(saved), "failed": failed, "dir": dir, "error": msg}, nil
}

// ---- equalizer + mono downmix ----

// EQState is the full equalizer snapshot shared by every host: the current
// settings plus the engine's band and preset tables, so no host hardcodes them.
type EQState struct {
	Enabled  bool      `json:"enabled"`
	Mono     bool      `json:"mono"`
	PreampDB float64   `json:"preampDb"`
	GainsDB  []float64 `json:"gainsDb"`
	Preset   string    `json:"preset"`
	Bands    []float64 `json:"bands"`
	Presets  []string  `json:"presets"`
}

// EQ returns the equalizer state; ok is false before Init made the player.
func EQ() (st EQState, ok bool) {
	p := Player()
	if p == nil {
		return EQState{}, false
	}
	return EQState{
		Enabled:  p.EQEnabled(),
		Mono:     p.MonoDownmix(),
		PreampDB: p.EQPreampDB(),
		GainsDB:  p.EQGains(),
		Preset:   p.EQPreset(),
		Bands:    p.EQBands(),
		Presets:  audio.EQPresetNames,
	}, true
}

// SetEQJSON applies a partial equalizer update. Recognized keys (all
// optional): enabled (bool), mono (bool), preampDb (number), gainsDb
// ([10]number), preset (string), band ({"index":N,"gainDb":X}). It reports
// false when js does not parse, there is no player, or a present key failed to
// apply (unknown preset, bad band index or length).
func SetEQJSON(js string) bool {
	var req struct {
		Enabled  *bool      `json:"enabled"`
		Mono     *bool      `json:"mono"`
		PreampDB *float64   `json:"preampDb"`
		GainsDB  *[]float64 `json:"gainsDb"`
		Preset   *string    `json:"preset"`
		Band     *struct {
			Index  int     `json:"index"`
			GainDB float64 `json:"gainDb"`
		} `json:"band"`
	}
	if err := json.Unmarshal([]byte(js), &req); err != nil {
		return false
	}
	p := Player()
	if p == nil {
		return false
	}
	ok := true
	if req.Enabled != nil {
		p.SetEQEnabled(*req.Enabled)
	}
	if req.Mono != nil {
		p.SetMonoDownmix(*req.Mono)
	}
	if req.PreampDB != nil {
		p.SetEQPreamp(*req.PreampDB)
	}
	if req.Preset != nil && p.SetEQPreset(*req.Preset) != nil {
		ok = false
	}
	if req.GainsDB != nil && p.SetEQGains(*req.GainsDB) != nil {
		ok = false
	}
	if req.Band != nil && p.SetEQGain(req.Band.Index, req.Band.GainDB) != nil {
		ok = false
	}
	return ok
}
