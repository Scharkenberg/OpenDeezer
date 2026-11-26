package ui

import (
	"github.com/charmbracelet/lipgloss"
)

// palette is a named color scheme for the TUI. dim and onAccent keep a WCAG contrast of
// at least 4.5:1 on a dark terminal and on the accent (TestPaletteContrast).
type palette struct {
	name     string
	accent   lipgloss.Color // titles, progress fill, now-playing, spinner
	dim      lipgloss.Color // secondary text
	empty    lipgloss.Color // progress remainder
	status   lipgloss.Color // status line
	sel      lipgloss.Color // selected row background
	onAccent lipgloss.Color // text on the accent (header chips)
}

// palettes are cycled from Settings; the first is the default.
var palettes = []palette{
	{"deezer", "#A238FF", "#A0A0A0", "#4A4A4A", "#FFD700", "#3A1F5C", "#FFFFFF"},
	{"ocean", "#00AFFF", "#A0A0A0", "#3A3A3A", "#00FFFF", "#0F3550", "#111111"},
	{"sunset", "#FF8700", "#A0A0A0", "#3A3A3A", "#FFAF00", "#4A2A10", "#111111"},
	{"mono", "#D0D0D0", "#A8A8A8", "#444444", "#BCBCBC", "#3A3A3A", "#111111"},
	{"matrix", "#00FF00", "#8FBF8F", "#303030", "#5FFF00", "#103A10", "#111111"},
}

// applyTheme restyles the shared style vars and the spinner for palette i.
func (m *Model) applyTheme(i int) {
	if i < 0 || i >= len(palettes) {
		i = 0
	}
	m.theme = i
	p := palettes[i]

	accentColor, onAccent = p.accent, p.onAccent
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
