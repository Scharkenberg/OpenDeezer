package connect

import (
	"time"

	internalcontrol "github.com/Scharkenberg/PurpleDisc/v4/internal/control"
	internaldiscovery "github.com/Scharkenberg/PurpleDisc/v4/internal/discovery"
	sdkcontrol "github.com/Scharkenberg/PurpleDisc/v4/sdk/control"
)

// ---- discovery ----

// Device is a discovered Purple Disc instance: Name is the account's display name, Addr
// the control API address (host:port) for [NewRemoteClient], Client the platform id
// (e.g. "tui", "macos", "gnome") and Version the Purple Disc version.
type Device = internaldiscovery.Device

// AdvertiseInfo is the identity advertised to probers. The function you pass
// to [Advertise] returns one of these on each probe so changes (e.g. a
// re-login updating the display name) are reflected immediately.
type AdvertiseInfo = internaldiscovery.Info

// Responder is a running Purple Disc Connect discovery responder. Stop it with
// Close.
type Responder = internaldiscovery.Responder

// Discover multicasts a probe and returns the Purple Disc devices that reply within
// timeout, minus local replies from selfPort (0 without a control server). staticPeers
// ("host" or "host:port") are also probed by unicast, for networks without multicast.
func Discover(timeout time.Duration, selfPort int, staticPeers ...string) ([]Device, error) {
	return internaldiscovery.Discover(timeout, selfPort, staticPeers...)
}

// Advertise starts an Purple Disc Connect responder on UDP port 7655. info gives the
// current name and version per probe; controlPort is the local control server's TCP
// port. Close the returned Responder to stop advertising.
func Advertise(info func() AdvertiseInfo, controlPort int) (*Responder, error) {
	return internaldiscovery.Advertise(info, controlPort)
}

// ---- remote control ----

// State is the playback snapshot returned by [RemoteClient.Status] and all
// mutation methods.
type State = internalcontrol.State

// Track is a now-playing or queue entry in a [State].
type Track = internalcontrol.Track

// Whoami holds a device's identity and required auth: Name (account display name, not
// the user id), Offer (plan name), Auth ("token", "account", "session" or "none"),
// Client (platform id) and Device (device label).
type Whoami = internalcontrol.Whoami

// Config configures the control endpoint a [Host] exposes (the inbound side).
// It is the same type as control.Config.
type Config = internalcontrol.Config

// Commands are the playback actions an inbound controller can dispatch to this
// device via a [Host]. It is the same type as control.Commands.
type Commands = internalcontrol.Commands

// Account is this device's Deezer identity snapshot, provided to [NewHost].
// It is the same type as control.Account.
type Account = internalcontrol.Account

// EQState is a device's equalizer snapshot, returned by [RemoteClient.EQ] and
// [RemoteClient.SetEQ]. It is the same type as control.EQState.
type EQState = internalcontrol.EQState

// Event is a typed control event (a "state" snapshot or a "finished" event).
type Event = internalcontrol.Event

// EQ is the equalizer bridge a [Host] can serve at /eq; wire it via
// Host.Server().SetEQ before Start (control.PlayerEQ builds one from a player). It is
// the same type as control.EQ.
type EQ = internalcontrol.EQ

// RemoteClient drives a discovered Purple Disc device via its HTTP/JSON control API.
// Mutation methods return the device's playback snapshot after the command. Safe for
// concurrent use. It is the same type as control.Client.
type RemoteClient = sdkcontrol.Client

// NewRemoteClient builds a client for the device at addr ([Device.Addr]). Pass token
// for token auth or accountID (your Deezer user id) for account auth, both "" for none.
// Devices in "session" auth (web remote) cannot be controlled with it.
func NewRemoteClient(addr, token, accountID string) *RemoteClient {
	return sdkcontrol.NewClient("http://"+addr, token, accountID)
}