package ui

import (
	"strings"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/audio"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/i18n"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/queue"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// ScreenReaderAtStart reports whether the app starts in screen-reader mode: the
// saved choice, else on while the system says a screen reader runs.
func ScreenReaderAtStart() bool {
	switch LoadScreenReader() {
	case "on":
		return true
	case "off":
		return false
	}
	return screenReaderRunning()
}

// SetScreenReader turns screen-reader mode on or off without touching the
// terminal; main picks the matching program options.
func (m *Model) SetScreenReader(on bool) { m.sr = on }

// toggleScreenReader flips the mode, saves it and switches the terminal: the
// plain inline frame without mouse reporting, or the full-screen view with it.
func (m *Model) toggleScreenReader() tea.Cmd {
	m.sr = !m.sr
	_ = SaveScreenReader(m.sr)
	if m.sr {
		return tea.Sequence(tea.ExitAltScreen, tea.DisableMouse, tea.ClearScreen)
	}
	return tea.Sequence(tea.EnterAltScreen, tea.EnableMouseCellMotion)
}

// srView is the screen-reader frame: a focus line saying where the cursor is and
// what it is on, then an event line with the status or the playback state. Plain
// text only, no line wider than the terminal, nothing that changes on its own.
func (m *Model) srView() string {
	var lines []string
	switch {
	case !m.ready || m.screen == screenLoading || m.screen == screenNoInternet:
		lines = m.srSplash()
	case m.screen == screenHelp:
		lines = m.srHelp()
	case m.screen == screenCredits:
		lines = []string{"Purple Disc for Deezer " + Version,
			i18n.T("Unofficial open-source client for Deezer."),
			i18n.T("by Scharkenberg, based on OpenDeezer by Cycl0o0")}
		if s := m.srEvent(); s != "" {
			lines = append(lines, s)
		}
	default:
		lines = []string{m.srFocus()}
		if s := m.srEvent(); s != "" {
			lines = append(lines, s)
		}
	}
	for i, s := range lines {
		s = strings.ReplaceAll(s, "\n", " ")
		if m.width > 0 {
			s = ansi.Truncate(s, m.width, "…")
		}
		lines[i] = s
	}
	return strings.Join(lines, "\n")
}

// srSplash is the login and no-internet screen as text.
func (m *Model) srSplash() []string {
	if m.screen == screenNoInternet {
		hint := i18n.T("r to retry") + ", " + i18n.T("q to quit")
		if m.loading {
			hint = i18n.T("Reconnecting…")
		}
		return []string{i18n.T("No internet connection"), hint}
	}
	if m.status != "" {
		return []string{m.status}
	}
	return []string{i18n.T("Loading…")}
}

// srHelp is the help table one key per line under its group's name, from the
// scroll offset, as many lines as the terminal has rows.
func (m *Model) srHelp() []string {
	var lines []string
	for _, g := range helpGroups {
		lines = append(lines, i18n.T(g.title))
		for _, b := range g.binds {
			if b[0] == "mouse" {
				continue
			}
			lines = append(lines, b[0]+": "+i18n.T(b[1]))
		}
	}
	rows := len(lines)
	if m.height > 0 {
		rows = min(rows, m.height)
	}
	m.helpOffset = min(max(0, m.helpOffset), len(lines)-rows)
	return lines[m.helpOffset : m.helpOffset+rows]
}

// srJoin joins the non-empty parts with commas.
func srJoin(parts ...string) string {
	out := parts[:0:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ", ")
}

// srAt is "title, n of total: what", or the title alone for an empty list.
func srAt(title string, n, total int, what string) string {
	if total == 0 {
		return title
	}
	return title + ", " + i18n.Tf("%d of %d", n, total) + ": " + what
}

// srInput is an input's label and what has been typed, masked like the input.
func srInput(label string, in textinput.Model) string {
	v := in.Value()
	if in.EchoMode != textinput.EchoNormal {
		v = strings.Repeat("*", len([]rune(v)))
	}
	return label + ": " + v
}

// srFocus is the focus line of the screen in front.
func (m *Model) srFocus() string {
	switch m.screen {
	case screenSearch:
		if m.searchPodcast {
			return srInput(i18n.T("Podcasts"), m.search)
		}
		return srInput(i18n.T("Search"), m.search)
	case screenPlaylistPrompt:
		if m.plPrompt == plRename {
			return srInput(i18n.T("Rename playlist"), m.search)
		}
		return srInput(i18n.T("New playlist"), m.search)
	case screenNowPlaying:
		t, ok := m.q.Current()
		if !ok {
			return i18n.T("Nothing playing.")
		}
		return i18n.Tf("Now playing: %s", srJoin(t.Name, t.ArtistLine(), t.AlbumName))
	case screenQueue:
		ts := m.q.Tracks()
		if len(ts) == 0 {
			return i18n.T("Queue is empty.")
		}
		m.queueSel = min(max(0, m.queueSel), len(ts)-1)
		t := ts[m.queueSel]
		playing := ""
		if m.queueSel == m.q.Index() && m.playing {
			playing = i18n.T("playing")
		}
		return srAt(i18n.T("Queue"), m.queueSel+1, len(ts), srJoin(t.Name, t.ArtistLine(), playing))
	case screenLyrics:
		return m.srLyric()
	case screenRemoteInput:
		return srInput(i18n.T("Remote control"), m.search)
	case screenRemoteCtl:
		return m.srRemote()
	case screenWebRemote:
		return m.srWebRemote()
	}
	if !m.onListScreen() {
		return m.screenTitle()
	}
	if m.list.FilterState() == list.Filtering {
		return srInput(i18n.T("Filter"), m.list.FilterInput)
	}
	title := m.list.Title
	if m.screen == screenMenu {
		title = i18n.T("Home")
	}
	n, at := 0, 0
	for i, it := range m.list.VisibleItems() {
		if it.(row).kind == rowSection {
			continue
		}
		n++
		if i <= m.list.Index() {
			at = n
		}
	}
	r, ok := m.list.SelectedItem().(row)
	if !ok || n == 0 {
		return title
	}
	return srAt(title, max(1, at), n, m.srRow(r))
}

// srRow says a list row in a few words: its title, then what kind it is and the
// facts that matter for it.
func (m *Model) srRow(r row) string {
	cur, ok := m.q.Current()
	playing := ""
	if ok && m.playing && cur.ID != "" && (r.track.ID == cur.ID || r.episode.ID == cur.ID || r.histID == cur.ID) {
		playing = i18n.T("playing")
	}
	plain := func(s string) string { return strings.ReplaceAll(s, " · ", ", ") }
	switch r.kind {
	case rowTrack:
		var explicit, liked string
		if r.track.Explicit {
			explicit = i18n.T("explicit")
		}
		if m.likedIDs[r.track.ID] {
			liked = i18n.T("liked")
		}
		return srJoin(r.title, r.track.ArtistLine(), r.right, explicit, liked, playing)
	case rowAlbum:
		return srJoin(r.title, i18n.T("album"), r.desc)
	case rowArtist:
		return srJoin(r.title, i18n.T("artist"))
	case rowPlaylist:
		return srJoin(r.title, i18n.T("playlist"))
	case rowPodcast:
		return srJoin(r.title, i18n.T("podcast"))
	case rowEpisode:
		return srJoin(r.title, i18n.T("episode"), playing)
	case rowSetting:
		return srJoin(r.title, r.right)
	case rowDevice:
		current := ""
		if r.desc == i18n.T("current") {
			current = r.desc
		}
		return srJoin(strings.TrimPrefix(r.title, "● "), current)
	case rowHistory, rowInfo, rowPeer:
		return srJoin(r.title, plain(r.desc), playing)
	}
	return r.title
}

// srLyric is the lyrics line at the playback position, or the scrolled-to line
// of plain lyrics.
func (m *Model) srLyric() string {
	_, ok := m.q.Current()
	pos := m.player.PositionMS()
	if m.peer() != nil && m.remoteState.Track != nil {
		ok, pos = true, m.remoteState.PositionMS
	}
	switch {
	case !ok:
		return i18n.T("Nothing playing.")
	case m.lyrics == nil:
		return i18n.T("(loading lyrics…)")
	case m.lyrics.IsSynced():
		line := ""
		for _, ln := range m.lyrics.Synced {
			if ln.TimeMS <= pos {
				line = ln.Text
			}
		}
		if strings.TrimSpace(line) == "" {
			return i18n.T("Lyrics")
		}
		return line
	case m.lyrics.Plain == "":
		return i18n.T("(no lyrics available)")
	}
	body := strings.Split(m.lyrics.Plain, "\n")
	m.lyricsOffset = min(max(0, m.lyricsOffset), len(body)-1)
	if s := strings.TrimSpace(body[m.lyricsOffset]); s != "" {
		return s
	}
	return i18n.T("Lyrics")
}

// srRemote is the connected peer and what it plays, without its position.
func (m *Model) srRemote() string {
	st := m.remoteState
	if st.Track == nil {
		return m.screenTitle()
	}
	return m.screenTitle() + ": " + srJoin(st.Track.Title, st.Track.Artist, i18n.T(st.State))
}

// srWebRemote is the pairing code and address, or that the web remote is off.
func (m *Model) srWebRemote() string {
	if m.ctrlEditToken {
		return srInput(i18n.T("Token"), m.search)
	}
	if !m.webRemoteActive {
		return i18n.T("Web remote is off.")
	}
	return srJoin(i18n.T("Web Remote"), i18n.T("Code")+" "+m.webRemotePairingCode(), m.webRemoteURL)
}

// srEvent is the event line: the status, the delete question, else the playback
// state.
func (m *Model) srEvent() string {
	switch {
	case m.signOutConfirm:
		return signOutQuestion()
	case m.status != "":
		return m.status
	case m.plConfirm:
		return i18n.Tf("Delete playlist %s? (y/n)", m.plTarget.Name)
	}
	return m.srPlayback()
}

// srPlayback says the playback state in a few words; it changes with the state
// and the track only.
func (m *Model) srPlayback() string {
	cur, ok := m.q.Current()
	if !ok {
		return i18n.T("Nothing playing.")
	}
	switch st := m.player.State(); {
	case st == audio.Paused:
		return i18n.T("Paused")
	case st == audio.Errored:
		return i18n.Tf("stopped — %s", m.player.LastError())
	case st == audio.Playing || m.playing:
		return i18n.Tf("Playing: %s", srJoin(cur.Name, cur.ArtistLine()))
	}
	return i18n.T("Stopped")
}

// whereStatus is the w key's answer: position, state, track and the modes that
// are on, then the volume.
func (m *Model) whereStatus() string {
	var parts []string
	if cur, ok := m.q.Current(); ok {
		dur := m.player.DurationMS()
		if dur <= 0 {
			dur = cur.DurationMS
		}
		state := i18n.T("stopped")
		switch st := m.player.State(); {
		case st == audio.Paused:
			state = i18n.T("paused")
		case st == audio.Playing || m.playing:
			state = i18n.T("playing")
		}
		parts = append(parts, i18n.Tf("%s of %s", fmtMS(m.player.PositionMS()), fmtMS(dur)), state, cur.Name, cur.ArtistLine())
	} else {
		parts = append(parts, i18n.T("Nothing playing."))
	}
	parts = append(parts, modeParts(m.q.Shuffle(), m.q.Repeat() == queue.RepeatAll, m.q.Repeat() == queue.RepeatOne,
		m.player.SleepActive(), m.player.SleepEndOfTrack(), m.player.SleepRemainingMS(), m.player.Volume())...)
	return srJoin(parts...)
}

// remoteWhereStatus is whereStatus for the peer the remote-control screen drives.
func (m *Model) remoteWhereStatus() string {
	st := m.remoteState
	var parts []string
	if st.Track != nil {
		dur := st.DurationMS
		if dur <= 0 {
			dur = st.Track.DurationMS
		}
		parts = append(parts, i18n.Tf("%s of %s", fmtMS(st.PositionMS), fmtMS(dur)), i18n.T(st.State), st.Track.Title, st.Track.Artist)
	} else {
		parts = append(parts, i18n.T("Nothing playing."))
	}
	parts = append(parts, modeParts(st.Shuffle, st.Repeat == "all", st.Repeat == "one",
		st.SleepActive, st.SleepEndOfTrack, st.SleepRemainingMS, st.Volume)...)
	return srJoin(parts...)
}

// modeParts names the playback modes that are on, then the volume.
func modeParts(shuffle, repeatAll, repeatOne, sleep, sleepEnd bool, sleepMS int64, vol float64) []string {
	var parts []string
	if shuffle {
		parts = append(parts, i18n.T("shuffle"))
	}
	if repeatAll {
		parts = append(parts, i18n.T("repeat"))
	}
	if repeatOne {
		parts = append(parts, i18n.T("repeat 1"))
	}
	if sleep {
		if sleepEnd {
			parts = append(parts, i18n.T("sleep end"))
		} else {
			parts = append(parts, i18n.Tf("sleep %d min", int((sleepMS+59999)/60000)))
		}
	}
	return append(parts, volStatus(vol))
}
