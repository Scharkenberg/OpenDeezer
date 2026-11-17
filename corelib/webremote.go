package main

// Web remote C exports: remote-control settings, the phone web remote's
// pairing info and the QR code PNG the GUI shows for pairing.

/*
#include <stdlib.h>
*/
import "C"

import (
	"github.com/Cycl0o0/OpenDeezer/v3/internal/engine"
)

// DZControlConfigJSON returns the current remote-control settings for the
// Settings UI: {enabled, addr, token, lan, running}. Lets a GUI show the same
// values env vars / config files provide, so they become editable in-app.
//
//export DZControlConfigJSON
func DZControlConfigJSON() *C.char { return jsonStr(engine.ControlConfig(), nil) }

// DZSetControlConfig persists the remote-control settings to the config files the
// engine reads at startup (~/.config/opendeezer/{control.txt,control-token.txt})
// AND applies them live: (re)starts the control server when enabled (LAN binds
// require same-account auth when no token is set), stops it when disabled.
//
//export DZSetControlConfig
func DZSetControlConfig(enabled C.int, addr *C.char, token *C.char) {
	engine.SetControlConfig(enabled != 0, C.GoString(addr), C.GoString(token))
}

// DZWebRemoteSetEnabled enables (on!=0) or disables (on==0) the phone web
// remote. Enabling starts (or rebinds) the control server on a LAN-reachable
// address (0.0.0.0:<port>) with pairing active, so any phone on the same network
// can scan the QR and connect. Disabling ends pairing and the paired sessions
// and restores the server the remote replaced (or none), so the LAN bind and
// its advertiser go away with it. Off by default.
//
//export DZWebRemoteSetEnabled
func DZWebRemoteSetEnabled(on C.int) { engine.WebRemoteSetEnabled(on != 0) }

// DZWebRemoteInfoJSON returns a malloc'd JSON string (free with DZFree):
//
//	{"enabled":bool,"code":"123456","url":"http://<lanip>:<port>/remote","port":<int>}
//
// enabled stays true while the remote serves (a paired phone keeps control for
// its session after the single-use code is spent); code is "" when no code is
// active, and code and url are "" when the remote is disabled.
//
//export DZWebRemoteInfoJSON
func DZWebRemoteInfoJSON() *C.char { return jsonStr(engine.WebRemoteInfo(), nil) }

// DZWebRemoteQRPNG returns a malloc'd PNG buffer (free with DZFreeBytes) for a
// QR code encoding the web remote URL, writing its size to *outLen. Returns
// nil/0 when the remote is disabled or the URL is unavailable.
//
//export DZWebRemoteQRPNG
func DZWebRemoteQRPNG(outLen *C.int) *C.uchar { return cBytes(engine.WebRemoteQRPNG(), outLen) }
