package ui

import (
	"fmt"
	"math"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// contrast is the WCAG contrast ratio of two #RRGGBB colors.
func contrast(a, b lipgloss.Color) float64 {
	lum := func(c lipgloss.Color) float64 {
		var r, g, bl int
		_, _ = fmt.Sscanf(string(c), "#%02x%02x%02x", &r, &g, &bl)
		ch := func(v int) float64 {
			s := float64(v) / 255
			if s <= 0.03928 {
				return s / 12.92
			}
			return math.Pow((s+0.055)/1.055, 2.4)
		}
		return 0.2126*ch(r) + 0.7152*ch(g) + 0.0722*ch(bl)
	}
	la, lb := lum(a), lum(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

// Secondary text reads on a dark terminal (black up to #303030) and on the selected
// row; the header chips' text reads on the accent.
func TestPaletteContrast(t *testing.T) {
	for _, p := range palettes {
		for _, bg := range []lipgloss.Color{"#000000", "#1E1E1E", "#303030", p.sel} {
			if c := contrast(p.dim, bg); c < 4.5 {
				t.Errorf("%s: dim %s on %s is %.2f:1, want 4.5", p.name, p.dim, bg, c)
			}
		}
		if c := contrast(p.onAccent, p.accent); c < 4.5 {
			t.Errorf("%s: %s on the accent %s is %.2f:1, want 4.5", p.name, p.onAccent, p.accent, c)
		}
	}
}
