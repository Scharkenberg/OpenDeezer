//go:build windows

package main

import (
	"github.com/Scharkenberg/PurpleDisc/v4/internal/update"

	"golang.org/x/sys/windows"
)

// quitOnInstall makes the installed copy save and quit when an installer sets the
// "PurpleDisc.Quit" event (gui/windows/Setup/Installer.cs), so that it can replace the
// install folder. The event is manual-reset: the desktop app waits on it too. The handle
// stays open until the process ends; the installer waits for that.
func quitOnInstall(quit func()) {
	if !update.InstalledApp() {
		return
	}
	name, _ := windows.UTF16PtrFromString("PurpleDisc.Quit")
	h, _ := windows.CreateEvent(nil, 1, 0, name) // an existing event comes back with ERROR_ALREADY_EXISTS
	if h == 0 {
		return
	}
	go func() {
		windows.WaitForSingleObject(h, windows.INFINITE)
		quit()
	}()
}
