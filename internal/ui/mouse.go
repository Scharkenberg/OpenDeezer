package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// handleMouse: the wheel scrolls the current screen, a left click selects the row (a
// click on the selected row activates it), a click on a header view opens it, a click
// in the side panel opens Now Playing, a click on the progress bar seeks. Other clicks
// are ignored.
func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.screen == screenNoInternet || m.screen == screenLoading { // splash, no frame
		return m, nil
	}
	m.plConfirm, m.signOutConfirm = false, false // a pending question is canceled by any mouse action
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return m.handleWheel(true)
	case tea.MouseButtonWheelDown:
		return m.handleWheel(false)
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		return m.handleClick(msg.X, msg.Y)
	}
	return m, nil
}

// handleWheel scrolls lists, the help overlay, the queue cursor and plain
// lyrics. Synced lyrics follow the playback position on their own.
func (m *Model) handleWheel(up bool) (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenHelp:
		if up {
			m.helpOffset = max(0, m.helpOffset-1)
		} else {
			m.helpOffset++ // clamped by helpView
		}
	case screenQueue:
		if up {
			m.queueSel = max(0, m.queueSel-1)
		} else {
			m.queueSel = min(max(0, m.q.Len()-1), m.queueSel+1)
		}
	case screenLyrics:
		if up {
			m.lyricsOffset = max(0, m.lyricsOffset-1)
		} else {
			m.lyricsOffset++ // clamped by lyricsView
		}
	default:
		if m.onListScreen() {
			if up {
				m.list.CursorUp()
			} else {
				m.list.CursorDown()
			}
			m.skipSections(!up)
		}
	}
	return m, nil
}

// handleClick dispatches a left click by the frame's layout: the header's views, the
// progress bar, the side panel, then the body's rows under the column header.
func (m *Model) handleClick(x, y int) (tea.Model, tea.Cmd) {
	l := m.layout()
	switch {
	case y == 0:
		return m.clickView(l, x)
	case y == l.bar:
		if ms, ok := l.seekAt(x, m.player.DurationMS()); ok {
			return m, m.seekCmd(ms)
		}
		return m, nil
	case y < l.top || y >= l.top+l.rows:
		return m, nil
	case l.panel > 0 && x > l.main:
		m.toggleScreen(screenNowPlaying)
		return m, nil
	}
	r := y - l.top - l.head
	switch {
	case r < 0:
		return m, nil
	case m.onListScreen():
		return m.clickList(r)
	case m.screen == screenQueue:
		return m.clickQueue(r, l.rows-l.head)
	}
	return m, nil
}

// clickView opens the header view under x, as its key does; Browse leaves a view for
// the screen under it.
func (m *Model) clickView(l layout, x int) (tea.Model, tea.Cmd) {
	for i, t := range l.tabs {
		if x < t[0] || x >= t[1] {
			continue
		}
		v := views[i]
		switch {
		case m.screen == v.s || (i == 0 && m.onListScreen()):
			return m, nil
		case i == 0:
			m.screen = m.prevScreen
			return m, nil
		}
		return m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(v.key)})
	}
	return m, nil
}

// clickList selects list row r; a click on the selected row activates it (same as
// enter). Clicks on a section or below the last item are ignored.
func (m *Model) clickList(r int) (tea.Model, tea.Cmd) {
	idx := m.list.Paginator.Page*m.list.Paginator.PerPage + r
	items := m.list.VisibleItems()
	if r >= m.list.Paginator.PerPage || idx >= len(items) || items[idx].(row).kind == rowSection {
		return m, nil
	}
	if idx == m.list.Index() {
		return m.activate()
	}
	m.list.Select(idx)
	return m, nil
}

// clickQueue selects the queue entry on row r of the rows entries shown; a click on
// the selected entry jumps playback to it (same as enter on the queue screen).
func (m *Model) clickQueue(r, rows int) (tea.Model, tea.Cmd) {
	n := m.q.Len()
	start, _ := m.queueWindow(rows, n)
	idx := start + r
	if idx >= n {
		return m, nil
	}
	if idx == m.queueSel {
		return m, m.queueJump(idx)
	}
	m.queueSel = idx
	return m, nil
}

// skipSections moves the list cursor off a section row: on in the direction it went,
// back the other way at the end of the list.
func (m *Model) skipSections(down bool) {
	items := m.list.VisibleItems()
	for _, d := range []bool{down, !down} {
		i := m.list.Index()
		for i >= 0 && i < len(items) && items[i].(row).kind == rowSection {
			if d {
				i++
			} else {
				i--
			}
		}
		if i >= 0 && i < len(items) {
			m.list.Select(i)
			return
		}
	}
}
