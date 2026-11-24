package ui

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/control"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/discovery"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/i18n"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	qrcode "github.com/skip2/go-qrcode"
)

// webRemoteMsg is the result of enabling or disabling the phone web remote.
type webRemoteMsg struct {
	srv          *control.Server // new/active server; nil when disabling
	replacedCtrl bool            // true when srv replaced the old m.ctrl (loopback rebind)
	closed       bool            // the disable closed a server the enable had bound
	enabled      bool
	code         string // 6-digit pairing code
	url          string // http://<lan-ip>:<port>/remote
	qr           string // terminal QR block from qrcode.ToSmallString
	errStr       string
}

// webRemoteEnableCmd starts the phone web remote, binding LAN-reachably if
// needed and enabling pairing, as the engine's WebRemoteSetEnabled does.
func (m *Model) webRemoteEnableCmd() tea.Cmd {
	// Capture everything we need before the closure runs off the update loop.
	existingCtrl := m.ctrl
	existingWR := m.webRemoteSrv
	build := m.controlServerFunc()
	ctrlCfg := config.LoadControl() // carry the configured token/same-account to the new server

	return func() tea.Msg {
		// Case 1: dedicated web-remote server already LAN-reachable.
		if existingWR != nil && !config.IsLoopbackAddr(existingWR.Addr()) {
			existingWR.EnablePairing()
			return buildWebRemoteMsg(existingWR, false)
		}
		// Case 2: existing control server is already on a LAN address.
		if existingCtrl != nil && !config.IsLoopbackAddr(existingCtrl.Addr()) {
			existingCtrl.EnablePairing()
			return buildWebRemoteMsg(existingCtrl, false)
		}

		startNew := func(addr string) *control.Server {
			// Carry the saved credentials so token and same-account clients keep working after the
			// LAN rebind: auth() checks the token before the web-remote branch.
			s := build(control.Config{
				Addr: addr, Token: ctrlCfg.Token,
				SameAccountOnly: ctrlCfg.SameAccount, WebRemote: true,
			})
			if err := s.Start(); err != nil {
				return nil
			}
			return s
		}

		// Case 3: the control server is loopback: close it and rebind on the LAN.
		if existingCtrl != nil {
			_, portStr, _ := net.SplitHostPort(existingCtrl.Addr())
			existingCtrl.Close()
			newSrv := startNew("0.0.0.0:" + portStr)
			if newSrv == nil {
				newSrv = startNew("0.0.0.0:0")
			}
			if newSrv == nil {
				return webRemoteMsg{errStr: i18n.T("failed to bind web remote server")}
			}
			newSrv.EnablePairing()
			return buildWebRemoteMsg(newSrv, true) // replaced ctrl
		}

		// Case 4: no server yet: start one.
		newSrv := startNew("0.0.0.0:7654")
		if newSrv == nil {
			newSrv = startNew("0.0.0.0:0")
		}
		if newSrv == nil {
			return webRemoteMsg{errStr: i18n.T("failed to bind web remote server")}
		}
		newSrv.EnablePairing()
		return buildWebRemoteMsg(newSrv, true)
	}
}

// webRemoteDisableCmd ends the web remote: a server the enable bound on the LAN in place
// of the loopback API is closed (the handler starts that API again); an existing LAN
// server stops pairing and ends the paired sessions (DisablePairing).
func (m *Model) webRemoteDisableCmd() tea.Cmd {
	srv := m.webRemoteSrv
	if srv == nil {
		srv = m.ctrl
	}
	rebound := m.webRemoteRebound
	return func() tea.Msg {
		if srv != nil && rebound {
			srv.Close()
			return webRemoteMsg{enabled: false, closed: true}
		}
		if srv != nil {
			srv.DisablePairing()
		}
		return webRemoteMsg{enabled: false}
	}
}

// buildWebRemoteMsg constructs a webRemoteMsg from a running, pairing-active server.
func buildWebRemoteMsg(srv *control.Server, replacedCtrl bool) webRemoteMsg {
	if srv == nil || !srv.PairingActive() {
		return webRemoteMsg{enabled: false, replacedCtrl: replacedCtrl}
	}
	_, portStr, _ := net.SplitHostPort(srv.Addr())
	port, _ := strconv.Atoi(portStr)
	url := fmt.Sprintf("http://%s:%d/remote", discovery.LANIPv4(), port)
	code := srv.PairingCode()
	qr := ""
	if q, err := qrcode.New(url, qrcode.Medium); err == nil {
		qr = q.ToSmallString(false)
	}
	return webRemoteMsg{
		srv: srv, replacedCtrl: replacedCtrl,
		enabled: true, code: code, url: url, qr: qr,
	}
}

// handleWebRemoteKey drives the Web Remote screen.
func (m *Model) handleWebRemoteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.ctrlEditToken {
		switch msg.String() {
		case "esc":
			m.endTokenEdit()
			m.status = ""
			return m, nil
		case "enter":
			tok := strings.TrimSpace(m.search.Value())
			m.endTokenEdit()
			err := config.SaveControlToken(tok)
			m.webCtrlCfg = config.LoadControl()
			if err != nil {
				m.status = i18n.Tf("Couldn't save token: %s", err.Error())
			} else if tok == "" {
				m.status = i18n.T("Token cleared — restart to apply")
			} else {
				m.status = i18n.T("Token saved — restart to apply")
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "a":
		cfg := config.LoadControl()
		enable := !cfg.Enabled
		err := config.SaveControlEnabled(enable, cfg.Addr)
		m.webCtrlCfg = config.LoadControl()
		if err != nil {
			m.status = i18n.Tf("Couldn't save: %s", err.Error())
		} else if enable {
			m.status = i18n.Tf("Control API on (%s) — restart to apply", cfg.Addr)
		} else {
			m.status = i18n.T("Control API off — restart to apply")
		}
		return m, nil
	case "t":
		cfg := config.LoadControl()
		m.search.CharLimit = 0 // tokens can be longer than a search
		m.search.SetValue(cfg.Token)
		m.search.EchoMode = textinput.EchoPassword
		m.search.Focus()
		m.ctrlEditToken = true
		m.status = ""
		return m, nil
	case "esc", "backspace":
		m.showMenu()
		m.status = ""
		return m, nil
	case "q":
		return m, tea.Quit
	case "enter", " ":
		if m.webRemoteActive && m.webRemoteSrv != nil && m.webRemoteSrv.PairingCode() == "" {
			// The code was used or expired: a fresh one; paired phones keep their sessions.
			srv := m.webRemoteSrv
			return m, func() tea.Msg {
				srv.EnablePairing()
				return buildWebRemoteMsg(srv, false)
			}
		}
		if m.webRemoteActive {
			m.status = i18n.T("Disabling web remote…")
			return m, m.webRemoteDisableCmd()
		}
		if m.loading {
			return m, nil // an enable is already in flight; don't leak a second server
		}
		m.loading = true
		m.status = i18n.T("Starting web remote…")
		return m, m.webRemoteEnableCmd()
	}
	return m, nil
}

// endTokenEdit returns the shared text input to its search settings.
func (m *Model) endTokenEdit() {
	m.ctrlEditToken = false
	m.search.Blur()
	m.search.EchoMode = textinput.EchoNormal
	m.search.CharLimit = searchCharLimit
}

// webRemotePairingCode is the server's live code: "" once used or expired.
func (m *Model) webRemotePairingCode() string {
	if m.webRemoteSrv == nil {
		return ""
	}
	return m.webRemoteSrv.PairingCode()
}

// webRemoteView renders the Web Remote screen.
func (m *Model) webRemoteView(rows int) string {
	lines := []string{
		accent.Render("📱 "+i18n.T("Web Remote")) + dim.Render("  — "+i18n.T("control from your phone")),
		"",
	}
	if !m.webRemoteActive {
		lines = append(lines,
			i18n.T("Web remote is off."),
			"",
			dim.Render(i18n.T("Press enter to enable. Your phone must be on the same Wi-Fi.")),
			"",
		)
	} else {
		lines = append(lines,
			i18n.T("Scan with your phone (same Wi-Fi), then enter the code."),
			"",
			i18n.T("Code")+":  "+accent.Render(m.webRemotePairingCode()),
			"URL:   "+dim.Render(m.webRemoteURL),
			"",
		)
		if m.webRemoteQR != "" {
			lines = append(lines, strings.Split(strings.TrimRight(m.webRemoteQR, "\n"), "\n")...)
			lines = append(lines, "")
		}
	}

	// Control API: the persisted setting used for the remote-control feature and
	// MCP, separate from (but shareable with) the ad-hoc pairing above.
	cfg := m.webCtrlCfg
	apiStatus := i18n.T("off")
	if cfg.Enabled {
		apiStatus = i18n.T("on") + " · " + cfg.Addr
	}
	tokenStatus := i18n.T("not set")
	if cfg.Token != "" {
		tokenStatus = i18n.T("set")
	}
	lines = append(lines,
		dim.Render("Control API"),
		"  "+i18n.T("Status")+": "+apiStatus,
		"  "+i18n.T("Token")+":  "+tokenStatus,
		"",
	)

	if m.ctrlEditToken {
		lines = append(lines,
			i18n.T("Token")+": "+m.search.View(),
			dim.Render(i18n.T("enter save · esc cancel")),
		)
	} else {
		toggle := i18n.T("enter enable")
		if m.webRemoteActive {
			toggle = i18n.T("enter disable")
		}
		lines = append(lines, dim.Render(i18n.Tf("%s · a control API on/off · t set token · esc back · q quit", toggle)))
	}

	if m.status != "" {
		lines = append(lines, "", statusSty.Render(m.status))
	}
	return padTo(lines, rows)
}
