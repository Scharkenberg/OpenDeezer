// Command deezercore exposes the OpenDeezer engine as a C library for native GUIs
// (go build -buildmode=c-archive ./corelib, which also emits the header). List and
// search calls return a malloc'd JSON string the caller releases with DZFree.
package main

/*
#include <stdlib.h>
#include <string.h>
*/
import "C"

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/bridge"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/engine"
	odlog "github.com/Cycl0o0/OpenDeezer/v3/internal/log"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/update"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/version"
)

func main() {} // required for buildmode=c-archive

var errNotReady = errors.New("not logged in")

// jsonStr is engine.ReplyJSON as a malloc'd C string.
func jsonStr(v any, err error) *C.char { return C.CString(string(engine.ReplyJSON(v, err))) }

// cBytes copies b into a malloc'd buffer (free with DZFreeBytes) and writes
// its length to outLen; nil for an empty b.
func cBytes(b []byte, outLen *C.int) *C.uchar {
	*outLen = 0
	if len(b) == 0 {
		return nil
	}
	p := C.malloc(C.size_t(len(b)))
	if p == nil {
		return nil
	}
	C.memcpy(p, unsafe.Pointer(&b[0]), C.size_t(len(b)))
	*outLen = C.int(len(b))
	return (*C.uchar)(p)
}

func ok(err error) C.int {
	if err != nil {
		return 0
	}
	return 1
}

func cBool(v bool) C.int {
	if v {
		return 1
	}
	return 0
}

// withClient is engine.OnlineJSON as a malloc'd C string.
func withClient(fn func(c *deezer.Client) (any, error)) *C.char {
	return C.CString(string(engine.OnlineJSON(fn)))
}

// errorReply is {"error":""} for nil, else {"error":"reason"}.
func errorReply(err error) *C.char {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return jsonStr(map[string]string{"error": msg}, nil)
}

// ---- exported C API ----

//export DZFree
func DZFree(s *C.char) { C.free(unsafe.Pointer(s)) }

// DZFetch downloads raw bytes (e.g. cover art) for frontends without an HTTP stack.
// Returns a malloc'd buffer (free with DZFreeBytes) with its length in outLen, or
// NULL on error and in offline mode.
//
//export DZFetch
func DZFetch(url *C.char, outLen *C.int) *C.uchar {
	*outLen = 0
	b, err := engine.Fetch(C.GoString(url))
	if err != nil {
		return nil
	}
	return cBytes(b, outLen)
}

//export DZFreeBytes
func DZFreeBytes(p *C.uchar) { C.free(unsafe.Pointer(p)) }

// logOnce opens the log file at the first DZInit (a re-login reuses it).
var logOnce sync.Once

// DZInit logs in with the ARL and starts the engine: 1 on success, 0 with the reason
// in DZLoginErrorKind. Without a network (kind 2) it starts the player, cache and pins
// in offline mode for the last account; a later successful DZInit turns that off.
//
//export DZInit
func DZInit(arl *C.char) C.int {
	logOnce.Do(func() {
		if base, err := os.UserConfigDir(); err == nil {
			_ = odlog.OpenFile(filepath.Join(base, "opendeezer"))
		}
	})
	return cBool(engine.Init(C.GoString(arl)))
}

// DZLoginErrorKind returns why the last DZInit failed: 0 ok, 1 ARL expired or invalid,
// 2 no internet, 3 other, 4 no audio output device.
//
//export DZLoginErrorKind
func DZLoginErrorKind() C.int { return C.int(engine.LoginErrorKind()) }

// DZLastErrorJSON returns {"error":"...","network":bool}: why the player last stopped or a play
// failed ("" for none), and whether a lost network caused it. Release with DZFree.
//
//export DZLastErrorJSON
func DZLastErrorJSON() *C.char { return C.CString(string(engine.LastErrorJSON())) }

// DZSetQuality sets and saves the stream quality: 0 Normal (MP3 128), 1 High (MP3 320),
// 2 HiFi (FLAC, MP3 when not entitled). Saved in playback.json, shared with the TUI,
// and applied at each DZInit; it can be called before DZInit.
//
//export DZSetQuality
func DZSetQuality(level C.int) { engine.SetQuality(int(level)) }

// DZQuality returns the quality level (0..2).
//
//export DZQuality
func DZQuality() C.int { return C.int(engine.Quality()) }

// DZHighQuality reports whether quality is at least MP3_320 (kept for the
// frontends that use a simple toggle; level 2 also counts as high).
//
//export DZHighQuality
func DZHighQuality() C.int { return cBool(engine.Quality() >= deezer.QualityHigh) }

// DZFormat returns a human label for the current stream's actual format
// (e.g. "FLAC · lossless", "MP3 · 320 kbps"), or "" if nothing is playing.
//
//export DZFormat
func DZFormat() *C.char { return C.CString(engine.Format()) }

//export DZUserID
func DZUserID() *C.char {
	if c := engine.Client(); c != nil {
		return C.CString(c.UserID())
	}
	return C.CString("")
}

// DZFavoritesJSON returns the liked tracks, {"tracks":[...]}, or {"error":"..."}. Release with DZFree.
//
//export DZFavoritesJSON
func DZFavoritesJSON() *C.char {
	return withClient(func(c *deezer.Client) (any, error) {
		ts, err := c.Favorites()
		return map[string]any{"tracks": bridge.FromTracks(ts)}, err
	})
}

// DZFavoriteIDsJSON returns the liked-track ids, newest first, as a JSON array of
// strings ("[]" when none). Unchanged lists are answered "Not modified", so frequent
// calls are cheap. Release with DZFree.
//
//export DZFavoriteIDsJSON
func DZFavoriteIDsJSON() *C.char {
	return withClient(bridge.FavoriteIDs)
}

// DZLibraryJSON returns the favorite albums, artists or podcasts by kind ("album",
// "artist", "podcast"), newest first, e.g. {"albums":[...]}. Release with DZFree.
//
//export DZLibraryJSON
func DZLibraryJSON(kind *C.char) *C.char {
	k := C.GoString(kind)
	return withClient(func(c *deezer.Client) (any, error) { return bridge.Library(c, k) })
}

// DZSetFavorite adds (on=1) or removes an album, artist, playlist or podcast (kind
// "album", "artist", "playlist", "podcast") from the favorites: 1 on success.
//
//export DZSetFavorite
func DZSetFavorite(kind, id *C.char, on C.int) C.int {
	k, i := C.GoString(kind), C.GoString(id)
	return clientOp(func(c *deezer.Client) error { return c.SetFavorite(k, i, on != 0) })
}

// DZSetRecommendationBan stops (on=1) or allows again a track ("track") or an
// artist ("artist") in the account's recommendations: 1 on success.
//
//export DZSetRecommendationBan
func DZSetRecommendationBan(kind, id *C.char, on C.int) C.int {
	k, i := C.GoString(kind), C.GoString(id)
	return clientOp(func(c *deezer.Client) error { return c.SetRecommendationBan(k, i, on != 0) })
}

// DZSearchHistoryJSON returns the account's search history, newest first, at
// most n: [{"type":"Track","id":"..."},...]. Release with DZFree.
//
//export DZSearchHistoryJSON
func DZSearchHistoryJSON(n C.int) *C.char {
	return withClient(func(c *deezer.Client) (any, error) { return c.SearchHistory(int(n)) })
}

// DZAddSearchHistory records an item picked from a search (type "Track", "Album",
// "Artist", "Playlist", "Podcast", "PodcastEpisode", "Livestream"): 1 on success.
//
//export DZAddSearchHistory
func DZAddSearchHistory(typ, id *C.char) C.int {
	item := deezer.SearchHistoryItem{Type: C.GoString(typ), ID: C.GoString(id)}
	return clientOp(func(c *deezer.Client) error { return c.AddSearchHistory(item) })
}

// DZClearSearchHistory empties the account's search history: 1 on success.
//
//export DZClearSearchHistory
func DZClearSearchHistory() C.int {
	return clientOp(func(c *deezer.Client) error { return c.ClearSearchHistory() })
}

// DZEpisodeBookmarksJSON returns the podcast bookmarks by episode id, offsets in
// seconds: {"<id>":{"offset":754,"heard":false},...}. Release with DZFree.
//
//export DZEpisodeBookmarksJSON
func DZEpisodeBookmarksJSON() *C.char {
	return withClient(func(*deezer.Client) (any, error) { return engine.EpisodeBookmarks() })
}

// DZMarkEpisodePlayed marks an episode as played (played=1) or not: 1 on
// success.
//
//export DZMarkEpisodePlayed
func DZMarkEpisodePlayed(id *C.char, played C.int) C.int {
	i := C.GoString(id)
	return clientOp(func(*deezer.Client) error { return engine.MarkEpisodePlayed(i, played != 0) })
}

// DZSetPlaybackSpeed sets the speed episodes play at, from 0.5 to 2 (1 is
// normal), at once when an episode plays; songs play at normal speed.
//
//export DZSetPlaybackSpeed
func DZSetPlaybackSpeed(speed C.double) { engine.SetPlaybackSpeed(float64(speed)) }

// DZPlaybackSpeed returns the speed episodes play at.
//
//export DZPlaybackSpeed
func DZPlaybackSpeed() C.double { return C.double(engine.PlaybackSpeed()) }

// DZPlaylistDetailsJSON returns {"id","title","description","isPrivate",
// "isCollaborative","ownerId"} for a playlist. Release with DZFree.
//
//export DZPlaylistDetailsJSON
func DZPlaylistDetailsJSON(id *C.char) *C.char {
	i := C.GoString(id)
	return withClient(func(c *deezer.Client) (any, error) { return c.PlaylistDetails(i) })
}

// DZUpdatePlaylist changes any of "title", "description", "isPrivate",
// "isCollaborative" (from changeJSON) on an owned playlist: 1 on success.
//
//export DZUpdatePlaylist
func DZUpdatePlaylist(id, changeJSON *C.char) C.int {
	i := C.GoString(id)
	var u deezer.PlaylistUpdate
	if json.Unmarshal([]byte(C.GoString(changeJSON)), &u) != nil {
		return 0
	}
	return clientOp(func(c *deezer.Client) error { return c.UpdatePlaylist(i, u) })
}

// DZReorderPlaylist puts a playlist's tracks in the order of idsJSON (a JSON
// array of track ids). 1 on success.
//
//export DZReorderPlaylist
func DZReorderPlaylist(id, idsJSON *C.char) C.int {
	i := C.GoString(id)
	var ids []string
	if json.Unmarshal([]byte(C.GoString(idsJSON)), &ids) != nil {
		return 0
	}
	return clientOp(func(c *deezer.Client) error { return c.ReorderPlaylist(i, ids) })
}

// DZIsFavorite reports whether an album, artist, playlist or podcast (by kind) is a
// favorite: 1 yes, 0 no, -1 unknown (the request failed).
//
//export DZIsFavorite
func DZIsFavorite(kind, id *C.char) C.int {
	c, err := engine.Online()
	if err != nil {
		return -1
	}
	return C.int(bridge.IsFavorite(c, C.GoString(kind), C.GoString(id)))
}

// DZBestResultJSON returns a search's best result, {"type","id","title",
// "subtitle","picture"}, or {} when it has none. Release with DZFree.
//
//export DZBestResultJSON
func DZBestResultJSON(query *C.char) *C.char {
	return C.CString(string(engine.BestResultJSON(C.GoString(query))))
}

// DZPinnedJSON returns what the account pinned to Home, in its order:
// {"items":[{"type","id","title","subtitle","picture"},...]}. Release with DZFree.
//
//export DZPinnedJSON
func DZPinnedJSON() *C.char {
	return withClient(bridge.Pinned)
}

// DZSetPinned pins (on=1) or unpins an item on Home (type "Album", "Artist",
// "Playlist", "Podcast", "Track"): 1 on success, 2 when Home is full, 0 otherwise.
//
//export DZSetPinned
func DZSetPinned(typ, id *C.char, on C.int) C.int {
	c, err := engine.Online()
	if err != nil {
		return 0
	}
	return C.int(bridge.SetPinned(c, C.GoString(typ), C.GoString(id), on != 0))
}

// DZExplicitJSON returns the explicit-content setting as {"level":"explicit_display",
// "available":[...],"canEdit":true}. Release with DZFree.
//
//export DZExplicitJSON
func DZExplicitJSON() *C.char {
	return withClient(func(c *deezer.Client) (any, error) { return c.Explicit(), nil })
}

// DZSetExplicitLevel changes the account's explicit-content level (one of
// DZExplicitJSON's available): 1 on success.
//
//export DZSetExplicitLevel
func DZSetExplicitLevel(level *C.char) C.int {
	l := C.GoString(level)
	return clientOp(func(c *deezer.Client) error { return c.SetExplicitLevel(l) })
}

// DZPlaylistsJSON returns the user's and followed playlists, {"playlists":[...]}, or {"error":"..."}.
//
//export DZPlaylistsJSON
func DZPlaylistsJSON() *C.char {
	return withClient(func(c *deezer.Client) (any, error) {
		ps, err := c.Playlists()
		return map[string]any{"playlists": bridge.FromPlaylists(ps)}, err
	})
}

// DZPlaylistTracksJSON lists a playlist's tracks: {"tracks":[...]}. A playlist pinned
// offline is kept in step: added tracks are pinned and fetched, removed ones unpinned.
//
//export DZPlaylistTracksJSON
func DZPlaylistTracksJSON(id *C.char) *C.char {
	ts, err := engine.PlaylistTracks(C.GoString(id))
	return jsonStr(map[string]any{"tracks": bridge.FromTracks(ts)}, err)
}

// DZAlbumTracksJSON lists an album's tracks: {"tracks":[...]}. An album pinned
// for offline use is kept in step.
//
//export DZAlbumTracksJSON
func DZAlbumTracksJSON(id *C.char) *C.char {
	ts, err := engine.AlbumTracks(C.GoString(id))
	return jsonStr(map[string]any{"tracks": bridge.FromTracks(ts)}, err)
}

// DZSearchJSON returns a search's {"tracks","albums","artists","playlists"}, or {"error":"..."}.
//
//export DZSearchJSON
func DZSearchJSON(q *C.char) *C.char { return C.CString(string(engine.SearchJSON(C.GoString(q)))) }

// DZPlay plays a track (on the Connect device when selected); the current stream plays
// until the new one starts. 1 started, 0 failed (reason in DZLastErrorJSON), 2 when a
// newer play took the player first (not a failure).
//
//export DZPlay
func DZPlay(trackID *C.char, durationMS C.longlong) C.int {
	return C.int(engine.Play(C.GoString(trackID), int64(durationMS)))
}

//export DZPause
func DZPause() { engine.Pause() }

//export DZResume
func DZResume() { engine.Resume() }

// DZTogglePause only flips Playing and Paused; from Stopped or Errored the GUI
// calls DZPlay again.
//
//export DZTogglePause
func DZTogglePause() { engine.TogglePause() }

//export DZStop
func DZStop() { engine.Stop() }

//export DZSeek
func DZSeek(ms C.longlong) { engine.Seek(int64(ms)) }

// DZSetRepeat sets the repeat mode (0 off, 1 all, 2 one) on the engine queue. It is
// not forwarded to a Connect device: the engine feeds it one track at a time, and
// repeat-all there would loop that track forever.
//
//export DZSetRepeat
func DZSetRepeat(mode C.int) { engine.SetRepeat(int(mode)) }

// DZSetShuffle turns shuffle on (1) or off (0) on the engine queue and forwards it to
// the selected Connect device.
//
//export DZSetShuffle
func DZSetShuffle(on C.int) { engine.SetShuffle(on != 0) }

// DZSetShuffleHostOwned(1) declares that the GUI owns play order: shuffle changes only
// record the flag (for /status, DZGetShuffle, notifications) and never reorder the
// engine queue. Call before DZInit.
//
//export DZSetShuffleHostOwned
func DZSetShuffleHostOwned(on C.int) { engine.SetShuffleHostOwned(on != 0) }

// DZGetRepeat returns the engine queue's repeat mode ("off", "all", "one"), also while
// routed to a Connect device. Release with DZFree.
//
//export DZGetRepeat
func DZGetRepeat() *C.char { return C.CString(engine.Repeat()) }

// DZGetShuffle returns 1 when shuffle is on, else 0. Routed to a Connect device
// it reflects the routed snapshot; otherwise the engine queue.
//
//export DZGetShuffle
func DZGetShuffle() C.int { return cBool(engine.Shuffle()) }

// DZState returns the playback state: 0 stopped, 1 loading, 2 playing, 3 paused, 4 errored.
//
//export DZState
func DZState() C.int { return C.int(engine.State()) }

//export DZPositionMS
func DZPositionMS() C.longlong { return C.longlong(engine.PositionMS()) }

// DZDurationMS returns the playing track's length: the player's, else (a play
// started without one) the current track's once its metadata has arrived.
//
//export DZDurationMS
func DZDurationMS() C.longlong { return C.longlong(engine.DurationMS()) }

//export DZSetVolume
func DZSetVolume(v C.double) { engine.SetVolume(float64(v)) }

//export DZVolume
func DZVolume() C.double { return C.double(engine.Volume()) }

// DZFinishedCount returns an increasing count of tracks that ended naturally without
// the engine queue advancing; native UIs poll it to drive auto-advance.
//
//export DZFinishedCount
func DZFinishedCount() C.int { return C.int(engine.FinishedCount()) }

// ---- downloads (premium-only, full track to disk) ----

// DZDownloadTrack saves trackID into destDir ("" = DZDownloadDir) and returns
// {"path":"..."} or {"error":"..."}. It blocks, so call it off the UI thread; free
// accounts get an error (premium only). Release with DZFree.
//
//export DZDownloadTrack
func DZDownloadTrack(trackID, destDir *C.char) *C.char {
	path, err := engine.DownloadTrack(C.GoString(trackID), C.GoString(destDir))
	if err != nil {
		return jsonStr(nil, err)
	}
	return jsonStr(map[string]string{"path": path}, nil)
}

// DZDownloadAlbum saves every track of albumID not yet in DZDownloadDir and returns
// {"saved":N,"failed":N,"dir":"...","error":""}. It blocks and is premium only, like
// DZDownloadTrack. Release with DZFree.
//
//export DZDownloadAlbum
func DZDownloadAlbum(id *C.char) *C.char { return jsonStr(engine.DownloadAlbum(C.GoString(id))) }

// DZDownloadPlaylist downloads every track of playlistID to the shared download
// folder and returns the same batch summary as DZDownloadAlbum.
//
//export DZDownloadPlaylist
func DZDownloadPlaylist(id *C.char) *C.char { return jsonStr(engine.DownloadPlaylist(C.GoString(id))) }

// DZDownloadDir returns the current download folder (env/config/default). The
// result must be released with DZFree.
//
//export DZDownloadDir
func DZDownloadDir() *C.char { return C.CString(config.LoadDownloadDir()) }

// DZSetDownloadDir persists the download folder ("" resets to the default) and
// returns 1 on success, 0 on failure.
//
//export DZSetDownloadDir
func DZSetDownloadDir(path *C.char) C.int { return ok(config.SaveDownloadDir(C.GoString(path))) }

// DZMediaCacheMB returns the on-disk raw-stream cache budget in megabytes
// (media.json; 0 = cache disabled, the default).
//
//export DZMediaCacheMB
func DZMediaCacheMB() C.int { return C.int(config.LoadMedia().MediaCacheMB) }

// DZSetMediaCacheMB saves the cache budget in MB and applies it: a lower budget evicts
// the least recent tracks, <= 0 removes all unpinned tracks and stops caching. Pinned
// tracks are outside the budget. 1 on success, 0 on failure.
//
//export DZSetMediaCacheMB
func DZSetMediaCacheMB(mb C.int) C.int { return ok(engine.Caches().SetMB(int(mb))) }

// DZMediaCacheMaxAgeDays returns how many days a cached track may go
// unplayed before it is removed (media.json; 0 = kept at any age, the default).
//
//export DZMediaCacheMaxAgeDays
func DZMediaCacheMaxAgeDays() C.int { return C.int(config.LoadMedia().MediaCacheMaxAgeDays) }

// DZSetMediaCacheMaxAgeDays saves that age (<= 0: no limit) and applies it: tracks not
// played or downloaded for longer are removed at once, then at most hourly during
// playback. 1 on success, 0 on failure.
//
//export DZSetMediaCacheMaxAgeDays
func DZSetMediaCacheMaxAgeDays(days C.int) C.int {
	return ok(engine.Caches().SetMaxAgeDays(int(days)))
}

// DZMediaCacheStatsJSON returns {"entries","bytes","pinnedBytes","maxBytes","dir"}:
// bytes includes pinned tracks, maxBytes 0 means off, dir is the chosen folder (the
// cache is its opendeezer-cache subfolder). Release with DZFree.
//
//export DZMediaCacheStatsJSON
func DZMediaCacheStatsJSON() *C.char { return jsonStr(engine.Caches().Stats(), nil) }

// DZClearMediaCache removes every unpinned cached track; includePinned also removes
// the offline tracks with every account's pins. 1 on success, 0 on failure.
//
//export DZClearMediaCache
func DZClearMediaCache(includePinned C.int) C.int { return ok(engine.ClearCache(includePinned != 0)) }

// DZMediaCacheDir returns the media cache folder (default <user cache dir>\opendeezer,
// "" when unknown); the cache is its opendeezer-cache subfolder. Release with DZFree.
//
//export DZMediaCacheDir
func DZMediaCacheDir() *C.char {
	dir, _ := config.LoadMedia().CacheDir()
	return C.CString(dir)
}

// DZSetMediaCacheDir moves the cache to dir\opendeezer-cache (dir absolute, "" =
// default), copying pinned tracks first; on any error the old folder stays in use.
// Returns {"error":""} or {"error":"reason"}. Release with DZFree.
//
//export DZSetMediaCacheDir
func DZSetMediaCacheDir(dir *C.char) *C.char {
	return errorReply(engine.SetMediaCacheDir(C.GoString(dir)))
}

// DZIsPreview returns 1 when the current track is Deezer's 30-second preview
// (the free-account fallback) rather than the full stream, else 0.
//
//export DZIsPreview
func DZIsPreview() C.int { return cBool(engine.IsPreview()) }

// DZSetAdsDisabled turns play reporting (gw log.listen) off (1) or on (0) and saves
// it. Reporting credits artists and drives free-tier ad accounting; disabling it is
// at the user's own risk.
//
//export DZSetAdsDisabled
func DZSetAdsDisabled(disabled C.int) C.int {
	off := disabled != 0
	if c := engine.Client(); c != nil {
		c.SetAdsDisabled(off)
	}
	return ok(config.SaveAdsDisabled(off))
}

// DZAdsDisabled returns 1 when play reporting is turned off.
//
//export DZAdsDisabled
func DZAdsDisabled() C.int {
	if c := engine.Client(); c != nil {
		return cBool(c.AdsDisabled())
	}
	return cBool(config.LoadAdsDisabled())
}

// ---- account / browse / lyrics / loudness ----

// DZAccountJSON returns the plan and entitlements as {userId,name,offer,canHq,canHifi,
// loggedIn}, so GUIs can show the tier and why a quality is unavailable.
//
//export DZAccountJSON
func DZAccountJSON() *C.char {
	return withClient(func(c *deezer.Client) (any, error) { return c.Account(), nil })
}

// DZChartsJSON returns the global top tracks/albums/artists/playlists.
//
//export DZChartsJSON
func DZChartsJSON() *C.char { return C.CString(string(engine.ChartsJSON())) }

// DZHomeJSON returns the Home sections in one call: {topTracks, topAlbums} from the
// charts and the user's {playlists}. A section that fails to load comes back empty.
// Greeting and quick-pick cards are client-side.
//
//export DZHomeJSON
func DZHomeJSON() *C.char { return C.CString(string(engine.HomeJSON())) }

// DZCheckUpdateJSON checks GitHub for a newer OpenDeezer release and returns
// {current, latest, hasUpdate, url, notes}. Network failure -> hasUpdate:false.
//
//export DZCheckUpdateJSON
func DZCheckUpdateJSON() *C.char {
	info, _ := update.Check(version.Number)
	return jsonStr(info, nil)
}

// DZArtistTopJSON returns an artist's most popular tracks.
//
//export DZArtistTopJSON
func DZArtistTopJSON(id *C.char) *C.char {
	aid := C.GoString(id)
	return withClient(func(c *deezer.Client) (any, error) {
		ts, err := c.ArtistTop(aid)
		return map[string]any{"tracks": bridge.FromTracks(ts)}, err
	})
}

// DZArtistProfileJSON returns an artist profile with top tracks, albums and
// related artists: {artist,top,albums,related}.
//
//export DZArtistProfileJSON
func DZArtistProfileJSON(id *C.char) *C.char {
	aid := C.GoString(id)
	return withClient(func(c *deezer.Client) (any, error) { return bridge.ArtistProfile(c, aid) })
}

// DZTrackInfoJSON returns everything known about one track (bridge.TrackInfo): title,
// artists with roles, album, numbers, BPM, gain, ISRC and credits. Fields that are
// unknown or whose source failed are omitted.
//
//export DZTrackInfoJSON
func DZTrackInfoJSON(id *C.char) *C.char {
	tid := C.GoString(id)
	return withClient(func(c *deezer.Client) (any, error) {
		ti, err := c.TrackInfo(tid)
		if err != nil {
			return nil, err
		}
		return bridge.FromTrackInfo(ti), nil
	})
}

// DZLyricsJSON returns a track's lyrics: {plain, synced:[{timeMs,text}], isSynced}.
//
//export DZLyricsJSON
func DZLyricsJSON(trackID *C.char) *C.char {
	tid := C.GoString(trackID)
	return withClient(func(c *deezer.Client) (any, error) { return bridge.Lyrics(c, tid) })
}

// DZSetReplayGain enables (1) / disables (0) loudness normalization and saves
// it (playback.json).
//
//export DZSetReplayGain
func DZSetReplayGain(on C.int) { engine.SetReplayGain(on != 0) }

// DZReplayGain reports whether ReplayGain is enabled (1/0).
//
//export DZReplayGain
func DZReplayGain() C.int { return cBool(engine.ReplayGain()) }

// ---- library write ops: 1 on success, 0 on failure ----

// clientOp runs fn with the client for a request: 1 on success, 0 on failure,
// in offline mode (refused, not queued; the log says why) and before login.
func clientOp(fn func(c *deezer.Client) error) C.int {
	c, err := engine.Online()
	if err != nil {
		odlog.Warn("%v", err)
		return 0
	}
	return ok(fn(c))
}

//export DZAddFavorite
func DZAddFavorite(trackID *C.char) C.int {
	id := C.GoString(trackID)
	return clientOp(func(c *deezer.Client) error { return c.AddFavoriteTrack(id) })
}

//export DZRemoveFavorite
func DZRemoveFavorite(trackID *C.char) C.int {
	id := C.GoString(trackID)
	return clientOp(func(c *deezer.Client) error { return c.RemoveFavoriteTrack(id) })
}

//export DZAddToPlaylist
func DZAddToPlaylist(playlistID, trackID *C.char) C.int {
	pid, tid := C.GoString(playlistID), C.GoString(trackID)
	return clientOp(func(c *deezer.Client) error { return c.AddToPlaylist(pid, tid) })
}

//export DZRemoveFromPlaylist
func DZRemoveFromPlaylist(playlistID, trackID *C.char) C.int {
	pid, tid := C.GoString(playlistID), C.GoString(trackID)
	return clientOp(func(c *deezer.Client) error { return c.RemoveFromPlaylist(pid, tid) })
}

// DZCreatePlaylist creates a playlist with the tracks of idsJSON (a JSON array
// of ids; "" or "[]" for none) and returns {"id":"..."}. Release with DZFree.
//
//export DZCreatePlaylist
func DZCreatePlaylist(title, idsJSON *C.char) *C.char {
	t, ids := C.GoString(title), C.GoString(idsJSON)
	return withClient(func(c *deezer.Client) (any, error) { return bridge.CreatePlaylist(c, t, ids) })
}

//export DZRenamePlaylist
func DZRenamePlaylist(playlistID, title *C.char) C.int {
	pid, t := C.GoString(playlistID), C.GoString(title)
	return clientOp(func(c *deezer.Client) error { return c.RenamePlaylist(pid, t) })
}

//export DZDeletePlaylist
func DZDeletePlaylist(playlistID *C.char) C.int {
	pid := C.GoString(playlistID)
	return clientOp(func(c *deezer.Client) error { return c.DeletePlaylist(pid) })
}

// DZFlowJSON returns a batch of Flow, or of a mood ("" for Flow itself), as
// {tracks:[...]}. Played with listen context "dynamic_page_user_radio" or
// "dynamic_page_flow_config", the engine refills the queue as it runs low.
//
//export DZFlowJSON
func DZFlowJSON(mood *C.char) *C.char {
	m := C.GoString(mood)
	return withClient(func(c *deezer.Client) (any, error) {
		ts, err := c.Flow(m)
		return map[string]any{"tracks": bridge.FromTracks(ts)}, err
	})
}

// DZTrackMixJSON returns a song mix seeded from a track (seed first) as {tracks:[...]},
// like DZFlowJSON. It fetches locally even when routed to a device. Release with
// DZFree.
//
//export DZTrackMixJSON
func DZTrackMixJSON(id *C.char) *C.char {
	tid := C.GoString(id)
	return withClient(func(c *deezer.Client) (any, error) {
		ts, err := c.TrackMix(tid)
		return map[string]any{"tracks": bridge.FromTracks(ts)}, err
	})
}

// DZFlowMoodsJSON lists Flow's moods: [{"id","title"},...]. Release
// with DZFree.
//
//export DZFlowMoodsJSON
func DZFlowMoodsJSON() *C.char {
	return withClient(func(c *deezer.Client) (any, error) { return c.FlowMoods() })
}

// DZArtistMixJSON returns an "artist radio" mix seeded from an artist:
// {tracks:[...]} in the shared wire shape, as DZFlowJSON.
//
//export DZArtistMixJSON
func DZArtistMixJSON(id *C.char) *C.char {
	aid := C.GoString(id)
	return withClient(func(c *deezer.Client) (any, error) {
		ts, err := c.ArtistMix(aid)
		return map[string]any{"tracks": bridge.FromTracks(ts)}, err
	})
}

// DZHistoryRecentJSON returns the newest n local history entries (n <= 0: all, "[]"
// when none), as /history/recent serves them. "kind" "episode" marks a podcast episode
// (replay with DZPlayEpisode). Release with DZFree.
//
//export DZHistoryRecentJSON
func DZHistoryRecentJSON(n C.int) *C.char {
	entries, err := engine.HistoryRecent(int(n))
	if err != nil {
		return C.CString("[]")
	}
	return jsonStr(entries, nil)
}

// DZHistoryStatsJSON returns {topTracks, topArtists, totalSeconds} over the last
// sinceDays (<= 0: all). Podcast episodes count only in totalSeconds; no history gives
// empty arrays. Release with DZFree.
//
//export DZHistoryStatsJSON
func DZHistoryStatsJSON(sinceDays C.int) *C.char {
	return jsonStr(engine.HistoryStats(int(sinceDays)), nil)
}

// ---- podcasts ----

// DZSearchPodcastsJSON returns {podcasts:[...]} for a query.
//
//export DZSearchPodcastsJSON
func DZSearchPodcastsJSON(q *C.char) *C.char {
	query := C.GoString(q)
	return withClient(func(c *deezer.Client) (any, error) {
		ps, err := c.SearchPodcasts(query)
		if err != nil {
			return nil, err
		}
		return map[string]any{"podcasts": bridge.FromPodcasts(ps)}, nil
	})
}

// DZPodcastEpisodesJSON returns {episodes:[...]} for a show id.
//
//export DZPodcastEpisodesJSON
func DZPodcastEpisodesJSON(podcastID *C.char) *C.char {
	pid := C.GoString(podcastID)
	return withClient(func(c *deezer.Client) (any, error) {
		es, err := c.PodcastEpisodes(pid)
		if err != nil {
			return nil, err
		}
		return map[string]any{"episodes": bridge.FromEpisodes(es)}, nil
	})
}

// ---- audio: devices, gapless, crossfade, preload ----

// DZAudioDevicesJSON returns the available output devices:
// {devices:[{id,name,isDefault}]}. id "" is the system default.
//
//export DZAudioDevicesJSON
func DZAudioDevicesJSON() *C.char {
	p := engine.Player()
	if p == nil {
		return jsonStr(nil, errNotReady)
	}
	ds, err := p.Devices()
	if err != nil {
		return jsonStr(nil, err)
	}
	type jDev struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		IsDefault bool   `json:"isDefault"`
	}
	out := make([]jDev, len(ds))
	for i, d := range ds {
		out[i] = jDev{ID: d.ID, Name: d.Name, IsDefault: d.IsDefault}
	}
	return jsonStr(map[string]any{"devices": out}, nil)
}

// DZSetAudioDevice switches output to the given device id ("" = default).
//
//export DZSetAudioDevice
func DZSetAudioDevice(id *C.char) C.int {
	p := engine.Player()
	if p == nil {
		return 0
	}
	return ok(p.SetDevice(C.GoString(id)))
}

// DZCurrentAudioDevice returns the selected device id ("" = default).
//
//export DZCurrentAudioDevice
func DZCurrentAudioDevice() *C.char {
	if p := engine.Player(); p != nil {
		return C.CString(p.CurrentDevice())
	}
	return C.CString("")
}

// DZSetGapless turns gapless playback on (1) or off (0) and saves it
// (playback.json).
//
//export DZSetGapless
func DZSetGapless(on C.int) { engine.SetGapless(on != 0) }

// DZGapless returns 1 while gapless playback is on.
//
//export DZGapless
func DZGapless() C.int { return cBool(engine.Gapless()) }

// DZSetSingleDevice turns single-device playback on (1, default) or off (0) and saves
// it: when on, playback pauses as another session of the account starts a track, and
// local starts are announced so those sessions pause.
//
//export DZSetSingleDevice
func DZSetSingleDevice(on C.int) { engine.SetSingleDevice(on != 0) }

// DZSessionExpired reports (1) that Deezer refused the signed-in account's ARL
// since it signed in: the host asks for a new sign-in. 0 otherwise.
//
//export DZSessionExpired
func DZSessionExpired() C.int { return cBool(engine.SessionExpired()) }

// DZSetListenContext sets the page type ("playlist_page", "album_page", ...) and id
// that Deezer's listen log gets for every start until the next call; "" for both
// clears it.
//
//export DZSetListenContext
func DZSetListenContext(pageType, id *C.char) {
	engine.SetListenContext(C.GoString(pageType), C.GoString(id))
}

// DZSingleDevice returns 1 while single-device playback is on.
//
//export DZSingleDevice
func DZSingleDevice() C.int { return cBool(engine.SingleDevice()) }

// DZSetCrossfadeMS sets the crossfade length in ms (0 = off) and saves it
// (playback.json).
//
//export DZSetCrossfadeMS
func DZSetCrossfadeMS(ms C.int) { engine.SetCrossfadeMS(int(ms)) }

// DZCrossfadeMS returns the crossfade length in ms.
//
//export DZCrossfadeMS
func DZCrossfadeMS() C.int { return C.int(engine.CrossfadeMS()) }

// DZSetSleepTimer pauses after `minutes` with a fade-out, or at the end of the track
// when endOfTrack != 0. minutes <= 0 with endOfTrack == 0 cancels.
//
//export DZSetSleepTimer
func DZSetSleepTimer(minutes C.int, endOfTrack C.int) {
	engine.SetSleepTimer(int(minutes), endOfTrack != 0)
}

// DZCancelSleepTimer disarms the sleep timer.
//
//export DZCancelSleepTimer
func DZCancelSleepTimer() { engine.CancelSleepTimer() }

// DZSleepTimerActive returns 1 if a sleep timer is armed, else 0.
//
//export DZSleepTimerActive
func DZSleepTimerActive() C.int {
	p := engine.Player()
	return cBool(p != nil && p.SleepActive())
}

// DZSleepTimerEndOfTrack returns 1 if the armed timer is end-of-track mode.
//
//export DZSleepTimerEndOfTrack
func DZSleepTimerEndOfTrack() C.int {
	p := engine.Player()
	return cBool(p != nil && p.SleepEndOfTrack())
}

// DZSleepTimerRemainingMS returns milliseconds until the timer fires (0 if none).
//
//export DZSleepTimerRemainingMS
func DZSleepTimerRemainingMS() C.longlong {
	if p := engine.Player(); p != nil {
		return C.longlong(p.SleepRemainingMS())
	}
	return 0
}

// DZPreload records the track to follow the current one (gapless or crossfade); the
// engine fetches it once max(30 s, crossfade + 20 s) remain. A new request replaces it;
// no-op when routed to a Connect device.
//
//export DZPreload
func DZPreload(trackID *C.char, durationMS C.longlong) {
	engine.Preload(C.GoString(trackID), int64(durationMS))
}

// DZPlayEpisode plays a podcast episode (unencrypted stream), setting it as the current
// track at once and adding title and artwork from REST /episode in the background.
// Returns 1 started, 0 failed, 2 superseded, like DZPlay.
//
//export DZPlayEpisode
func DZPlayEpisode(episodeID *C.char, durationMS C.longlong) C.int {
	return C.int(engine.PlayEpisode(C.GoString(episodeID), int64(durationMS)))
}

// ---- equalizer + mono downmix ----

// DZEQJSON returns the equalizer state:
// {enabled,mono,preampDb,gainsDb:[10],preset,bands:[10],presets:[...]}.
//
//export DZEQJSON
func DZEQJSON() *C.char {
	st, _ := engine.EQ()
	return jsonStr(st, nil)
}

// DZSetEQJSON applies a partial EQ update with optional keys enabled, mono, preampDb,
// gainsDb ([10]), preset, band ({"index","gainDb"}). 1 on success, 0 when a key fails.
//
//export DZSetEQJSON
func DZSetEQJSON(js *C.char) C.int { return cBool(engine.SetEQJSON(C.GoString(js))) }

// DZClearPreload discards a preloaded next track; call it when the upcoming track
// changes (shuffle or repeat toggled) so a stale preload is not swapped in.
//
//export DZClearPreload
func DZClearPreload() { engine.ClearPreload() }

// ---- engine queue sync (GUI queue -> engine) ----

// DZQueueSet replaces the engine queue with the GUI's (a JSON array of wire tracks,
// "[]" clears) and resets the cursor to 0. Once the cursor matches the playing track
// the engine owns auto-advance. 1 on success, 0 on a parse error.
//
//export DZQueueSet
func DZQueueSet(js *C.char) C.int { return ok(engine.QueueSet(C.GoString(js))) }

// DZQueueAppend adds a JSON array of tracks to the end of the engine queue; the cursor
// stays put and DZQueueVersion bumps when anything was added. 1 on success, 0 on a
// parse error (queue unchanged).
//
//export DZQueueAppend
func DZQueueAppend(js *C.char) C.int { return ok(engine.QueueAppend(C.GoString(js))) }

// DZQueueSetIndex moves the engine queue's cursor to i (clamped). Call it when the GUI
// changes the playing row so the engine can own auto-advance.
//
//export DZQueueSetIndex
func DZQueueSetIndex(i C.int) { engine.QueueSetIndex(int(i)) }

// DZQueueIndex returns the engine queue's cursor (-1 when empty), so a GUI can resync
// after an engine-driven advance.
//
//export DZQueueIndex
func DZQueueIndex() C.int { return C.int(engine.QueueIndex()) }

// DZQueueVersion returns a counter bumped on every engine-queue content change (not on
// cursor moves), so a GUI pulls DZQueueJSON only after a remote edit.
//
//export DZQueueVersion
func DZQueueVersion() C.longlong { return C.longlong(engine.QueueVersion()) }

// DZQueueJSON returns the engine queue as {"version":N,"index":I,"tracks":[...]}.
// Release with DZFree.
//
//export DZQueueJSON
func DZQueueJSON() *C.char {
	ts, idx, ver := engine.QueueSnapshot()
	return jsonStr(map[string]any{
		"version": ver,
		"index":   idx,
		"tracks":  bridge.FromTracks(ts),
	}, nil)
}

// ---- engine queue edits (GUI Up-Next) ----

// DZQueueRemove removes the track at index i from the engine queue (not the playing
// row); when routed it forwards to the Connect device. Resync via DZQueueVersion.
//
//export DZQueueRemove
func DZQueueRemove(i C.int) { _ = engine.QueueRemove(int(i)) }

// DZQueueMove moves the track at `from` to `to`; cursor, history and shuffle cycle
// follow it. Forwards to a routed Connect device and bumps the version.
//
//export DZQueueMove
func DZQueueMove(from, to C.int) { _ = engine.QueueMove(int(from), int(to)) }

// DZQueueInsertNext inserts one wire track or a JSON array of them right after the
// playing row, drops the preloaded next and bumps the version.
//
//export DZQueueInsertNext
func DZQueueInsertNext(js *C.char) { engine.QueueInsertNext(C.GoString(js)) }
