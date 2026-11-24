package ui

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/control"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/discovery"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/i18n"
	tea "github.com/charmbracelet/bubbletea"
)

// normalizePeer turns user input into a base URL + host:port (default port 7654).
func normalizePeer(addr string) (base, hostport string) {
	return config.NormalizePeer(addr)
}

// remoteConnectCmd connects to a peer (/whoami, then status) with same-account auth.
// The local token goes only to a manually typed address, never to a discovered device,
// whose advertisement can be spoofed.
func (m *Model) remoteConnectCmd(addr string, trusted bool) tea.Cmd {
	base, hostport := normalizePeer(addr)
	if base == "" {
		return func() tea.Msg {
			return remoteConnMsg{err: fmt.Errorf("%s", i18n.T("enter a host or host:port"))}
		}
	}
	token := ""
	if trusted {
		token = config.LoadControl().Token
	}
	account := m.client.UserID()
	return func() tea.Msg {
		rc := control.NewClient(base, token, account)
		who, err := rc.Whoami()
		if err != nil {
			return remoteConnMsg{err: err}
		}
		st, _ := rc.Status() // initial snapshot; an error leaves it zero
		return remoteConnMsg{
			client: rc, addr: hostport, name: who.Name,
			clientType: who.Client, version: who.Version, state: st,
		}
	}
}

// discoverDevicesCmd scans the LAN for Connect devices; nothing is asked of them before
// the user picks one.
func (m *Model) discoverDevicesCmd() tea.Cmd {
	selfPort := 0
	if m.ctrl != nil {
		if _, port, err := net.SplitHostPort(m.ctrl.Addr()); err == nil {
			selfPort, _ = strconv.Atoi(port)
		}
	}
	return func() tea.Msg {
		devs, _ := discovery.ProbePeers(700*time.Millisecond, selfPort)
		peers := make([]peerDevice, 0, len(devs))
		for _, d := range devs {
			peers = append(peers, peerDevice{dev: d})
		}
		return devicesDiscoveredMsg{peers: peers}
	}
}

// peer returns the connected peer while its screens are shown: the remote
// screen and the lyrics opened from it. Elsewhere keys act locally.
func (m *Model) peer() *control.Client {
	if m.screen == screenRemoteCtl || (m.screen == screenLyrics && m.prevScreen == screenRemoteCtl) {
		return m.remote
	}
	return nil
}

// detachRemote stops driving the peer (its playback goes on).
func (m *Model) detachRemote() {
	m.remote = nil
	m.remoteState = control.State{}
	if m.prevScreen == screenRemoteCtl {
		m.prevScreen = screenMenu
	}
}

// remotePoll fetches the peer's status, one request at a time.
func (m *Model) remotePoll() tea.Cmd {
	if m.remotePolling {
		return nil
	}
	m.remotePolling = true
	rc := m.remote
	return func() tea.Msg {
		st, err := rc.Status()
		return remoteStateMsg{client: rc, poll: true, state: st, err: err}
	}
}

// remoteCmd runs a peer command and reports the resulting status.
func remoteCmd(rc *control.Client, call func() (control.State, error)) tea.Cmd {
	return func() tea.Msg {
		st, err := call()
		return remoteStateMsg{client: rc, state: st, err: err}
	}
}

// remoteKey maps a transport key to a peer command. Seek and volume update the
// local copy of the peer's state at once, so repeated presses add up.
func (m *Model) remoteKey(rc *control.Client, key string) (tea.Cmd, bool) {
	switch key {
	case " ":
		return remoteCmd(rc, rc.PlayPause), true
	case "n":
		return remoteCmd(rc, rc.Next), true
	case "p":
		return remoteCmd(rc, rc.Prev), true
	case "s":
		return remoteCmd(rc, rc.Stop), true
	case "r":
		return remoteCmd(rc, rc.CycleRepeat), true
	case "z":
		return remoteCmd(rc, rc.ToggleShuffle), true
	case "+", "=", "-", "_":
		d := 0.1
		if key == "-" || key == "_" {
			d = -d
		}
		v := clamp01(m.remoteState.Volume + d)
		m.remoteState.Volume = v
		return remoteCmd(rc, func() (control.State, error) { return rc.SetVolume(v) }), true
	case "left", "right":
		d := int64(10000)
		if key == "left" {
			d = -d
		}
		ms := max(0, m.remoteState.PositionMS+d)
		m.remoteState.PositionMS = ms
		return remoteCmd(rc, func() (control.State, error) { return rc.SeekMS(ms) }), true
	}
	return nil, false
}

// handleRemoteKey drives the connected peer from the remote-control screen.
func (m *Model) handleRemoteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rc := m.remote
	if rc == nil {
		m.showMenu()
		return m, nil
	}
	switch msg.String() {
	case "esc", "backspace":
		// Leaving the remote screen stops driving the peer but not its music, and makes no
		// synchronous HTTP call that could freeze the update loop. 'S' stops the peer on the
		// way out.
		m.detachRemote()
		m.showMenu()
		m.status = i18n.T("Disconnected from remote")
		return m, nil
	case "S":
		// Stop the peer's playback AND detach. The Stop runs as a tea.Cmd (off the
		// update loop) so an unreachable peer can't block; its result is discarded
		// since we've already left the screen.
		m.detachRemote()
		m.showMenu()
		m.status = i18n.T("Stopped remote and disconnected")
		return m, remoteStopDetachCmd(rc)
	case "Q":
		return m, tea.Quit
	case "l":
		// Show the connected peer's now-playing lyrics (the peer drives position).
		if m.remoteState.Track == nil {
			return m, nil
		}
		t := remoteTrack(m.remoteState.Track)
		m.toggleScreen(screenLyrics)
		if m.screen == screenLyrics && (m.lyrics == nil || m.lyricsTrack != t.ID) {
			m.lyricsTrack = t.ID
			m.loading = true
			m.status = i18n.T("Loading…")
			return m, m.lyricsCmd(t)
		}
		return m, nil
	}
	cmd, _ := m.remoteKey(rc, msg.String())
	return m, cmd
}

// remoteStopDetachCmd stops the peer's playback in the background after the user left
// the remote screen ('S'), off the update loop, discarding the result.
func remoteStopDetachCmd(rc *control.Client) tea.Cmd {
	if rc == nil {
		return nil
	}
	return func() tea.Msg {
		_, _ = rc.Stop()
		return nil
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// remoteEntryView is the connect screen (address input).
func (m *Model) remoteEntryView(rows int) string {
	lines := []string{
		"📡 " + i18n.T("Remote control") + " — " + i18n.T("drive another OpenDeezer client"),
		"",
		i18n.T("Peer address (host or host:port, default port 7654):"),
		"  " + m.search.View(),
		"",
		i18n.T("The peer must have its control API enabled (OPENDEEZER_CONTROL=:7654),"),
		i18n.T("and be on the same Deezer account (or share a token)."),
		"",
		i18n.T("enter connect · esc cancel"),
	}
	if m.status != "" {
		lines = append(lines, "", m.status)
	}
	return padTo(lines, rows)
}

// remoteCtlView shows the connected peer's playback + remote key hints.
func (m *Model) remoteCtlView(rows int) string {
	st := m.remoteState
	name := m.remoteName
	if name == "" {
		name = m.remoteAddr
	}
	track := "—"
	if st.Track != nil {
		track = st.Track.Title
		if st.Track.Artist != "" {
			track += " — " + st.Track.Artist
		}
	}
	state := st.State
	if state == "" {
		state = "unknown"
	}
	repeat := st.Repeat
	if repeat == "" {
		repeat = "off"
	}
	device := deviceTypeLabel(m.remoteClient)
	if m.remoteVersion != "" {
		device += " · OpenDeezer v" + m.remoteVersion
	}
	lines := []string{
		"📡 " + i18n.Tf("Connected to %s", name) + "  (" + m.remoteAddr + ")",
		i18n.T("Device") + ": " + device,
		"",
		i18n.T("State") + ":  " + i18n.T(state),
		i18n.T("Track") + ":  " + track,
		i18n.T("Time") + ":   " + fmtMS(st.PositionMS) + " / " + fmtMS(st.DurationMS),
		i18n.T("Volume") + ": " + strconv.Itoa(int(st.Volume*100+0.5)) + "%" +
			"   " + i18n.T("Repeat") + ": " + i18n.T(repeat) + "   " + i18n.T("Shuffle") + ": " + boolLabel(st.Shuffle),
		"",
		i18n.T("space play/pause · n next · p prev · s stop · ←/→ seek ±10s · +/- volume"),
		i18n.T("r repeat · z shuffle · esc disconnect (peer keeps playing) · S stop & disconnect"),
	}
	if m.status != "" {
		lines = append(lines, "", m.status)
	}
	return padTo(lines, rows)
}

func boolLabel(b bool) string {
	if b {
		return i18n.T("on")
	}
	return i18n.T("off")
}
