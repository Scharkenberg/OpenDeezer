package engine

// Control settings, the phone web remote (pairing, QR code) and the Connect
// host switch.

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/control"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/discovery"
	odlog "github.com/Cycl0o0/OpenDeezer/v3/internal/log"
)

// ControlConfig returns the current remote-control settings for a settings
// screen: {enabled, addr, token, lan, running}, the values env vars and config
// files provide, so they become editable in the host.
func ControlConfig() map[string]any {
	cfg := config.LoadControl()
	mu.Lock()
	running := ctrlSrv != nil
	addr := cfg.Addr
	if running {
		addr = ctrlSrv.Addr()
	}
	mu.Unlock()
	if addr == "" {
		addr = "127.0.0.1:7654"
	}
	return map[string]any{
		"enabled": cfg.Enabled || running,
		"addr":    addr,
		"token":   cfg.Token,
		"lan":     !config.IsLoopbackAddr(addr),
		"running": running,
	}
}

// resolveControlAddr turns a UI address hint into a bindable host:port.
func resolveControlAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	switch {
	case addr == "" || addr == "1" || addr == "on" || addr == "true":
		return "127.0.0.1:7654"
	case strings.HasPrefix(addr, ":"):
		return lanHost + addr
	default:
		return addr
	}
}

// SetControlConfig saves the remote-control settings (control.txt, control-token.txt
// in the config dir) and applies them: starts or restarts the control server when
// enabled, stops it otherwise. LAN binds without a token need same-account auth.
func SetControlConfig(on bool, addr, token string) {
	a := strings.TrimSpace(addr)
	tok := strings.TrimSpace(token)

	// Saved for the next launch: control.txt holds "" when off, else the address or "1".
	saved := a
	if saved == "" {
		saved = "1"
	}
	if err := config.SaveControlEnabled(on, saved); err != nil {
		odlog.Warn("control.txt: %v", err)
	}
	if err := config.SaveControlToken(tok); err != nil {
		odlog.Warn("control-token.txt: %v", err)
	}

	// Apply live: tear down any running server, then start fresh if enabled.
	ctrlMu.Lock()
	defer ctrlMu.Unlock()
	webRemotePrev = nil // an explicit config replaces whatever the phone remote rebound
	if !on {
		rebuildControlServer(control.Config{}, Client())
		return
	}
	bind := resolveControlAddr(a)
	rebuildControlServer(control.Config{Token: tok, SameAccountOnly: !config.IsLoopbackAddr(bind) && tok == ""}, Client(), bind)
}

// webRemotePrev is the control config from before the phone remote rebound the
// server (Addr "" when there was none), restored when it is turned off. nil
// when the phone remote did not rebind. Guarded by ctrlMu.
var webRemotePrev *control.Config

// lanHost is the host a LAN-reachable control server binds: every interface.
var lanHost = "0.0.0.0"

// WebRemoteSetEnabled turns the phone web remote on or off. On binds the control
// server to lanHost (same port) with pairing; off ends pairing and sessions and
// restores the previous server, unless the Connect host needs it. Off by default.
func WebRemoteSetEnabled(on bool) {
	ctrlMu.Lock()
	defer ctrlMu.Unlock()
	if on {
		if webRemotePrev == nil {
			mu.Lock()
			prev := ctrlCfg
			if ctrlSrv == nil {
				prev = control.Config{}
			}
			mu.Unlock()
			webRemotePrev = &prev
		}
		if srv := ensureLANServer(true); srv != nil {
			srv.EnablePairing()
		}
		return
	}
	mu.Lock()
	srv := ctrlSrv
	mu.Unlock()
	if srv != nil {
		srv.DisablePairing()
	}
	if prev := webRemotePrev; prev != nil {
		webRemotePrev = nil
		switch {
		case connectHostSwitch && connectHost:
			// The Connect host keeps the LAN server; a rebuild must not bring
			// the remote back.
			mu.Lock()
			ctrlCfg.WebRemote = false
			mu.Unlock()
		case prev.Addr == "":
			rebuildControlServer(control.Config{}, Client())
		default:
			rebuildControlServer(*prev, Client(), prev.Addr)
		}
	}
}

// ensureLANServer returns the control server on a LAN address, starting or rebinding
// one on lanHost (same port) with the configured auth (same-account always with a
// Connect host switch). nil when no address binds. Caller holds ctrlMu.
func ensureLANServer(webRemote bool) *control.Server {
	mu.Lock()
	srv, cur := ctrlSrv, ctrlCfg
	mu.Unlock()
	if srv != nil && !config.IsLoopbackAddr(srv.Addr()) {
		if webRemote {
			mu.Lock()
			ctrlCfg.WebRemote = true
			mu.Unlock()
		}
		return srv
	}
	// Preserve any configured token / same-account auth so MCP and Connect keep
	// working after the rebind: Server.auth() evaluates the pairing session
	// token first, so phone pairing coexists with these.
	cfg := config.LoadControl()
	port := "7654"
	if srv != nil {
		_, port, _ = net.SplitHostPort(srv.Addr())
	}
	ccfg := control.Config{Token: cfg.Token, SameAccountOnly: cfg.SameAccount || connectHostSwitch, WebRemote: webRemote || cur.WebRemote}
	return rebuildControlServer(ccfg, Client(), lanHost+":"+port, lanHost+":0")
}

// srvPort returns the TCP port srv listens on.
func srvPort(srv *control.Server) int {
	_, port, err := net.SplitHostPort(srv.Addr())
	if err != nil {
		return 7654
	}
	p, _ := strconv.Atoi(port)
	return p
}

// webRemoteURL is the phone remote's URL and its server; nil while it is off.
func webRemoteURL() (string, *control.Server) {
	mu.Lock()
	srv := ctrlSrv
	mu.Unlock()
	if srv == nil || !srv.WebRemoteEnabled() {
		return "", nil
	}
	return fmt.Sprintf("http://%s:%d/remote", discovery.LANIPv4(), srvPort(srv)), srv
}

// WebRemoteInfo returns {"enabled","code","url","port"}. enabled stays true while the
// remote serves; code is "" when no code is active, and code and url are "" when off.
func WebRemoteInfo() map[string]any {
	url, srv := webRemoteURL()
	if srv == nil {
		return map[string]any{"enabled": false, "code": "", "url": "", "port": 0}
	}
	return map[string]any{
		"enabled": true,
		"code":    srv.PairingCode(),
		"url":     url,
		"port":    srvPort(srv),
	}
}

// WebRemoteQRPNG returns a PNG of a QR code encoding the phone remote's URL,
// nil while the remote is off.
func WebRemoteQRPNG() []byte {
	url, srv := webRemoteURL()
	if srv == nil {
		return nil
	}
	b, err := qrcode.Encode(url, qrcode.Medium, 512)
	if err != nil {
		return nil
	}
	return b
}

// SetConnectHostSwitch(true) declares a Connect host switch: the control server is
// advertised only while it is on, and a LAN server it starts accepts the account's
// devices. Without it auth follows the control config. Call before Init.
func SetConnectHostSwitch(on bool) {
	ctrlMu.Lock()
	connectHostSwitch = on
	ctrlMu.Unlock()
}

// ConnectHostSetEnabled(true) advertises this device on the LAN as a Connect target
// for the account's other devices; false stops advertising (the control server
// stays). Idempotent.
func ConnectHostSetEnabled(on bool) {
	ctrlMu.Lock()
	defer ctrlMu.Unlock()
	connectHost = on
	if on {
		ensureLANServer(false)
	}
	refreshAdvertiser()
}

// ConnectHostInfo returns {"enabled":bool,"addr":"<lanip>:<port>",
// "port":<int>,"name":"<account>"}; addr and name are "" while this device is
// not advertised.
func ConnectHostInfo() map[string]any {
	mu.Lock()
	srv := ctrlSrv
	enabled := advertiser != nil
	mu.Unlock()
	if !enabled || srv == nil {
		return map[string]any{"enabled": false, "addr": "", "port": 0, "name": ""}
	}
	port := srvPort(srv)
	return map[string]any{
		"enabled": true,
		"addr":    fmt.Sprintf("%s:%d", discovery.LANIPv4(), port),
		"port":    port,
		"name":    engineAccount().Name,
	}
}
