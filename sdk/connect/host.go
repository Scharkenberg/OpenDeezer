package connect

import (
	"errors"
	"fmt"
	"net"
	"strconv"

	sdkcontrol "github.com/Cycl0o0/OpenDeezer/v3/sdk/control"
	sdkdeezer "github.com/Cycl0o0/OpenDeezer/v3/sdk/deezer"
)

// ErrAdvertise is wrapped into [Host.Start]'s error when the control endpoint bound but
// LAN advertising failed. The endpoint keeps serving by address; callers must still
// call [Host.Close].
var ErrAdvertise = errors.New("connect: control endpoint is serving but LAN advertising failed")

// This file is the inbound side of OpenDeezer Connect: a control server for
// [RemoteClient] paired with a discovery responder for [Discover], so becoming a
// Connect device is a single call.

// HostConfig configures a [Host]: how the control endpoint binds and
// authenticates, plus the identity advertised over LAN discovery.
type HostConfig struct {
	// Control is the control-endpoint configuration (listen address and auth
	// mode). For a LAN-reachable device, bind a non-loopback address such as
	// ":7654" and set Token or SameAccountOnly.
	Control Config

	// Name is the advertised display name. When an account snapshot is provided
	// to [NewHost] and reports a non-empty name, that name is used instead so a
	// re-login is reflected automatically.
	Name string
	// Client is the advertised client/platform id (e.g. "myapp").
	Client string
	// Device is the human device label reported by [RemoteClient.Whoami]
	// (e.g. "Kitchen Speaker"). Defaults to Client when empty.
	Device string
	// Version is the advertised OpenDeezer/app version.
	Version string
}

// Host is the inbound side of OpenDeezer Connect: a control endpoint advertised on the
// LAN for [Discover]. Create it with [NewHost], [Host.Start] binds and advertises,
// [Host.Close] stops. Safe for concurrent use once started.
type Host struct {
	srv             *sdkcontrol.Server
	resp            *Responder
	account         func() Account
	name            string
	client          string
	version         string
	sameAccountOnly bool
}

// NewHost builds a Connect host from cfg (control bind, auth, identity), status
// (snapshot provider), account (identity and same-account credential, required with
// SameAccountOnly), cmds (remote actions) and dz (browse routes; nil disables them).
func NewHost(
	cfg HostConfig,
	status func() State,
	account func() Account,
	cmds Commands,
	dz *sdkdeezer.Client,
) *Host {
	device := cfg.Device
	if device == "" {
		device = cfg.Client
	}
	srv := sdkcontrol.NewServer(cfg.Control, status, account, cmds, dz)
	srv.SetClientInfo(cfg.Client, device)
	srv.SetVersion(cfg.Version)
	return &Host{
		srv:             srv,
		account:         account,
		name:            cfg.Name,
		client:          cfg.Client,
		version:         cfg.Version,
		sameAccountOnly: cfg.Control.SameAccountOnly,
	}
}

// Server returns the underlying control server so callers can enable web-remote
// pairing ([sdkcontrol.Server.EnablePairing]), read the listen address, or
// override the device label.
func (h *Host) Server() *sdkcontrol.Server { return h.srv }

// Addr returns the control endpoint's listen address (valid after [Host.Start]).
func (h *Host) Addr() string { return h.srv.Addr() }

// Start binds the control endpoint and advertises it on UDP 7655 when it is not
// loopback. If advertising fails the endpoint keeps serving and the error wraps
// [ErrAdvertise] (call [Host.Close]); any other error means nothing runs.
func (h *Host) Start() error {
	if h.sameAccountOnly && h.account == nil {
		// SameAccountOnly rejects every request when there is no account to
		// compare against, so the host would be uncontrollable. Fail loudly
		// rather than serving a dead endpoint.
		return fmt.Errorf("connect: Control.SameAccountOnly requires a non-nil account provider")
	}
	if err := h.srv.Start(); err != nil {
		return err
	}
	if host, _, err := net.SplitHostPort(h.srv.Addr()); err == nil {
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
	}
	resp, err := Advertise(h.advertiseInfo, portOf(h.srv.Addr()))
	if err != nil {
		// The control endpoint is up: return the advertising error but keep serving, so the
		// device is still reachable by address on a network without multicast.
		return fmt.Errorf("%w: %v", ErrAdvertise, err)
	}
	h.resp = resp
	return nil
}

// Close stops advertising and shuts down the control endpoint.
func (h *Host) Close() {
	if h.resp != nil {
		h.resp.Close()
		h.resp = nil
	}
	h.srv.Close()
}

// advertiseInfo is called on each discovery probe. It prefers the live account
// name (so a re-login updates what controllers see) and falls back to the
// configured name.
func (h *Host) advertiseInfo() AdvertiseInfo {
	name := h.name
	if h.account != nil {
		if n := h.account().Name; n != "" {
			name = n
		}
	}
	return AdvertiseInfo{Name: name, Client: h.client, Version: h.version}
}

// portOf extracts the numeric TCP port from a host:port address (0 on failure).
func portOf(addr string) int {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(p)
	return n
}
