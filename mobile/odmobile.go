// Package odmobile is the OpenDeezer engine (internal/engine) exposed for
// gomobile (gobind), so a native Android (or iOS) app can drive the same
// login/decrypt/decode/playback pipeline the desktop GUIs use. Build it with:
//
//	gomobile bind -target=android -androidapi 24 -o gui/android/app/libs/odmobile.aar ./mobile
//
// Every browse/list call returns a JSON string (the wire shape the GUIs already
// use); mutations return bool/string. The app advances its own queue on
// FinishedCount. This package only converts arguments and results.
package odmobile

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/bridge"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/engine"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/update"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/version"
)

// Version is the engine/app version (single source: internal/version).
const Version = version.Number

// The app advances its own queue on FinishedCount and has a Connect host
// switch (ConnectHostSetEnabled).
func init() {
	engine.SetHostAdvances(true)
	engine.SetConnectHostSwitch(true)
}

var errNotLoggedIn = fmt.Errorf("not logged in")

func jstr(v any, err error) string {
	if err != nil {
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		return string(b)
	}
	b, e := json.Marshal(v)
	if e != nil {
		eb, _ := json.Marshal(map[string]string{"error": e.Error()})
		return string(eb)
	}
	return string(b)
}

func ok(err error) bool { return err == nil }

// ---- lifecycle ----

// SetDataDir points the engine's config at dir — an app-private directory the
// host guarantees is writable and persists across reboots (on Android:
// context.getFilesDir(); on iOS: Application Support). os.UserConfigDir is not
// such a path on Android, so the host calls this once at startup, before
// Init — otherwise media.json (the stream-cache setting) is written somewhere
// that isn't there next launch.
func SetDataDir(dir string) {
	if dir != "" {
		config.SetBaseDir(filepath.Join(dir, "opendeezer"))
	}
}

// SetCacheDir points the engine's stream temp files and default media cache
// at dir/opendeezer — the app's cache directory (on Android:
// context.getCacheDir()). os.UserCacheDir is not such a path on Android, so
// the host calls this once at startup, before Init; on iOS the default
// (Library/Caches) is already the app's.
func SetCacheDir(dir string) {
	if dir != "" {
		config.SetCacheBaseDir(filepath.Join(dir, "opendeezer"))
	}
}

// Init logs in with the ARL and starts the engine. Returns true on success.
func Init(arl string) bool { return engine.Init(arl) }

// LoginErrorKind returns why the most recent Init failed so the UI shows a
// No-Internet retry screen instead of forcing re-auth: 0 = ok, 1 = ARL expired
// or invalid, 2 = no internet, 3 = other, 4 = no audio output device.
func LoginErrorKind() int { return engine.LoginErrorKind() }

// LoggedIn reports whether Init succeeded.
func LoggedIn() bool { c := engine.Client(); return c != nil && c.LoggedIn() }

// Logout tears the engine session down: stops playback, closes the control
// server (dropping web-remote pairing sessions and the old account's
// same-account auth), stops Connect-host advertising, and forgets the Deezer
// client. A later Init starts services fresh for the new account.
func Logout() { engine.Logout() }

// CheckUpdate checks GitHub for a newer release; returns JSON
// {current, latest, hasUpdate, url, notes}.
func CheckUpdate() string {
	info, _ := update.Check(Version)
	return jstr(info, nil)
}

// ---- account / settings ----

func Account() string {
	c := engine.Client()
	if c == nil {
		return jstr(nil, errNotLoggedIn)
	}
	return jstr(c.Account(), nil)
}
func UserID() string {
	if c := engine.Client(); c != nil {
		return c.UserID()
	}
	return ""
}

// SetQuality sets and saves the stream quality (0 MP3 128, 1 MP3 320, 2 FLAC);
// the engine keeps it with the other playback settings in playback.json and
// applies them at each Init. The other setters below save theirs alike.
func SetQuality(level int)  { engine.SetQuality(level) }
func Quality() int          { return engine.Quality() }
func SetReplayGain(on bool) { engine.SetReplayGain(on) }
func ReplayGain() bool      { return engine.ReplayGain() }
func SetGapless(on bool)    { engine.SetGapless(on) }
func Gapless() bool         { return engine.Gapless() }
func SetCrossfadeMS(ms int) { engine.SetCrossfadeMS(ms) }
func CrossfadeMS() int      { return engine.CrossfadeMS() }

// SetSleepTimer arms the sleep timer: pause after `minutes` (with a fade-out), or
// when the current track ends if endOfTrack != 0 (minutes ignored). minutes <= 0
// with endOfTrack == 0 cancels it.
func SetSleepTimer(minutes int, endOfTrack int) { engine.SetSleepTimer(minutes, endOfTrack != 0) }

// CancelSleepTimer disarms the sleep timer.
func CancelSleepTimer() { engine.CancelSleepTimer() }

// SleepActive reports whether a sleep timer is armed (1) or not (0).
func SleepActive() int {
	if p := engine.Player(); p != nil && p.SleepActive() {
		return 1
	}
	return 0
}

// SleepEndOfTrack reports whether the armed timer is end-of-track mode (1/0).
func SleepEndOfTrack() int {
	if p := engine.Player(); p != nil && p.SleepEndOfTrack() {
		return 1
	}
	return 0
}

// SleepRemainingMS returns milliseconds until the timer fires (0 if none).
func SleepRemainingMS() int64 {
	if p := engine.Player(); p != nil {
		return p.SleepRemainingMS()
	}
	return 0
}

// Preload asks for trackID to follow the current track gaplessly or with a
// crossfade, with the length the synced queue has for it. It only records the
// request: the engine resolves and downloads it once the current track has
// max(30 s, crossfade + 20 s) left (at once when it is shorter or already
// there). A new request replaces the recorded one; a request for the track
// already recorded or preloaded changes nothing. No-op when routed to a
// Connect device.
func Preload(id string) error {
	if engine.ConnectedDevice() == "" && engine.Player() == nil {
		return fmt.Errorf("engine not ready")
	}
	engine.Preload(id, 0)
	return nil
}

// ClearPreload discards a preloaded next track. Call when the upcoming track is
// no longer determined (shuffle/repeat toggled, queue edited after a preload
// was armed) so a stale preload can't be gaplessly swapped in.
func ClearPreload() { engine.ClearPreload() }

// ---- browse ----

// withClient runs fn with the client for a request, or answers why there is
// none (offline mode, no login).
func withClient(fn func(c *deezer.Client) (any, error)) string {
	c, err := engine.Online()
	if err != nil {
		return jstr(nil, err)
	}
	return jstr(fn(c))
}

func Favorites() string {
	return withClient(func(c *deezer.Client) (any, error) {
		ts, err := c.Favorites()
		return map[string]any{"tracks": bridge.FromTracks(ts)}, err
	})
}

// FavoriteIDsJSON returns the account's liked (favorite) track ids as a JSON
// array of strings, e.g. ["123","456"], so the app can render truthful "liked"
// hearts across lists without a per-track lookup. Returns "[]" when not logged
// in, in offline mode or on a fetch error.
func FavoriteIDsJSON() string {
	c, err := engine.Online()
	if err != nil {
		return "[]"
	}
	ts, err := c.Favorites()
	if err != nil {
		return "[]"
	}
	ids := make([]string, 0, len(ts))
	for _, t := range ts {
		if t.ID != "" {
			ids = append(ids, t.ID)
		}
	}
	return jstr(ids, nil)
}
func Playlists() string {
	return withClient(func(c *deezer.Client) (any, error) {
		ps, err := c.Playlists()
		return map[string]any{"playlists": bridge.FromPlaylists(ps)}, err
	})
}

// PlaylistTracks lists a playlist's tracks; a pinned playlist is kept in
// step with the list.
func PlaylistTracks(id string) string {
	ts, err := engine.PlaylistTracks(id)
	return jstr(map[string]any{"tracks": bridge.FromTracks(ts)}, err)
}

// AlbumTracks lists an album's tracks; a pinned album is kept in step with
// the list.
func AlbumTracks(id string) string {
	ts, err := engine.AlbumTracks(id)
	return jstr(map[string]any{"tracks": bridge.FromTracks(ts)}, err)
}
func Flow() string {
	return withClient(func(c *deezer.Client) (any, error) {
		ts, err := c.Flow()
		return map[string]any{"tracks": bridge.FromTracks(ts)}, err
	})
}

// TrackMixJSON returns a "song radio" mix seeded from a track (the seed kept as
// the first entry): {tracks:[...]} in the shared wire shape, mirroring Flow.
func TrackMixJSON(id string) string {
	return withClient(func(c *deezer.Client) (any, error) {
		ts, err := c.TrackMix(id)
		return map[string]any{"tracks": bridge.FromTracks(ts)}, err
	})
}

// ArtistMixJSON returns an "artist radio" mix seeded from an artist:
// {tracks:[...]} in the shared wire shape, mirroring Flow.
func ArtistMixJSON(id string) string {
	return withClient(func(c *deezer.Client) (any, error) {
		ts, err := c.ArtistMix(id)
		return map[string]any{"tracks": bridge.FromTracks(ts)}, err
	})
}
func ArtistTop(id string) string {
	return withClient(func(c *deezer.Client) (any, error) {
		ts, err := c.ArtistTop(id)
		return map[string]any{"tracks": bridge.FromTracks(ts)}, err
	})
}
func ArtistProfile(id string) string {
	return withClient(func(c *deezer.Client) (any, error) { return c.ArtistProfile(id) })
}
func Lyrics(id string) string {
	return withClient(func(c *deezer.Client) (any, error) { return c.Lyrics(id) })
}

func Search(q string) string {
	return withClient(func(c *deezer.Client) (any, error) {
		r, err := c.Search(q)
		if err != nil {
			return nil, err
		}
		return searchResult(r.Tracks, r.Albums, r.Artists, r.Playlists), nil
	})
}
func Charts() string {
	return withClient(func(c *deezer.Client) (any, error) {
		ch, err := c.Charts("0")
		if err != nil {
			return nil, err
		}
		return searchResult(ch.Tracks, ch.Albums, ch.Artists, ch.Playlists), nil
	})
}

// Home aggregates the Home-screen sections (charts top tracks/albums + the
// user's playlists) in one call, mirroring corelib DZHomeJSON. Best-effort.
func Home() string {
	return withClient(func(c *deezer.Client) (any, error) {
		var tracks []deezer.Track
		var albums []deezer.Album
		if ch, err := c.Charts("0"); err == nil && ch != nil {
			tracks, albums = ch.Tracks, ch.Albums
		}
		ps, _ := c.Playlists()
		return map[string]any{
			"topTracks": bridge.FromTracks(tracks),
			"topAlbums": bridge.FromAlbums(albums),
			"playlists": bridge.FromPlaylists(ps),
		}, nil
	})
}

func searchResult(tracks []deezer.Track, albums []deezer.Album, artists []deezer.ArtistInfo, playlists []deezer.Playlist) map[string]any {
	return map[string]any{
		"tracks": bridge.FromTracks(tracks), "albums": bridge.FromAlbums(albums),
		"artists": bridge.FromArtistInfos(artists), "playlists": bridge.FromPlaylists(playlists),
	}
}

// ---- podcasts ----

func SearchPodcasts(q string) string {
	return withClient(func(c *deezer.Client) (any, error) {
		ps, err := c.SearchPodcasts(q)
		return map[string]any{"podcasts": bridge.FromPodcasts(ps)}, err
	})
}
func PodcastEpisodes(id string) string {
	return withClient(func(c *deezer.Client) (any, error) {
		es, err := c.PodcastEpisodes(id)
		return map[string]any{"episodes": bridge.FromEpisodes(es)}, err
	})
}

// PlayEpisode resolves + plays a podcast episode (plain stream) with an unknown
// duration. Prefer PlayEpisodeMS when the caller knows the duration. Returns
// as Play does.
func PlayEpisode(id string) int { return PlayEpisodeMS(id, 0) }

// PlayEpisodeMS resolves + plays a podcast episode (plain stream). Sets the
// episode as the current track immediately (id + duration, the player never
// derives duration from the stream), then asynchronously enriches title /
// podcast name / artwork via REST /episode. Returns as Play does.
func PlayEpisodeMS(id string, durationMS int64) int { return int(engine.PlayEpisode(id, durationMS)) }

// ---- library writes ----

// clientOp runs fn with the client for a request: false on its failure, in
// offline mode (refused, not queued) and before login.
func clientOp(fn func(c *deezer.Client) error) bool {
	c, err := engine.Online()
	return err == nil && ok(fn(c))
}

func AddFavorite(id string) bool {
	return clientOp(func(c *deezer.Client) error { return c.AddFavoriteTrack(id) })
}
func RemoveFavorite(id string) bool {
	return clientOp(func(c *deezer.Client) error { return c.RemoveFavoriteTrack(id) })
}
func AddToPlaylist(playlistID, trackID string) bool {
	return clientOp(func(c *deezer.Client) error { return c.AddToPlaylist(playlistID, trackID) })
}
func RemoveFromPlaylist(playlistID, trackID string) bool {
	return clientOp(func(c *deezer.Client) error { return c.RemoveFromPlaylist(playlistID, trackID) })
}
func CreatePlaylist(title string) string {
	return withClient(func(c *deezer.Client) (any, error) {
		id, err := c.CreatePlaylist(title, nil)
		return map[string]string{"id": id}, err
	})
}
func RenamePlaylist(id, title string) bool {
	return clientOp(func(c *deezer.Client) error { return c.RenamePlaylist(id, title) })
}
func DeletePlaylist(id string) bool {
	return clientOp(func(c *deezer.Client) error { return c.DeletePlaylist(id) })
}

// ---- playback ----

// Play resolves + plays a track. When routed to a Connect device, it plays there.
// 1 when it started, 0 on failure, 2 when a newer play (the app's or a
// remote's) took the player before this one started (not a failure).
func Play(trackID string, durationMS int64) int { return int(engine.Play(trackID, durationMS)) }

// ---- downloads (premium-only, full track to disk) ----

// DownloadTrack downloads trackID to a file in destDir and returns JSON
// {"path":"..."} on success or {"error":"..."} on failure. Pass "" for destDir
// to use the shared default folder (DownloadDir). Blocking — call it off the UI
// thread. Downloads are premium-only; a free account gets an error.
func DownloadTrack(trackID, destDir string) string {
	path, err := engine.DownloadTrack(trackID, destDir)
	if err != nil {
		return jstr(nil, err)
	}
	return jstr(map[string]string{"path": path}, nil)
}

// DownloadAlbum downloads every track of albumID to the shared download folder
// (DownloadDir), skipping files already there, and returns the batch summary
// {"saved":N,"failed":N,"dir":"...","error":""}. Blocking and premium-only, the
// same gate + folder DownloadTrack uses — call it off the UI thread. On a
// partial download "failed" counts the per-track failures and "error" carries
// the batch message; a free account yields saved:0,failed:0 with the premium
// error.
func DownloadAlbum(id string) string { return jstr(engine.DownloadAlbum(id)) }

// DownloadPlaylist downloads every track of playlistID to the shared download
// folder and returns the same batch summary as DownloadAlbum.
func DownloadPlaylist(id string) string { return jstr(engine.DownloadPlaylist(id)) }

// DownloadDir returns the current download folder (env/config/default).
func DownloadDir() string { return config.LoadDownloadDir() }

// SetDownloadDir persists the download folder ("" resets to the default).
func SetDownloadDir(path string) bool { return ok(config.SaveDownloadDir(path)) }

// MediaCacheMB returns the on-disk raw-stream cache budget in megabytes
// (media.json; 0 = cache disabled, the default).
func MediaCacheMB() int { return config.LoadMedia().MediaCacheMB }

// SetMediaCacheMB persists the raw-stream cache budget in megabytes and
// applies it at once: a lower budget evicts the least recent tracks, 0 (or a
// negative value) removes every cached track not pinned, also from a folder
// not opened in this session, and stops caching. Pinned tracks are outside
// the budget.
func SetMediaCacheMB(mb int) bool { return ok(engine.Caches().SetMB(mb)) }

// MediaCacheMaxAgeDays returns how many days a cached track may go unplayed
// before it is removed (media.json; 0 = kept at any age, the default).
func MediaCacheMaxAgeDays() int { return config.LoadMedia().MediaCacheMaxAgeDays }

// SetMediaCacheMaxAgeDays persists that age (0 or a negative value keeps
// tracks at any age) and applies it at once: cached tracks not played or
// downloaded for longer are removed now, and then at most once an hour while
// tracks play.
func SetMediaCacheMaxAgeDays(days int) bool { return ok(engine.Caches().SetMaxAgeDays(days)) }

// MediaCacheStatsJSON returns the media cache's state:
// {"entries":int,"bytes":int64,"pinnedBytes":int64,"maxBytes":int64,"dir":string}.
// bytes counts every stored track, pinnedBytes those kept for offline use;
// maxBytes is the budget (0 = off); dir is the folder
// picked (the cache lives in its subfolder opendeezer-cache), also while off.
func MediaCacheStatsJSON() string { return jstr(engine.Caches().Stats(), nil) }

// ClearMediaCache removes every cached track not pinned; includePinned also
// removes the tracks kept for offline use, the pins of every account with
// them. Returns 1 on success, 0 on failure.
func ClearMediaCache(includePinned int) int {
	if engine.ClearCache(includePinned != 0) != nil {
		return 0
	}
	return 1
}

// MediaCacheDir returns the folder picked for the media cache, by default
// the app's cache directory (SetCacheDir) + /opendeezer ("" when that is
// unknown); the cache lives in its subfolder opendeezer-cache.
func MediaCacheDir() string {
	dir, _ := config.LoadMedia().CacheDir()
	return dir
}

// SetMediaCacheDir moves the media cache to the subfolder opendeezer-cache of
// dir, an absolute path ("" = the default folder), and returns {"error":""}
// or {"error":"reason"}. The pinned tracks are copied there, then the old
// cache is cleared; offline fetches stop for the move and go on in the new
// folder. A relative path or a file is refused; on any error (a copy too) the
// old folder stays in use.
func SetMediaCacheDir(dir string) string { return errorReply(engine.SetMediaCacheDir(dir)) }

// IsPreview reports whether the current track is Deezer's 30-second preview (the
// free-account fallback) rather than the full stream.
func IsPreview() bool { return engine.IsPreview() }

// SetAdsDisabled turns Deezer Free's play-reporting/ads off/on and persists it.
// FREE accounts only: reporting plays (log.listen) credits artists and drives the
// ad schedule; disabling it is ad-free but stops reporting — the user's
// at-own-risk choice. Paid accounts are unaffected.
func SetAdsDisabled(disabled bool) bool {
	if c := engine.Client(); c != nil {
		c.SetAdsDisabled(disabled)
	}
	return ok(config.SaveAdsDisabled(disabled))
}

// AdsDisabled reports whether the free-tier ads/play-reporting opt-out is on.
func AdsDisabled() bool {
	if c := engine.Client(); c != nil {
		return c.AdsDisabled()
	}
	return config.LoadAdsDisabled()
}

func Pause()       { engine.Pause() }
func Resume()      { engine.Resume() }
func TogglePause() { engine.TogglePause() }
func Stop()        { engine.Stop() }

// SetOutputSuspended suspends (true) or resumes (false) the local OS audio
// device without touching playback state — for audio-focus/route handling on
// mobile. Local-only: it never routes to a Connect device.
func SetOutputSuspended(on bool) {
	if p := engine.Player(); p != nil {
		_ = p.SetOutputSuspended(on)
	}
}
func Seek(ms int64)       { engine.Seek(ms) }
func SetVolume(v float64) { engine.SetVolume(v) }
func Volume() float64     { return engine.Volume() }
func State() int          { return engine.State() }
func PositionMS() int64   { return engine.PositionMS() }
func DurationMS() int64   { return engine.DurationMS() }
func Format() string      { return engine.Format() }
func FinishedCount() int  { return engine.FinishedCount() }

// NowPlaying returns the track actually playing (remote when routed, else
// local), "{}" when nothing plays.
func NowPlaying() string { return engine.NowPlayingJSON() }

// NowPlayingVersion returns a counter that moves exactly when NowPlaying's
// result changes: poll it and fetch NowPlaying only then.
func NowPlayingVersion() int64 { return engine.NowPlayingVersion() }

// ---- engine queue sync (app queue -> engine) ----

// SetQueueJSON replaces the engine-side playback queue with the app's queue so
// remote controllers see it on /status and /next + /prev walk it. js is a JSON
// array of tracks in the same wire shape every list call returns
// ({id,name,durationMs,artistLine,artistId,artists,albumName,artworkUrl,
// explicit}); only id is required, but durationMs should be set so controllers'
// end-of-track detection works. The cursor resets to 0 — follow with
// SetQueueIndex to point it at the playing row. Pass "[]" to clear. Returns an
// error on a parse failure.
func SetQueueJSON(js string) error { return engine.QueueSet(js) }

// AppendQueueJSON adds tracks to the end of the engine queue ("Add to queue")
// without resending the queue: js is a JSON array in SetQueueJSON's shape. The
// cursor stays where it is; QueueVersion bumps when at least one track was
// added. A preloaded next is dropped only when the track after the cursor
// changed. Returns an error on a parse failure (the queue is left as it was).
func AppendQueueJSON(js string) error { return engine.QueueAppend(js) }

// SetQueueIndex aligns the engine queue's cursor to i (clamped; no-op when the
// queue is empty). Call it whenever the app changes the playing row so the
// engine queue tracks what is audible.
func SetQueueIndex(i int) { engine.QueueSetIndex(i) }

// QueueIndex returns the engine queue's cursor (-1 when empty/unsynced), so the
// app can resync its own cursor after an engine-driven move (remote
// /next|/prev, gapless promote).
func QueueIndex() int { return engine.QueueIndex() }

// QueueVersion returns a counter bumped on every engine-queue CONTENT change
// (SetQueueJSON, remote /queue/add|remove|move, /play/album, /play/mix/{track,artist}) —
// cursor moves don't bump it. The app polls this cheaply and pulls QueueJSON
// only when a remote controller actually edited the queue.
func QueueVersion() int64 { return int64(engine.QueueVersion()) }

// QueueJSON returns the engine queue as {"version":N,"index":I,"tracks":[..]}
// (tracks in the shared wire shape), so the app can adopt remote queue edits.
func QueueJSON() string {
	ts, idx, ver := engine.QueueSnapshot()
	return jstr(map[string]any{
		"version": ver,
		"index":   idx,
		"tracks":  bridge.FromTracks(ts),
	}, nil)
}

// QueueRemove removes the track at index from the engine queue (if valid and
// not the playing row); routed to a Connect device, the edit goes to it.
func QueueRemove(index int) { _ = engine.QueueRemove(index) }

// QueueMove reorders the engine queue (from -> to). Cursor/history follow.
// from==to and out-of-range are no-ops.
func QueueMove(from, to int) { _ = engine.QueueMove(from, to) }

// QueueInsertNext inserts the track or tracks described by trackJSON (a single
// wire track object or an array of them) right after the current row, in
// order. Bumps the version.
func QueueInsertNext(trackJSON string) { engine.QueueInsertNext(trackJSON) }

// ---- network helper (cover art) ----

// Fetch downloads raw bytes (e.g. cover art) using a browser User-Agent.
func Fetch(url string) []byte {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 OpenDeezer")
	resp, err := deezer.HTTPClient(15 * time.Second).Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return b
}

// ---- OpenDeezer Connect (controller side) ----

// SetClientInfo overrides the advertised client id + device label (before Init).
func SetClientInfo(clientName, device string) { engine.SetClientInfo(clientName, device) }

// DiscoverDevices returns LAN + configured Connect devices as a JSON array.
func DiscoverDevices(timeoutMS int) string {
	if timeoutMS <= 0 {
		timeoutMS = 700
	}
	return jstr(engine.DiscoverDevices(time.Duration(timeoutMS)*time.Millisecond), nil)
}

// ConnectDevice routes playback to the device at addr ("host", "host:port" or
// a URL). Stops local playback (audio moves to the device). Returns true on
// success.
func ConnectDevice(addr string) bool { return engine.ConnectDevice(addr) }

// DisconnectDevice returns control to local playback. Stops the remote device
// (so it doesn't keep playing unattended) before clearing the connection.
func DisconnectDevice() { engine.DisconnectDevice() }

// ConnectedDevice returns the connected device address ("" if local).
func ConnectedDevice() string { return engine.ConnectedDevice() }

// SetRepeat sets the repeat mode (mode: 0=off, 1=all, 2=one). The mode is
// recorded on the engine-side queue — /status reports it and the engine queue
// honors it when it advances. It is NOT forwarded to a connected remote device:
// while casting, the controller keeps repeat local so the host's single-
// track queue never loops the current track.
func SetRepeat(mode int) { engine.SetRepeat(mode) }

// GetRepeat returns the engine queue's repeat mode ("off"|"all"|"one"), also
// while routed to a Connect device (repeat is never forwarded).
func GetRepeat() string { return engine.Repeat() }

// GetShuffle reports whether shuffle is on. When routed to a Connect device it
// returns the remote host's flag (the routed snapshot); otherwise the engine
// queue's flag.
func GetShuffle() bool { return engine.Shuffle() }

// SetShuffle sets shuffle on (non-zero) or off (0). The flag is always recorded
// on the engine-side queue and forwarded to the connected remote device when
// one is selected.
func SetShuffle(on int) { engine.SetShuffle(on != 0) }

// ---- Web remote (pairing-based phone remote) ----

// WebRemoteSetEnabled enables (on!=0) or disables (on==0) the web remote. When
// enabling, the control server is started on a LAN-reachable address if it is
// not already, and pairing is activated so a phone can scan the QR and connect.
// Disabling ends pairing and restores the server the remote replaced, unless
// the Connect host is on.
func WebRemoteSetEnabled(on int) { engine.WebRemoteSetEnabled(on != 0) }

// WebRemoteInfo returns a JSON string:
// {"enabled":bool,"code":"123456","url":"http://<lanip>:<port>/remote","port":<int>}.
// code and url are empty when the remote is disabled.
func WebRemoteInfo() string { return jstr(engine.WebRemoteInfo(), nil) }

// WebRemoteQRPNG returns a PNG-encoded QR code for the web remote URL, or nil
// when the remote is disabled.
func WebRemoteQRPNG() []byte { return engine.WebRemoteQRPNG() }

// ---- OpenDeezer Connect host (make this device controllable) ----

// ConnectHostSetEnabled makes this device a discoverable OpenDeezer Connect
// target on the LAN (on!=0), so other devices signed into the same Deezer
// account can find it in their device picker and drive its playback; or stops
// advertising it (on==0). The control server runs on a LAN address with
// same-account auth. Idempotent.
func ConnectHostSetEnabled(on int) { engine.ConnectHostSetEnabled(on != 0) }

// ConnectHostInfo returns a JSON string:
// {"enabled":bool,"addr":"<lanip>:<port>","port":<int>,"name":"<account>"}.
// addr/name are empty when the Connect host is disabled.
func ConnectHostInfo() string { return jstr(engine.ConnectHostInfo(), nil) }

// ---- equalizer + mono downmix ----

// EQJSON returns the equalizer state:
// {enabled,mono,preampDb,gainsDb:[10],preset,bands:[10],presets:[...]}.
// Same wire shape as corelib's DZEQJSON so every client renders the same UI.
func EQJSON() string {
	st, ok := engine.EQ()
	if !ok {
		return jstr(nil, fmt.Errorf("engine not ready"))
	}
	return jstr(st, nil)
}

// SetEQJSON applies a partial EQ update. Recognized keys (all optional):
// enabled (bool), mono (bool), preampDb (number), gainsDb ([10]number),
// preset (string), band ({"index":N,"gainDb":X}). Returns false if any present
// key failed to apply.
func SetEQJSON(js string) bool { return engine.SetEQJSON(js) }

// ---- local listening history ----

// HistoryRecentJSON returns the newest n entries of the machine-local listening
// history as a JSON array (newest first, the same stable shape the control API's
// /history/recent serves); n <= 0 returns all. Empty/unavailable history yields
// "[]".
//
// Each entry is {trackId,title,artist,album?,kind?,startedAt,durationPlayedSec,
// artworkUrl?,durationMs?}. "kind" is omitted or "track" for a song and
// "episode" for a podcast episode — an app routes replay on it: episode ->
// PlayEpisodeMS(trackId), else Play.
func HistoryRecentJSON(n int) string {
	entries, err := engine.HistoryRecent(n)
	if err != nil {
		return "[]"
	}
	return jstr(entries, nil)
}

// HistoryStatsJSON returns local listening stats over the last sinceDays
// (sinceDays <= 0 = all history) as
// {topTracks:[{trackId,title,artist,plays,totalSec}],
//
//	topArtists:[{artist,plays,totalSec}], totalSeconds:N}. Empty/unavailable
//
// history yields the same shape with empty arrays and totalSeconds:0. topTracks
// and topArtists are music-only (podcast episodes excluded); totalSeconds counts
// all listening time.
func HistoryStatsJSON(sinceDays int) string { return jstr(engine.HistoryStats(sinceDays), nil) }

// ---- offline music ----

// errorReply is {"error":""} for nil, else {"error":"reason"}.
func errorReply(err error) string {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return jstr(map[string]string{"error": msg}, nil)
}

// PinOffline pins a track, an album or a playlist (kind "track", "album" or
// "playlist"; episodes are refused) for the signed-in account and fetches it
// in the background (OfflineProgressJSON). An album's or a playlist's track
// list is fetched first; offline it must be stored already. Returns
// {"error":""} or {"error":"reason"}.
func PinOffline(kind, id string) string { return errorReply(engine.PinOffline(kind, id)) }

// UnpinOffline removes the account's pin of kind and id (kind "track" with id
// "": every single-track pin of the account); a track also pinned through a
// collection stays pinned through it. Returns {"error":""} or
// {"error":"reason"}.
func UnpinOffline(kind, id string) string { return errorReply(engine.UnpinOffline(kind, id)) }

// OfflineProgressJSON returns the fetches of the account's pins:
// {"active":bool,"jobs":[{"kind","id","done","total","failed","error"}],"error":""}.
// A job is listed while it runs; one that stopped (a full disk, a cancel)
// stays with its reason until the next pin, unpin or cancel.
func OfflineProgressJSON() string {
	active, jobs := engine.OfflineProgress()
	return jstr(map[string]any{"active": active, "jobs": jobs, "error": ""}, nil)
}

// CancelOffline stops the account's fetches.
func CancelOffline() { engine.CancelOffline() }

// OfflineCollectionsJSON returns the account's pins (offline: the last account
// that signed in), newest first: {"collections":[{"kind","id","title","cover",
// "trackCount","fetched","pinnedAt"}],"error":""}. All single-track pins are
// one entry of kind "track" with id "". cover is a local file once stored.
func OfflineCollectionsJSON() string {
	return jstr(map[string]any{"collections": engine.OfflineCollections(), "error": ""}, nil)
}

// OfflineTracksJSON returns the tracks of the account's pin of kind and id in
// order ({"tracks":[...],"error":""}; kind "track", id "": the single-track
// pins, newest first), pictures local once stored. Needs no network.
func OfflineTracksJSON(kind, id string) string {
	ts, err := engine.OfflineTracks(kind, id)
	if err != nil {
		return jstr(nil, err)
	}
	return jstr(map[string]any{"tracks": ts, "error": ""}, nil)
}

// OfflineIDsJSON returns {"ids":[...],"error":""}: every track the account can
// play offline (pinned and fetched).
func OfflineIDsJSON() string {
	return jstr(map[string]any{"ids": engine.OfflineIDs(), "error": ""}, nil)
}

// SetOfflineMode turns offline mode on or off: nothing is requested in it and
// only pinned or cached tracks play. Init turns it on when the login fails
// for want of a network.
func SetOfflineMode(on bool) { engine.SetOfflineMode(on) }

// OfflineMode reports whether offline mode is on.
func OfflineMode() bool { return engine.OfflineMode() }
