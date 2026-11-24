// Package odmobile exposes the OpenDeezer engine to gomobile for native Android and iOS
// apps (gomobile bind -target=android ./mobile). Browse calls return JSON strings; the
// app advances its own queue on FinishedCount.
package odmobile

import (
	"encoding/json"
	"fmt"
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

func jstr(v any, err error) string { return string(engine.ReplyJSON(v, err)) }

func ok(err error) bool { return err == nil }

// ---- lifecycle ----

// SetDataDir points the engine's config at dir, an app-private, persistent directory
// (Android: getFilesDir(); iOS: Application Support). Call it once at startup before
// Init, since os.UserConfigDir is not such a path on Android.
func SetDataDir(dir string) {
	if dir != "" {
		config.SetBaseDir(filepath.Join(dir, "opendeezer"))
	}
}

// SetCacheDir points stream temp files and the default media cache at dir/opendeezer,
// the app's cache directory (Android: getCacheDir()). Call it at startup before Init on
// Android; on iOS the default is already the app's.
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

// Logout ends the engine session: stops playback, closes the control server (dropping
// pairing sessions and same-account auth), stops Connect-host advertising and forgets
// the client. A later Init starts the services again.
func Logout() { engine.Logout() }

// CheckUpdate checks GitHub for a newer release; returns JSON
// {current, latest, hasUpdate, url, notes}.
func CheckUpdate() string {
	info, _ := update.Check(Version)
	return jstr(info, nil)
}

// ---- account / settings ----

func Account() string {
	return withClient(func(c *deezer.Client) (any, error) { return c.Account(), nil })
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

// SetSingleDevice turns single-device playback on (default) or off and saves it: when
// on, playback pauses as another session of the account starts a track, and local
// starts are announced so those sessions pause.
func SetSingleDevice(on bool) { engine.SetSingleDevice(on) }
func SingleDevice() bool      { return engine.SingleDevice() }

// SessionExpired reports that Deezer refused the signed-in account's ARL since
// it signed in: the host asks for a new sign-in.
func SessionExpired() bool { return engine.SessionExpired() }

// SetListenContext sets the page type ("playlist_page", "album_page", ...) and id that
// Deezer's listen log gets for every start until the next call; "" for both clears it.
func SetListenContext(pageType, id string) { engine.SetListenContext(pageType, id) }

// ElsewhereJSON returns what another session of the account plays as {"track",
// "sinceMs", "untilMs"} ("{}" when none since login; no "track" for an unnamed track).
// ElsewhereVersion changes exactly when that payload changes.
func ElsewhereJSON() string   { return engine.ElsewhereJSON() }
func ElsewhereVersion() int64 { return engine.ElsewhereVersion() }

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

// Preload records trackID to follow the current track (gapless or crossfade), with the
// synced queue's length for it; it is fetched once max(30 s, crossfade + 20 s) remain.
// A new request replaces it; no-op when routed.
func Preload(id string) error {
	if engine.ConnectedDevice() == "" && engine.Player() == nil {
		return fmt.Errorf("engine not ready")
	}
	engine.Preload(id, 0)
	return nil
}

// ClearPreload discards a preloaded next track. Call it when the upcoming track changes
// (shuffle/repeat toggled, queue edited) so a stale preload is not swapped in.
func ClearPreload() { engine.ClearPreload() }

// ---- browse ----

// withClient is engine.OnlineJSON as a string.
func withClient(fn func(c *deezer.Client) (any, error)) string { return string(engine.OnlineJSON(fn)) }

func Favorites() string {
	return withClient(func(c *deezer.Client) (any, error) {
		ts, err := c.Favorites()
		return map[string]any{"tracks": bridge.FromTracks(ts)}, err
	})
}

// FavoriteIDsJSON returns the liked track ids, newest first, as a JSON array of strings
// ("[]" when none, {"error"} without a client or on failure). Unchanged lists are
// answered "Not modified", so frequent calls are cheap.
func FavoriteIDsJSON() string { return withClient(bridge.FavoriteIDs) }

// LibraryJSON returns the account's favorite albums ({"albums":[...]}),
// artists ({"artists":[...]}) or podcasts ({"podcasts":[...]}) by kind
// ("album", "artist", "podcast"), newest first.
func LibraryJSON(kind string) string {
	return withClient(func(c *deezer.Client) (any, error) { return bridge.Library(c, kind) })
}

// SetFavorite adds (on) or removes an album, artist, playlist or podcast (kind
// "album", "artist", "playlist", "podcast") from the favorites.
func SetFavorite(kind, id string, on bool) bool {
	return clientOp(func(c *deezer.Client) error { return c.SetFavorite(kind, id, on) })
}

// SetRecommendationBan stops (on) or allows again a track ("track") or an
// artist ("artist") in the account's recommendations.
func SetRecommendationBan(kind, id string, on bool) bool {
	return clientOp(func(c *deezer.Client) error { return c.SetRecommendationBan(kind, id, on) })
}

// SearchHistoryJSON returns the account's search history, newest first, at
// most n: [{"type":"Track","id":"..."},...].
func SearchHistoryJSON(n int) string {
	return withClient(func(c *deezer.Client) (any, error) { return c.SearchHistory(n) })
}

// AddSearchHistory records that an item (type "Track", "Album", "Artist",
// "Playlist", "Podcast", "PodcastEpisode", "Livestream") was picked from a
// search.
func AddSearchHistory(typ, id string) bool {
	return clientOp(func(c *deezer.Client) error { return c.AddSearchHistory(deezer.SearchHistoryItem{Type: typ, ID: id}) })
}

// ClearSearchHistory empties the account's search history.
func ClearSearchHistory() bool {
	return clientOp(func(c *deezer.Client) error { return c.ClearSearchHistory() })
}

// EpisodeBookmarksJSON returns the account's podcast bookmarks by episode id:
// {"<id>":{"offset":754,"heard":false},...} (seconds).
func EpisodeBookmarksJSON() string {
	return withClient(func(*deezer.Client) (any, error) { return engine.EpisodeBookmarks() })
}

// MarkEpisodePlayed marks an episode as played, or as not played.
func MarkEpisodePlayed(id string, played bool) bool {
	return clientOp(func(*deezer.Client) error { return engine.MarkEpisodePlayed(id, played) })
}

// SetPlaybackSpeed sets the speed episodes play at, from 0.5 to 2 (1 is
// normal), at once when an episode plays; songs play at normal speed.
func SetPlaybackSpeed(speed float64) { engine.SetPlaybackSpeed(speed) }

// PlaybackSpeed returns the speed episodes play at.
func PlaybackSpeed() float64 { return engine.PlaybackSpeed() }

// PlaylistDetailsJSON returns a playlist's details:
// {"id","title","description","isPrivate","isCollaborative","ownerId"}.
func PlaylistDetailsJSON(id string) string {
	return withClient(func(c *deezer.Client) (any, error) { return c.PlaylistDetails(id) })
}

// UpdatePlaylist changes a playlist the account owns: changeJSON holds the
// fields to change ({"title","description","isPrivate","isCollaborative"}).
func UpdatePlaylist(id, changeJSON string) bool {
	var u deezer.PlaylistUpdate
	if json.Unmarshal([]byte(changeJSON), &u) != nil {
		return false
	}
	return clientOp(func(c *deezer.Client) error { return c.UpdatePlaylist(id, u) })
}

// ReorderPlaylist puts a playlist's tracks in the order of idsJSON (a JSON
// array of track ids).
func ReorderPlaylist(id, idsJSON string) bool {
	var ids []string
	if json.Unmarshal([]byte(idsJSON), &ids) != nil {
		return false
	}
	return clientOp(func(c *deezer.Client) error { return c.ReorderPlaylist(id, ids) })
}

// BestResultJSON returns a search's best result, {"type","id","title",
// "subtitle","picture"}, or {} when it has none.
func BestResultJSON(query string) string { return string(engine.BestResultJSON(query)) }

// PinnedJSON returns what the account pinned to Home, in its order:
// {"items":[{"type","id","title","subtitle","picture"},...]}.
func PinnedJSON() string { return withClient(bridge.Pinned) }

// SetPinned pins an item to Home or unpins it (type "Album", "Artist",
// "Playlist", "Podcast", "Track"): 1 on success, 2 when Home holds no more,
// 0 on another failure.
func SetPinned(typ, id string, on bool) int {
	c, err := engine.Online()
	if err != nil {
		return 0
	}
	return bridge.SetPinned(c, typ, id, on)
}

// IsFavorite reports whether an album, artist, playlist or podcast (kind
// "album", "artist", "playlist", "podcast") is among the favorites: 1 yes,
// 0 no, -1 unknown (the request failed).
func IsFavorite(kind, id string) int {
	c, err := engine.Online()
	if err != nil {
		return -1
	}
	return bridge.IsFavorite(c, kind, id)
}

// ExplicitJSON returns the account's explicit-content setting:
// {"level":"explicit_display","available":[...],"canEdit":true}.
func ExplicitJSON() string {
	return withClient(func(c *deezer.Client) (any, error) { return c.Explicit(), nil })
}

// SetExplicitLevel changes the account's explicit-content level (one of
// ExplicitJSON's available).
func SetExplicitLevel(level string) bool {
	return clientOp(func(c *deezer.Client) error { return c.SetExplicitLevel(level) })
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

// Flow returns a batch of the account's Flow, or of one of its moods (a
// FlowMoodsJSON id; "" for Flow itself): {tracks:[...]}.
func Flow(mood string) string {
	return withClient(func(c *deezer.Client) (any, error) {
		ts, err := c.Flow(mood)
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

// FlowMoodsJSON lists Flow's moods: [{"id","title"},...].
func FlowMoodsJSON() string {
	return withClient(func(c *deezer.Client) (any, error) { return c.FlowMoods() })
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
	return withClient(func(c *deezer.Client) (any, error) { return bridge.ArtistProfile(c, id) })
}
func Lyrics(id string) string {
	return withClient(func(c *deezer.Client) (any, error) { return bridge.Lyrics(c, id) })
}

func Search(q string) string { return string(engine.SearchJSON(q)) }
func Charts() string         { return string(engine.ChartsJSON()) }

// Home returns the Home sections (chart top tracks and albums, the user's playlists) in
// one call, like corelib DZHomeJSON. A section that fails comes back empty.
func Home() string { return string(engine.HomeJSON()) }

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

// PlayEpisodeMS plays a podcast episode (plain stream), setting it as the current track
// at once with id and duration, then adding title and artwork from REST /episode in the
// background. Returns as Play does.
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

// CreatePlaylist creates a playlist with the tracks of idsJSON (a JSON array
// of ids; "" or "[]" for none) and returns {"id":"..."}.
func CreatePlaylist(title, idsJSON string) string {
	return withClient(func(c *deezer.Client) (any, error) { return bridge.CreatePlaylist(c, title, idsJSON) })
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

// DownloadTrack saves trackID into destDir ("" = DownloadDir) and returns
// {"path":"..."} or {"error":"..."}. It blocks, so call it off the UI thread. Premium
// only.
func DownloadTrack(trackID, destDir string) string {
	path, err := engine.DownloadTrack(trackID, destDir)
	if err != nil {
		return jstr(nil, err)
	}
	return jstr(map[string]string{"path": path}, nil)
}

// DownloadAlbum saves every track of albumID not yet in DownloadDir and returns
// {"saved":N,"failed":N,"dir":"...","error":""}. It blocks (call it off the UI thread)
// and is premium only, like DownloadTrack.
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

// SetMediaCacheMB saves the cache budget in MB and applies it: a lower budget evicts
// the least recent tracks, <= 0 removes all unpinned tracks and stops caching. Pinned
// tracks are outside the budget.
func SetMediaCacheMB(mb int) bool { return ok(engine.Caches().SetMB(mb)) }

// MediaCacheMaxAgeDays returns how many days a cached track may go unplayed
// before it is removed (media.json; 0 = kept at any age, the default).
func MediaCacheMaxAgeDays() int { return config.LoadMedia().MediaCacheMaxAgeDays }

// SetMediaCacheMaxAgeDays saves that age (<= 0: no limit) and applies it: tracks not
// played or downloaded for longer are removed at once, then at most hourly during
// playback.
func SetMediaCacheMaxAgeDays(days int) bool { return ok(engine.Caches().SetMaxAgeDays(days)) }

// MediaCacheStatsJSON returns {"entries","bytes","pinnedBytes","maxBytes","dir"}: bytes
// includes pinned tracks, maxBytes 0 means off, dir is the chosen folder (the cache is
// its opendeezer-cache subfolder).
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

// SetMediaCacheDir moves the cache to dir/opendeezer-cache (dir absolute, "" =
// default), copying pinned tracks first, and returns {"error":""} or {"error":"reason"}.
// On any error the old folder stays in use.
func SetMediaCacheDir(dir string) string { return errorReply(engine.SetMediaCacheDir(dir)) }

// IsPreview reports whether the current track is Deezer's 30-second preview (the
// free-account fallback) rather than the full stream.
func IsPreview() bool { return engine.IsPreview() }

// SetAdsDisabled turns free-tier play reporting (log.listen) off or on and saves it.
// Reporting credits artists and drives the ad schedule; disabling it is the user's own
// risk. Paid accounts are unaffected.
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

// SetOutputSuspended suspends (true) or resumes (false) the local audio device while
// keeping playback state, for audio-focus handling. It never routes to a Connect
// device.
func SetOutputSuspended(on bool) {
	if p := engine.Player(); p != nil {
		_ = p.SetOutputSuspended(on)
	}
}
func Seek(ms int64)       { engine.Seek(ms) }
func SetVolume(v float64) { engine.SetVolume(v) }
func Volume() float64     { return engine.Volume() }

// LastErrorJSON is {"error":"...","network":bool}, as corelib's DZLastErrorJSON.
func LastErrorJSON() string { return string(engine.LastErrorJSON()) }

// State is the playback state: 0 stopped, 1 loading, 2 playing, 3 paused, 4 errored.
func State() int         { return engine.State() }
func PositionMS() int64  { return engine.PositionMS() }
func DurationMS() int64  { return engine.DurationMS() }
func Format() string     { return engine.Format() }
func FinishedCount() int { return engine.FinishedCount() }

// NowPlaying returns the track actually playing (remote when routed, else
// local), "{}" when nothing plays.
func NowPlaying() string { return engine.NowPlayingJSON() }

// NowPlayingVersion returns a counter that moves exactly when NowPlaying's
// result changes: poll it and fetch NowPlaying only then.
func NowPlayingVersion() int64 { return engine.NowPlayingVersion() }

// ---- engine queue sync (app queue -> engine) ----

// SetQueueJSON replaces the engine queue with the app's (a JSON array of wire tracks,
// "[]" clears) and resets the cursor to 0; follow with SetQueueIndex. Returns an error
// on a parse failure.
func SetQueueJSON(js string) error { return engine.QueueSet(js) }

// AppendQueueJSON adds a JSON array of tracks to the end of the engine queue; the
// cursor stays and QueueVersion bumps when anything was added. Returns an error on a
// parse failure (queue unchanged).
func AppendQueueJSON(js string) error { return engine.QueueAppend(js) }

// SetQueueIndex aligns the engine queue's cursor to i (clamped; no-op when the
// queue is empty). Call it whenever the app changes the playing row so the
// engine queue tracks what is audible.
func SetQueueIndex(i int) { engine.QueueSetIndex(i) }

// QueueIndex returns the engine queue's cursor (-1 when empty/unsynced), so the
// app can resync its own cursor after an engine-driven move (remote
// /next|/prev, gapless promote).
func QueueIndex() int { return engine.QueueIndex() }

// QueueVersion returns a counter bumped on every engine-queue content change (not on
// cursor moves), so the app pulls QueueJSON only after a remote edit.
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

// Fetch downloads raw bytes (e.g. cover art); nil on any failure and in offline mode.
func Fetch(url string) []byte {
	b, err := engine.Fetch(url)
	if err != nil {
		return nil
	}
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

// SetRepeat sets the repeat mode (0 off, 1 all, 2 one) on the engine queue. It is not
// forwarded to a Connect device, whose single-track queue would loop.
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

// WebRemoteSetEnabled turns the web remote on (on!=0) or off. On starts the control
// server on a LAN address if needed and activates pairing; off ends pairing and
// restores the previous server unless the Connect host is on.
func WebRemoteSetEnabled(on int) { engine.WebRemoteSetEnabled(on != 0) }

// WebRemoteInfo returns a JSON string:
// {"enabled":bool,"code":"123456","url":"http://<lanip>:<port>/remote","port":<int>}.
// code and url are empty when the remote is disabled.
func WebRemoteInfo() string { return jstr(engine.WebRemoteInfo(), nil) }

// WebRemoteQRPNG returns a PNG-encoded QR code for the web remote URL, or nil
// when the remote is disabled.
func WebRemoteQRPNG() []byte { return engine.WebRemoteQRPNG() }

// ---- OpenDeezer Connect host (make this device controllable) ----

// ConnectHostSetEnabled(on!=0) advertises this device on the LAN as a Connect target
// for the account's other devices (control server on a LAN address, same-account
// auth); 0 stops advertising. Idempotent.
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

// SetEQJSON applies a partial EQ update with optional keys enabled, mono, preampDb,
// gainsDb ([10]), preset, band ({"index","gainDb"}). False when a key fails.
func SetEQJSON(js string) bool { return engine.SetEQJSON(js) }

// ---- local listening history ----

// HistoryRecentJSON returns the newest n local history entries (n <= 0: all, "[]" when
// none), as /history/recent serves them. "kind" "episode" marks a podcast episode
// (replay with PlayEpisodeMS).
func HistoryRecentJSON(n int) string {
	entries, err := engine.HistoryRecent(n)
	if err != nil {
		return "[]"
	}
	return jstr(entries, nil)
}

// HistoryStatsJSON returns {topTracks, topArtists, totalSeconds} over the last
// sinceDays (<= 0: all). Podcast episodes count only in totalSeconds; no history gives
// empty arrays.
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

// PinOffline pins a track, album or playlist (episodes are refused) for the account and
// fetches it in the background (OfflineProgressJSON); offline, a collection's list must
// be stored already. Returns {"error":""} or {"error":"reason"}.
func PinOffline(kind, id string) string { return errorReply(engine.PinOffline(kind, id)) }

// UnpinOffline removes the account's pin of kind and id (kind "track", id "": every
// single-track pin); tracks pinned through a collection stay. Returns {"error":...}.
func UnpinOffline(kind, id string) string { return errorReply(engine.UnpinOffline(kind, id)) }

// OfflineProgressJSON returns {"active","jobs":[{"kind","id","done","total","failed",
// "error"}],"error"}. A stopped job keeps its reason until the next pin, unpin or
// cancel.
func OfflineProgressJSON() string {
	active, jobs := engine.OfflineProgress()
	return jstr(map[string]any{"active": active, "jobs": jobs, "error": ""}, nil)
}

// CancelOffline stops the account's fetches.
func CancelOffline() { engine.CancelOffline() }

// OfflineCollectionsJSON returns the account's pins newest first as {"collections":
// [{"kind","id","title","cover","trackCount","fetched","pinnedAt"}]}. Single-track pins
// form one entry (kind "track", id ""); cover is a local file once stored.
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
