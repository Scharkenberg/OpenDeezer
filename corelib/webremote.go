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

// DZControlConfigJSON returns the remote-control settings {enabled, addr, token, lan,
// running} so the Settings UI can edit them.
//
//export DZControlConfigJSON
func DZControlConfigJSON() *C.char { return jsonStr(engine.ControlConfig(), nil) }

// DZSetControlConfig saves the remote-control settings (control.txt, control-token.txt)
// and applies them: starts or restarts the control server when enabled, stops it
// otherwise. A LAN bind without a token requires same-account auth.
//
//export DZSetControlConfig
func DZSetControlConfig(enabled C.int, addr *C.char, token *C.char) {
	engine.SetControlConfig(enabled != 0, C.GoString(addr), C.GoString(token))
}

// DZWebRemoteSetEnabled turns the phone web remote on (on!=0) or off. On binds the
// control server to 0.0.0.0:<port> with pairing; off ends pairing and sessions and
// restores the previous server. Off by default.
//
//export DZWebRemoteSetEnabled
func DZWebRemoteSetEnabled(on C.int) { engine.WebRemoteSetEnabled(on != 0) }

// DZWebRemoteInfoJSON returns {"enabled","code","url","port"} (free with DZFree).
// code is "" when no code is active; code and url are "" when disabled.
//
//export DZWebRemoteInfoJSON
func DZWebRemoteInfoJSON() *C.char { return jsonStr(engine.WebRemoteInfo(), nil) }

// DZWebRemoteQRPNG returns a malloc'd PNG of the web remote URL's QR code (free with
// DZFreeBytes), its size in *outLen; nil/0 when disabled or without a URL.
//
//export DZWebRemoteQRPNG
func DZWebRemoteQRPNG(outLen *C.int) *C.uchar { return cBytes(engine.WebRemoteQRPNG(), outLen) }
