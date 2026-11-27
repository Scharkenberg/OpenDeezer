//go:build !windows

package main

// quitOnInstall is Windows-only: no installer replaces this copy elsewhere.
func quitOnInstall(func()) {}
