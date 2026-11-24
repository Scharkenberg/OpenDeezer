package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
)

// The remote-control settings go to the config folder in use, both files written.
func TestSetControlConfigWritesConfigDir(t *testing.T) {
	base := t.TempDir()
	config.SetBaseDir(base)
	t.Cleanup(func() { config.SetBaseDir("") })
	SetControlConfig(false, "", "")
	for _, name := range []string{"control.txt", "control-token.txt"} {
		if _, err := os.Stat(filepath.Join(base, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
