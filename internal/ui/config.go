package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/config"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	pdlog "github.com/Scharkenberg/PurpleDisc/v4/internal/log"
)

// LoadARL resolves the Deezer ARL from, in order: $DEEZER_ARL, then
// ~/.config/purpledisc/arl.txt. Returns "" if neither is set.
func LoadARL() string {
	if v := strings.TrimSpace(os.Getenv("DEEZER_ARL")); v != "" {
		return v
	}
	dir, err := config.Dir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, "arl.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// SaveARL writes the ARL to ~/.config/purpledisc/arl.txt (0600, atomically).
func SaveARL(arl string) error {
	return config.WriteFile("arl.txt", strings.TrimSpace(arl)+"\n")
}

// DeleteARL removes the saved ARL; none saved is not an error.
func DeleteARL() error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, "arl.txt")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
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
		pdlog.Warn("playback.json: %v", err)
	}
	return pb
}

// savePlayback saves playback.json with edit applied (to the values in force,
// should the file be gone).
func (m *Model) savePlayback(edit func(*config.Playback)) {
	current := func() config.Playback {
		return config.Playback{Quality: m.client.Quality(), Gapless: m.player.Gapless(),
			CrossfadeMS: m.player.CrossfadeMS(), ReplayGain: m.player.ReplayGain(), GainTarget: m.player.ReplayGainTarget()}
	}
	if err := config.UpdatePlayback(current, edit); err != nil {
		pdlog.Warn("playback.json: %v", err)
	}
}
func saveStringFile(name, v string) error {
	return config.WriteFile(name, v+"\n")
}

// LoadAudioDevice / SaveAudioDevice persist the selected output device id.
func LoadAudioDevice() string {
	dir, err := config.Dir()
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
	dir, err := config.Dir()
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
	dir, err := config.Dir()
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

// LoadScreenReader returns the saved screen-reader mode: "on", "off", or "" for
// automatic.
func LoadScreenReader() string {
	dir, err := config.Dir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, "screen-reader.txt"))
	if err != nil {
		return ""
	}
	switch v := strings.TrimSpace(string(b)); v {
	case "on", "off":
		return v
	}
	return ""
}

// SaveScreenReader persists the screen-reader mode as "on" or "off", atomically.
func SaveScreenReader(on bool) error {
	if on {
		return saveStringFile("screen-reader.txt", "on")
	}
	return saveStringFile("screen-reader.txt", "off")
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
	dir, err := config.Dir()
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
	return config.WriteFile("resume.json", string(b))
}

// LoadResume reads the saved resume state, or nil if none/invalid.
func LoadResume() *ResumeState {
	dir, err := config.Dir()
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
