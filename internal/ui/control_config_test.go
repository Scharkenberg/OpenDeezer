package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/config"
)

// The TUI's control config is the shared one: an empty control.txt still
// loads the token (a LAN server the TUI starts honors it), from the folder
// config.SetBaseDir names.
func TestTUIControlConfigIsShared(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("PURPLEDISC_CONTROL", "")
	t.Setenv("PURPLEDISC_CONTROL_TOKEN", "")
	config.SetBaseDir(dir)
	t.Cleanup(func() { config.SetBaseDir("") })
	_ = os.WriteFile(filepath.Join(dir, "control.txt"), []byte("\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "control-token.txt"), []byte("abc\n"), 0o600)
	if got := config.LoadControl().Token; got != "abc" {
		t.Fatalf("token %q with an empty control.txt, want abc", got)
	}
}
