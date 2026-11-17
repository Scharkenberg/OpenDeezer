package main

// OpenDeezer Connect exports: discover other OpenDeezer instances on the LAN
// and route this client's playback to a chosen one. While a device is
// connected the playback exports forward to it.

/*
#include <stdlib.h>
*/
import "C"

import (
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/engine"
)

// DZSetClientInfo overrides the client id + device label this instance reports
// over discovery and /whoami (e.g. "macos", "OpenDeezer for macOS"). Call it
// BEFORE DZInit; it has no effect afterwards.
//
//export DZSetClientInfo
func DZSetClientInfo(client, device *C.char) {
	engine.SetClientInfo(C.GoString(client), C.GoString(device))
}

// DZNowPlayingJSON returns the track actually playing right now as a wire
// track: when routed to a device it is the remote's current track (so the
// controller's now-playing stays in sync); otherwise the local current track
// (which also reflects tracks started via the control API). "{}" when nothing
// is playing.
//
//export DZNowPlayingJSON
func DZNowPlayingJSON() *C.char { return C.CString(engine.NowPlayingJSON()) }

// DZNowPlayingVersion returns a counter that moves exactly when the payload of
// DZNowPlayingJSON changes (a new track, its metadata arriving, a Connect
// device's track), so a host fetches DZNowPlayingJSON only then.
//
//export DZNowPlayingVersion
func DZNowPlayingVersion() C.longlong { return C.longlong(engine.NowPlayingVersion()) }

// DZDiscoverDevices broadcasts a LAN probe and returns the OpenDeezer devices
// found, as a JSON array of {name, addr}. timeoutMS bounds the whole wait (~600ms
// is a good default): the configured peers are probed alongside the broadcast.
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

// DZDisconnectDevice returns control to local playback. It stops the remote
// device (so it doesn't keep playing unattended) before clearing the connection.
//
//export DZDisconnectDevice
func DZDisconnectDevice() { engine.DisconnectDevice() }

// DZConnectedDevice returns the connected device's address ("" if local).
//
//export DZConnectedDevice
func DZConnectedDevice() *C.char { return C.CString(engine.ConnectedDevice()) }
