package ui

import (
	"github.com/charmbracelet/lipgloss"
)

// palette is a named color scheme for the TUI.
type palette struct {
	name   string
	accent lipgloss.Color // titles, progress fill, now-playing, spinner
	dim    lipgloss.Color // secondary text
	empty  lipgloss.Color // progress remainder
	status lipgloss.Color // status line
	sel    lipgloss.Color // selected row background
}

// palettes are cycled from Settings; the first is the default.
var palettes = []palette{
	{"deezer", "#A238FF", "#626262", "#444444", "#FFD700", "#3A1F5C"},
	{"ocean", "#00AFFF", "#8A8A8A", "#3A3A3A", "#00FFFF", "#0F3550"},
	{"sunset", "#FF8700", "#8A8A8A", "#3A3A3A", "#FFAF00", "#4A2A10"},
	{"mono", "#D0D0D0", "#767676", "#444444", "#BCBCBC", "#3A3A3A"},
	{"matrix", "#00FF00", "#585858", "#303030", "#5FFF00", "#103A10"},
}

// applyTheme restyles the shared style vars and the spinner for palette i.
func (m *Model) applyTheme(i int) {
	if i < 0 || i >= len(palettes) {
		i = 0
	}
	m.theme = i
	p := palettes[i]

	accentColor = p.accent
	accent = lipgloss.NewStyle().Foreground(p.accent).Bold(true)
	dim = lipgloss.NewStyle().Foreground(p.dim)
	barEmpty = lipgloss.NewStyle().Foreground(p.empty)
	statusSty = lipgloss.NewStyle().Foreground(p.status)
	selBg = p.sel
	m.spinner.Style = lipgloss.NewStyle().Foreground(p.accent)
}

// applyThemeByName applies a saved theme name (no-op fallback to default).
func (m *Model) applyThemeByName(name string) {
	for i, p := range palettes {
		if p.name == name {
			m.applyTheme(i)
			return
		}
	}
	m.applyTheme(0)
}

// cycleTheme advances to the next palette and persists it.
func (m *Model) cycleTheme() {
	next := (m.theme + 1) % len(palettes)
	m.applyTheme(next)
	_ = SaveTheme(palettes[next].name)
}
