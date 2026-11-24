package control

import (
	"context"
	"encoding/json"
	"net/url"

	internalcontrol "github.com/Cycl0o0/OpenDeezer/v3/internal/control"
	sdkdeezer "github.com/Cycl0o0/OpenDeezer/v3/sdk/deezer"
)

// ---- type aliases ----

// Config configures a [Server]: Addr is the listen address (e.g. ":7654"), Token
// enables bearer auth, SameAccountOnly (without Token) requires the same Deezer
// account, and WebRemote serves the phone SPA at GET /remote with pairing auth.
type Config = internalcontrol.Config

// Commands are the playback actions a [Server] dispatches. Set only the
// functions your application implements; the endpoint of a nil entry answers
// 501 Not Implemented (or 404 where the route is optional).
type Commands = internalcontrol.Commands

// State is the playback snapshot returned by GET /status and all mutation
// endpoints. It is also the return type of all [Client] mutation methods.
type State = internalcontrol.State

// Track is a now-playing or queue entry in a [State].
type Track = internalcontrol.Track

// Account is the controlled client's Deezer identity, provided to [NewServer]
// via a snapshot callback. UserID is the credential in same-account auth mode.
type Account = internalcontrol.Account

// Whoami is the unauthenticated identity returned by GET /whoami. It carries
// the account display Name but never the UserID (which is the credential in
// same-account mode).
type Whoami = internalcontrol.Whoami

// EQState is the equalizer snapshot returned by GET /eq (and by all [Client]
// equalizer methods): enabled flag, mono downmix, preamp, per-band gains, the
// active preset and the preset/band lists.
type EQState = internalcontrol.EQState

// Event is a typed control event (a "state" snapshot or a "finished" event).
type Event = internalcontrol.Event

// EQ is the equalizer bridge a [Server] serves at /eq; each function may be nil. Pass
// it to [Server.SetEQ] before Start (without it /eq is 404). [PlayerEQ] builds one from
// an [EQController].
type EQ = internalcontrol.EQ

// EQController is the player subset an [EQ] bridge built with [PlayerEQ]
// drives. [sdk/player.Player] satisfies it.
type EQController = internalcontrol.EQController

// PlayerEQ builds an [EQ] bridge from a live player getter (get may return nil
// while the engine is starting) and the preset-name list (see
// [sdk/player.EQPresetNames]).
func PlayerEQ(get func() EQController, presets []string) *EQ {
	return internalcontrol.PlayerEQ(get, presets)
}

// ---- Server ----

// Server hosts the OpenDeezer remote-control API. Create it with [NewServer],
// [Server.Start] binds the port and [Server.Close] stops it. Safe for concurrent use
// once started.
type Server struct {
	s *internalcontrol.Server
}

// NewServer builds a control server from cfg (address and auth), status (a race-free
// playback snapshot per request), account (the logged-in identity), cmds (player
// actions) and dz (serves /search and /playlists; nil disables them).
func NewServer(
	cfg Config,
	status func() State,
	account func() Account,
	cmds Commands,
	dz *sdkdeezer.Client,
) *Server {
	inner := sdkdeezer.Unwrap(dz) // nil-safe: returns nil when dz is nil
	return &Server{s: internalcontrol.New(cfg, status, account, cmds, inner)}
}

// Start binds the port and begins serving in a background goroutine.
func (s *Server) Start() error { return s.s.Start() }

// Close stops the server and releases the port.
func (s *Server) Close() { s.s.Close() }

// Addr returns the actual listen address (valid after [Server.Start]).
func (s *Server) Addr() string { return s.s.Addr() }

// SetVersion records the app version reported by GET /whoami.
func (s *Server) SetVersion(v string) { s.s.SetVersion(v) }

// SetClientInfo records the client/platform id and human device label for
// GET /whoami (e.g. "myapp", "My Player v1.0").
func (s *Server) SetClientInfo(client, device string) { s.s.SetClientInfo(client, device) }

// SetEQ wires the equalizer bridge served at GET/POST /eq. Call it before
// [Server.Start]; without a bridge the /eq endpoints are 404. Build one with
// [PlayerEQ] or fill the [EQ] functions yourself.
func (s *Server) SetEQ(eq *EQ) { s.s.SetEQ(eq) }

// EnablePairing mints a fresh 6-digit pairing code, activates the pairing
// flow, and returns the code. Display it to the user; they enter it in the
// phone web remote at http://<addr>/remote. Each call resets the code.
func (s *Server) EnablePairing() string { return s.s.EnablePairing() }

// DisablePairing clears the pairing code so no new devices can pair and revokes
// all existing session tokens (individual sessions can be revoked via their id).
func (s *Server) DisablePairing() { s.s.DisablePairing() }

// PairingActive reports whether a pairing code is currently active.
func (s *Server) PairingActive() bool { return s.s.PairingActive() }

// PairingCode returns the current 6-digit code, or an empty string when
// pairing is not active.
func (s *Server) PairingCode() string { return s.s.PairingCode() }

// RevokeSession revokes one paired web-remote session by its public id (the
// "id" returned from a successful POST /pair) or by its session token. Use it
// for per-device revocation; [Server.DisablePairing] revokes all sessions.
func (s *Server) RevokeSession(idOrToken string) { s.s.RevokeSession(idOrToken) }

// ---- Client ----

// Client talks to a [Server] or compatible endpoint over HTTP. Mutation methods return
// the snapshot after the command, which may lag by one server tick (poll GET /status).
// Safe for concurrent use.
type Client struct {
	c *internalcontrol.Client
}

// NewClient builds a control client for base (e.g. "http://192.168.1.5:7654"). token
// sets X-OpenDeezer-Token and accountID X-OpenDeezer-Account; "" omits either.
func NewClient(base, token, accountID string) *Client {
	return &Client{c: internalcontrol.NewClient(base, token, accountID)}
}

// Whoami fetches the server's identity and auth mode. This endpoint is
// unauthenticated and is safe to call before supplying credentials.
func (c *Client) Whoami() (Whoami, error) { return c.c.Whoami() }

// Status returns the current playback snapshot.
func (c *Client) Status() (State, error) { return c.c.Status() }

// Events subscribes to the server's state stream (GET /events, SSE): an initial [State]
// and then one per visible change, coalesced so a slow reader still gets the newest.
// The channel closes when ctx is canceled or the connection drops (no reconnect).
func (c *Client) Events(ctx context.Context) (<-chan State, error) { return c.c.Events(ctx) }

// PlayPause toggles play/pause on the server.
func (c *Client) PlayPause() (State, error) { return c.c.PlayPause() }

// Next skips to the next track.
func (c *Client) Next() (State, error) { return c.c.Next() }

// Prev jumps to the previous track.
func (c *Client) Prev() (State, error) { return c.c.Prev() }

// Stop halts playback.
func (c *Client) Stop() (State, error) { return c.c.Stop() }

// Restart seeks to position 0 in the current track.
func (c *Client) Restart() (State, error) { return c.c.Restart() }

// SeekMS seeks to ms milliseconds from the start of the current track.
func (c *Client) SeekMS(ms int64) (State, error) { return c.c.SeekMS(ms) }

// SetVolume sets the volume (0.0 = silent, 1.0 = full).
func (c *Client) SetVolume(v float64) (State, error) { return c.c.SetVolume(v) }

// SetRepeat sets the repeat mode: "off", "all", or "one".
func (c *Client) SetRepeat(mode string) (State, error) { return c.c.SetRepeat(mode) }

// SetShuffle enables (true) or disables (false) shuffle.
func (c *Client) SetShuffle(on bool) (State, error) { return c.c.SetShuffle(on) }

// CycleRepeat advances the repeat mode one step (off → all → one → off).
func (c *Client) CycleRepeat() (State, error) { return c.c.CycleRepeat() }

// ToggleShuffle flips shuffle on/off.
func (c *Client) ToggleShuffle() (State, error) { return c.c.ToggleShuffle() }

// PlayTrack instructs the server to play the track with the given Deezer id.
func (c *Client) PlayTrack(id string) (State, error) { return c.c.PlayTrack(id) }

// PlayPlaylist instructs the server to play the playlist with the given id.
func (c *Client) PlayPlaylist(id string) (State, error) { return c.c.PlayPlaylist(id) }

// PlayAlbum instructs the server to play the album with the given Deezer id
// from its first track (replacing the queue).
func (c *Client) PlayAlbum(id string) (State, error) { return c.c.PlayAlbum(id) }

// PlayMixTrack starts a Deezer mix (radio) seeded by the track with the given id.
func (c *Client) PlayMixTrack(id string) (State, error) { return c.c.PlayMixTrack(id) }

// PlayMixArtist starts a Deezer mix (radio) seeded by the artist with the given id.
func (c *Client) PlayMixArtist(id string) (State, error) { return c.c.PlayMixArtist(id) }

// QueueAdd appends the track with the given Deezer id to the server's queue,
// or inserts it right after the current track when next is true.
func (c *Client) QueueAdd(id string, next bool) (State, error) { return c.c.QueueAdd(id, next) }

// QueueJump jumps playback to the queue entry at index (0-based).
func (c *Client) QueueJump(index int) (State, error) { return c.c.QueueJump(index) }

// QueueRemove removes the queue entry at index (0-based).
func (c *Client) QueueRemove(index int) (State, error) { return c.c.QueueRemove(index) }

// QueueMove moves the queue entry at position from to position to (both 0-based).
func (c *Client) QueueMove(from, to int) (State, error) { return c.c.QueueMove(from, to) }

// SetSleepTimer arms the server's sleep timer: playback fades out and pauses
// after minutes, or when the current track ends if endOfTrack is true (minutes
// is then ignored).
func (c *Client) SetSleepTimer(minutes int, endOfTrack bool) (State, error) {
	return c.c.SetSleepTimer(minutes, endOfTrack)
}

// CancelSleepTimer disarms the server's sleep timer.
func (c *Client) CancelSleepTimer() (State, error) { return c.c.CancelSleepTimer() }

// EQ returns the server's equalizer snapshot (enabled, mono downmix, preamp,
// per-band gains, active preset and the preset/band lists). Errors when the
// server has no equalizer bridge (the /eq endpoints are 404).
func (c *Client) EQ() (EQState, error) { return c.c.EQ() }

// SetEQ applies a partial EQ update. params holds any of the POST /eq query params
// (on=0|1, mono=0|1, preset=name, band=N&db=X, preamp=dB); at least one is required.
func (c *Client) SetEQ(params url.Values) (EQState, error) { return c.c.SetEQ(params) }

// Search returns the server's raw search-results JSON for query q
// (GET /search). Errors when the server was built without a Deezer client.
func (c *Client) Search(q string) (json.RawMessage, error) { return c.c.Search(q) }

// Playlists returns the server account's raw playlists JSON (GET /playlists).
// Errors when the server was built without a Deezer client.
func (c *Client) Playlists() (json.RawMessage, error) { return c.c.Playlists() }

// HistoryRecent returns the server's recent-listening history as raw JSON,
// most recent first (GET /history/recent). n caps the number of entries
// (1..500). Errors when the server exposes no history.
func (c *Client) HistoryRecent(n int) (json.RawMessage, error) { return c.c.HistoryRecent(n) }

// SetQueue pushes a whole queue to the host (POST /queue/set): tracksJSON is the
// bridge.Track JSON array and index is the cursor (-1 for an empty queue). It is
// the groundwork for host-owned casting. Errors when the host exposes no SetQueue.
func (c *Client) SetQueue(tracksJSON string, index int) (State, error) {
	return c.c.SetQueue(tracksJSON, index)
}

// EventsTyped subscribes to GET /events with both "state" snapshots and "finished"
// events, so a controller learns a natural track end at once. The channel closes on
// ctx cancel or when the stream drops (no reconnect).
func (c *Client) EventsTyped(ctx context.Context) (<-chan Event, error) {
	return c.c.EventsTyped(ctx)
}
