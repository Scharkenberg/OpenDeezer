package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
	tea "github.com/charmbracelet/bubbletea"
)

// The TUI plays with playback.json's settings, shared with the engine; while
// that file does not exist, its own older files are migrated into it once.
func TestPlaybackMigratesTUIFilesOnce(t *testing.T) {
	useTempConfig(t)
	dir, _ := configDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, v := range map[string]string{"quality.txt": "hifi\n", "gapless.txt": "0\n", "crossfade.txt": "6000\n", "replaygain.txt": "1\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want := config.Playback{Quality: 2, Gapless: false, CrossfadeMS: 6000, ReplayGain: true}
	if pb := loadPlayback(); pb != want {
		t.Fatalf("loaded %+v, want %+v", pb, want)
	}
	if pb, ok := config.LoadPlayback(); !ok || pb != want {
		t.Fatalf("playback.json %+v %v, want the migrated %+v", pb, ok, want)
	}
	_ = os.WriteFile(filepath.Join(dir, "quality.txt"), []byte("normal\n"), 0o600)
	if pb := loadPlayback(); pb.Quality != 2 {
		t.Fatalf("quality %d: the old file won over playback.json", pb.Quality)
	}
}

// The TUI's playback keys save into playback.json.
func TestPlaybackKeysSaveSharedFile(t *testing.T) {
	useTempConfig(t)
	if err := config.SavePlayback(config.Playback{Gapless: true}); err != nil {
		t.Fatal(err)
	}
	m := interactionModel(screenList)
	m.client = deezer.New("arl")
	for _, k := range []rune{'x', 'h', 'R'} {
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlG})
	pb, ok := config.LoadPlayback()
	if !ok || pb.CrossfadeMS != 3000 || pb.Quality != 1 || !pb.ReplayGain || pb.Gapless != m.player.Gapless() {
		t.Fatalf("playback.json %+v %v after the keys", pb, ok)
	}
}
