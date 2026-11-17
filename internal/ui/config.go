package ui

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
	odlog "github.com/Cycl0o0/OpenDeezer/v3/internal/log"
)

// configDir is ~/.config/opendeezer.
func configDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "opendeezer"), nil
}

// LoadARL resolves the Deezer ARL from, in order: $DEEZER_ARL, then
// ~/.config/opendeezer/arl.txt. Returns "" if neither is set.
func LoadARL() string {
	if v := strings.TrimSpace(os.Getenv("DEEZER_ARL")); v != "" {
		return v
	}
	dir, err := configDir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, "arl.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// SaveARL writes the ARL to ~/.config/opendeezer/arl.txt (0600, atomically).
func SaveARL(arl string) error {
	return config.WriteFile("arl.txt", strings.TrimSpace(arl)+"\n")
}

// loadPlayback returns the playback settings of playback.json, shared with
// the engine (config.LoadPlayback moves the TUI's older files into it). With
// neither, the TUI's defaults are written there.
func loadPlayback() config.Playback {
	pb, ok := config.LoadPlayback()
	if ok {
		return pb
	}
	pb = config.Playback{Gapless: true}
	if err := config.SavePlayback(pb); err != nil {
		odlog.Warn("playback.json: %v", err)
	}
	return pb
}

// savePlayback saves playback.json with edit applied (to the values in force,
// should the file be gone).
func (m *Model) savePlayback(edit func(*config.Playback)) {
	current := func() config.Playback {
		return config.Playback{Quality: m.client.Quality(), Gapless: m.player.Gapless(),
			CrossfadeMS: m.player.CrossfadeMS(), ReplayGain: m.player.ReplayGain()}
	}
	if err := config.UpdatePlayback(current, edit); err != nil {
		odlog.Warn("playback.json: %v", err)
	}
}
func saveStringFile(name, v string) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), []byte(v+"\n"), 0600)
}

// ControlConfig holds the control-API settings (remote control + MCP).
type ControlConfig struct {
	Enabled     bool
	Addr        string // host:port; "" -> 127.0.0.1:7654
	Token       string // bearer token ("" = no auth, localhost only)
	SameAccount bool   // require a matching Deezer account when no token (LAN)
}

// LoadControl reads the control-API config: $OPENDEEZER_CONTROL ("1"/addr) +
// $OPENDEEZER_CONTROL_TOKEN, else ~/.config/opendeezer/{control.txt,control-token.txt}.
func LoadControl() ControlConfig {
	c := ControlConfig{Addr: "127.0.0.1:7654"}
	v := strings.TrimSpace(os.Getenv("OPENDEEZER_CONTROL"))
	if v == "" {
		if dir, err := configDir(); err == nil {
			if b, e := os.ReadFile(filepath.Join(dir, "control.txt")); e == nil {
				v = strings.TrimSpace(string(b))
			}
		}
	}
	switch {
	case v == "":
		return c
	case v == "1" || strings.EqualFold(v, "on") || strings.EqualFold(v, "true"):
		c.Enabled = true
	case v == "0" || strings.EqualFold(v, "off") || strings.EqualFold(v, "false") || strings.EqualFold(v, "no"):
		c.Enabled = false
	default:
		c.Enabled = true
		c.Addr = v // an explicit host:port
	}
	c.Token = strings.TrimSpace(os.Getenv("OPENDEEZER_CONTROL_TOKEN"))
	if c.Token == "" {
		if dir, err := configDir(); err == nil {
			if b, e := os.ReadFile(filepath.Join(dir, "control-token.txt")); e == nil {
				c.Token = strings.TrimSpace(string(b))
			}
		}
	}
	// When bound to a non-loopback address (LAN remote) with no token, default
	// to same-account auth: the user's own devices (same Deezer login) connect
	// without copying a token; foreign accounts are rejected. Override with
	// $OPENDEEZER_CONTROL_SAMEACCOUNT=0.
	if c.Enabled && c.Token == "" && !isLoopbackAddr(c.Addr) {
		c.SameAccount = true
	}
	if v := strings.TrimSpace(os.Getenv("OPENDEEZER_CONTROL_SAMEACCOUNT")); v != "" {
		c.SameAccount = v == "1" || strings.EqualFold(v, "on") || strings.EqualFold(v, "true")
	}
	return c
}

// isLoopbackAddr reports whether a host:port binds only the loopback interface.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	switch host {
	case "", "0.0.0.0", "::":
		return false // wildcard = all interfaces
	case "localhost":
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// LoadDiscordAppID returns the Discord application id for Rich Presence, from
// $OPENDEEZER_DISCORD_APP_ID or ~/.config/opendeezer/discord-app-id.txt. Empty
// disables the feature.
func LoadDiscordAppID() string {
	if v := strings.TrimSpace(os.Getenv("OPENDEEZER_DISCORD_APP_ID")); v != "" {
		return v
	}
	dir, err := configDir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, "discord-app-id.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// LoadAudioDevice / SaveAudioDevice persist the selected output device id.
func LoadAudioDevice() string {
	dir, err := configDir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, "device.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
func SaveAudioDevice(id string) error { return saveStringFile("device.txt", id) }

// LoadLastPeer / SaveLastPeer remember the last remote-control peer address so
// the connect screen can prefill it.
func LoadLastPeer() string {
	dir, err := configDir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, "remote-peer.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
func SaveLastPeer(addr string) error { return saveStringFile("remote-peer.txt", addr) }

// LoadTheme returns the saved theme name ("" if none).
func LoadTheme() string {
	dir, err := configDir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, "theme.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// SaveTheme persists the theme name, atomically.
func SaveTheme(name string) error {
	return config.WriteFile("theme.txt", name+"\n")
}

// ResumeState is the last-played track plus the position to resume from.
type ResumeState struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ArtistLine string `json:"artistLine"`
	AlbumName  string `json:"albumName"`
	ArtworkURL string `json:"artworkUrl"`
	DurationMS int64  `json:"durationMs"`
	PositionMS int64  `json:"positionMs"`
	Episode    bool   `json:"episode,omitempty"` // a podcast episode, not a song
}

// Track reconstructs a deezer.Track from the saved state.
func (r ResumeState) Track() deezer.Track {
	return deezer.Track{
		ID:         r.ID,
		Name:       r.Name,
		DurationMS: r.DurationMS,
		Artists:    []deezer.Artist{{Name: r.ArtistLine}},
		AlbumName:  r.AlbumName,
		ArtworkURL: r.ArtworkURL,
	}
}

// SaveResume writes the current track + position to resume.json. Positions in
// the first few seconds are treated as "start over" and clear the state.
func SaveResume(t deezer.Track, positionMS int64, episode bool) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, "resume.json")
	if t.ID == "" || positionMS < 3000 {
		_ = os.Remove(path)
		return nil
	}
	rs := ResumeState{
		ID: t.ID, Name: t.Name, ArtistLine: t.ArtistLine(), AlbumName: t.AlbumName,
		ArtworkURL: t.ArtworkURL, DurationMS: t.DurationMS, PositionMS: positionMS,
		Episode: episode,
	}
	b, err := json.Marshal(rs)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0600)
}

// LoadResume reads the saved resume state, or nil if none/invalid.
func LoadResume() *ResumeState {
	dir, err := configDir()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(dir, "resume.json"))
	if err != nil {
		return nil
	}
	var rs ResumeState
	if json.Unmarshal(b, &rs) != nil || rs.ID == "" {
		return nil
	}
	return &rs
}
