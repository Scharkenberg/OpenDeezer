package ui

import (
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// artworkSupported reports whether the terminal has enough color depth to make
// half-block cover rendering worthwhile (truecolor or 256-color).
func artworkSupported() bool {
	switch lipgloss.ColorProfile() {
	case termenv.TrueColor, termenv.ANSI256:
		return true
	default:
		return false
	}
}

// fetchCover downloads and decodes an artwork image.
func fetchCover(url string) (image.Image, error) {
	resp, err := deezer.HTTPClient(10 * time.Second).Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cover %s", resp.Status)
	}
	img, _, err := image.Decode(resp.Body)
	return img, err
}

// boxMean averages the source pixels under cell (gx, gy) of a cols×rows grid.
func boxMean(img image.Image, gx, gy, cols, rows int) color.RGBA {
	b := img.Bounds()
	x0, y0 := b.Min.X+gx*b.Dx()/cols, b.Min.Y+gy*b.Dy()/rows
	x1 := max(b.Min.X+(gx+1)*b.Dx()/cols, x0+1)
	y1 := max(b.Min.Y+(gy+1)*b.Dy()/rows, y0+1)
	var r, g, bl, a, n uint64
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			cr, cg, cb, ca := img.At(x, y).RGBA()
			r, g, bl, a, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca), n+1
		}
	}
	if a == 0 {
		return color.RGBA{}
	}
	return color.RGBA{uint8(r * 255 / a), uint8(g * 255 / a), uint8(bl * 255 / a), uint8(a / n >> 8)}
}

func hexColor(c color.RGBA) lipgloss.Color {
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B))
}

// renderCover draws an image as a block of half-height cells, each the mean of its
// source box: "▀" with the top half as foreground and the bottom half as background,
// "▄" or a blank where a half is transparent. It also returns the mean color.
func renderCover(img image.Image, cols, rows int) (string, color.RGBA) {
	var sb strings.Builder
	var r, g, b, n int
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			top, bot := boxMean(img, x, 2*y, cols, 2*rows), boxMean(img, x, 2*y+1, cols, 2*rows)
			for _, c := range []color.RGBA{top, bot} {
				if c.A >= 128 {
					r, g, b, n = r+int(c.R), g+int(c.G), b+int(c.B), n+1
				}
			}
			switch {
			case top.A < 128 && bot.A < 128:
				sb.WriteByte(' ')
			case bot.A < 128:
				sb.WriteString(lipgloss.NewStyle().Foreground(hexColor(top)).Render("▀"))
			case top.A < 128:
				sb.WriteString(lipgloss.NewStyle().Foreground(hexColor(bot)).Render("▄"))
			default:
				sb.WriteString(lipgloss.NewStyle().Foreground(hexColor(top)).Background(hexColor(bot)).Render("▀"))
			}
		}
		if y < rows-1 {
			sb.WriteByte('\n')
		}
	}
	if n == 0 {
		return sb.String(), color.RGBA{}
	}
	return sb.String(), color.RGBA{uint8(r / n), uint8(g / n), uint8(b / n), 255}
}

// lifted raises c to a lightness of at least one half, to stay readable on a dark
// background.
func lifted(c color.RGBA) lipgloss.Color {
	l := (int(max(c.R, c.G, c.B)) + int(min(c.R, c.G, c.B))) / 2
	if l < 128 {
		k := float64(128-l) / float64(255-l)
		for _, ch := range []*uint8{&c.R, &c.G, &c.B} {
			*ch += uint8(float64(255-*ch) * k)
		}
	}
	return hexColor(c)
}

// coverAt is the current track's cover cols×rows cells wide, rendered once per track
// and size; the first render sets the cover's tint.
func (m *Model) coverAt(cols, rows int) string {
	if m.curImg == nil || cols < 1 || rows < 1 {
		return ""
	}
	k := [2]int{cols, rows}
	if c, ok := m.covers[k]; ok {
		return c
	}
	c, mean := renderCover(m.curImg, cols, rows)
	if m.covers == nil {
		m.covers = map[[2]int]string{}
	}
	m.covers[k] = c
	m.tint = lifted(mean)
	return c
}

// markImage is the app's mark: a disc in the accent with a light play triangle, size
// pixels square, drawn on a 64-unit grid.
type markImage struct {
	disc color.RGBA
	size int
}

func (markImage) ColorModel() color.Model   { return color.RGBAModel }
func (k markImage) Bounds() image.Rectangle { return image.Rect(0, 0, k.size, k.size) }

func (k markImage) At(x, y int) color.Color {
	u, v := (float64(x)+0.5)*64/float64(k.size), (float64(y)+0.5)*64/float64(k.size)
	du, dv := u-32, v-32
	switch {
	case du*du+dv*dv > 31*31:
		return color.RGBA{}
	case u >= 24 && u <= 48 && math.Abs(dv) <= 14*(48-u)/24:
		return color.RGBA{0xF5, 0xF5, 0xF5, 0xFF}
	}
	return k.disc
}

// markView draws the mark rows cells tall. Each half cell covers an 8-pixel square, so
// the disc stays symmetric.
func markView(rows int) string {
	var c color.RGBA
	_, _ = fmt.Sscanf(string(accentColor), "#%02x%02x%02x", &c.R, &c.G, &c.B)
	c.A = 0xFF
	s, _ := renderCover(markImage{c, 16 * rows}, 2*rows, rows)
	return s
}
