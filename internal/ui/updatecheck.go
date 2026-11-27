package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/update"

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

// installStartedMsg ends installUpdateCmd: the installer runs (err nil) or a step failed.
type installStartedMsg struct{ err error }

// installUpdateCmd downloads the release's Windows installer into the temp folder, checked
// against its SHA-256, and starts it. The installer asks which apps to install and then
// quits this one through the "PurpleDisc.Quit" event (cmd/purpledisc).
func (m *Model) installUpdateCmd() tea.Cmd {
	info := m.updateInfo
	return func() tea.Msg {
		path := filepath.Join(os.TempDir(), "PurpleDisc-"+info.Latest+"-windows-x64.exe")
		err := update.Download(info.Installer, info.InstallerSHA256, path)
		if err == nil {
			err = exec.Command(path, "-y").Start()
		}
		return installStartedMsg{err}
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
