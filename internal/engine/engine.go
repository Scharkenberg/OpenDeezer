// Package engine is the Purple Disc engine hosted by corelib (C ABI) and mobile
// (gomobile): client, player, queue, media cache, history, control server, phone
// remote and Connect. The shells only convert arguments and results.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/audio"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/config"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/control"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	pdlog "github.com/Scharkenberg/PurpleDisc/v4/internal/log"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/mediacache"
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

// Init logs in with arl and starts the engine services; the first call also cleans up
// leftovers and opens the player and media cache. Another account drops the Connect
// route and rebuilds the control server; a network failure turns offline mode on.
func Init(arl string) bool {
	forgetMarks() // another account's, or read before this login
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
			pdlog.Warn("audio output: %v", err)
			return false
		}
		player = p
		player.SetOnFinish(onNaturalFinish)
		player.SetOnListenEnd(listenLog.Ended)
		preloads.Attach(p)
		applyPlayback(p, nil)
	}
	mu.Unlock()
	if first {
		// The on-disk raw-stream cache (media.json: mediaCacheMB > 0, or anything pinned). A
		// cache failure is logged and playback runs uncached.
		if err := caches.Open(); err != nil {
			pdlog.Warn("media cache: %v", err)
		} else if caches.Current() != nil {
			pdlog.Info("media cache on (%d MB)", config.LoadMedia().MediaCacheMB)
		}
	}

	// Login is a network round-trip (up to 30s): do it WITHOUT holding mu, so it
	// can't block every other engine call meanwhile.
	deezer.SetWebRelease(config.LoadWebRelease())
	c := deezer.New(arl)
	if err := c.Login(); err != nil {
		kind := loginKind(err)
		lastLoginKind.Store(kind)
		pdlog.Warn("login failed: %v", err)
		if kind == 2 && !offlineOn.Load() {
			offlineOn.Store(true)
			offlineAuto.Store(true)
			stopNetJobs()
			stopElsewhere()
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
	listenLog.Flush() // listens kept while offline
	// Offline mode the host set stays on: nothing goes to the network until it ends.
	online := !offlineOn.Load()
	if online {
		go learnRelease(c)
	}
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

	pdlog.Info("logged in: %s (%s)", c.Account().Name, c.Account().Offer)
	// Start engine-hosted services (Discord RP + control API) once.
	startServices(c)
	// On a re-login, hand the control server the new client: a same-account
	// re-login swaps it into the running server, another account rebuilds the
	// server (startServices runs under a sync.Once and won't re-run).
	refreshControlServer(c)
	if online {
		watchElsewhere(c)
	}
	return true
}

// LoginErrorKind returns why the most recent Init failed: 0 = ok / logged in,
// 1 = ARL expired or invalid, 2 = no internet, 3 = other, 4 = no audio output
// device.
func LoginErrorKind() int { return int(lastLoginKind.Load()) }

// Logout ends the session: disconnects a Connect device, stops the live watch,
// playback and offline fetches, closes the control server and its advertiser, and
// forgets the client. The next Init restarts the services.
func Logout() {
	DisconnectDevice()
	stopElsewhere()
	forgetMarks()
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

// SessionExpired reports that Deezer refused the signed-in account's ARL since
// it signed in (a call's re-login failed): only a new sign-in brings the
// session back. ARLs are not renewed by use; Deezer ends them itself.
func SessionExpired() bool {
	c := Client()
	return c != nil && c.Refused()
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

// Play plays a track locally or on the routed Connect device; durationMS is the known
// length (0 unknown). A newer play wins (PlaySuperseded). Without a client only pinned
// or cached tracks play; on failure the player's LastError says why.
func Play(id string, durationMS int64) PlayResult {
	// A fresh user play supersedes any in-flight resolve (newest wins) and holds
	// off the engine's auto-advance while it resolves.
	seq, done := plays.beginUser()
	defer done()
	// Purple Disc Connect: when a device is selected, play there instead.
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
		if plays.superseded(seq) {
			return PlaySuperseded
		}
		p.SetError(err)
		return PlayFailed
	}
	// Track the playing track for Discord RP and remote status. A cache-sourced plan has
	// no metadata (unless pinned), so title, artist and album are filled in asynchronously.
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
	return PlayStarted
}

// PlayEpisode plays a podcast episode (unencrypted) at the episode speed, from its
// bookmark unless heard. It is the current track at once; title, podcast name and
// artwork follow asynchronously. Reports as Play does.
func PlayEpisode(id string, durationMS int64) PlayResult {
	seq, done := plays.beginUser() // as in Play
	defer done()
	p := Player()
	if p == nil {
		return PlayFailed
	}
	c, err := Online()
	if err != nil {
		if plays.superseded(seq) {
			return PlaySuperseded
		}
		p.SetError(err)
		return PlayFailed
	}
	bookmark := make(chan deezer.EpisodeBookmark, 1)
	go func() { bookmark <- episodeMark(id) }()
	plan, err := c.PodcastEpisodeStream(id)
	if err != nil {
		pdlog.Warn("episode %s: %v", id, err)
		if plays.superseded(seq) {
			return PlaySuperseded
		}
		p.SetError(err)
		return PlayFailed
	}
	// setCurrentEpisode records the listen as Kind="episode" so history replays it via
	// PlayEpisode. The bookmark is set before the start, so the episode starts there at
	// the episode speed.
	b := <-bookmark
	var gen uint64
	if r := startResult(startPlay(p, plan, durationMS, seq, func() { gen = setCurrentEpisode(deezer.Track{ID: id, DurationMS: durationMS}) })); r != PlayStarted {
		return r
	}
	if b.OffsetS > 0 && !b.Heard {
		p.SeekMS(int64(b.OffsetS) * 1000)
	}
	p.SetSpeed(PlaybackSpeed())
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

// Resume resumes playback (on the routed device while it is paused); a local
// resume is announced to the account's other sessions (announceLocal).
func Resume() {
	if rc := routedRemote(); rc != nil {
		if remoteSnapshot().State == "paused" {
			if st, err := rc.PlayPause(); err == nil {
				setRemoteState(rc, st)
			}
		}
		return
	}
	withPlayerNotify(func(p *audio.Player) { p.Resume(); announceIfPlaying(p) })
}

// TogglePause flips Playing and Paused only (a local resume is announced, as
// in Resume); from Stopped or Errored the host plays again.
func TogglePause() {
	if rc := routedRemote(); rc != nil {
		if st, err := rc.PlayPause(); err == nil {
			setRemoteState(rc, st)
		}
		return
	}
	withPlayerNotify(func(p *audio.Player) { p.TogglePause(); announceIfPlaying(p) })
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

// Preload records the track to follow the current one (gapless or crossfade); it is
// fetched once max(30 s, crossfade + 20 s) remain. A new request replaces it;
// durationMS 0 takes the queue's length. No-op when routed.
func Preload(id string, durationMS int64) {
	if routedRemote() != nil {
		return // remote playback: nothing to preload locally
	}
	if durationMS == 0 {
		durationMS = queuedDurationMS(id)
	}
	preloads.Request(id, durationMS)
}

// ClearPreload discards the recorded and the preloaded next track. Call it when the
// upcoming track changes, so a stale preload is not swapped in.
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

// DownloadAlbum saves every album track not yet in the download folder and returns
// {"saved":N,"failed":N,"dir":"...","error":""} (error "" on full success). Blocking;
// premium only.
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

// SetEQJSON applies a partial EQ update (optional keys enabled, mono, preampDb,
// gainsDb, preset, band). It returns false when js does not parse, there is no player,
// or a key fails to apply.
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
