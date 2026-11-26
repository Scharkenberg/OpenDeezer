package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/audio"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/i18n"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/queue"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/version"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	accentColor = palettes[0].accent
	accent      = lipgloss.NewStyle().Foreground(palettes[0].accent).Bold(true)
	dim         = lipgloss.NewStyle().Foreground(palettes[0].dim)
	barEmpty    = lipgloss.NewStyle().Foreground(palettes[0].empty)
	statusSty   = lipgloss.NewStyle().Foreground(palettes[0].status)
	selBg       = palettes[0].sel
)

// chip draws s padded by a space on each side on background bg: the header's title
// and view (on the accent), the footer's badges (on the selection background).
func chip(s string, bg lipgloss.Color) string {
	fg := lipgloss.Color("#F0F0F0")
	if bg == accentColor {
		fg = "#111111"
	}
	return lipgloss.NewStyle().Foreground(fg).Background(bg).Bold(bg == accentColor).Render(" " + s + " ")
}

// views are the header's view switches: the key that opens each, its label and screen.
var views = []struct {
	key, label string
	s          screen
}{{"", "Browse", screenList}, {"c", "Playing", screenNowPlaying}, {"u", "Queue", screenQueue}, {"l", "Lyrics", screenLyrics}}

func viewLabel(i int) string {
	if views[i].key == "" {
		return i18n.T(views[i].label)
	}
	return views[i].key + " " + i18n.T(views[i].label)
}

// hints are the keys of each screen, shown on the footer's last row.
var hints = map[screen]string{
	screenMenu:           "enter open · / search · , settings · ? help · q quit",
	screenList:           "enter play · e queue · E next · f like · a add · m radio · / search · , settings · ? help",
	screenSettings:       "enter choose · esc back",
	screenDevices:        "enter choose · esc back",
	screenPlaylistPick:   "enter choose · esc back",
	screenRemote:         "enter choose · esc back",
	screenSearch:         "enter search · esc cancel",
	screenPlaylistPrompt: "enter save · esc cancel",
	screenNowPlaying:     "space pause · n/p next/prev · ←/→ seek · f like · b album · m radio · esc back",
	screenLyrics:         "space pause · n/p next/prev · ←/→ seek · f like · b album · m radio · esc back",
	screenQueue:          "enter play · x remove · J/K move · m radio · esc back",
	screenHelp:           "↑/↓ scroll · esc back",
	screenCredits:        "enter check for updates · esc back",
	screenRemoteInput:    "enter connect · esc cancel",
	screenRemoteCtl:      "space pause · n/p next/prev · s stop · ←/→ seek · +/- volume · r repeat · z shuffle · l lyrics · esc disconnect · S stop & disconnect",
	screenWebRemote:      "enter on/off · a control API on/off · t set token · esc back",
}

// layout is the frame's geometry; drawing and the mouse both read it.
type layout struct {
	top    int      // first body row: 1, or 2 under the header's rule (from 30 rows)
	rows   int      // body rows
	main   int      // width of the screen's column
	panel  int      // width of the side panel right of main and its │, 0 when none
	head   int      // column header rows above a track list or the queue (0 or 1)
	footer int      // footer rows: progress, now playing and the hint; no hint under 8 rows
	bar    int      // row of the progress bar
	barX   int      // first column of the bar
	barW   int      // width of the bar
	narrow bool     // under 60 columns: no descriptions, no view switches
	tabs   [][2]int // first and last+1 column of each view switch; nil when not shown
}

// shown is the screen whose body is drawn: the search and title inputs leave the
// screen they were opened from under them.
func (m *Model) shown() screen {
	switch m.screen {
	case screenSearch:
		return m.searchReturn
	case screenPlaylistPrompt:
		return m.plReturn
	}
	return m.screen
}

// layout computes the geometry for the window's size and the screen in front.
func (m *Model) layout() layout {
	w, h := max(1, m.width), max(1, m.height)
	l := layout{top: 1, main: w, footer: 3, narrow: w < 60}
	if h < 8 {
		l.footer = 2
	}
	if h >= 30 {
		l.top = 2
	}
	l.rows = max(0, h-l.top-l.footer)
	l.bar = h - l.footer
	s := m.shown()
	if w >= 100 && l.rows >= 16 && s != screenNowPlaying {
		l.panel = min(max(w/3, 32), 48)
		l.main = w - l.panel - 1
	}
	if (s == screenList && m.hasTrackRows()) || (s == screenQueue && m.q.Len() > 0) {
		l.head = 1
	}
	tw := lipgloss.Width(fmtMS(m.player.DurationMS()))
	l.barX, l.barW = tw+2, w-2*tw-4

	tabsW := len(views) - 1
	for i := range views {
		tabsW += lipgloss.Width(viewLabel(i)) + 2
	}
	if l.narrow || w < tabsW+20 || m.screen == screenSearch || m.screen == screenPlaylistPrompt ||
		m.screen == screenRemoteInput || m.ctrlEditToken || m.list.FilterState() == list.Filtering {
		return l
	}
	x := w - 1 - tabsW
	for i := range views {
		lw := lipgloss.Width(viewLabel(i)) + 2
		l.tabs = append(l.tabs, [2]int{x, x + lw})
		x += lw + 1
	}
	return l
}

// seekAt maps column x of the progress bar to a position in a track dur ms long.
func (l layout) seekAt(x int, dur int64) (int64, bool) {
	if dur <= 0 || l.barW < 2 || x < l.barX || x >= l.barX+l.barW {
		return 0, false
	}
	return dur * int64(x-l.barX) / int64(l.barW), true
}

// hasTrackRows reports whether the list shows track rows, which get a column header.
func (m *Model) hasTrackRows() bool {
	for _, it := range m.list.VisibleItems() {
		if it.(row).kind == rowTrack {
			return true
		}
	}
	return false
}

// View renders the whole screen: the header row (and its rule from 30 rows), the
// body with the side panel when it fits, and the footer.
func (m *Model) View() string {
	if !m.ready || m.screen == screenLoading || m.screen == screenNoInternet {
		return m.splashView()
	}
	l := m.layout()
	w := max(1, m.width)
	m.list.SetSize(l.main, max(1, l.rows-l.head))
	lines := []string{m.header(l)}
	if l.top == 2 {
		lines = append(lines, dim.Render(strings.Repeat("─", w)))
	}
	if l.rows > 0 {
		body := strings.Split(padTo(strings.Split(m.body(m.screen, l), "\n"), l.rows), "\n")
		if l.panel > 0 {
			panel := strings.Split(padTo(strings.Split(m.panelView(l), "\n"), l.rows), "\n")
			for i, b := range body {
				b = ansi.Truncate(b, l.main, "")
				body[i] = b + strings.Repeat(" ", l.main-ansi.StringWidth(b)) + dim.Render("│") + panel[i]
			}
		}
		lines = append(lines, body...)
	}
	lines = append(lines, m.progressBar(l), m.nowLine())
	if l.footer == 3 {
		lines = append(lines, m.hintLine())
	}
	lines = lines[max(0, len(lines)-max(1, m.height)):]
	for i, s := range lines {
		lines[i] = ansi.Truncate(s, w, "")
	}
	return strings.Join(lines, "\n")
}

// centered places each line in the middle of width w.
func centered(w int, lines ...string) []string {
	for i, s := range lines {
		lines[i] = lipgloss.PlaceHorizontal(max(1, w), lipgloss.Center, ansi.Truncate(s, max(1, w), "…"))
	}
	return lines
}

// splashView is the login and no-internet screen: the mark, wordmark, spinner and
// status centered.
func (m *Model) splashView() string {
	spin := ""
	if m.loading {
		spin = m.spinner.View()
	}
	text := []string{accent.Render("Purple Disc"), "", spin}
	if m.screen == screenNoInternet {
		hint := i18n.T("r to retry") + "  ·  " + i18n.T("q to quit")
		if m.loading {
			hint = i18n.T("Reconnecting…")
		}
		text = append(text, statusSty.Render(i18n.T("No internet connection")), "",
			i18n.T("Check your connection and try again."), "", dim.Render(hint))
	} else {
		text = append(text, statusSty.Render(m.status))
	}
	w, h := max(1, m.width), max(1, m.height)
	var lines []string
	if r := min(8, h-len(text)-1, (w-2)/2); r >= 3 {
		lines = append(strings.Split(markView(r), "\n"), "")
	}
	lines = append(lines, text...)
	out := make([]string, max(0, (h-len(lines))/2), h)
	return padTo(append(out, centered(w, lines...)...), h)
}

// header is the top row: the screen's title as a chip or its input on the left, the
// view switches on the right with the one in front as a chip.
func (m *Model) header(l layout) string {
	var left string
	switch {
	case m.screen == screenSearch:
		m.search.Placeholder = i18n.T("Search Deezer…")
		if m.searchPodcast {
			m.search.Placeholder = i18n.T("Podcasts")
		}
		left = accent.Render("/ ") + m.search.View()
	case m.screen == screenPlaylistPrompt:
		title := i18n.T("New playlist")
		if m.plPrompt == plRename {
			title = i18n.T("Rename playlist")
		}
		left = accent.Render(title+": ") + m.search.View()
	case m.onListScreen() && m.list.FilterState() == list.Filtering:
		left = m.list.FilterInput.View()
	case m.onListScreen():
		left = chip(m.list.Title, accentColor)
		if n := len(m.list.VisibleItems()); n > 0 {
			left += dim.Render(fmt.Sprintf("  %d/%d", m.list.Index()+1, n))
		}
	case m.screenTitle() != "":
		left = chip(m.screenTitle(), accentColor)
	}
	if l.tabs == nil {
		return " " + left
	}
	x := l.tabs[0][0]
	left = ansi.Truncate(" "+left, x-2, "…")
	out := left + strings.Repeat(" ", x-lipgloss.Width(left))
	for i, v := range views {
		if i > 0 {
			out += " "
		}
		if m.screen == v.s || (i == 0 && m.onListScreen()) {
			out += chip(viewLabel(i), accentColor)
		} else {
			out += dim.Render(" " + viewLabel(i) + " ")
		}
	}
	return out
}

// screenTitle names the screens that do not show a list.
func (m *Model) screenTitle() string {
	switch m.screen {
	case screenNowPlaying:
		return i18n.T("Now Playing")
	case screenQueue:
		return i18n.Tn("Queue (%d track)", "Queue (%d tracks)", m.q.Len())
	case screenLyrics:
		return i18n.T("Lyrics")
	case screenHelp:
		return i18n.T("Keybindings")
	case screenCredits:
		return i18n.T("About")
	case screenRemoteInput:
		return i18n.T("Remote control")
	case screenRemoteCtl:
		name := m.remoteName
		if name == "" {
			name = m.remoteAddr
		}
		return i18n.Tf("Connected to %s", name)
	case screenWebRemote:
		return i18n.T("Web Remote")
	}
	return ""
}

// body renders screen s in the layout's main column.
func (m *Model) body(s screen, l layout) string {
	switch s {
	case screenSearch:
		return m.body(m.searchReturn, l)
	case screenPlaylistPrompt:
		return m.body(m.plReturn, l)
	case screenNowPlaying:
		return m.nowPlayingView(l)
	case screenCredits:
		return m.creditsView(l.main)
	case screenQueue:
		return m.queueView(l.rows, l.main)
	case screenLyrics:
		return m.lyricsView(l.rows, l.main)
	case screenHelp:
		return m.helpView(l.rows, l.main)
	case screenRemoteInput:
		return m.remoteEntryView(l.rows)
	case screenRemoteCtl:
		return m.remoteCtlView(l.rows)
	case screenWebRemote:
		return m.webRemoteView(l.rows, l.main)
	}
	if l.head == 1 {
		return m.columnHeader(l.main, 3) + "\n" + m.list.View()
	}
	return m.list.View()
}

// pairs lays label and value rows in two columns: the dim labels, the values one
// column after the longest label.
func pairs(rows [][2]string) []string {
	w := 0
	for _, r := range rows {
		w = max(w, ansi.StringWidth(r[0]))
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = " " + dim.Render(r[0]+strings.Repeat(" ", w-ansi.StringWidth(r[0])+1)) + r[1]
	}
	return out
}

// Version is the app version, set from main at startup (defaults to the
// release number so library users get the right value without main).
var Version = version.Number

func (m *Model) creditsView(width int) string {
	lines := centered(width, append(strings.Split(markView(6), "\n"),
		accent.Render("Purple Disc for Deezer")+dim.Render(" "+Version),
		dim.Render(i18n.T("Unofficial open-source client for Deezer.")), "")...)
	lines = append(lines, pairs([][2]string{
		{"", i18n.T("by Scharkenberg, based on OpenDeezer by Cycl0o0")},
		{i18n.T("Built with:"), "Bubble Tea / Bubbles / Lip Gloss — Charm"},
		{"", "miniaudio (malgo) — David Reid / gen2brain · oto — Ebitengine"},
		{"", "minimp3 — lieff (CC0) · dr_flac — David Reid (public domain)"},
		{"", "go-mp3 — Hajime Hoshi · flac — mewkiz · x/crypto — Go authors"},
	})...)
	lines = append(lines, "",
		" "+dim.Render(i18n.T("Audio is decrypted and decoded on this device. Your ARL goes only to Deezer.")),
		" "+dim.Render(i18n.T("AGPL-3.0. Not affiliated with Deezer.")), "")
	switch {
	case m.updateChecking:
		lines = append(lines, " "+statusSty.Render(i18n.T("Checking for updates…")))
	case m.updateInfo.HasUpdate:
		lines = append(lines, " "+statusSty.Render(i18n.Tf("Update available: v%s", m.updateInfo.Latest)))
	case m.updateChecked:
		lines = append(lines, " "+dim.Render(i18n.T("You're on the latest version.")))
	}
	return strings.Join(lines, "\n")
}

// trackText is the current track's title, artist, album and codec, then Next and the
// upcoming tracks that fit in rows lines (none when upcoming is false).
func (m *Model) trackText(title lipgloss.Style, rows int, upcoming bool) []string {
	t, _ := m.q.Current()
	text := []string{title.Render(t.Name), t.ArtistLine(), dim.Render(t.AlbumName)}
	if f := codecLabel(m.player.Format()); f != "" {
		text = append(text, dim.Render(f))
	}
	if next := m.q.Tracks()[m.q.Index()+1:]; upcoming && len(next) > 0 && rows-len(text) > 2 {
		text = append(text, "", dim.Render(i18n.T("Next")))
		for _, n := range next[:min(len(next), rows-len(text))] {
			text = append(text, dim.Render(n.Name+" · "+n.ArtistLine()))
		}
	}
	return text
}

// coverOr is the cover cols×rows cells wide, or the line that says why there is none.
func (m *Model) coverOr(cols, rows int) string {
	if c := m.coverAt(cols, rows); c != "" {
		return c
	}
	switch {
	case !artworkSupported():
		return dim.Render(i18n.T("(artwork needs a 256-color / truecolor terminal)"))
	case m.playing:
		return dim.Render(i18n.T("(loading cover…)"))
	}
	return dim.Render(i18n.T("(no cover)"))
}

// nowPlayingView centers the cover and the track's text as one block in the body: the
// text beside the cover and centered on it, or under it below 60 columns.
func (m *Model) nowPlayingView(l layout) string {
	if _, ok := m.q.Current(); !ok {
		return strings.Join(centered(l.main, dim.Render(i18n.T("Nothing playing."))), "\n")
	}
	var block string
	if l.narrow {
		r := min(max(0, l.rows-8), (l.main-2)/2)
		text := m.trackText(accent, l.rows-r-1, true)
		block = lipgloss.JoinVertical(lipgloss.Center, m.coverOr(2*r, r), "", strings.Join(text, "\n"))
	} else {
		r := min(l.rows, l.main/4)
		text := m.trackText(accent, l.rows, true)
		block = lipgloss.JoinHorizontal(lipgloss.Center, m.coverOr(2*r, r), "   ", strings.Join(text, "\n"))
	}
	return lipgloss.Place(l.main, l.rows, lipgloss.Center, lipgloss.Center, block)
}

// panelView is the side panel: while a track plays its cover and text, otherwise the
// mark, the account and the latest local listening history.
func (m *Model) panelView(l layout) string {
	w := l.panel
	var lines []string
	if _, ok := m.q.Current(); ok && m.playing {
		r := min((w-2)/2, l.rows-6)
		if c := m.coverAt(2*r, r); c != "" {
			lines = append(strings.Split(c, "\n"), "")
		}
		title := accent
		if m.tint != "" {
			title = lipgloss.NewStyle().Foreground(m.tint).Bold(true)
		}
		lines = append(lines, m.trackText(title, l.rows-len(lines), m.shown() != screenQueue)...)
	} else {
		lines = centered(w-2, append(strings.Split(markView(6), "\n"), accent.Render("Purple Disc"), "")...)
		if m.acct.Name != "" {
			lines = append(lines, m.acct.Name)
		}
		if m.acct.Offer != "" {
			lines = append(lines, dim.Render(m.acct.Offer))
		}
		if n := l.rows - len(lines) - 2; m.hist != nil && n > 0 {
			if es, _ := m.hist.Recent(n); len(es) > 0 {
				lines = append(lines, "", accent.Render(i18n.T("Recently played")))
				for _, e := range es {
					lines = append(lines, e.Title+dim.Render(" · "+e.Artist))
				}
			}
		}
	}
	for i, s := range lines {
		lines[i] = " " + ansi.Truncate(s, w-2, "…")
	}
	return strings.Join(lines, "\n")
}

// codecLabel names the codec of a raw Deezer media format (MP3, AAC or FLAC),
// without its bitrate.
func codecLabel(raw string) string {
	u := strings.ToUpper(raw)
	for _, c := range []string{"FLAC", "AAC", "MP3"} {
		if strings.Contains(u, c) {
			return c
		}
	}
	return raw
}

// padTo joins lines and pads with blanks to fill n rows.
func padTo(lines []string, n int) string {
	if n < 1 {
		n = 1
	}
	rendered := strings.Split(strings.Join(lines, "\n"), "\n")
	if len(rendered) > n {
		rendered = rendered[:n]
	}
	for len(rendered) < n {
		rendered = append(rendered, "")
	}
	return strings.Join(rendered, "\n")
}

// progressBar is the footer's first row: position, the bar with its knob, length. While
// a track plays the filled part takes the cover's tint.
func (m *Model) progressBar(l layout) string {
	pos, dur := m.player.PositionMS(), m.player.DurationMS()
	length := fmtMS(dur)
	at := fmt.Sprintf("%*s", len(length), fmtMS(pos))
	if l.barW < 2 {
		return " " + at + " " + length
	}
	filled := 0
	if dur > 0 {
		filled = min(max(0, int(int64(l.barW)*pos/dur)), l.barW-1)
	}
	fill := lipgloss.NewStyle().Foreground(accentColor)
	if m.tint != "" && m.playing {
		fill = fill.Foreground(m.tint)
	}
	return " " + at + " " + fill.Render(strings.Repeat("━", filled)+"●") +
		barEmpty.Render(strings.Repeat("─", l.barW-1-filled)) + " " + length
}

// stateGlyph is the player's state: playing, paused, loading (the spinner), error.
func (m *Model) stateGlyph() string {
	switch m.player.State() {
	case audio.Paused:
		return "‖"
	case audio.Loading:
		return m.spinner.View()
	case audio.Errored:
		return "!" // the error itself is in the status line (tick)
	}
	return "►"
}

// nowLine is the footer's second row: state glyph, title and artist on the left, the
// badges that are on at the right as chips.
func (m *Model) nowLine() string {
	st := m.player.State()
	cur, hasCur := m.q.Current()
	var left string
	var badges []string
	if hasCur && (m.playing || st == audio.Playing || st == audio.Paused || st == audio.Errored) {
		left = accent.Render(m.stateGlyph()) + " " + cur.Name + dim.Render(" · "+cur.ArtistLine())
		if m.likedIDs[cur.ID] {
			badges = append(badges, accent.Render("♥"))
		}
		if f := codecLabel(m.player.Format()); f != "" {
			badges = append(badges, chip(f, selBg))
		}
		if m.player.IsPreview() {
			badges = append(badges, chip(i18n.T("preview"), selBg))
		}
	} else if e := m.player.LastError(); e != "" {
		left = dim.Render("■ " + i18n.Tf("stopped — %s", e))
	} else {
		left = dim.Render("■ " + i18n.T("nothing playing"))
	}
	if m.q.Shuffle() {
		badges = append(badges, chip(i18n.T("shuffle"), selBg))
	}
	switch m.q.Repeat() {
	case queue.RepeatAll:
		badges = append(badges, chip(i18n.T("repeat"), selBg))
	case queue.RepeatOne:
		badges = append(badges, chip(i18n.T("repeat 1"), selBg))
	}
	if m.player.SleepActive() {
		if m.player.SleepEndOfTrack() {
			badges = append(badges, chip(i18n.T("sleep end"), selBg))
		} else {
			badges = append(badges, chip(i18n.Tf("sleep %dm", int((m.player.SleepRemainingMS()+59999)/60000)), selBg))
		}
	}
	v := int(m.player.Volume()*5 + 0.5)
	badges = append(badges, chip(i18n.T("vol")+" "+strings.Repeat("▮", v)+strings.Repeat("▯", 5-v), selBg))
	right := strings.Join(badges, " ")
	left = ansi.Truncate(" "+left, max(0, m.width-lipgloss.Width(right)-3), "…")
	return left + strings.Repeat(" ", max(1, m.width-1-lipgloss.Width(left)-lipgloss.Width(right))) + right
}

// hintLine is the footer's last row: the status or the delete confirmation while one
// is shown, else the keys of the screen in front.
func (m *Model) hintLine() string {
	s := m.status
	if m.plConfirm {
		s = i18n.Tf("Delete playlist %s? (y/n)", m.plTarget.Name)
	}
	if s != "" {
		if m.loading {
			s = m.spinner.View() + s
		}
		return " " + statusSty.Render(ansi.Truncate(s, max(0, m.width-2), "…"))
	}
	h := i18n.T(hints[m.screen])
	for lipgloss.Width(h) > m.width-2 {
		i := strings.LastIndex(h, " · ")
		if i < 0 {
			h = ansi.Truncate(h, max(0, m.width-2), "…")
			break
		}
		h = h[:i]
	}
	return " " + dim.Render(h)
}

func fmtMS(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	s := ms / 1000
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// queueWindow computes the queue view's scroll window: the first entry shown and how
// many fit. Centered on the cursor and clamped, so mouse clicks can map body rows back
// to entries.
func (m *Model) queueWindow(rows, n int) (start, visible int) {
	visible = max(1, rows)
	sel := min(max(0, m.queueSel), n-1)
	start = sel - visible/2
	start = min(start, n-visible)
	start = max(start, 0)
	return start, visible
}

// queueView lists the queued tracks as list rows with their index under the column
// header. Rows are edited in place (enter jump, x remove, J/K move; see handleKey).
func (m *Model) queueView(rows, width int) string {
	ts := m.q.Tracks()
	if len(ts) == 0 {
		return dim.Render(" " + i18n.T("Queue is empty."))
	}
	m.queueSel = min(max(0, m.queueSel), len(ts)-1) // clamp after external edits
	start, visible := m.queueWindow(rows-1, len(ts))
	lines := []string{m.columnHeader(width, 7)}
	for i := start; i < len(ts) && i < start+visible; i++ {
		lines = append(lines, m.rowLine(trackRow(ts[i]), i+1, i == m.q.Index(), i == m.queueSel, width, 0))
	}
	return strings.Join(lines, "\n")
}

// lyricsView shows lyrics centered; synced lyrics follow the playback position, the
// active line in the accent and its neighbors plain.
func (m *Model) lyricsView(bodyRows, width int) string {
	// When driving a remote peer, show its now-playing track and position (the
	// local queue/player are unrelated to what the peer is playing).
	t, ok := m.q.Current()
	pos := m.player.PositionMS()
	if m.peer() != nil && m.remoteState.Track != nil {
		t, ok = remoteTrack(m.remoteState.Track), true
		pos = m.remoteState.PositionMS
	}
	center := func(lines ...string) string { return strings.Join(centered(width, lines...), "\n") }
	if !ok {
		return center(dim.Render(i18n.T("Nothing playing.")))
	}
	header := accent.Render(t.Name) + dim.Render(" — "+t.ArtistLine())
	if m.lyrics == nil {
		return center(header, "", dim.Render(i18n.T("(loading lyrics…)")))
	}
	rows := max(1, bodyRows-2)

	if m.lyrics.IsSynced() {
		active := 0
		for i, ln := range m.lyrics.Synced {
			if ln.TimeMS <= pos {
				active = i
			}
		}
		lines := []string{header, ""}
		start := max(0, active-rows/2)
		for i := start; i < len(m.lyrics.Synced) && len(lines) < rows+2; i++ {
			ln := m.lyrics.Synced[i].Text
			switch i - active {
			case 0:
				ln = accent.Render(ln)
			case -1, 1:
			default:
				ln = dim.Render(ln)
			}
			lines = append(lines, ln)
		}
		return center(lines...)
	}

	if m.lyrics.Plain == "" {
		return center(header, "", dim.Render(i18n.T("(no lyrics available)")))
	}
	// Plain lyrics scroll manually (mouse wheel); synced lyrics above follow the
	// playback position on their own, so the offset only applies here.
	body := strings.Split(m.lyrics.Plain, "\n")
	m.lyricsOffset = min(max(0, m.lyricsOffset), max(0, len(body)-1))
	end := min(len(body), m.lyricsOffset+rows)
	return center(append([]string{header, ""}, body[m.lyricsOffset:end]...)...)
}

// helpGroups are the help screen's keys by group.
var helpGroups = []struct {
	title string
	binds [][2]string
}{
	{"Playback", [][2]string{
		{"space", "play / pause"},
		{"n / p", "next / previous track"},
		{"← / →", "seek −10s / +10s"},
		{"+ / -", "volume up / down"},
		{"z", "toggle shuffle"},
		{"r", "cycle repeat (off → all → one)"},
		{"s", "stop"},
		{"T", "cycle the sleep timer"},
		{"m", "start a radio"},
	}},
	{"Browse", [][2]string{
		{"↑↓ j k", "move selection"},
		{"g / G", "jump to top / bottom"},
		{"enter", "play or open"},
		{"/", "search Deezer"},
		{"ctrl+f", "filter this list"},
		{"esc", "back"},
		{"mouse", "select, play, scroll, seek"},
	}},
	{"Library", [][2]string{
		{"e / E", "add to queue / play next"},
		{"f", "like / unlike"},
		{"a", "add to a playlist"},
		{"b", "go to album"},
		{"D", "download (paid plans)"},
		{"N R X", "new, rename, delete playlist"},
	}},
	{"Views", [][2]string{
		{"c", "now playing / cover"},
		{"u", "queue view"},
		{"x J K", "queue: remove, move"},
		{"l", "lyrics (synced when available)"},
		{"?", "this help"},
	}},
	{"App", [][2]string{
		{",", "settings"},
		{"i", "about / credits"},
		{"q", "quit"},
	}},
}

// helpView lays the key groups in as many columns as the width holds; when they do not
// fit the rows, one column scrolls.
func (m *Model) helpView(rows, width int) string {
	cols := max(1, width/40)
	colW := max(1, width/cols)
	var columns [][]string
	var col []string
	for _, g := range helpGroups {
		block := []string{" " + accent.Render(i18n.T(g.title))}
		for _, b := range g.binds {
			block = append(block, ansi.Truncate(
				"  "+accent.Render(fmt.Sprintf("%-6s", b[0]))+" "+dim.Render(i18n.T(b[1])), colW-1, "…"))
		}
		if len(col) > 0 && len(col)+1+len(block) > rows && len(columns) < cols-1 {
			columns = append(columns, col)
			col = nil
		}
		if len(col) > 0 {
			col = append(col, "")
		}
		col = append(col, block...)
	}
	columns = append(columns, col)

	var flat []string
	fits := true
	for i, c := range columns {
		fits = fits && len(c) <= rows
		if i > 0 {
			flat = append(flat, "")
		}
		flat = append(flat, c...)
	}
	if fits {
		blocks := make([]string, len(columns))
		for i, c := range columns {
			blocks[i] = lipgloss.NewStyle().Width(colW).Render(strings.Join(c, "\n"))
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, blocks...)
	}
	visible := max(1, rows-1)
	m.helpOffset = min(max(0, m.helpOffset), max(0, len(flat)-visible))
	end := min(len(flat), m.helpOffset+visible)
	lines := append(flat[m.helpOffset:end:end],
		dim.Render(fmt.Sprintf(" ↑/↓  %d-%d/%d", m.helpOffset+1, end, len(flat))))
	return padTo(lines, rows)
}

// rowDelegate draws each list row on one line (see rowLine). Home and Settings rows
// align their second column one cell after the longest title of their kind.
type rowDelegate struct{ m *Model }

func (rowDelegate) Height() int                         { return 1 }
func (rowDelegate) Spacing() int                        { return 0 }
func (rowDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d rowDelegate) Render(w io.Writer, l list.Model, i int, it list.Item) {
	r := it.(row)
	cur, ok := d.m.q.Current()
	playing := ok && d.m.playing && cur.ID != "" &&
		(r.track.ID == cur.ID || r.episode.ID == cur.ID || r.histID == cur.ID)
	label := 0
	if r.kind == rowMenu || r.kind == rowSetting {
		for _, o := range l.Items() {
			if o.(row).kind == r.kind {
				label = max(label, ansi.StringWidth(o.(row).title)+1)
			}
		}
	}
	fmt.Fprint(w, d.m.rowLine(r, 0, playing, i == l.Index(), l.Width(), label))
}

// trackCols splits a track row's middle (mid cells of a row width wide) into title,
// artist and album widths: only the title under 60 columns, the album from 70.
func trackCols(width, mid int) (title, artist, album int) {
	switch {
	case width < 60:
		return mid, 0, 0
	case width < 70:
		title = mid * 11 / 20
		return title, mid - title, 0
	}
	title = mid * 2 / 5
	artist = (mid - title) / 2
	return title, artist, mid - title - artist
}

// trackTail is the width of a track row's right part: the ♥ slot, the duration and a
// space.
const trackTail = 11

// columnHeader is the dim row above track rows naming their columns; lead is the
// width before the title.
func (m *Model) columnHeader(width, lead int) string {
	t, a, al := trackCols(width, max(0, width-lead-trackTail))
	s := strings.Repeat(" ", lead) + ansi.Truncate(i18n.T("Title"), t, "")
	s += strings.Repeat(" ", max(0, lead+t-ansi.StringWidth(s)))
	if a > 0 {
		s += ansi.Truncate("  "+i18n.T("Artist"), a, "")
		s += strings.Repeat(" ", max(0, lead+t+a-ansi.StringWidth(s)))
	}
	if al > 0 {
		s += ansi.Truncate("  "+i18n.T("Album"), al, "")
	}
	return dim.Render(s)
}

// playMarker marks the playing row: a level stepping with the position's second while
// playing, ► otherwise.
func (m *Model) playMarker() string {
	if m.player.State() == audio.Playing {
		return string([]rune("▁▃▅▇")[m.player.PositionMS()/1000%4])
	}
	return "►"
}

// rowLine draws a list or queue row width cells wide: the selected row is a bar with
// an accent ▌ at its left edge; then the playing row's marker, the queue index (num >
// 0) or the kind glyph, and the columns. Tracks have title (a dim E when explicit),
// artist, album, ♥ and duration; Home and Settings rows (label > 0) put their
// description or value one cell after the longest title; other rows have a title and
// a dim description, dropped under 60 columns. A section is an accent title on a dim
// rule, an empty one a blank row.
func (m *Model) rowLine(r row, num int, playing, selected bool, width, label int) string {
	if r.kind == rowSection {
		if r.title == "" {
			return ""
		}
		rule := dim.Render(" ── ") + accent.Render(r.title) + dim.Render(" "+strings.Repeat("─", width))
		return ansi.Truncate(rule, width, "")
	}
	plain, faint, hi := lipgloss.NewStyle(), dim, accent
	if selected {
		plain = plain.Foreground(lipgloss.Color("#F0F0F0")).Background(selBg)
		faint, hi = faint.Background(selBg), hi.Background(selBg)
	}
	cell := func(s string, w int, st lipgloss.Style) string {
		s = ansi.Truncate(s, max(0, w), "…")
		return st.Render(s) + plain.Render(strings.Repeat(" ", max(0, w-ansi.StringWidth(s))))
	}

	edge := plain.Render(" ")
	if selected {
		edge = hi.Render("▌")
	}
	lead := " "
	if playing {
		lead = m.playMarker()
	}
	lead = edge + hi.Render(lead) + plain.Render(" ")
	if num > 0 {
		lead += plain.Render(fmt.Sprintf("%2d. ", num))
	}
	if g := kindGlyphs[r.kind] + r.glyph; g != "" {
		st := plain
		if r.glyph != "" {
			st = hi
		}
		lead += st.Render(g) + plain.Render(" ")
	}

	var tail string
	switch {
	case r.kind == rowTrack:
		heart := plain.Render("   ")
		if m.likedIDs[r.track.ID] {
			heart = hi.Render("  ♥")
		}
		tail = heart + faint.Render(fmt.Sprintf("  %5s", r.right)) + plain.Render(" ")
	case r.right != "" && label == 0:
		tail = faint.Render("  "+r.right) + plain.Render(" ")
	default:
		tail = plain.Render(" ")
	}
	mid := max(0, width-lipgloss.Width(lead)-lipgloss.Width(tail))
	titleSty := plain
	if playing {
		titleSty = hi
	}

	var cols string
	switch {
	case r.kind == rowTrack:
		t, a, al := trackCols(width, mid)
		mark := ""
		if r.track.Explicit {
			mark = " E"
		}
		title := ansi.Truncate(r.title, max(0, t-len(mark)), "…")
		cols = titleSty.Render(title) + faint.Render(mark) +
			plain.Render(strings.Repeat(" ", max(0, t-ansi.StringWidth(title)-len(mark))))
		if a > 0 {
			cols += cell("  "+r.track.ArtistLine(), a, faint)
		}
		if al > 0 {
			cols += cell("  "+r.track.AlbumName, al, faint)
		}
	case label > 0:
		lw := min(label, mid)
		cols = cell(r.title, lw, titleSty)
		switch {
		case r.kind == rowSetting:
			cols += cell(r.right, mid-lw, hi)
		case width >= 60:
			cols += cell(r.desc, mid-lw, faint)
		default:
			cols += cell("", mid-lw, faint)
		}
	case r.desc == "" || width < 60:
		cols = cell(r.title, mid, titleSty)
	default:
		tw := mid * 2 / 5
		cols = cell(r.title, tw, titleSty) + cell("  "+r.desc, mid-tw, faint)
	}
	return lead + cols + tail
}
