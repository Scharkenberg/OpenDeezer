//go:build windows

package discord

import (
	"errors"
	"testing"
)

func TestDialIPCWindowsNotRunning(t *testing.T) {
	c, err := dialIPC()
	if err == nil {
		c.Close()
		t.Skip("Discord is running")
	}
	if !errors.Is(err, errNoIPC) {
		t.Errorf("expected error %v, got %v", errNoIPC, err)
	}
}
