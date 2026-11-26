//go:build !windows

package ui

// screenReaderRunning has no system-wide signal to read outside Windows.
func screenReaderRunning() bool { return false }
