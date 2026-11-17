// Command deezercore exposes the OpenDeezer engine (internal/engine: login,
// browse, decrypt + decode + playback, queue, control API, Connect) as a
// C-callable library so native GUIs can drive it in-process. This package only
// converts arguments and results.
//
// Build:
//
//	CGO_ENABLED=1 go build -buildmode=c-archive -o libdeezercore.a ./corelib
//
// which also emits libdeezercore.h. All list/search calls return a malloc'd
// JSON string the caller must release with DZFree.
package main

/*
#include <stdlib.h>
#include <string.h>
*/
import "C"

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
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

// jsonStr marshals v (or an {"error":...} object) to a malloc'd C string.
func jsonStr(v any, err error) *C.char {
	if err != nil {
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		return C.CString(string(b))
	}
	b, e := json.Marshal(v)
	if e != nil {
		eb, _ := json.Marshal(map[string]string{"error": e.Error()})
		return C.CString(string(eb))
	}
	return C.CString(string(b))
}

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

// withClient runs fn with the client for a request and returns its JSON, or
// {"error":"..."} saying why there is none (offline mode, no login).
func withClient(fn func(c *deezer.Client) (any, error)) *C.char {
	c, err := engine.Online()
	if err != nil {
		return jsonStr(nil, err)
	}
	return jsonStr(fn(c))
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

// DZFetch downloads raw bytes (e.g. cover art) so GTK/Qt frontends don't need
// their own HTTP stack. Returns a malloc'd buffer (free with DZFreeBytes) and
// writes its length to outLen; returns NULL on error.
//
//export DZFetch
func DZFetch(url *C.char, outLen *C.int) *C.uchar {
	*outLen = 0
	resp, err := deezer.HTTPClient(15 * time.Second).Get(C.GoString(url))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	// Bounded: the length goes out as a C int, and artwork is far smaller.
	const maxFetch = 32 << 20
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxFetch+1))
	if err != nil || len(b) > maxFetch {
		return nil
	}
	return cBytes(b, outLen)
}

//export DZFreeBytes
func DZFreeBytes(p *C.uchar) { C.free(unsafe.Pointer(p)) }

// logOnce opens the log file at the first DZInit (a re-login reuses it).
var logOnce sync.Once

// DZInit logs in with the ARL and starts the engine; 1 on success, 0 with the
// reason in DZLoginErrorKind. Another ARL re-logs in, rebuilds the control
// server and drops the Connect route; the same account keeps both. When the
// login fails for want of a network (kind 2) the player, the media cache and
// the offline pins are up anyway and offline mode is on, for the last account
// that signed in: pinned and cached tracks play (DZOfflineCollectionsJSON,
// DZOfflineTracksJSON, DZPlay, DZPreload, DZQueueSet). A later DZInit that
// succeeds turns that mode off.
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

// DZLoginErrorKind returns why the most recent DZInit failed, so the UI can show
// a No-Internet retry screen instead of pushing the user to re-authenticate:
// 0 = ok / logged in, 1 = ARL expired or invalid, 2 = no internet, 3 = other,
// 4 = no audio output device.
//
//export DZLoginErrorKind
func DZLoginErrorKind() C.int { return C.int(engine.LoginErrorKind()) }

//export DZLastErrorJSON
func DZLastErrorJSON() *C.char {
	msg := ""
	if p := engine.Player(); p != nil {
		msg = p.LastError()
	}
	return jsonStr(map[string]string{"error": msg}, nil)
}

// DZSetQuality sets and saves the stream quality level: 0=Normal(MP3 128),
// 1=High(MP3 320), 2=HiFi(FLAC, falls back to MP3 if the account/track isn't
// entitled). The engine keeps it in playback.json (shared with the TUI) and
// applies it at each DZInit; also before DZInit.
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

//export DZFavoritesJSON
func DZFavoritesJSON() *C.char {
	return withClient(func(c *deezer.Client) (any, error) {
		ts, err := c.Favorites()
		return map[string]any{"tracks": bridge.FromTracks(ts)}, err
	})
}

// DZFavoriteIDsJSON returns the account's liked-track ids as a JSON array of
// strings (e.g. ["123","456"]). It backs truthful heart/like state across the
// desktop GUIs without shipping every track's full metadata. Derived from the
// engine's existing favorites fetch. "[]" when there are none. Release with
// DZFree.
//
//export DZFavoriteIDsJSON
func DZFavoriteIDsJSON() *C.char {
	return withClient(func(c *deezer.Client) (any, error) {
		ts, err := c.Favorites()
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(ts))
		for _, t := range ts {
			if t.ID != "" {
				ids = append(ids, t.ID)
			}
		}
		return ids, nil
	})
}

//export DZPlaylistsJSON
func DZPlaylistsJSON() *C.char {
	return withClient(func(c *deezer.Client) (any, error) {
		ps, err := c.Playlists()
		return map[string]any{"playlists": bridge.FromPlaylists(ps)}, err
	})
}

// DZPlaylistTracksJSON lists a playlist's tracks: {"tracks":[...]}. A playlist
// pinned for offline use is listed whole and kept in step: new tracks are
// pinned and fetched, removed ones unpinned.
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

//export DZSearchJSON
func DZSearchJSON(q *C.char) *C.char {
	query := C.GoString(q)
	return withClient(func(c *deezer.Client) (any, error) {
		r, err := c.Search(query)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"tracks": bridge.FromTracks(r.Tracks), "albums": bridge.FromAlbums(r.Albums),
			"artists": bridge.FromArtistInfos(r.Artists), "playlists": bridge.FromPlaylists(r.Playlists),
		}, nil
	})
}

// DZPlay resolves and plays a track (on the Connect device when one is
// selected); the old stream stays audible until the new one starts. 1 when
// it started, 0 on failure with the reason in DZLastErrorJSON, 2 when a newer
// play (the host's or a remote's) took the player before this one started
// (not a failure; no error is set).
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

// DZSetRepeat sets the repeat mode (mode: 0=off, 1=all, 2=one). The mode is
// always recorded on the engine-side queue — /status reports it and the engine
// queue honors it when it advances. It is deliberately NOT forwarded to a
// connected remote device: this instance is the controller and owns the queue,
// streaming the host one track at a time. Forwarding Repeat-All would make the
// host loop its single-item queue forever and never accept the next track —
// trapping playback. Repeat is applied locally so the engine's own auto-advance
// (which drives the remote via /play) honors it.
//
//export DZSetRepeat
func DZSetRepeat(mode C.int) { engine.SetRepeat(int(mode)) }

// DZSetShuffle sets shuffle on (1) or off (0). The flag is always recorded on
// the engine-side queue — /status reports it and the engine queue honors it
// when it advances — and it is forwarded to the connected remote device when
// one is selected.
//
//export DZSetShuffle
func DZSetShuffle(on C.int) { engine.SetShuffle(on != 0) }

// DZSetShuffleHostOwned(1) declares that the GUI owns play order: it shuffles
// its own queue and syncs that order (DZQueueSet), so DZSetShuffle and a
// controller's shuffle only record the flag for /status, DZGetShuffle and
// notifications, without reordering the engine queue. The GUI follows a
// controller's change by polling DZGetShuffle. Call before DZInit.
//
//export DZSetShuffleHostOwned
func DZSetShuffleHostOwned(on C.int) { engine.SetShuffleHostOwned(on != 0) }

// DZGetRepeat returns the engine queue's repeat mode as "off", "all" or "one",
// also while routed to a Connect device (repeat is never forwarded). The
// result must be released with DZFree.
//
//export DZGetRepeat
func DZGetRepeat() *C.char { return C.CString(engine.Repeat()) }

// DZGetShuffle returns 1 when shuffle is on, else 0. Routed to a Connect device
// it reflects the routed snapshot; otherwise the engine queue.
//
//export DZGetShuffle
func DZGetShuffle() C.int { return cBool(engine.Shuffle()) }

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

// DZFinishedCount returns a monotonically increasing count of tracks that ended
// naturally and that the engine queue did not advance itself; native UIs poll
// it to drive auto-advance.
//
//export DZFinishedCount
func DZFinishedCount() C.int { return C.int(engine.FinishedCount()) }

// ---- downloads (premium-only, full track to disk) ----

// DZDownloadTrack downloads trackID to a file in destDir and returns JSON
// {"path":"..."} on success or {"error":"..."} on failure. Pass "" for destDir
// to use the shared default folder (DZDownloadDir). The filename is derived from
// the track title/artist and the resolved format's extension. Blocking — GUIs
// should call it off the UI thread (as they already do for DZPlay). The result
// string must be released with DZFree. Downloads are premium-only; a free
// account gets {"error":"downloads require a paid Deezer plan"}.
//
//export DZDownloadTrack
func DZDownloadTrack(trackID, destDir *C.char) *C.char {
	path, err := engine.DownloadTrack(C.GoString(trackID), C.GoString(destDir))
	if err != nil {
		return jsonStr(nil, err)
	}
	return jsonStr(map[string]string{"path": path}, nil)
}

// DZDownloadAlbum downloads every track of albumID to the shared download folder
// (DZDownloadDir), skipping tracks already saved there, and returns the batch
// summary
// {"saved":N,"failed":N,"dir":"...","error":""}. Blocking and premium-only, the
// same gate + folder DZDownloadTrack uses (GUIs already call the single-track
// download off the UI thread). On a partial download the per-track failures are
// counted in "failed" and "error" carries the batch message; a free account
// yields saved:0,failed:0 with the premium error. Release with DZFree.
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

// DZSetMediaCacheMB persists the raw-stream cache budget in megabytes and
// applies it at once: a lower budget evicts the least recent tracks, 0 (or a
// negative value) removes every cached track not pinned, also from a folder
// not opened in this session, and stops caching. Pinned tracks are outside the
// budget. Returns 1 on success, 0 on failure.
//
//export DZSetMediaCacheMB
func DZSetMediaCacheMB(mb C.int) C.int { return ok(engine.Caches().SetMB(int(mb))) }

// DZMediaCacheMaxAgeDays returns how many days a cached track may go
// unplayed before it is removed (media.json; 0 = kept at any age, the default).
//
//export DZMediaCacheMaxAgeDays
func DZMediaCacheMaxAgeDays() C.int { return C.int(config.LoadMedia().MediaCacheMaxAgeDays) }

// DZSetMediaCacheMaxAgeDays persists that age (0 or a negative value keeps
// tracks at any age) and applies it at once: cached tracks not played or
// downloaded for longer are removed now, and then at most once an hour while
// tracks play. Returns 1 on success, 0 on failure.
//
//export DZSetMediaCacheMaxAgeDays
func DZSetMediaCacheMaxAgeDays(days C.int) C.int {
	return ok(engine.Caches().SetMaxAgeDays(int(days)))
}

// DZMediaCacheStatsJSON returns the media cache's state:
// {"entries":int,"bytes":int64,"pinnedBytes":int64,"maxBytes":int64,"dir":string}.
// bytes counts every stored track, pinned ones included, pinnedBytes those
// kept for offline use; maxBytes is the budget (0 = off); dir is the folder
// picked (the cache lives in its subfolder opendeezer-cache), also while
// off. Release with DZFree.
//
//export DZMediaCacheStatsJSON
func DZMediaCacheStatsJSON() *C.char { return jsonStr(engine.Caches().Stats(), nil) }

// DZClearMediaCache removes every cached track not pinned. includePinned also
// removes the tracks kept for offline use, with the pins of every account.
// Returns 1 on success, 0 on failure.
//
//export DZClearMediaCache
func DZClearMediaCache(includePinned C.int) C.int { return ok(engine.ClearCache(includePinned != 0)) }

// DZMediaCacheDir returns the folder picked for the media cache, by default
// <user cache dir>\opendeezer (%LOCALAPPDATA%\opendeezer on Windows; "" when
// that is unknown); the cache lives in its subfolder opendeezer-cache.
// Release with DZFree.
//
//export DZMediaCacheDir
func DZMediaCacheDir() *C.char {
	dir, _ := config.LoadMedia().CacheDir()
	return C.CString(dir)
}

// DZSetMediaCacheDir moves the media cache to the subfolder opendeezer-cache
// of dir, an absolute path ("" = the default folder), and returns
// {"error":""} or {"error":"reason"}. The pinned tracks are copied there,
// then the old cache is cleared; offline fetches stop for the move and go on
// in the new folder. A relative path or a file is refused; on any error (a
// copy too) the old folder stays in use. Release with DZFree.
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

// DZSetAdsDisabled turns Deezer Free's play-reporting/ads off (1) or on (0) and
// persists the choice. FREE accounts only: reporting plays (log.listen) credits
// artists and drives the ad schedule; disabling it is ad-free but stops
// reporting — the user's at-own-risk choice. Paid accounts are unaffected.
//
//export DZSetAdsDisabled
func DZSetAdsDisabled(disabled C.int) C.int {
	off := disabled != 0
	if c := engine.Client(); c != nil {
		c.SetAdsDisabled(off)
	}
	return ok(config.SaveAdsDisabled(off))
}

// DZAdsDisabled returns 1 when the free-tier ads/play-reporting opt-out is on.
//
//export DZAdsDisabled
func DZAdsDisabled() C.int {
	if c := engine.Client(); c != nil {
		return cBool(c.AdsDisabled())
	}
	return cBool(config.LoadAdsDisabled())
}

// ---- account / browse / lyrics / loudness ----

// DZAccountJSON returns the logged-in plan + entitlements as JSON
// {userId,name,offer,canHq,canHifi,loggedIn} so GUIs can show the tier and
// explain why a quality tier is unavailable.
//
//export DZAccountJSON
func DZAccountJSON() *C.char {
	return withClient(func(c *deezer.Client) (any, error) { return c.Account(), nil })
}

// DZChartsJSON returns the global top tracks/albums/artists/playlists.
//
//export DZChartsJSON
func DZChartsJSON() *C.char {
	return withClient(func(c *deezer.Client) (any, error) {
		ch, err := c.Charts("0")
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"tracks":    bridge.FromTracks(ch.Tracks),
			"albums":    bridge.FromAlbums(ch.Albums),
			"artists":   bridge.FromArtistInfos(ch.Artists),
			"playlists": bridge.FromPlaylists(ch.Playlists),
		}, nil
	})
}

// DZHomeJSON aggregates the Home-screen sections in one call so every GUI shows
// the same landing page: {topTracks, topAlbums} from the charts and the user's
// own {playlists}. Best-effort — a section that fails to load comes back empty
// rather than failing the whole page. The greeting + quick-pick cards
// (Liked/Flow/Charts/Podcasts) are client-side.
//
//export DZHomeJSON
func DZHomeJSON() *C.char {
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
	return withClient(func(c *deezer.Client) (any, error) {
		pg, err := c.ArtistProfile(aid)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"artist":  bridge.FromArtistInfo(pg.Artist),
			"top":     bridge.FromTracks(pg.Top),
			"albums":  bridge.FromAlbums(pg.Albums),
			"related": bridge.FromArtistInfos(pg.Related),
		}, nil
	})
}

// DZTrackInfoJSON returns everything known about one track for a track
// overview page (bridge.TrackInfo): title, version, artists with roles, album
// (1000x1000 cover, label, genres, track count), numbers, BPM, gain, ISRC and
// the ordered contributor credits. Unknown fields are omitted; a source that
// fails leaves its fields out instead of failing the call.
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
	return withClient(func(c *deezer.Client) (any, error) {
		l, err := c.Lyrics(tid)
		if err != nil {
			return nil, err
		}
		type jLine struct {
			TimeMS int64  `json:"timeMs"`
			Text   string `json:"text"`
		}
		synced := make([]jLine, len(l.Synced))
		for i, s := range l.Synced {
			synced[i] = jLine{TimeMS: s.TimeMS, Text: s.Text}
		}
		return map[string]any{"plain": l.Plain, "synced": synced, "isSynced": l.IsSynced()}, nil
	})
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

// ---- library write ops — return 1 on success, 0 on failure ----

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

// DZCreatePlaylist creates an empty playlist and returns its id as a JSON
// string {"id":"..."} (or {"error":"..."}).
//
//export DZCreatePlaylist
func DZCreatePlaylist(title *C.char) *C.char {
	t := C.GoString(title)
	return withClient(func(c *deezer.Client) (any, error) {
		id, err := c.CreatePlaylist(t, nil)
		if err != nil {
			return nil, err
		}
		return map[string]string{"id": id}, nil
	})
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

// DZFlowJSON returns the user's Flow personalized stream: {tracks:[...]}.
//
//export DZFlowJSON
func DZFlowJSON() *C.char {
	return withClient(func(c *deezer.Client) (any, error) {
		ts, err := c.Flow()
		return map[string]any{"tracks": bridge.FromTracks(ts)}, err
	})
}

// DZTrackMixJSON returns a "song radio" mix seeded from a track (the seed track
// is kept as the first entry): {tracks:[...]} in the shared wire shape, exactly
// like DZFlowJSON so a GUI reuses its Flow parsing. Browse-style (like
// DZFlowJSON, it fetches from the local client regardless of any routed remote —
// only the /play/mix/* engine path forwards to a device). Release with DZFree.
//
//export DZTrackMixJSON
func DZTrackMixJSON(id *C.char) *C.char {
	tid := C.GoString(id)
	return withClient(func(c *deezer.Client) (any, error) {
		ts, err := c.TrackMix(tid)
		return map[string]any{"tracks": bridge.FromTracks(ts)}, err
	})
}

// DZArtistMixJSON returns an "artist radio" mix seeded from an artist:
// {tracks:[...]} in the shared wire shape, mirroring DZFlowJSON.
//
//export DZArtistMixJSON
func DZArtistMixJSON(id *C.char) *C.char {
	aid := C.GoString(id)
	return withClient(func(c *deezer.Client) (any, error) {
		ts, err := c.ArtistMix(aid)
		return map[string]any{"tracks": bridge.FromTracks(ts)}, err
	})
}

// DZHistoryRecentJSON returns the newest n entries of the machine-local
// listening history as a JSON array (newest first, same stable shape the
// control API's /history/recent serves); n <= 0 returns all. Empty/unavailable
// history yields "[]". Release with DZFree.
//
// Each entry is {trackId,title,artist,album?,kind?,startedAt,durationPlayedSec,
// artworkUrl?,durationMs?}. "kind" is omitted or "track" for a song and
// "episode" for a podcast episode; a GUI routes replay on it: episode ->
// DZPlayEpisode(trackId), else DZPlay. durationMs is the track's length. A song
// recorded without artwork or length gets them from the track (looked up once
// per id and process).
//
//export DZHistoryRecentJSON
func DZHistoryRecentJSON(n C.int) *C.char {
	entries, err := engine.HistoryRecent(int(n))
	if err != nil {
		return C.CString("[]")
	}
	return jsonStr(entries, nil)
}

// DZHistoryStatsJSON returns local listening stats over the last sinceDays
// (sinceDays <= 0 = all history) as
// {topTracks:[{trackId,title,artist,plays,totalSec}],
//
//	topArtists:[{artist,plays,totalSec}], totalSeconds:N}. Empty/unavailable
//
// history yields the same shape with empty arrays and totalSeconds:0. topTracks
// and topArtists are music-only (podcast episodes are excluded so a binged show
// can't pollute the charts); totalSeconds is all listening time. Release with
// DZFree.
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

// DZSetCrossfadeMS sets the crossfade length in ms (0 = off) and saves it
// (playback.json).
//
//export DZSetCrossfadeMS
func DZSetCrossfadeMS(ms C.int) { engine.SetCrossfadeMS(int(ms)) }

// DZCrossfadeMS returns the crossfade length in ms.
//
//export DZCrossfadeMS
func DZCrossfadeMS() C.int { return C.int(engine.CrossfadeMS()) }

// DZSetSleepTimer arms the sleep timer: pause after `minutes` (with a fade-out),
// or when the current track ends if endOfTrack != 0 (minutes ignored). Pass
// minutes <= 0 with endOfTrack == 0 to cancel.
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

// DZPreload asks for a track to follow the current one gaplessly or with a
// crossfade. It only records the request: the engine resolves and downloads
// it once the current track has max(30 s, crossfade + 20 s) left (at once when
// it is shorter or already there). A new request replaces the recorded one; a
// request for the track already recorded or preloaded changes nothing. No-op
// when routed to a Connect device.
//
//export DZPreload
func DZPreload(trackID *C.char, durationMS C.longlong) {
	engine.Preload(C.GoString(trackID), int64(durationMS))
}

// DZPlayEpisode resolves + plays a podcast episode (plain, unencrypted stream).
// Mirrors DZPlay's now-playing pattern: sets the episode as the current track
// immediately (with id + duration), then asynchronously enriches title / podcast
// name / artwork via the REST /episode endpoint. Returns as DZPlay does: 1
// started, 0 failed, 2 superseded by a newer play.
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

// DZSetEQJSON applies a partial EQ update. Recognized keys (all optional):
// enabled (bool), mono (bool), preampDb (number), gainsDb ([10]number),
// preset (string), band ({"index":N,"gainDb":X}). Returns 1 on success, 0 if
// any present key failed to apply (unknown preset, bad band index/length).
//
//export DZSetEQJSON
func DZSetEQJSON(js *C.char) C.int { return cBool(engine.SetEQJSON(C.GoString(js))) }

// DZClearPreload discards a preloaded next track. Call when the upcoming track
// is no longer determined (shuffle/repeat toggled after a preload was armed) so
// a stale preload can't be gaplessly swapped in.
//
//export DZClearPreload
func DZClearPreload() { engine.ClearPreload() }

// ---- engine queue sync (GUI queue -> engine) ----

// DZQueueSet replaces the engine-side playback queue with the GUI's queue so
// remote controllers see it on /status and /next + /prev walk it. js is a JSON
// array of tracks in the same wire shape every list call returns
// ({id,name,durationMs,artistLine,artistId,artists,albumName,artworkUrl,
// explicit}); only id is required, but durationMs should be set so controllers'
// end-of-track detection works. The cursor resets to 0 — follow with
// DZQueueSetIndex to point it at the playing row. Pass "[]" to clear. Once the
// cursor is aligned with the playing track, the ENGINE owns auto-advance for
// natural finishes (DZFinishedCount no longer bumps for queue-owned finishes;
// poll DZQueueIndex / DZNowPlayingJSON instead). GUIs that never call this keep
// today's behavior exactly. Returns 1 on success, 0 on a parse error.
//
//export DZQueueSet
func DZQueueSet(js *C.char) C.int { return ok(engine.QueueSet(C.GoString(js))) }

// DZQueueAppend adds tracks to the end of the engine queue ("Add to queue")
// without resending the queue: js is a JSON array in DZQueueSet's shape. The
// cursor stays where it is; DZQueueVersion bumps when at least one track was
// added. A preloaded next is dropped only when the track after the cursor
// changed. Returns 1 on success ("[]" adds nothing), 0 on a parse error (the
// queue is left as it was).
//
//export DZQueueAppend
func DZQueueAppend(js *C.char) C.int { return ok(engine.QueueAppend(C.GoString(js))) }

// DZQueueSetIndex moves the engine queue's cursor to i (clamped; no-op when the
// queue is empty). Call it whenever the GUI changes the playing row so the
// engine queue tracks what is audible — that alignment is what lets the engine
// own auto-advance and gapless cursor sync for a synced queue.
//
//export DZQueueSetIndex
func DZQueueSetIndex(i C.int) { engine.QueueSetIndex(int(i)) }

// DZQueueIndex returns the engine queue's cursor (-1 when empty/unsynced), so a
// GUI can resync its own cursor after an engine-driven advance (natural finish,
// remote /next|/prev, gapless promote).
//
//export DZQueueIndex
func DZQueueIndex() C.int { return C.int(engine.QueueIndex()) }

// DZQueueVersion returns a counter bumped on every engine-queue CONTENT change
// (DZQueueSet, remote /queue/add|remove|move, /play/album, /play/mix/*) —
// cursor moves don't bump it. A GUI polls this cheaply and pulls DZQueueJSON
// only when a remote controller actually edited the queue.
//
//export DZQueueVersion
func DZQueueVersion() C.longlong { return C.longlong(engine.QueueVersion()) }

// DZQueueJSON returns the engine queue as {"version":N,"index":I,"tracks":[..]}
// (tracks in the shared wire shape), so a GUI can adopt remote queue edits.
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

// DZQueueRemove removes the track at index i from the engine queue. The playing
// row can't be removed (guarded like the control API's /queue/remove — the audio
// would keep playing a track no longer in the queue, desyncing controllers).
// When routed to a Connect device the edit forwards to it. The version bump +
// state notify let a GUI resync its Up-Next via DZQueueVersion/DZQueueJSON.
//
//export DZQueueRemove
func DZQueueRemove(i C.int) { _ = engine.QueueRemove(int(i)) }

// DZQueueMove reorders the engine queue, moving the track at `from` to `to`. The
// cursor, history and shuffle cycle follow the moved track (queue.Move), so what
// is audible never changes; from == to is a no-op. Forwards to a routed Connect
// device. Bumps the version + notifies for GUI resync.
//
//export DZQueueMove
func DZQueueMove(from, to C.int) { _ = engine.QueueMove(int(from), int(to)) }

// DZQueueInsertNext splices tracks into the engine queue right after the playing
// row ("play next"), for the GUI Up-Next editor. js is a single wire track
// object {...} or a JSON array [...] of them (same shape every list call
// returns; only id is required). A preloaded next is dropped (the upcoming track
// changed) and the version bumps + state notifies for GUI/controller resync.
//
//export DZQueueInsertNext
func DZQueueInsertNext(js *C.char) { engine.QueueInsertNext(C.GoString(js)) }
