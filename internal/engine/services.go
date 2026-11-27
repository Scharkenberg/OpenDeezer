package engine

// Engine-hosted services shared by every host: Discord Rich Presence and the control
// API. next/prev/shuffle/repeat walk the engine queue (engineQ), which a host mirrors
// its queue into (QueueSet, QueueSetIndex).

import (
	"encoding/json"
	"fmt"
	"net"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/audio"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/bridge"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/config"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/control"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/discord"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/discovery"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/history"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/listens"
	pdlog "github.com/Scharkenberg/PurpleDisc/v4/internal/log"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/mediacache"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/preload"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/queue"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/version"
)

var (
	servicesOnce sync.Once
	dp           discord.Presence
	ctrlSrv      *control.Server
	coreVersion  = version.Number

	// ctrlCfg and ctrlSrvUserID record how ctrlSrv was built, so refreshControlServer can
	// rebuild it after an account switch. The UserID is compared (Init makes a new client
	// each call), so a same-account re-login is a no-op.
	ctrlCfg       control.Config
	ctrlSrvUserID string

	// advertiser is the LAN discovery responder for Connect (nil when the control server
	// is loopback-only or off). It is kept so it can be closed and re-pointed when the
	// server is rebuilt or torn down. Guarded by mu.
	advertiser *discovery.Responder

	// connectHostSwitch is set by a host with a Connect host switch
	// (SetConnectHostSwitch); connectHost is that switch. Both guarded by
	// ctrlMu.
	connectHostSwitch, connectHost bool

	// caches is the on-disk raw-stream cache attached to the player (Current is nil while
	// the budget is 0 and nothing is pinned). preparePlan prefers its plans, pins are
	// fetched into it, and an opening cache takes offline.json's pins.
	caches = &mediacache.Host{Attach: attachCache, Pinned: anyPinned}

	// preloads is the next-track preload: Preload records it and the near-end hook resolves
	// and arms it. Its armed track is the one a gapless or crossfade promote swaps in, so
	// engineAdvanceOnFinish can move now-playing even without a synced queue.
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
	// curKind is curTrack's media kind for history: "" for a song (setCurrentTrack),
	// history.KindEpisode for a podcast (setCurrentEpisode). Guarded by curMu with
	// curTrack.
	curKind string

	// engineQ is the engine-side playback queue: enginePlayPlaylist / enginePlayTrack load
	// it, and the player's onFinish advances it, so control-driven playlists play through.
	// Guarded by queueMu.
	queueMu sync.Mutex
	engineQ = queue.New()
)

// setCurrentTrack makes t the playing song and returns its generation for
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
	radioTopUp()
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

// notifyControlState nudges the control server's SSE loop to publish a snapshot at
// once. The loop also ticks every second; notifications are coalesced and never block.
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
	select {
	case episodeWake <- struct{}{}:
	default:
	}
}

// radioTopUp tops a queue played from Flow or a mix up as it runs low, off the
// caller: at each track change and each new queue. RadioFill runs one fetch at
// a time.
func radioTopUp() {
	if ctx := listenCtx.Load(); ctx != nil && deezer.IsRadio(ctx.Type) {
		go func() {
			queueMu.Lock()
			n, left := engineQ.Len(), engineQ.Len()-engineQ.Index()-1
			queueMu.Unlock()
			if n == 0 {
				return // the host has yet to set the list
			}
			more := radioMore(*ctx, left, func() []deezer.Track {
				ts, _, _ := QueueSnapshot()
				return ts
			})
			engineQueueAppend(more)
		}()
	}
}

var (
	// radioFill tops the engine queue up from Flow, a Flow mood or a mix.
	radioFill deezer.RadioFill
	// radioMore asks radioFill for the tracks to append; tests replace it.
	radioMore = func(ctx listens.Context, left int, queued func() []deezer.Track) []deezer.Track {
		c := netClient()
		if c == nil {
			return nil
		}
		return radioFill.More(c, ctx.Type, ctx.ID, left, queued)
	}
)

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

// currentTrackSnapshot returns the playing track with its media kind ("" = song,
// history.KindEpisode = podcast), read under one lock.
func currentTrackSnapshot() (deezer.Track, string) {
	curMu.Lock()
	defer curMu.Unlock()
	return curTrack, curKind
}

// getCtrlSrv reads the shared control server under mu (nil when disabled), so callers
// off the UI thread, such as onFinish, can publish without racing the writers.
func getCtrlSrv() *control.Server {
	mu.Lock()
	defer mu.Unlock()
	return ctrlSrv
}

// preparePlan resolves a stream plan for id, preferring a fully cached copy (CDNURL "",
// no network) over PrepareStream. Without a client the plan comes from the cache alone;
// a pinned track that fails online plays from its pin.
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

// SetHostAdvances(true) declares that the host advances its own queue on FinishedCount,
// also for a mirrored queue; otherwise the engine advances a mirrored queue on the
// finished track and FinishedCount does not bump. Call before Init.
func SetHostAdvances(on bool) { hostAdvances.Store(on) }

// onNaturalFinish is the player's onFinish callback. A track that ended Errored (a failed
// stream, a lost network) records the listen it gave and refreshes state: nothing advances
// and the counter stays, so Play goes on from it. Otherwise it records the listen, advances
// the engine queue (or bumps the host's counter), publishes the finished edge and a fresh
// snapshot.
func onNaturalFinish() {
	if p := Player(); p != nil && p.State() == audio.Errored {
		noteTrackFinished()
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

// tracksFromInsertJSON decodes QueueInsertNext's payload: one wire track or an array.
// Entries without an id are dropped; a bad payload yields an empty slice.
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

// QueueInsertNext inserts js's tracks (a wire track or an array) after the current
// track, drops a preloaded next, bumps the version and notifies. Local only: the
// host's Up-Next editor owns this queue.
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

// fetchTrackMeta fills in the current track's title, artist and album for Discord and
// remote status, unless another track was set since gen. A failure leaves the id only.
func fetchTrackMeta(c *deezer.Client, id string, gen uint64) {
	if c == nil || id == "" {
		return
	}
	if t, err := c.Track(id); err == nil && t.ID != "" {
		setCurrentTrackAt(gen, t)
	}
}

// engineEQ bridges the control API's /eq endpoint to the engine player.
func engineEQ() *control.EQ {
	return control.PlayerEQ(func() control.EQController {
		if p := Player(); p != nil {
			return p
		}
		return nil
	}, audio.EQPresetNames)
}

// startServices starts Discord RP + the control API once, after a successful
// login. The just-logged-in client is passed in; closures read globals lazily.
func startServices(c *deezer.Client) {
	ensureEpisodeLoop()
	servicesOnce.Do(func() {
		appID := config.LoadDiscordAppID()
		dp = discord.New(appID)
		if appID == "" {
			pdlog.Info("discord: rich presence disabled (no app id; set discord-app-id.txt)")
		} else {
			pdlog.Info("discord: rich presence enabled (app %s)", appID)
			go serviceTicker()
		}

		if cfg := config.LoadControl(); cfg.Enabled {
			if cfg.SameAccount && cfg.Token == "" {
				pdlog.Warn("control api: LAN-exposed with same-account auth only; the Deezer " +
					"user id is not a strong secret. Set PURPLEDISC_CONTROL_TOKEN for a real " +
					"credential on untrusted networks.")
			}
			ctrlMu.Lock()
			// This replaces any server the phone remote started before login,
			// so there is nothing for turning the remote off to restore.
			webRemotePrev = nil
			// A server configured onto the LAN is a Connect host.
			connectHost = !config.IsLoopbackAddr(cfg.Addr)
			if srv := rebuildControlServer(control.Config{Addr: cfg.Addr, Token: cfg.Token, SameAccountOnly: cfg.SameAccount}, c, cfg.Addr); srv != nil {
				pdlog.Info("control api on %s", srv.Addr())
			}
			ctrlMu.Unlock()
		}
	})
}

// refreshAdvertiser closes the LAN discovery responder and, when the control server is
// on a non-loopback address (and the Connect host switch is on), starts one for its
// port. Call it after each rebuild or switch change, holding ctrlMu, not mu.
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
		pdlog.Info("discovery advertising control port %d", p)
	} else {
		mu.Unlock()
		resp.Close()
	}
}

// ctrlMu serializes every control-server rebuild, since overlapping rebuilds could fail
// to bind or publish a stale config. mu guards the published pointers.
var ctrlMu sync.Mutex

// rebuildControlServer closes the control server, starts one for cfg around c on the
// first of addrs that binds (none: no server), publishes it and re-points the
// advertiser. Caller holds ctrlMu.
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
			pdlog.Warn("control api on %s: %v", a, err)
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

// refreshControlServer rebuilds the control server around the current client after a
// re-login with another account, dropping paired sessions. For the same account it
// swaps c into the running server; without a server it does nothing.
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

// discordWake is signaled by notifyControlState, so the presence loop wakes
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
	// Routed to a remote, the local player is stopped and would clear the presence, so
	// derive it from the remote snapshot.
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
	// Queue, repeat and shuffle come from the engine queue. A host that never syncs it
	// reports no queue, repeat "off" and shuffle false.
	queueMu.Lock()
	st.Repeat = engineQ.Repeat().String()
	st.Shuffle = shuffleOn
	st.QueueVersion = engineQ.Version()
	if i := engineQ.Index(); i >= 0 {
		st.QueueIndex = &i
	}
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

// queueTracksFromJSON decodes a host queue: a JSON array of wire tracks (bridge.Track),
// "id" required, "durationMs" recommended. Entries without an id are dropped; "" and
// "null" give an empty queue.
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
		AlbumName: w.AlbumName, AlbumID: w.AlbumID, ArtworkURL: w.ArtworkURL, Explicit: w.Explicit,
		Unavailable: w.Unavailable,
	}
	for _, a := range w.Artists {
		t.Artists = append(t.Artists, deezer.Artist{ID: a.ID, Name: a.Name})
	}
	if len(t.Artists) == 0 && (w.ArtistID != "" || w.ArtistLine != "") {
		t.Artists = []deezer.Artist{{ID: w.ArtistID, Name: w.ArtistLine}}
	}
	return t
}

// QueueSet replaces the engine queue with the host's (a JSON array of wire tracks,
// "[]" clears), resets the cursor to 0 and bumps the version. A parse error leaves the
// queue as it was.
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
	radioTopUp()
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

// engineQueueAppend adds ts to the end of the engine queue, keeping the cursor; the
// version bumps once when anything was added. A preload is dropped only when the
// track after the cursor changed.
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

// QueueSetIndex aligns the engine queue cursor to the host's playing row (clamped)
// with AlignIndex, which records no history, so Prev never jumps to an unplayed track.
// Remote jumps use engineQueueSelect (SetIndex).
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

// SetRepeat sets the engine queue's repeat mode (0 off, 1 all, 2 one). It is never
// forwarded to a Connect device, which gets one track at a time and would loop it.
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

// Repeat returns the engine queue's repeat mode ("off", "all" or "one"), also while
// routed: repeat is kept here and the device is set to "off".
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

// SetShuffleHostOwned(true) declares that the host owns play order: shuffle changes
// only record the flag (for /status, Shuffle, notifications) and never reorder the
// engine queue. Call before Init.
func SetShuffleHostOwned(on bool) {
	queueMu.Lock()
	shuffleHostOwned = on
	if shuffleHostOwned {
		engineQ.SetShuffle(false)
	}
	queueMu.Unlock()
}

// The shuffle flag as reported. With shuffleHostOwned it is recorded but never applied
// to engineQ. Both guarded by queueMu.
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
	deviceLabel = "Purple Disc (" + platformName(runtime.GOOS) + ")"
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

// engineCommands maps control commands to player actions. next/prev drive engineQ
// locally or forward to the selected device; shuffle is recorded on engineQ and
// forwarded too.
func engineCommands() control.Commands {
	return control.Commands{
		PlayPause: enginePlayPause,
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
		// CycleRepeat / SetRepeat: record the mode locally only. A Repeat-All forwarded to the
		// host would loop its single-item queue; the engine's auto-advance applies repeat.
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
		// SetQueue lets a controller replace the engine queue (cursor at index): QueueSet and
		// QueueSetIndex in one step, clearing a stale preload and notifying. A parse error is
		// returned.
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

// fetchEpisodeMeta adds the current episode's title, podcast name (its artist line)
// and artwork via REST /episode/{id}, unless another track was set since gen. A failure
// leaves the episode as it is.
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

// startPlay starts plan on p unless a newer play claimed a seq meanwhile, runs started
// in the same step and announces the start (announceLocal). A cached plan whose entry
// is gone is planned again first, outside that step.
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
	listenStarted()
	announceLocal()
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
	SetListenContext("track_page", id)
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
	// A single-track "queue": loading it keeps the engine queue in step with what plays,
	// so an earlier playlist cannot auto-advance after this track.
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
	SetListenContext("playlist_page", id)
	_ = engineLoadAndPlay(p, ts, 0, seq)
}

// engineQueueAdvance advances the engine queue when finishedID ends naturally and
// returns the next track, only when finishedID is the queue's current track. The
// caller resolves and plays it.
func engineQueueAdvance(finishedID string) (deezer.Track, bool) {
	queueMu.Lock()
	defer queueMu.Unlock()
	cur, ok := engineQ.Current()
	if !ok || cur.ID == "" || cur.ID != finishedID {
		return deezer.Track{}, false
	}
	if !stepPlayable(engineQ.AdvanceAuto) {
		return deezer.Track{}, false
	}
	return engineQ.Current()
}

// stepPlayable moves the cursor with move, and on past tracks that cannot play without
// asking for them or counting them as failures. False, with the cursor where it was, when
// nothing in the queue plays or move stops first. Called with queueMu held.
func stepPlayable(move func() bool) bool {
	if !slices.ContainsFunc(engineQ.Tracks(), func(t deezer.Track) bool { return !t.Unavailable }) {
		return false
	}
	at := engineQ.Mark()
	// A shuffled cycle holds every row, so two cycles reach a playable one.
	for range 2 * engineQ.Len() {
		if !move() {
			break
		}
		if t, ok := engineQ.Current(); ok && !t.Unavailable {
			return true
		}
	}
	engineQ.Restore(at)
	return false
}

// engineAdvanceOnFinish runs from onFinish on manage()'s goroutine and starts the next
// track on a new goroutine so the manager never blocks. It returns true when the engine
// queue owned the finished track, so the host counter is not bumped too.
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

// engineAutoAdvance plays next as seq. An unresolvable track is skipped like Next;
// after maxAdvanceFailures in a row, or at the queue's end, the player reports the
// reason. A failed start (output device) skips nothing.
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
		pdlog.Warn("auto-advance: %s: %v", next.ID, err)
		queueMu.Lock()
		cur, ok := engineQ.Current()
		ok = ok && cur.ID == next.ID // else the queue moved on meanwhile
		// A lost network is not an unplayable track: the cursor stays on the one that could not start.
		ended := ok && (failures == maxAdvanceFailures || deezer.IsNoNetwork(err) || !stepPlayable(engineQ.Next))
		if ok && !ended {
			next, ok = engineQ.Current()
		}
		queueMu.Unlock()
		if ended {
			// One step with the check, as in startPlay: a play started meanwhile stays as it is.
			plays.startMu.Lock()
			if !plays.superseded(seq) {
				withPlayer(func(p *audio.Player) { p.FailErr(err) })
			}
			plays.startMu.Unlock()
			return
		}
		if !ok {
			return
		}
		notifyControlState()
	}
}

// engineSyncOnGaplessPromote updates the queue and now-playing after a gapless promote,
// walking the cursor along with the audio instead of replaying. It returns whether the
// engine queue owned the finished track.
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
		// Engine queue empty or misaligned: the host preloaded without QueueSet. The promoted
		// stream is the armed preload, so move now-playing onto it; noteTrackFinished already
		// recorded the outgoing listen.
		if armed {
			gen := setCurrentTrack(promoted)
			listenStarted()
			announceLocal()
			if promoted.Name == "" { // stashed from a cache-sourced plan
				go fetchTrackMeta(netClient(), promoted.ID, gen)
			}
		}
		return false // still not the engine queue's finish; the host's own queue advances
	}
	setCurrentTrack(next)
	listenStarted()
	announceLocal()
	return true
}

// engineNext / enginePrev back the control API's /next + /prev. When a remote
// device is selected they forward to it (local playback is stopped); otherwise
// they move the engine queue and play the new current track locally.
func engineNext() { engineStep(engineQ.Next, (*control.Client).Next) }

// enginePlayPause flips playing and paused; a stopped or errored player starts the queue's
// current row instead (a host's restored queue, a stop), as the hosts' own Play does.
func enginePlayPause() {
	if p := Player(); p != nil && routedRemote() == nil && (p.State() == audio.Stopped || p.State() == audio.Errored) {
		queueMu.Lock()
		t, ok := engineQ.Current()
		queueMu.Unlock()
		if ok && !t.Unavailable {
			seq, done := plays.beginUser()
			defer done()
			_ = enginePlayResolved(p, t, seq)
			return
		}
	}
	withPlayerNotify(func(p *audio.Player) { p.TogglePause() })
}

// prevRestartMS is the position from which Previous restarts the track, as Deezer's apps do.
const prevRestartMS = 5000

// enginePrev restarts a local track played for prevRestartMS or more, else steps back.
// While a user play resolves, the position is the outgoing track's: it steps back.
func enginePrev() {
	if routedRemote() == nil && plays.user.Load() == 0 {
		if p := Player(); p != nil && p.PositionMS() >= prevRestartMS {
			p.SeekMS(0)
			return
		}
	}
	engineStep(engineQ.Prev, (*control.Client).Prev)
}

// Next and Previous step the engine queue (shuffle and repeat applied) and play the new
// track, or drive the Connect device; Previous restarts a track played 5 s or more.
func Next()     { engineNext() }
func Previous() { enginePrev() }

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
	moved := stepPlayable(move)
	t, ok := engineQ.Current()
	queueMu.Unlock()
	if p := Player(); moved && ok && p != nil {
		_ = enginePlayResolved(p, t, seq)
	}
}

// ---- extended control commands: queue edits, album/mix play, history ----
// They back /queue/*, /play/album, /play/mix/* and /history/recent and forward to the
// routed Connect device, except HistoryRecent, which stays local.

// engineQueueInsert inserts t after the current track (next=true) or appends it,
// discarding a preloaded next stream.
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

// engineQueueSelect validates index, moves the cursor (recording history for Prev) and
// returns the new current track.
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

// QueueRemove removes the row at index (POST /queue/remove), forwarding to a routed
// device. The playing row cannot be removed.
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

// QueueMove moves the row at from to to (POST /queue/move), forwarding to a routed
// device. Cursor, history and shuffle cycle follow the moved track (queue.Move);
// from == to succeeds as a no-op.
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
	SetListenContext("album_page", id)
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
	SetListenContext("track_mix_contextual", id)
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
	SetListenContext("artist_smartradio", id)
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

// HistoryRecent returns the newest n local history entries (n <= 0: all), filling in
// missing song artwork and duration (fillHistoryMeta). Kind "episode" marks a podcast
// episode, replayed with PlayEpisode.
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
	artistPics  = map[string]string{} // artist id -> picture URL, also under trackMetaMu
)

// artistPictures returns the pictures of the artists of stats, found through each one's
// latest track: the track's artist of that name (else its main artist), then that
// artist's picture, each looked up once per process.
func artistPictures(c *deezer.Client, stats []history.ArtistStat) []string {
	meta := make([]history.Entry, len(stats))
	for i, a := range stats {
		meta[i] = history.Entry{TrackID: a.TrackID}
	}
	fillHistoryMeta(c, meta)
	ids := make([]string, len(stats))
	need := map[string]bool{}
	trackMetaMu.Lock()
	for i, a := range stats {
		artists := trackMeta[a.TrackID].Artists
		for _, ar := range artists {
			if ar.Name == a.Artist {
				ids[i] = ar.ID
			}
		}
		if ids[i] == "" && len(artists) > 0 {
			ids[i] = artists[0].ID
		}
		if _, ok := artistPics[ids[i]]; ids[i] != "" && !ok {
			need[ids[i]] = true
		}
	}
	trackMetaMu.Unlock()
	if c != nil {
		var wg sync.WaitGroup
		slots := make(chan struct{}, maxMetaLookups)
		for id := range need {
			wg.Go(func() {
				slots <- struct{}{}
				pic, err := c.ArtistPicture(id)
				<-slots
				if err == nil || !deezer.IsNoNetwork(err) {
					trackMetaMu.Lock()
					artistPics[id] = pic
					trackMetaMu.Unlock()
				}
			})
		}
		wg.Wait()
	}
	trackMetaMu.Lock()
	defer trackMetaMu.Unlock()
	pics := make([]string, len(stats))
	for i, id := range ids {
		pics[i] = artistPics[id]
	}
	return pics
}

// maxMetaLookups bounds the concurrent track lookups of fillHistoryMeta.
const maxMetaLookups = 4

// fillHistoryMeta fills missing artwork and duration of song entries from the track,
// looking each id up once per process, at most maxMetaLookups at a time. A failed
// lookup is retried only after a network failure. Episodes are left as they are.
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
					trackMeta[id] = deezer.Track{ArtworkURL: t.ArtworkURL, DurationMS: t.DurationMS, Artists: t.Artists}
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
	TrackID    string `json:"trackId"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Plays      int    `json:"plays"`
	TotalSec   int64  `json:"totalSec"`
	ArtworkURL string `json:"artworkUrl,omitempty"`
	DurationMS int64  `json:"durationMs,omitempty"`
}

type jArtistStat struct {
	Artist     string `json:"artist"`
	Plays      int    `json:"plays"`
	TotalSec   int64  `json:"totalSec"`
	PictureURL string `json:"pictureUrl,omitempty"`
}

// HistoryStats computes the local stats over the last sinceDays (<= 0: all) as
// {topTracks, topArtists, totalSeconds}. The tops are music only; the full shape with
// empty arrays comes back when there is no history.
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
			// Their artwork and length, as the recent plays get them.
			meta := make([]history.Entry, len(tt))
			for i, t := range tt {
				meta[i] = history.Entry{TrackID: t.TrackID}
			}
			fillHistoryMeta(netClient(), meta)
			topTracks = make([]jTrackStat, len(tt))
			for i, t := range tt {
				topTracks[i] = jTrackStat{TrackID: t.TrackID, Title: t.Title, Artist: t.Artist, Plays: t.Plays, TotalSec: t.TotalSec,
					ArtworkURL: meta[i].ArtworkURL, DurationMS: meta[i].DurationMS}
			}
		}
		if ta, err := st.TopArtists(since, 50); err == nil {
			pics := artistPictures(netClient(), ta)
			topArtists = make([]jArtistStat, len(ta))
			for i, a := range ta {
				topArtists[i] = jArtistStat{Artist: a.Artist, Plays: a.Plays, TotalSec: a.TotalSec, PictureURL: pics[i]}
			}
		}
		total, _ = st.TotalListenedSec(since)
	}
	return map[string]any{"topTracks": topTracks, "topArtists": topArtists, "totalSeconds": total}
}

// listenEntry converts an outgoing track, its media kind and the milliseconds listened
// into a history entry; ok is false without a track id or under one second listened.
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

// recordListen appends the outgoing track with its media kind to the local history.
// The write (append and fsync) runs on its own goroutine so the player's paths never
// wait on disk.
func recordListen(prev deezer.Track, kind string, playedMS int64) {
	e, ok := listenEntry(prev, kind, playedMS)
	if !ok {
		return
	}
	go func() {
		if st := historyStore(); st != nil {
			if err := st.Record(e); err != nil {
				pdlog.Debug("history: %v", err)
			}
		}
	}()
}

// noteTrackTransition records the outgoing track when a new stream replaces one being
// listened to; call it before p.Play. A Stopped or Errored player records nothing
// (already recorded or long stopped).
func noteTrackTransition(p *audio.Player) {
	if p == nil {
		return
	}
	recordTransition(p.State(), p.PositionMS())
}

// recordTransition is noteTrackTransition's pure core, for tests without an audio
// device. A Stopped or Errored state records nothing, so a second Stop or a Play after
// a natural finish adds no duplicate.
func recordTransition(state audio.State, positionMS int64) {
	switch state {
	case audio.Playing, audio.Paused:
		t, kind := currentTrackSnapshot()
		recordListen(t, kind, positionMS)
	}
}

// noteTrackFinished records the track that ended naturally, from onFinish on the manage
// goroutine. After a real finish the player holds the end position; after a promote the
// outgoing track played its full duration.
func noteTrackFinished() {
	var state audio.State
	var posMS int64
	havePlayer := false
	if p := Player(); p != nil {
		state, posMS, havePlayer = p.State(), p.PositionMS(), true
	}
	prev, kind := currentTrackSnapshot()
	recordFinished(prev, kind, state, posMS, havePlayer)
}

// recordFinished is noteTrackFinished's pure core. It records the listen with the
// player's end position when it has one, else the track duration; a track that ended
// Errored before any audio records nothing.
func recordFinished(prev deezer.Track, kind string, state audio.State, positionMS int64, havePlayer bool) {
	if havePlayer && state == audio.Errored && positionMS <= 0 {
		return
	}
	playedMS := prev.DurationMS
	if havePlayer && state != audio.Playing && positionMS > 0 {
		playedMS = positionMS
	}
	recordListen(prev, kind, playedMS)
}
