package ui

import (
	"os/exec"
	"runtime"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/update"

	tea "github.com/charmbracelet/bubbletea"
)

// updateCheckMsg carries a GitHub release check result (internal/update). A failed
// check (err) counts as "no update" and is shown only for a manual re-check.
type updateCheckMsg struct {
	info update.Info
	err  error
}

// updateCheckCmd asks GitHub for the latest release. It's fired once from
// Init (background, non-blocking) and again on a manual re-check from the
// About screen; it never downloads or installs anything.
func (m *Model) updateCheckCmd() tea.Cmd {
	return func() tea.Msg {
		info, err := update.Check(Version)
		return updateCheckMsg{info: info, err: err}
	}
}

// openInBrowser opens url with the OS's default handler; errors are ignored, so the UI
// never blocks or fails on it.
func openInBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default: // linux, bsd, etc.
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
