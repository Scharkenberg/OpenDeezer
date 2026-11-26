package main

// Purple Disc Connect exports: discover other Purple Disc instances on the LAN
// and route this client's playback to a chosen one. While a device is
// connected the playback exports forward to it.

/*
#include <stdlib.h>
*/
import "C"

import (
	"time"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/engine"
)

// DZSetClientInfo overrides the client id and device label this instance reports over
// discovery and /whoami (e.g. "macos", "Purple Disc for macOS"). Call it before DZInit.
//
//export DZSetClientInfo
func DZSetClientInfo(client, device *C.char) {
	engine.SetClientInfo(C.GoString(client), C.GoString(device))
}

// DZNowPlayingJSON returns the playing track as a wire track: the Connect device's
// track when routed, else the local one (control API starts included). "{}" when
// nothing plays.
//
//export DZNowPlayingJSON
func DZNowPlayingJSON() *C.char { return C.CString(engine.NowPlayingJSON()) }

// DZNowPlayingVersion returns a counter that changes exactly when DZNowPlayingJSON's
// payload changes, so a host fetches DZNowPlayingJSON only then.
//
//export DZNowPlayingVersion
func DZNowPlayingVersion() C.longlong { return C.longlong(engine.NowPlayingVersion()) }

// DZElsewhereJSON returns what another session of the account plays, as {"track",
// "sinceMs", "untilMs"} (untilMs: when it counts as ended). No "track" when that
// session played an unnamed track; "{}" when nothing was announced since login.
//
//export DZElsewhereJSON
func DZElsewhereJSON() *C.char { return C.CString(engine.ElsewhereJSON()) }

// DZElsewhereVersion returns a counter that moves exactly when the payload of
// DZElsewhereJSON changes.
//
//export DZElsewhereVersion
func DZElsewhereVersion() C.longlong { return C.longlong(engine.ElsewhereVersion()) }

// DZDiscoverDevices probes the LAN and the configured peers and returns the Purple Disc
// devices found as a JSON array of {name, addr}. timeoutMS bounds the wait (~600ms).
//
//export DZDiscoverDevices
func DZDiscoverDevices(timeoutMS C.int) *C.char {
	ms := int(timeoutMS)
	if ms <= 0 {
		ms = 600
	}
	return jsonStr(engine.DiscoverDevices(time.Duration(ms)*time.Millisecond), nil)
}

// DZConnectDevice routes playback to the device at addr (host:port). Local
// playback is stopped (audio moves to the device). Returns 1 on success.
//
//export DZConnectDevice
func DZConnectDevice(addr *C.char) C.int { return cBool(engine.ConnectDevice(C.GoString(addr))) }

// DZTakeOverJSON finds the Purple Disc host of the account on the LAN that plays trackID and
// returns its queue ({"tracks","index","positionMs","shuffle","repeat"}), for playing it on
// here; "" when no host plays it. Blocks for discovery (under a second) and up to 2 s for each
// host found that does not answer. Free with DZFree.
//
//export DZTakeOverJSON
func DZTakeOverJSON(trackID *C.char) *C.char {
	return C.CString(engine.TakeOverJSON(C.GoString(trackID)))
}

// DZDisconnectDevice returns control to local playback. It stops the remote
// device (so it doesn't keep playing unattended) before clearing the connection.
//
//export DZDisconnectDevice
func DZDisconnectDevice() { engine.DisconnectDevice() }

// DZConnectedDevice returns the connected device's address ("" if local).
//
//export DZConnectedDevice
func DZConnectedDevice() *C.char { return C.CString(engine.ConnectedDevice()) }
