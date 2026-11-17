package main

// Offline music exports: pins of tracks, albums and playlists kept in the
// media cache for play with no network, their background fetch, and offline
// mode. Every returned string is released with DZFree.

/*
#include <stdlib.h>
*/
import "C"

import "github.com/Cycl0o0/OpenDeezer/v3/internal/engine"

// DZPinOffline pins kind ("track", "album" or "playlist"; "episode" is
// refused) and id for the signed-in account and returns {"error":""} when the
// pin was taken, else {"error":"reason"}. The pin is recorded at once (an
// album's or a playlist's track list is fetched first; in offline mode it
// must be stored already) and its tracks are fetched in the background,
// also with a cache size of 0.
//
//export DZPinOffline
func DZPinOffline(kind, id *C.char) *C.char {
	return errorReply(engine.PinOffline(C.GoString(kind), C.GoString(id)))
}

// DZUnpinOffline removes the account's pin of kind and id and stops its fetch:
// {"error":""} or {"error":"reason"}. Kind "track" with id "" removes every
// single-track pin of the account. A track also pinned through a collection
// stays pinned through it.
//
//export DZUnpinOffline
func DZUnpinOffline(kind, id *C.char) *C.char {
	return errorReply(engine.UnpinOffline(C.GoString(kind), C.GoString(id)))
}

// DZOfflineProgressJSON returns the account's fetches:
// {"active":bool,"jobs":[{"kind","id","done","total","failed","error"}],"error":""}.
// A job is listed while it runs; one that stopped (a full disk, a cancel)
// stays with its reason until the next pin, unpin or cancel. active is true
// while any job runs.
//
//export DZOfflineProgressJSON
func DZOfflineProgressJSON() *C.char {
	active, jobs := engine.OfflineProgress()
	return jsonStr(map[string]any{"active": active, "jobs": jobs, "error": ""}, nil)
}

// DZCancelOffline stops the account's fetches.
//
//export DZCancelOffline
func DZCancelOffline() { engine.CancelOffline() }

// DZOfflineCollectionsJSON returns the account's pins (offline: the last
// account that signed in), newest first: {"collections":[{"kind","id","title",
// "cover","trackCount","fetched","pinnedAt"}],"error":""}. All single-track
// pins are one entry of kind "track" with id "" (its cover the newest
// track's). cover is an absolute local file once stored, else the CDN URL;
// pinnedAt is Unix ms.
//
//export DZOfflineCollectionsJSON
func DZOfflineCollectionsJSON() *C.char {
	return jsonStr(map[string]any{"collections": engine.OfflineCollections(), "error": ""}, nil)
}

// DZOfflineTracksJSON returns the tracks of the account's pin of kind and id,
// {"tracks":[...],"error":""} in DZPlaylistTracksJSON's shape, in the pin's
// order (kind "track", id "": the single-track pins, newest first), each
// track's artwork a local file once stored. Works with no network.
//
//export DZOfflineTracksJSON
func DZOfflineTracksJSON(kind, id *C.char) *C.char {
	ts, err := engine.OfflineTracks(C.GoString(kind), C.GoString(id))
	if err != nil {
		return jsonStr(nil, err)
	}
	return jsonStr(map[string]any{"tracks": ts, "error": ""}, nil)
}

// DZOfflineIDsJSON returns {"ids":[...],"error":""}: every track the account
// can play offline (pinned directly or through a collection, and fetched).
//
//export DZOfflineIDsJSON
func DZOfflineIDsJSON() *C.char {
	return jsonStr(map[string]any{"ids": engine.OfflineIDs(), "error": ""}, nil)
}

// DZSetOfflineMode turns offline mode on (1) or off (0). In it nothing is
// requested: only pinned or cached tracks play (DZPlay of another fails at
// once with the reason in DZLastErrorJSON), and browsing, likes, playlist
// edits and listen reports are refused. DZInit turns it on when the login
// fails for want of a network (DZLoginErrorKind 2), and a later successful
// DZInit turns that off again (not a mode set here).
//
//export DZSetOfflineMode
func DZSetOfflineMode(on C.int) { engine.SetOfflineMode(on != 0) }

// DZOfflineMode returns 1 while offline mode is on, else 0.
//
//export DZOfflineMode
func DZOfflineMode() C.int { return cBool(engine.OfflineMode()) }
