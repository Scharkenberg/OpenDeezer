package engine

// Engine-hosted services shared by every host: Discord Rich Presence and the
// control API (remote control + MCP). The host just plays tracks and the
// engine tracks the current one for now-playing and remote status.
//
// Engine-side control covers the player-level actions (play/pause, stop, seek,
// volume, restart) plus play-by-id. next/prev/shuffle/repeat walk the engine
// queue (engineQ); a host mirrors its own queue into it (QueueSet,
// QueueSetIndex) so remote controllers see and drive the real queue.

import (
	"encoding/json"
	"fmt"
	"net"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/audio"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/bridge"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/control"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/discord"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/discovery"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/history"
	odlog "github.com/Cycl0o0/OpenDeezer/v3/internal/log"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/mediacache"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/preload"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/queue"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/version"
)

var (
	servicesOnce sync.Once
	dp           discord.Presence
	ctrlSrv      *control.Server
	coreVersion  = version.Number

	// ctrlCfg + ctrlSrvUserID remember how ctrlSrv was built so
	// refreshControlServer can rebuild it around a new client after an account
	// switch (re-login), instead of the sync.Once keeping the old account's
	// session alive forever. We track the account UserID (not the *deezer.Client
	// pointer) because Init allocates a fresh client on every call, so a
	// same-account re-login must compare equal and stay a no-op.
	ctrlCfg       control.Config
	ctrlSrvUserID string

	// advertiser is the LAN discovery responder for OpenDeezer Connect (nil when
	// the control server is loopback-only or disabled). It advertises the control
	// port so other devices can find us; it is retained here — not discarded — so
	// it can be Closed + re-pointed whenever the control server is rebuilt on a
	// new port or torn down, instead of leaving a ghost advertising a dead port.
	// Guarded by mu (mirrors the TUI's model.advertiser).
	advertiser *discovery.Responder

	// connectHostSwitch is set by a host with a Connect host switch
	// (SetConnectHostSwitch); connectHost is that switch. Both guarded by
	// ctrlMu.
	connectHostSwitch, connectHost bool

	// caches is the on-disk raw-stream cache (its Current is nil while the
	// budget is 0 and nothing is pinned), attached to the player. Held here as
	// well so preparePlan can prefer a zero-network cache-sourced plan and
	// pins can be fetched into it; a cache that opens takes the pins of
	// offline.json.
	caches = &mediacache.Host{Attach: attachCache, Pinned: anyPinned}

	// preloads is the next-track preload: Preload records the request, the
	// player's near-end hook resolves and arms it. Its armed track is exactly
	// the one a gapless/crossfade promote swaps in, which lets
	// engineAdvanceOnFinish move now-playing forward even when the host never
	// synced its queue into engineQ.
	preloads = preload.New(Player, func(req deezer.Track) (*deezer.StreamPlan, deezer.Track, error) {
		plan, err := preparePlan(netClient(), req.ID)
		if err != nil {
			return nil, req, err
		}
		// A cache-sourced plan carries no metadata: the track is the id and
		// duration, enriched on promote.
		t := plan.Track
		if t.ID == "" {
			t = req
		}
		if t.DurationMS == 0 {
			t.DurationMS = req.DurationMS
		}
		return plan, t, nil
	})

	curMu    sync.Mutex
	curTrack deezer.Track
	curGen   uint64 // bumped by setCurrentTrack/setCurrentEpisode; lets an async metadata fetch detect a newer track
	// curKind is the media kind of curTrack for history recording: "" = song
	// (set by setCurrentTrack, the common path) and history.KindEpisode =
	// podcast (set by setCurrentEpisode). Guarded by curMu alongside curTrack so
	// the recorder never pairs a track with a stale kind.
	curKind string

	// engineQ is the engine-side playback queue. enginePlayPlaylist /
	// enginePlayTrack load the full track list into it and play the current track;
	// when a track ends naturally the player's onFinish callback advances this
	// queue and starts the next track, so remote/control-driven playlist playback
	// walks every track instead of stopping after the first. Guarded by queueMu.
	queueMu sync.Mutex
	engineQ = queue.New()
)

// setCurrentTrack makes t the now-playing song and returns its generation for
// a following metadata fetch (setCurrentTrackAt).
func setCurrentTrack(t deezer.Track) uint64 {
	curMu.Lock()
	curGen++
	curTrack = t
	curKind = "" // a song; episodes go through setCurrentEpisode
	gen := curGen
	curMu.Unlock()
	nowPlayingChanged()
	// Every track change (play, auto-advance, gapless promote, async metadata
	// enrichment, remote sync) is a state change controllers care about.
	notifyControlState()
	return gen
}

// setCurrentEpisode is setCurrentTrack for a podcast episode: it tags the
// media kind so the history recorder stamps Kind="episode", and a history
// screen replays the entry through PlayEpisode instead of Play.
func setCurrentEpisode(t deezer.Track) uint64 {
	curMu.Lock()
	curGen++
	curTrack = t
	curKind = history.KindEpisode
	gen := curGen
	curMu.Unlock()
	nowPlayingChanged()
	notifyControlState()
	return gen
}

// setCurrentTrackAt applies an async metadata fetch only when no newer track
// was set since gen (a check of the id would race a concurrent play and pin
// the previous track's metadata on the next one). The media kind stays.
func setCurrentTrackAt(gen uint64, t deezer.Track) {
	curMu.Lock()
	applied := curGen == gen
	if applied {
		curTrack = t
	}
	curMu.Unlock()
	if applied {
		nowPlayingChanged()
		notifyControlState()
	}
}

// notifyControlState nudges the control server's SSE event loop to publish a
// fresh /events snapshot right away. The loop has its own 1s fallback ticker,
// so this is purely a latency optimization — safe to skip (nil server) and
// cheap to repeat (notifications are coalesced, the send never blocks).
func notifyControlState() {
	mu.Lock()
	srv := ctrlSrv
	mu.Unlock()
	if srv != nil {
		srv.NotifyStateChanged()
	}
	select {
	case discordWake <- struct{}{}:
	default:
	}
}

// withPlayerNotify runs a state-mutating player action, then pokes the SSE
// loop. Use it instead of withPlayer for pause/stop/seek/volume-style
// mutations (read-only accessors keep plain withPlayer).
func withPlayerNotify(fn func(*audio.Player)) {
	withPlayer(fn)
	notifyControlState()
}

func currentTrack() deezer.Track {
	curMu.Lock()
	defer curMu.Unlock()
	return curTrack
}

// currentTrackSnapshot returns the now-playing track together with its media
// kind ("" = song, history.KindEpisode = podcast), read under a single lock so
// the history recorder never pairs a track with a stale kind.
func currentTrackSnapshot() (deezer.Track, string) {
	curMu.Lock()
	defer curMu.Unlock()
	return curTrack, curKind
}

// getCtrlSrv reads the shared control server under mu (nil when disabled), so
// callers off the UI thread — e.g. the player's onFinish callback — can publish
// a finished edge without racing the settings-toggle writers of ctrlSrv.
func getCtrlSrv() *control.Server {
	mu.Lock()
	defer mu.Unlock()
	return ctrlSrv
}

// preparePlan resolves a playable stream plan for id, preferring the on-disk
// media cache when one is attached: a fully-cached track (its StreamMeta was
// stored when the cache adopted its body) yields a plan with CDNURL=="" that the
// audio layer serves with zero network. On a cache miss it falls back to the
// normal PrepareStream network path. With no client (offline mode, no login)
// the plan comes from the cache alone (localPlan), or the track is not
// available; a track the account pinned that cannot be resolved online (Deezer
// no longer serves it, the network is gone) plays from its pin. Every local
// play path (Play, Preload, enginePlayResolved, engineAutoAdvance) goes
// through here for consistent offline behavior.
func preparePlan(c *deezer.Client, id string) (*deezer.StreamPlan, error) {
	if c == nil {
		return localPlan(id)
	}
	var plan *deezer.StreamPlan
	var err error
	if mc := caches.Current(); mc != nil {
		plan, err = c.PrepareStreamCached(id, mc)
	} else {
		plan, err = c.PrepareStream(id)
	}
	if err != nil {
		if lp, lerr := localPlan(id); lerr == nil && lp.Track.ID != "" {
			return lp, nil // pinned: localPlan fills the track only for a pin
		}
		return nil, err
	}
	return plan, nil
}

// hostAdvances is set by a host that advances its own queue on FinishedCount
// (SetHostAdvances): every natural finish then bumps the count, and the engine
// queue only follows a gapless promote.
var hostAdvances atomic.Bool

// SetHostAdvances declares (on) that the host advances its own queue on
// FinishedCount, also for a queue it mirrors into the engine; otherwise the
// engine advances a mirrored queue whose cursor is on the finished track
// itself and FinishedCount does not bump for it. Call before Init.
func SetHostAdvances(on bool) { hostAdvances.Store(on) }

// onNaturalFinish is the player's onFinish callback. An errored finish
// (CDN/decode failure, ~0 played) is not a real end: refresh state and return.
// Otherwise: capture the finishing track id (before any advance moves
// currentTrack on), record the listen, auto-advance the engine queue (or bump
// the host's finished counter so exactly one queue mechanism advances),
// publish the natural end-of-track edge to /events subscribers, and push a
// fresh state snapshot.
func onNaturalFinish() {
	if erroredFinish() {
		notifyControlState()
		return
	}
	tid := currentTrack().ID
	noteTrackFinished()
	advanced := false
	if !hostAdvances.Load() {
		advanced = engineAdvanceOnFinish()
	} else if p := Player(); p != nil && p.State() == audio.Playing {
		engineSyncOnGaplessPromote() // a promote: now-playing follows it
	}
	if !advanced {
		mu.Lock()
		finished++
		mu.Unlock()
	}
	if srv := getCtrlSrv(); srv != nil && tid != "" {
		srv.NotifyFinished(tid)
	}
	notifyControlState()
}

// tracksFromInsertJSON decodes QueueInsertNext's payload, which may be either
// a single wire track object {...} or a JSON array [...] of them. Reuses the
// shared queue decoders (queueTracksFromJSON/trackFromWire). Entries without an
// id are dropped; a bad payload yields an empty slice (a no-op insert).
func tracksFromInsertJSON(js string) []deezer.Track {
	js = strings.TrimSpace(js)
	if js == "" {
		return nil
	}
	if strings.HasPrefix(js, "[") {
		ts, _ := queueTracksFromJSON(js)
		return ts
	}
	var w bridge.Track
	if err := json.Unmarshal([]byte(js), &w); err != nil || w.ID == "" {
		return nil
	}
	return []deezer.Track{trackFromWire(w)}
}

// QueueInsertNext splices the tracks of js (a wire track or an array of them,
// in order) right after the current track ("play next"), discarding a
// preloaded next (the upcoming track changed) and bumping the version +
// notifying so a host or controller resyncs. Local-only: the host's Up-Next
// editor owns this queue.
func QueueInsertNext(js string) {
	ts := tracksFromInsertJSON(js)
	if len(ts) == 0 {
		return
	}
	queueMu.Lock()
	engineQ.InsertAfterCurrent(ts...)
	queueMu.Unlock()
	preloads.Clear()
	notifyControlState()
}

// fetchTrackMeta fills in the full metadata for the current track (title/artist/
// album), so Discord + remote status show more than an id, unless another
// track was set since gen. Best-effort.
func fetchTrackMeta(c *deezer.Client, id string, gen uint64) {
	if c == nil || id == "" {
		return
	}
	if t, err := c.Track(id); err == nil && t.ID != "" {
		setCurrentTrackAt(gen, t)
	}
}

// startServices starts Discord RP + the control API once, after a successful
// login. The just-logged-in client is passed in; closures read globals lazily.
// engineEQ bridges the control API's /eq endpoint to the engine player.
func engineEQ() *control.EQ {
	return control.PlayerEQ(func() control.EQController {
		if p := Player(); p != nil {
			return p
		}
		return nil
	}, audio.EQPresetNames)
}

func startServices(c *deezer.Client) {
	servicesOnce.Do(func() {
		appID := config.LoadDiscordAppID()
		dp = discord.New(appID)
		if appID == "" {
			odlog.Info("discord: rich presence disabled (no app id; set discord-app-id.txt)")
		} else {
			odlog.Info("discord: rich presence enabled (app %s)", appID)
			go serviceTicker()
		}

		if cfg := config.LoadControl(); cfg.Enabled {
			if cfg.SameAccount && cfg.Token == "" {
				odlog.Warn("control api: LAN-exposed with same-account auth only; the Deezer " +
					"user id is not a strong secret. Set OPENDEEZER_CONTROL_TOKEN for a real " +
					"credential on untrusted networks.")
			}
			ctrlMu.Lock()
			// This replaces any server the phone remote started before login,
			// so there is nothing for turning the remote off to restore.
			webRemotePrev = nil
			// A server configured onto the LAN is a Connect host.
			connectHost = !config.IsLoopbackAddr(cfg.Addr)
			if srv := rebuildControlServer(control.Config{Addr: cfg.Addr, Token: cfg.Token, SameAccountOnly: cfg.SameAccount}, c, cfg.Addr); srv != nil {
				odlog.Info("control api on %s", srv.Addr())
			}
			ctrlMu.Unlock()
		}
	})
}

// refreshAdvertiser Closes any existing LAN discovery responder and, when the
// control server is currently bound to a reachable (non-loopback) address,
// starts a fresh one advertising its port; with a Connect host switch
// (SetConnectHostSwitch) only while it is on. Call it whenever the control
// server is (re)built on a possibly-different port or torn down, or the switch
// changes, so a ghost responder never keeps advertising a dead port. Caller
// holds ctrlMu, not mu.
func refreshAdvertiser() {
	mu.Lock()
	srv := ctrlSrv
	old := advertiser
	advertiser = nil
	mu.Unlock()
	if old != nil {
		old.Close()
	}
	if srv == nil {
		return
	}
	addr := srv.Addr()
	if config.IsLoopbackAddr(addr) || connectHostSwitch && !connectHost {
		return // loopback-only, or the Connect host is off: nothing to advertise
	}
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		return
	}
	resp, err := discovery.Advertise(advertInfo, p)
	if err != nil {
		return
	}
	mu.Lock()
	// A concurrent teardown may have cleared/replaced ctrlSrv while we were
	// binding; only publish if this server is still current, else Close the
	// responder we just started so it doesn't advertise a stale port.
	if ctrlSrv == srv {
		advertiser = resp
		mu.Unlock()
		odlog.Info("discovery advertising control port %d", p)
	} else {
		mu.Unlock()
		resp.Close()
	}
}

// ctrlMu serialises every control-server rebuild (startup, a config change, the
// web remote on/off, an account refresh): two overlapping rebuilds could fail to
// bind or publish a server built from a stale config. mu still guards the
// published pointers.
var ctrlMu sync.Mutex

// rebuildControlServer replaces the control server: it closes the current one,
// starts a new one for cfg around c on the first of addrs that binds (none: no
// server), publishes it with its config, and re-points the LAN advertiser.
// Caller holds ctrlMu.
func rebuildControlServer(cfg control.Config, c *deezer.Client, addrs ...string) *control.Server {
	mu.Lock()
	old := ctrlSrv
	ctrlSrv, ctrlCfg, ctrlSrvUserID = nil, control.Config{}, ""
	mu.Unlock()
	if old != nil {
		old.Close() // before the new bind: it may reuse the port
	}
	var srv *control.Server
	id, dev := clientInfo()
	for _, a := range addrs {
		cfg.Addr = a
		s := control.New(cfg, engineState, engineAccount, engineCommands(), c)
		// Identity before Start so the serving goroutine never races these.
		s.SetVersion(coreVersion)
		s.SetClientInfo(id, dev)
		s.SetEQ(engineEQ())
		s.SetStatusSince(engineStateSince)
		if err := s.Start(); err != nil {
			odlog.Warn("control api on %s: %v", a, err)
			continue
		}
		srv = s
		break
	}
	if srv != nil {
		cfg.Addr = srv.Addr()
		mu.Lock()
		ctrlSrv, ctrlCfg = srv, cfg
		if c != nil {
			ctrlSrvUserID = c.Account().UserID
		}
		mu.Unlock()
	}
	refreshAdvertiser()
	return srv
}

// refreshControlServer rebuilds the control server around the current client
// after a re-login: control.New snapshots the *deezer.Client for the browse
// endpoints (/playlists, /search), so without this an account switch (a second
// Init with a new ARL) keeps serving the previous account's library on its
// stale session — startServices runs under a sync.Once and never re-runs.
// Pairing sessions are dropped deliberately: a phone paired under the old
// account must re-pair. For the same account it only swaps c into the running
// server (its sessions stay); without a server it does nothing.
func refreshControlServer(c *deezer.Client) {
	ctrlMu.Lock()
	defer ctrlMu.Unlock()
	mu.Lock()
	srv, cfg := ctrlSrv, ctrlCfg
	stale := srv != nil && ctrlSrvUserID != c.Account().UserID
	mu.Unlock()
	if srv != nil && !stale {
		srv.SetClient(c) // same account, new session: keep the server and its sessions
	}
	if !stale {
		return
	}
	pairing := srv.PairingActive()
	// The same host:port, whatever the config said (:0).
	if s := rebuildControlServer(cfg, c, srv.Addr()); s != nil && pairing {
		s.EnablePairing() // fresh code; old sessions must not carry across accounts
	}
}

// discordWake is signalled by notifyControlState, so the presence loop wakes
// when playback starts.
var discordWake = make(chan struct{}, 1)

// serviceTicker pushes Discord presence once a second while a track loads or
// plays, or a Connect device is routed; when playback stops it pushes once
// more and waits for the next state change.
func serviceTicker() {
	for {
		p := Player()
		if p != nil {
			publishDiscord(p)
		}
		if routedRemote() != nil || p != nil && (p.State() == audio.Playing || p.State() == audio.Loading) {
			time.Sleep(time.Second)
			continue
		}
		<-discordWake
	}
}

func publishDiscord(p *audio.Player) {
	// When routed to a remote device local playback is stopped, so the local
	// player would report "stopped" and Update() would clear the presence. Derive
	// it from the remote snapshot instead (matches the now-playing / lyrics sync
	// remotePoller already keeps) so RP reflects the remote track.
	if routedRemote() != nil {
		st := remoteSnapshot()
		ds := discord.State{PositionMS: st.PositionMS, DurationMS: st.DurationMS}
		if st.Track != nil {
			ds.Title, ds.Artist, ds.Album = st.Track.Title, st.Track.Artist, st.Track.Album
			if st.Track.DurationMS > 0 {
				ds.DurationMS = st.Track.DurationMS
			}
		}
		switch st.State {
		case "playing":
			ds.Status = "playing"
		case "paused":
			ds.Status = "paused"
		default:
			ds.Status = "stopped"
		}
		dp.Update(ds)
		return
	}
	cur := currentTrack()
	ds := discord.State{
		Title: cur.Name, Artist: cur.ArtistLine(), Album: cur.AlbumName,
		DurationMS: cur.DurationMS, PositionMS: p.PositionMS(),
	}
	switch p.State() {
	case audio.Playing:
		ds.Status = "playing"
	case audio.Paused:
		ds.Status = "paused"
	default:
		ds.Status = "stopped"
	}
	dp.Update(ds)
}

func engineState() control.State { return engineStateSince(0) }

// engineStateSince is the /status snapshot without the queue when the queue
// is still at version sent (a stream that already carries it); 0 always
// includes it.
func engineStateSince(sent uint64) control.State {
	st := control.State{State: "stopped"}
	// Queue + repeat + shuffle come from the engine queue, so /status reports the
	// real values once a host has synced them (QueueSet / SetRepeat /
	// SetShuffle). A host that never syncs keeps the old wire shape: no queue,
	// repeat "off", shuffle false.
	queueMu.Lock()
	st.Repeat = engineQ.Repeat().String()
	st.Shuffle = shuffleOn
	st.QueueVersion = engineQ.Version()
	if ts := engineQ.Tracks(); len(ts) > 0 && (sent == 0 || st.QueueVersion != sent) {
		qt := make([]control.Track, len(ts))
		for i, t := range ts {
			qt[i] = toControlTrack(t)
		}
		st.Queue = qt
	}
	queueMu.Unlock()
	p := Player()
	if p == nil {
		return st
	}
	st.PositionMS, st.DurationMS = p.PositionMS(), p.DurationMS()
	st.Volume, st.Format = p.Volume(), p.Format()
	st.SleepActive, st.SleepEndOfTrack = p.SleepActive(), p.SleepEndOfTrack()
	st.SleepRemainingMS = p.SleepRemainingMS()
	switch p.State() {
	case audio.Playing:
		st.State = "playing"
	case audio.Paused:
		st.State = "paused"
	case audio.Loading:
		st.State = "loading"
	case audio.Errored:
		st.State = "error"
	default:
		st.State = "stopped"
	}
	if cur := currentTrack(); cur.ID != "" {
		ct := toControlTrack(cur)
		st.Track = &ct
	}
	return st
}

// toControlTrack converts a Deezer track to the control API wire shape.
func toControlTrack(t deezer.Track) control.Track {
	ct := control.Track{
		ID: t.ID, Title: t.Name, Artist: t.ArtistLine(),
		Album: t.AlbumName, Explicit: t.Explicit, DurationMS: t.DurationMS,
		ArtworkURL: t.ArtworkURL,
	}
	if len(t.Artists) > 0 {
		ct.ArtistID = t.Artists[0].ID
	}
	return ct
}

// ---- engine queue sync (host queue -> engineQ) ----

// queueTracksFromJSON decodes a host queue payload: a JSON array of tracks in
// the same wire shape every list/browse call returns (bridge.Track). Only "id"
// is required; "durationMs" should be set so controllers' end-of-track
// detection works without a metadata refetch. Entries without an id are
// dropped. "" and "null" decode to an empty queue (clear).
func queueTracksFromJSON(js string) ([]deezer.Track, error) {
	if js == "" {
		return nil, nil
	}
	var wire []bridge.Track
	if err := json.Unmarshal([]byte(js), &wire); err != nil {
		return nil, err
	}
	ts := make([]deezer.Track, 0, len(wire))
	for _, w := range wire {
		if w.ID == "" {
			continue
		}
		ts = append(ts, trackFromWire(w))
	}
	return ts, nil
}

// trackFromWire converts a wire track (bridge.Track) back to a deezer.Track.
// When the artists array is absent it falls back to artistLine/artistId so the
// queue still shows an artist on /status.
func trackFromWire(w bridge.Track) deezer.Track {
	t := deezer.Track{
		ID: w.ID, Name: w.Name, DurationMS: w.DurationMS,
		AlbumName: w.AlbumName, ArtworkURL: w.ArtworkURL, Explicit: w.Explicit,
	}
	for _, a := range w.Artists {
		t.Artists = append(t.Artists, deezer.Artist{ID: a.ID, Name: a.Name})
	}
	if len(t.Artists) == 0 && (w.ArtistID != "" || w.ArtistLine != "") {
		t.Artists = []deezer.Artist{{ID: w.ArtistID, Name: w.ArtistLine}}
	}
	return t
}

// QueueSet replaces the engine-side queue with the host's queue so remote
// controllers see it on /status and /next + /prev walk it. js is a JSON array
// of tracks in the wire shape every list call returns; only id is required,
// but durationMs should be set so controllers' end-of-track detection works.
// The cursor resets to 0 (follow with QueueSetIndex) and the version always
// bumps; "[]" clears. A parse error leaves the queue as it was.
func QueueSet(js string) error {
	ts, err := queueTracksFromJSON(js)
	if err != nil {
		return err
	}
	engineQueueSet(ts)
	return nil
}

// engineQueueSet replaces the engine-side queue with ts (cursor at 0).
func engineQueueSet(ts []deezer.Track) {
	queueMu.Lock()
	engineQ.Set(ts, 0)
	queueMu.Unlock()
	notifyControlState()
}

// QueueAppend adds the tracks of js (QueueSet's shape) to the end of the
// engine queue ("Add to queue"); a parse error leaves the queue as it was.
func QueueAppend(js string) error {
	ts, err := queueTracksFromJSON(js)
	if err != nil {
		return err
	}
	engineQueueAppend(ts)
	return nil
}

// engineQueueAppend adds ts to the end of the engine queue, keeping the cursor;
// the version bumps once when anything was added. A preload is dropped only
// when the track after the cursor changed (the cursor was on the last row, or
// repeat-all wrapped to the first).
func engineQueueAppend(ts []deezer.Track) {
	if len(ts) == 0 {
		return
	}
	queueMu.Lock()
	before, hadNext := engineQ.PeekNext()
	engineQ.Append(ts...)
	after, hasNext := engineQ.PeekNext()
	queueMu.Unlock()
	if hadNext != hasNext || before.ID != after.ID {
		preloads.Clear()
	}
	notifyControlState()
}

// QueueSetIndex aligns the engine queue cursor to the row the host is
// already playing (clamped; no-op when the queue is empty). It uses
// AlignIndex, not SetIndex, so this pure synchronisation doesn't push a
// synthetic history entry that would make a later remote Prev jump to a
// never-played track. Genuine remote jumps go through engineQueueSelect
// (SetIndex), which does record history.
func QueueSetIndex(i int) {
	queueMu.Lock()
	engineQ.AlignIndex(i)
	queueMu.Unlock()
	notifyControlState()
}

// QueueIndex returns the engine queue cursor (-1 when empty), so a host can
// resync its own cursor after an engine-driven advance.
func QueueIndex() int {
	queueMu.Lock()
	defer queueMu.Unlock()
	return engineQ.Index()
}

// QueueVersion returns the queue's content-mutation counter (bumped by
// set/add/remove/move, NOT by cursor moves), so a host can poll it cheaply and
// pull QueueSnapshot only when a remote controller actually edited the queue.
func QueueVersion() uint64 {
	queueMu.Lock()
	defer queueMu.Unlock()
	return engineQ.Version()
}

// QueueSnapshot returns the engine queue's contents + cursor + version under
// one lock.
func QueueSnapshot() (ts []deezer.Track, index int, version uint64) {
	queueMu.Lock()
	defer queueMu.Unlock()
	return append([]deezer.Track(nil), engineQ.Tracks()...), engineQ.Index(), engineQ.Version()
}

// engineSetRepeat / engineSetShuffle persist the repeat/shuffle choice on the
// engine queue so /status reports them and the engine queue honors them when
// it advances.
func engineSetRepeat(mode string) {
	r := queue.RepeatOff
	switch mode {
	case "all":
		r = queue.RepeatAll
	case "one":
		r = queue.RepeatOne
	}
	queueMu.Lock()
	engineQ.SetRepeat(r)
	queueMu.Unlock()
	// The upcoming track changed (repeat-one replays the CURRENT track): a
	// preload armed for the old linear next must not gapless-promote.
	preloads.Clear()
	notifyControlState()
}

// SetRepeat sets the repeat mode (0=off, 1=all, 2=one) of the engine queue,
// which reports it on /status and honors it when it advances. It is never
// forwarded to a routed Connect device: this instance owns the queue and
// streams the device one track at a time, so a Repeat-All there would loop
// its single-item queue for ever.
func SetRepeat(mode int) {
	m := "off"
	switch mode {
	case 1:
		m = "all"
	case 2:
		m = "one"
	}
	engineSetRepeat(m)
}

// Repeat returns the engine queue's repeat mode ("off", "all" or "one"),
// also while routed to a Connect device: repeat is kept here and never
// forwarded (the device is set to "off"), so its snapshot would contradict
// SetRepeat.
func Repeat() string {
	queueMu.Lock()
	defer queueMu.Unlock()
	return engineQ.Repeat().String()
}

// SetShuffle records the shuffle flag on the engine queue (see
// SetShuffleHostOwned) and forwards it to a routed Connect device.
func SetShuffle(on bool) {
	engineSetShuffle(on)
	if rc := routedRemote(); rc != nil {
		if st, err := rc.SetShuffle(on); err == nil {
			setRemoteState(rc, st)
		}
	}
}

// Shuffle reports the shuffle flag: the routed device's snapshot while
// casting, else the engine queue's.
func Shuffle() bool {
	if routedRemote() != nil {
		return remoteSnapshot().Shuffle
	}
	return engineShuffle()
}

// SetShuffleHostOwned(true) declares that the host owns play order: it
// shuffles its own queue and syncs that order (QueueSet), so SetShuffle and a
// controller's shuffle only record the flag for /status, Shuffle and
// notifications, without reordering the engine queue. Call before Init.
func SetShuffleHostOwned(on bool) {
	queueMu.Lock()
	shuffleHostOwned = on
	if shuffleHostOwned {
		engineQ.SetShuffle(false)
	}
	queueMu.Unlock()
}

// The shuffle flag as reported (/status, Shuffle, notifications). With
// shuffleHostOwned (SetShuffleHostOwned) the host shuffles its own queue and
// syncs that order, so the flag is recorded but never applied to engineQ.
// Both guarded by queueMu.
var shuffleOn, shuffleHostOwned bool

func engineShuffle() bool {
	queueMu.Lock()
	defer queueMu.Unlock()
	return shuffleOn
}

func engineSetShuffle(on bool) {
	queueMu.Lock()
	shuffleOn = on
	apply := !shuffleHostOwned
	if apply {
		engineQ.SetShuffle(on)
	}
	queueMu.Unlock()
	// The upcoming track changed (re-shuffled order): a preload armed for the
	// old linear next must not gapless-promote.
	if apply {
		preloads.Clear()
	}
	notifyControlState()
}

// clientID / deviceLabel identify this host on the network. A host may
// override them (SetClientInfo); otherwise they default to the platform.
var (
	clientID    = runtime.GOOS
	deviceLabel = "OpenDeezer (" + platformName(runtime.GOOS) + ")"
)

func platformName(goos string) string {
	switch goos {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	case "android":
		return "Android"
	case "ios":
		return "iOS"
	default:
		return goos
	}
}

// SetClientInfo overrides the client id and device label this instance
// reports over discovery and /whoami ("" keeps a value). Call it before Init.
func SetClientInfo(client, device string) {
	mu.Lock()
	if client != "" {
		clientID = client
	}
	if device != "" {
		deviceLabel = device
	}
	mu.Unlock()
}

// clientInfo returns the client id + device label (mu-guarded; safe off the
// responder goroutine).
func clientInfo() (string, string) {
	mu.Lock()
	defer mu.Unlock()
	return clientID, deviceLabel
}

// advertInfo is the identity broadcast over LAN discovery.
func advertInfo() discovery.Info {
	id, _ := clientInfo()
	return discovery.Info{Name: engineAccount().Name, Client: id, Version: coreVersion}
}

func engineAccount() control.Account {
	c := Client()
	if c == nil {
		return control.Account{}
	}
	a := c.Account()
	return control.Account{UserID: a.UserID, Name: a.Name, Offer: a.Offer}
}

// engineCommands maps control commands to player-level actions. next/prev drive
// the engine playback queue (engineQ) when playing locally, or forward to the
// connected remote when a device is selected; shuffle/repeat are recorded on
// the engine queue (so /status reports them and the queue honors them) and also
// forward to the connected remote (if any) so a controller can drive the
// remote's queue.
func engineCommands() control.Commands {
	return control.Commands{
		PlayPause: func() { withPlayerNotify(func(p *audio.Player) { p.TogglePause() }) },
		Next:      engineNext,
		Prev:      enginePrev,
		// Record the in-progress listen before halting: the next Play sees the
		// player Stopped and skips recording, so this is the only chance.
		Stop:         func() { withPlayerNotify(func(p *audio.Player) { noteTrackTransition(p); p.Stop() }) },
		Restart:      func() { withPlayerNotify(func(p *audio.Player) { p.SeekMS(0) }) },
		Seek:         func(ms int64) { withPlayerNotify(func(p *audio.Player) { p.SeekMS(ms) }) },
		SetVolume:    func(v float64) { withPlayerNotify(func(p *audio.Player) { p.SetVolume(v) }) },
		PlayTrack:    enginePlayTrack,
		PlayPlaylist: enginePlayPlaylist,
		// CycleRepeat / SetRepeat: record the mode locally only. Repeat is NOT
		// forwarded to a routed remote — this instance owns the queue and streams
		// the host one track at a time, so a Repeat-All forwarded to the host would
		// trap it looping its single-item queue. The engine's own auto-advance
		// honors repeat when it drives the remote.
		CycleRepeat: func() {
			queueMu.Lock()
			next := ((engineQ.Repeat() + 1) % 3).String()
			queueMu.Unlock()
			engineSetRepeat(next)
		},
		SetRepeat: func(mode string) {
			engineSetRepeat(mode)
		},
		// CycleShuffle equivalents: shuffle IS forwarded (the host has no queue of
		// its own, but a controller's shuffle intent is harmless there and keeps
		// /status consistent).
		ToggleShuffle: func() { SetShuffle(!engineShuffle()) },
		SetShuffle:    SetShuffle,
		QueueAdd:      engineQueueAdd,
		QueueJump:     engineQueueJump,
		// SetQueue lets a controller replace the engine queue wholesale (cursor at
		// index). QueueSet + QueueSetIndex in one hop: parse, Set, clear a
		// now-stale preload, and notify. Nil-safe (parse error surfaced).
		SetQueue: func(tracksJSON string, index int) error {
			ts, err := queueTracksFromJSON(tracksJSON)
			if err != nil {
				return err
			}
			queueMu.Lock()
			engineQ.Set(ts, index)
			queueMu.Unlock()
			preloads.Clear()
			notifyControlState()
			return nil
		},
		QueueRemove:      QueueRemove,
		QueueMove:        QueueMove,
		PlayAlbum:        enginePlayAlbum,
		PlayMixTrack:     enginePlayMixTrack,
		PlayMixArtist:    enginePlayMixArtist,
		HistoryRecent:    engineHistoryRecent,
		SetSleepTimer:    SetSleepTimer,
		CancelSleepTimer: CancelSleepTimer,
	}
}

// fetchEpisodeMeta enriches the current episode with title/podcast name/artwork
// by calling the REST /episode/{id} endpoint, unless another track was set
// since gen. Best-effort, like fetchTrackMeta. The episode's "artist line" is
// the podcast/show name.
func fetchEpisodeMeta(c *deezer.Client, id string, gen uint64) {
	if c == nil || id == "" {
		return
	}
	ep, err := c.EpisodeMeta(id)
	if err != nil || ep.ID == "" {
		return
	}
	setCurrentTrackAt(gen, ep.AsTrack())
}

// startPlay starts plan on p unless a newer play claimed a seq meanwhile, then
// runs started (now-playing bookkeeping) in the same step. A cached plan whose
// entry is gone is planned again first, outside that step.
func startPlay(p *audio.Player, plan *deezer.StreamPlan, durationMS int64, seq uint64, started func()) error {
	plan = p.ReplanIfGone(plan)
	plays.startMu.Lock()
	defer plays.startMu.Unlock()
	if plays.superseded(seq) {
		return errSuperseded
	}
	noteTrackTransition(p) // record the outgoing listen before the swap
	if err := p.Play(plan, durationMS); err != nil {
		return err
	}
	preloads.Clear() // p.Play discarded any pending preload; drop its request and stash too
	started()
	return nil
}

// enginePlayResolved resolves a stream for t and starts it on the player as
// play seq, recording it as the current track. Blocking (network round-trip);
// callers must be off the realtime audio path.
func enginePlayResolved(p *audio.Player, t deezer.Track, seq uint64) error {
	plan, err := preparePlan(netClient(), t.ID)
	if err != nil {
		return err
	}
	return startPlay(p, plan, t.DurationMS, seq, func() { setCurrentTrack(t) })
}

// engineLoadAndPlay replaces the engine queue with ts (cursor at start) and
// plays the current track, so a natural finish auto-advances through the rest.
func engineLoadAndPlay(p *audio.Player, ts []deezer.Track, start int, seq uint64) error {
	queueMu.Lock()
	engineQ.Set(ts, start)
	t, ok := engineQ.Current()
	queueMu.Unlock()
	notifyControlState() // queue replaced even if the play below fails
	if !ok {
		return fmt.Errorf("empty queue")
	}
	return enginePlayResolved(p, t, seq)
}
func enginePlayTrack(id string) {
	p := Player()
	if p == nil {
		return
	}
	seq, done := plays.beginUser()
	defer done()
	c := netClient()
	plan, err := preparePlan(c, id)
	if err != nil {
		return
	}
	t := plan.Track
	if t.ID == "" { // a cache-sourced plan carries no metadata
		t = deezer.Track{ID: id}
		if c != nil {
			if t, err = c.Track(id); err != nil {
				return
			}
		}
	}
	// A single-track "queue": nothing to auto-advance to, but loading it keeps the
	// engine queue in sync with what's actually playing, so a previously loaded
	// playlist can't auto-advance after this ad-hoc single track finishes.
	queueMu.Lock()
	engineQ.Set([]deezer.Track{t}, 0)
	queueMu.Unlock()
	notifyControlState()
	_ = startPlay(p, plan, t.DurationMS, seq, func() { setCurrentTrack(t) })
}

func enginePlayPlaylist(id string) {
	p := Player()
	if p == nil {
		return
	}
	seq, done := plays.beginUser()
	defer done()
	ts, err := PlaylistTracks(id)
	if err != nil || len(ts) == 0 {
		return
	}
	_ = engineLoadAndPlay(p, ts, 0, seq)
}

// engineQueueAdvance advances the engine queue when the track finishedID ends
// naturally and returns the next track to play. It advances only when finishedID
// is the queue's current track, so an ad-hoc Play (which bypasses the engine
// queue) can't trigger a stale auto-advance into an unrelated playlist. Pure
// queue logic — the caller performs the network resolve + Play.
func engineQueueAdvance(finishedID string) (deezer.Track, bool) {
	queueMu.Lock()
	defer queueMu.Unlock()
	cur, ok := engineQ.Current()
	if !ok || cur.ID == "" || cur.ID != finishedID {
		return deezer.Track{}, false
	}
	if !engineQ.AdvanceAuto() {
		return deezer.Track{}, false
	}
	return engineQ.Current()
}

// engineAdvanceOnFinish runs from the player's onFinish callback on the
// player's manage() goroutine when a track ends naturally. It
// advances the engine queue and starts the next track on a FRESH goroutine, so
// the callback never blocks the manager on the network resolve + Play (which
// would stall prebuffer promotion + the sleep-timer fade).
//
// It returns true only when the engine queue actually drove this finish (the
// finished track was the engine queue's current track). The onFinish callback
// uses that to decide whether to also bump the host's finished counter, so the
// engine queue and the host's own queue never both advance on one finish.
func engineAdvanceOnFinish() bool {
	// Gapless/crossfade promote: the player swapped the preloaded source in and
	// kept playing (a real finish passes through Stopped before onFinish fires).
	if p := Player(); p != nil && p.State() == audio.Playing {
		return engineSyncOnGaplessPromote()
	}
	if plays.user.Load() > 0 {
		// A user play is resolving and replaces this finish: neither queue
		// advances (true keeps the host's counter still when the engine owned it).
		queueMu.Lock()
		cur, ok := engineQ.Current()
		queueMu.Unlock()
		return ok && cur.ID != "" && cur.ID == currentTrack().ID
	}
	next, ok := engineQueueAdvance(currentTrack().ID)
	if !ok {
		return false
	}
	// Claim a seq for this auto-advance launch; a user play that races the
	// async resolve below supersedes it.
	seq := plays.claim()
	advancing.Add(1)
	go func() {
		defer advancing.Done()
		engineAutoAdvance(next, seq)
	}()
	return true
}

// advancing counts the auto-advance launches still resolving (tests wait for
// them before they swap the engine's globals).
var advancing sync.WaitGroup

// maxAdvanceFailures bounds how many unresolvable tracks an auto-advance skips
// before it stops in the Errored state.
const maxAdvanceFailures = 3

// engineAutoAdvance plays next as seq. A track that cannot be resolved is
// skipped like a user Next (also under repeat-one), and after
// maxAdvanceFailures in a row, or when the queue ends first, the player
// reports the reason instead of sitting Stopped with nothing playing. A failed
// start (the output device) is the player's own error and skips nothing.
func engineAutoAdvance(next deezer.Track, seq uint64) {
	for failures := 1; ; failures++ {
		plan, err := preparePlan(netClient(), next.ID)
		if err == nil {
			if p := Player(); p != nil {
				_ = startPlay(p, plan, next.DurationMS, seq, func() { setCurrentTrack(next) })
			}
			return
		}
		if plays.superseded(seq) {
			return
		}
		odlog.Warn("auto-advance: %s: %v", next.ID, err)
		queueMu.Lock()
		cur, ok := engineQ.Current()
		ok = ok && cur.ID == next.ID // else the queue moved on meanwhile
		ended := ok && (failures == maxAdvanceFailures || !engineQ.Next())
		if ok && !ended {
			next, ok = engineQ.Current()
		}
		queueMu.Unlock()
		if ended {
			withPlayer(func(p *audio.Player) { p.Fail(err.Error()) })
			return
		}
		if !ok {
			return
		}
		notifyControlState()
	}
}

// engineSyncOnGaplessPromote does the queue + now-playing bookkeeping after
// the player gaplessly promoted its preloaded source (it is still Playing
// then). Split from engineAdvanceOnFinish so tests can drive a promote without
// a live audio device. Returns whether the engine queue owned the finished
// track (the caller uses that to keep exactly one queue mechanism advancing).
//
// The preloaded track is always the deterministic linear next, so when the
// engine queue owned the finished track just walk the cursor + now-playing
// along with the audio — re-resolving + Play here would kill the promoted
// stream and restart the track over the network.
func engineSyncOnGaplessPromote() bool {
	queueMu.Lock()
	cur, ok := engineQ.Current()
	owned := ok && cur.ID != "" && cur.ID == currentTrack().ID && engineQ.AdvanceLinear()
	var next deezer.Track
	if owned {
		next, _ = engineQ.Current()
	}
	queueMu.Unlock()
	// Either way the player just consumed its armed preload: take the stashed
	// identity so a later finish can't reuse it.
	promoted, armed := preloads.Take()
	if !owned {
		// Engine queue empty or misaligned: the host preloaded without mirroring
		// its queue (QueueSet). The promoted stream can only be the armed
		// preload — p.next is set solely by the preloader, which stashes it — so
		// move now-playing onto it. Otherwise /status would stay on the
		// finished track, whose NEXT finish would re-record it in history while
		// the promoted track never gets recorded. The outgoing listen itself was
		// already recorded by noteTrackFinished (it runs before this in the
		// onFinish callback).
		if armed {
			gen := setCurrentTrack(promoted)
			if promoted.Name == "" { // stashed from a cache-sourced plan
				go fetchTrackMeta(netClient(), promoted.ID, gen)
			}
		}
		return false // still not the engine queue's finish; the host's own queue advances
	}
	setCurrentTrack(next)
	return true
}

// engineNext / enginePrev back the control API's /next + /prev. When a remote
// device is selected they forward to it (local playback is stopped); otherwise
// they move the engine queue and play the new current track locally.
func engineNext() { engineStep(engineQ.Next, (*control.Client).Next) }

func enginePrev() { engineStep(engineQ.Prev, (*control.Client).Prev) }

func engineStep(move func() bool, remote func(*control.Client) (control.State, error)) {
	if rc := routedRemote(); rc != nil {
		if st, err := remote(rc); err == nil {
			setRemoteState(rc, st)
		}
		return
	}
	seq, done := plays.beginUser()
	defer done()
	queueMu.Lock()
	moved := move()
	t, ok := engineQ.Current()
	queueMu.Unlock()
	if p := Player(); moved && ok && p != nil {
		_ = enginePlayResolved(p, t, seq)
	}
}

// ---- extended control commands: queue edits, album/mix play, history ----
//
// These back the control API's /queue/*, /play/album, /play/mix/* and
// /history/recent endpoints. Like Next/Prev they forward to the routed Connect
// device when one is selected (this instance is then a controller, so queue
// edits belong to the remote's queue); HistoryRecent stays local — the
// listening history never leaves the machine that did the listening.

// engineQueueInsert is the pure queue mutation behind QueueAdd: insert t right
// after the current track (next=true) or append it to the end. A preloaded
// next stream is discarded — the upcoming track may have changed.
func engineQueueInsert(t deezer.Track, next bool) {
	queueMu.Lock()
	if next {
		engineQ.InsertAfterCurrent(t)
	} else {
		engineQ.Append(t)
	}
	queueMu.Unlock()
	preloads.Clear()
	notifyControlState()
}

// engineQueueAdd backs POST /queue/add: resolve full track metadata (so
// /status shows title/artist/duration, and end-of-track detection works), then
// insert or append it.
func engineQueueAdd(id string, next bool) error {
	if rc := routedRemote(); rc != nil {
		st, err := rc.QueueAdd(id, next)
		if err == nil {
			setRemoteState(rc, st)
		}
		return err
	}
	c, err := Online()
	if err != nil {
		return err
	}
	t, err := c.Track(id)
	if err != nil {
		return err
	}
	if t.ID == "" {
		return fmt.Errorf("track %s not found", id)
	}
	engineQueueInsert(t, next)
	return nil
}

// engineQueueSelect is the pure cursor move behind QueueJump: validate index,
// move the cursor (recording history so Prev retraces) and return the now-
// current track.
func engineQueueSelect(index int) (deezer.Track, error) {
	queueMu.Lock()
	defer queueMu.Unlock()
	if index < 0 || index >= engineQ.Len() {
		return deezer.Track{}, fmt.Errorf("index %d out of range (queue has %d tracks)", index, engineQ.Len())
	}
	engineQ.SetIndex(index)
	t, _ := engineQ.Current()
	return t, nil
}

// engineQueueJump backs POST /queue/jump: move the cursor to index and play
// that row through the normal engine play path.
func engineQueueJump(index int) error {
	if rc := routedRemote(); rc != nil {
		st, err := rc.QueueJump(index)
		if err == nil {
			setRemoteState(rc, st)
		}
		return err
	}
	p := Player()
	if p == nil {
		return errNotReady
	}
	seq, done := plays.beginUser()
	defer done()
	t, err := engineQueueSelect(index)
	if err != nil {
		return err
	}
	if err := enginePlayResolved(p, t, seq); err != nil {
		return fmt.Errorf("could not start track %s: %w", t.ID, err)
	}
	return nil
}

// QueueRemove removes the row at index (POST /queue/remove, the host's Up-Next
// editor); routed to a Connect device, the edit goes to it. The playing row
// can't be removed (same guard as the TUI): the audio would keep playing a
// track that no longer exists in the queue, desyncing every controller.
func QueueRemove(index int) error {
	if rc := routedRemote(); rc != nil {
		st, err := rc.QueueRemove(index)
		if err == nil {
			setRemoteState(rc, st)
		}
		return err
	}
	queueMu.Lock()
	if index < 0 || index >= engineQ.Len() {
		queueMu.Unlock()
		return fmt.Errorf("index %d out of range (queue has %d tracks)", index, engineQ.Len())
	}
	if index == engineQ.Index() {
		queueMu.Unlock()
		return fmt.Errorf("cannot remove the playing track")
	}
	engineQ.Remove(index)
	queueMu.Unlock()
	preloads.Clear()
	notifyControlState()
	return nil
}

// QueueMove moves the row at from to to (POST /queue/move, the host's Up-Next
// editor); routed to a Connect device, the edit goes to it. The cursor,
// history and shuffle cycle all follow the moved tracks (queue.Move), so
// reordering never changes what's audible. from == to is a no-op success.
func QueueMove(from, to int) error {
	if rc := routedRemote(); rc != nil {
		st, err := rc.QueueMove(from, to)
		if err == nil {
			setRemoteState(rc, st)
		}
		return err
	}
	queueMu.Lock()
	if n := engineQ.Len(); from < 0 || from >= n || to < 0 || to >= n {
		queueMu.Unlock()
		return fmt.Errorf("from/to out of range (queue has %d tracks)", n)
	}
	moved := from != to && engineQ.Move(from, to)
	queueMu.Unlock()
	if moved {
		preloads.Clear()
		notifyControlState()
	}
	return nil
}

// enginePlayFetched loads a fetched track list into the engine queue and plays
// the first track (shared tail of PlayAlbum / PlayMixTrack / PlayMixArtist).
// The fetch counts as part of the user play.
func enginePlayFetched(what string, fetch func(*deezer.Client) ([]deezer.Track, error)) error {
	c, err := Online()
	if err != nil {
		return err
	}
	p := Player()
	if p == nil {
		return errNotReady
	}
	seq, done := plays.beginUser()
	defer done()
	ts, err := fetch(c)
	if err != nil {
		return err
	}
	if len(ts) == 0 {
		return fmt.Errorf("%s has no playable tracks", what)
	}
	if err := engineLoadAndPlay(p, ts, 0, seq); err != nil {
		return fmt.Errorf("could not start %s playback: %w", what, err)
	}
	return nil
}

// enginePlayAlbum backs POST /play/album: replace the engine queue with the
// album's tracks and play the first (natural finishes auto-advance).
func enginePlayAlbum(id string) error {
	if rc := routedRemote(); rc != nil {
		st, err := rc.PlayAlbum(id)
		if err == nil {
			setRemoteState(rc, st)
		}
		return err
	}
	return enginePlayFetched("album", func(*deezer.Client) ([]deezer.Track, error) { return AlbumTracks(id) })
}

// enginePlayMixTrack backs POST /play/mix/track ("song radio", seed kept as
// the first entry).
func enginePlayMixTrack(id string) error {
	if rc := routedRemote(); rc != nil {
		st, err := rc.PlayMixTrack(id)
		if err == nil {
			setRemoteState(rc, st)
		}
		return err
	}
	return enginePlayFetched("mix", func(c *deezer.Client) ([]deezer.Track, error) { return c.TrackMix(id) })
}

// enginePlayMixArtist backs POST /play/mix/artist ("artist radio").
func enginePlayMixArtist(id string) error {
	if rc := routedRemote(); rc != nil {
		st, err := rc.PlayMixArtist(id)
		if err == nil {
			setRemoteState(rc, st)
		}
		return err
	}
	return enginePlayFetched("mix", func(c *deezer.Client) ([]deezer.Track, error) { return c.ArtistMix(id) })
}

// engineHistoryRecent backs GET /history/recent with the machine-local
// listening log (newest first; history.Store.Recent never returns nil, so an
// empty log marshals to []).
func engineHistoryRecent(n int) (json.RawMessage, error) {
	entries, err := HistoryRecent(n)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(entries)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// ---- local listening history recording ----

var (
	histMu     sync.Mutex
	histStore  *history.Store
	histInited bool
)

// HistoryRecent returns the newest n entries of the machine-local listening
// history (n <= 0: all), songs recorded without artwork or duration filled in
// from the track (see fillHistoryMeta). Each entry's Kind is "" for a song
// and "episode" for a podcast episode, which a host replays with PlayEpisode.
func HistoryRecent(n int) ([]history.Entry, error) {
	st := historyStore()
	if st == nil {
		return nil, fmt.Errorf("history unavailable")
	}
	entries, err := st.Recent(n)
	if err != nil {
		return nil, err
	}
	fillHistoryMeta(netClient(), entries)
	return entries, nil
}

// trackMeta caches the artwork and duration looked up for history entries
// recorded without them, by track id. Guarded by trackMetaMu.
var (
	trackMetaMu sync.Mutex
	trackMeta   = map[string]deezer.Track{}
)

// maxMetaLookups bounds the concurrent track lookups of fillHistoryMeta.
const maxMetaLookups = 4

// fillHistoryMeta fills a missing artwork URL and duration of song entries
// from the track itself: each id is looked up once per process, at most
// maxMetaLookups at a time. A lookup that fails is not asked again, unless it
// failed for want of a network. Episodes are left as they are.
func fillHistoryMeta(c *deezer.Client, entries []history.Entry) {
	missing := func(e history.Entry) bool {
		return e.Kind != history.KindEpisode && e.TrackID != "" && (e.ArtworkURL == "" || e.DurationMS == 0)
	}
	need := map[string]bool{}
	trackMetaMu.Lock()
	for _, e := range entries {
		if _, cached := trackMeta[e.TrackID]; missing(e) && !cached {
			need[e.TrackID] = true
		}
	}
	trackMetaMu.Unlock()
	if c != nil {
		var wg sync.WaitGroup
		slots := make(chan struct{}, maxMetaLookups)
		for id := range need {
			wg.Go(func() {
				slots <- struct{}{}
				t, err := c.Track(id)
				<-slots
				if err == nil || !deezer.IsNoNetwork(err) {
					trackMetaMu.Lock()
					trackMeta[id] = deezer.Track{ArtworkURL: t.ArtworkURL, DurationMS: t.DurationMS}
					trackMetaMu.Unlock()
				}
			})
		}
		wg.Wait()
	}
	trackMetaMu.Lock()
	defer trackMetaMu.Unlock()
	for i, e := range entries {
		if !missing(e) {
			continue
		}
		t := trackMeta[e.TrackID]
		if e.ArtworkURL == "" {
			entries[i].ArtworkURL = t.ArtworkURL
		}
		if e.DurationMS == 0 {
			entries[i].DurationMS = t.DurationMS
		}
	}
}

// historyStore lazily opens the shared local listening history at
// <config-dir>/history.jsonl (nil when no config dir is available). The store
// serializes its own file access; this only guards the lazy init.
func historyStore() *history.Store {
	histMu.Lock()
	defer histMu.Unlock()
	if !histInited {
		histInited = true
		if s, err := history.Default(); err == nil {
			histStore = s
		}
	}
	return histStore
}

// setHistoryStore overrides the history store (tests only).
func setHistoryStore(s *history.Store) {
	histMu.Lock()
	histStore, histInited = s, true
	histMu.Unlock()
}

// jTrackStat / jArtistStat are the stable wire shapes for the history-stats
// exports (history.TrackStat/ArtistStat carry no JSON tags, so we don't marshal
// them directly).
type jTrackStat struct {
	TrackID  string `json:"trackId"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Plays    int    `json:"plays"`
	TotalSec int64  `json:"totalSec"`
}

type jArtistStat struct {
	Artist   string `json:"artist"`
	Plays    int    `json:"plays"`
	TotalSec int64  `json:"totalSec"`
}

// HistoryStats computes the local listening stats over the last sinceDays
// (sinceDays <= 0 = all history): {topTracks:[{trackId,title,artist,plays,
// totalSec}], topArtists:[{artist,plays,totalSec}], totalSeconds:N}.
// topTracks and topArtists are music only; totalSeconds is all listening
// time. Always the full shape (empty arrays + 0) when the store is unavailable
// or empty.
func HistoryStats(sinceDays int) map[string]any {
	topTracks := []jTrackStat{}
	topArtists := []jArtistStat{}
	var total int64
	if st := historyStore(); st != nil {
		var since time.Time // zero = all history
		if sinceDays > 0 {
			since = time.Now().AddDate(0, 0, -sinceDays)
		}
		if tt, err := st.TopTracks(since, 50); err == nil {
			topTracks = make([]jTrackStat, len(tt))
			for i, t := range tt {
				topTracks[i] = jTrackStat{TrackID: t.TrackID, Title: t.Title, Artist: t.Artist, Plays: t.Plays, TotalSec: t.TotalSec}
			}
		}
		if ta, err := st.TopArtists(since, 50); err == nil {
			topArtists = make([]jArtistStat, len(ta))
			for i, a := range ta {
				topArtists[i] = jArtistStat{Artist: a.Artist, Plays: a.Plays, TotalSec: a.TotalSec}
			}
		}
		total, _ = st.TotalListenedSec(since)
	}
	return map[string]any{"topTracks": topTracks, "topArtists": topArtists, "totalSeconds": total}
}

// listenEntry converts an outgoing track + its media kind ("" = song,
// history.KindEpisode = podcast) + listened milliseconds into a history entry.
// ok=false when it isn't worth recording: no track id, or under a second of
// listening (start-skips, double-taps).
func listenEntry(prev deezer.Track, kind string, playedMS int64) (history.Entry, bool) {
	if prev.ID == "" || playedMS < 1000 {
		return history.Entry{}, false
	}
	if prev.DurationMS > 0 && playedMS > prev.DurationMS {
		playedMS = prev.DurationMS
	}
	return history.Entry{
		TrackID:           prev.ID,
		Title:             prev.Name,
		Artist:            prev.ArtistLine(),
		Album:             prev.AlbumName,
		Kind:              kind,
		ArtworkURL:        prev.ArtworkURL,
		DurationMS:        prev.DurationMS,
		StartedAt:         time.Now().Unix() - playedMS/1000,
		DurationPlayedSec: playedMS / 1000,
	}, true
}

// recordListen appends the outgoing track to the local listening history,
// tagged with its media kind so replay can route (song -> Play, episode ->
// PlayEpisode). The file write (append + fsync) runs on its own goroutine so
// callers on the player's manage/callback path never block on disk I/O.
func recordListen(prev deezer.Track, kind string, playedMS int64) {
	e, ok := listenEntry(prev, kind, playedMS)
	if !ok {
		return
	}
	go func() {
		if st := historyStore(); st != nil {
			if err := st.Record(e); err != nil {
				odlog.Debug("history: %v", err)
			}
		}
	}()
}

// noteTrackTransition records the outgoing track when a new stream is about to
// replace one the user was actually listening to (manual skip / pick / new
// selection). Call BEFORE p.Play. A
// Stopped/Errored player means the previous track either ended naturally
// (already recorded by noteTrackFinished) or stopped long ago; neither may be re-recorded here, which also keeps
// pause/resume free of duplicates (they never reach this path at all).
func noteTrackTransition(p *audio.Player) {
	if p == nil {
		return
	}
	recordTransition(p.State(), p.PositionMS())
}

// recordTransition is noteTrackTransition's pure core, split out so tests can
// drive the Playing/Paused duplicate guard without a live audio device (a real
// player can't reach Playing in tests). A Stopped/Errored state records
// nothing — that's what makes a second Stop (or a Play after a natural finish)
// duplicate-free.
func recordTransition(state audio.State, positionMS int64) {
	switch state {
	case audio.Playing, audio.Paused:
		t, kind := currentTrackSnapshot()
		recordListen(t, kind, positionMS)
	}
}

// erroredFinishMaxMS bounds how much audio can have played for a finish to be
// treated as an error (CDN/decode failure) rather than a listen. A track that
// dies near its start played ~0; a failure well into the track is a real
// (partial) listen and is recorded normally.
const erroredFinishMaxMS = 1000

// isErroredFinish is the pure predicate behind erroredFinish (testable without
// a live audio device): the just-finished track carried an error (the player is
// Errored, or LastError is non-empty because internal/audio stored the source's
// download/decode error) AND almost no audio played.
//
// A still-Playing state is never an errored finish: that is a gapless/crossfade
// promote (the next track is already playing successfully). manage() copies the
// OUTGOING track's error into LastError even on a promote, so without this guard
// a promote following an early-errored track would be misclassified and its
// now-playing/queue bookkeeping skipped.
func isErroredFinish(state audio.State, lastErr string, positionMS int64) bool {
	if state == audio.Playing {
		return false
	}
	errored := state == audio.Errored || lastErr != ""
	return errored && positionMS < erroredFinishMaxMS
}

// erroredFinish reports whether the finish now firing is a CDN/decode failure
// rather than a natural end, reading the live player. False when no player.
func erroredFinish() bool {
	p := Player()
	if p == nil {
		return false
	}
	return isErroredFinish(p.State(), p.LastError(), p.PositionMS())
}

// noteTrackFinished records the track that just ended naturally. It runs from
// the player's onFinish callback (the manage goroutine): for a real finish the
// player retains the end position; for a gapless/crossfade promote the player
// is already Playing the NEXT track, so the outgoing one played its full
// duration.
func noteTrackFinished() {
	var state audio.State
	var lastErr string
	var posMS int64
	havePlayer := false
	if p := Player(); p != nil {
		state, lastErr, posMS, havePlayer = p.State(), p.LastError(), p.PositionMS(), true
	}
	prev, kind := currentTrackSnapshot()
	recordFinished(prev, kind, state, lastErr, posMS, havePlayer)
}

// recordFinished is noteTrackFinished's pure core (testable without a live audio
// device). It drops an errored finish (a CDN/decode failure with ~0 played is
// not a real listen) and otherwise records the outgoing listen using the
// track duration — or the player's actual end position when it retained one (a
// real finish, not a gapless promote where the player is already Playing next).
func recordFinished(prev deezer.Track, kind string, state audio.State, lastErr string, positionMS int64, havePlayer bool) {
	if havePlayer && isErroredFinish(state, lastErr, positionMS) {
		return
	}
	playedMS := prev.DurationMS
	if havePlayer && state != audio.Playing && positionMS > 0 {
		playedMS = positionMS
	}
	recordListen(prev, kind, playedMS)
}
