package main

// Offline music exports: pins of tracks, albums and playlists kept in the
// media cache for play with no network, their background fetch, and offline
// mode. Every returned string is released with DZFree.

/*
#include <stdlib.h>
*/
import "C"

import "github.com/Scharkenberg/PurpleDisc/v4/internal/engine"

// DZPinOffline pins kind ("track", "album", "playlist") and id for the account and
// returns {"error":""} or {"error":"reason"}. The pin is recorded at once and its
// tracks are fetched in the background, even with a cache size of 0.
//
//export DZPinOffline
func DZPinOffline(kind, id *C.char) *C.char {
	return errorReply(engine.PinOffline(C.GoString(kind), C.GoString(id)))
}

// DZUnpinOffline removes the account's pin of kind and id and stops its fetch;
// kind "track" with id "" removes every single-track pin. Returns {"error":...}.
//
//export DZUnpinOffline
func DZUnpinOffline(kind, id *C.char) *C.char {
	return errorReply(engine.UnpinOffline(C.GoString(kind), C.GoString(id)))
}

// DZOfflineProgressJSON returns {"active","jobs":[{"kind","id","done","total",
// "failed","error"}],"error"}. A stopped job keeps its reason until the next pin,
// unpin or cancel.
//
//export DZOfflineProgressJSON
func DZOfflineProgressJSON() *C.char {
	active, jobs := engine.OfflineProgress()
	return jsonStr(map[string]any{"active": active, "jobs": jobs, "error": ""}, nil)
}

// DZOfflineCollectionsJSON returns the account's pins newest first as {"collections":
// [{"kind","id","title","cover","trackCount","fetched","pinnedAt"}]}. Single-track
// pins form one entry (kind "track", id ""); pinnedAt is Unix ms.
//
//export DZOfflineCollectionsJSON
func DZOfflineCollectionsJSON() *C.char {
	return jsonStr(map[string]any{"collections": engine.OfflineCollections(), "error": ""}, nil)
}

// DZOfflineTracksJSON returns the tracks of a pin as {"tracks":[...],"error":""}, in
// pin order, with local artwork once stored. Works without a network.
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

// DZSetOfflineMode turns offline mode on (1) or off (0): only pinned or cached tracks
// play and browsing, likes, edits and listen reports are refused. DZInit also turns
// it on when the login fails for lack of a network.
//
//export DZSetOfflineMode
func DZSetOfflineMode(on C.int) { engine.SetOfflineMode(on != 0) }

// DZOfflineMode returns 1 while offline mode is on, else 0.
//
//export DZOfflineMode
func DZOfflineMode() C.int { return cBool(engine.OfflineMode()) }
