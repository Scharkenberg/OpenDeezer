package ui

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"strings"
	"testing"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/i18n"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/queue"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// TestMain points every profile and cache directory at a temp folder, so no test
// reads or writes the user's own.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "purpledisc-ui-test")
	if err != nil {
		panic(err)
	}
	for _, k := range []string{"APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "HOME"} {
		_ = os.Setenv(k, dir)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestPadToFitsExactHeight(t *testing.T) {
	if got := padTo([]string{"one", "two", "three"}, 2); got != "one\ntwo" {
		t.Fatalf("clamped content = %q", got)
	}
	if got := padTo([]string{"one"}, 3); got != "one\n\n" {
		t.Fatalf("padded content = %q", got)
	}
}

// The hint row is cut at a " · " boundary to fit.
func TestHintCutsAtSeparator(t *testing.T) {
	m := interactionModel(screenList)
	m.width = 140
	if wide := m.hintLine(); !strings.Contains(wide, ", settings") || !strings.Contains(wide, "enter play") {
		t.Fatalf("wide hint should be whole: %q", wide)
	}
	m.width = 34
	narrow := strings.TrimSpace(ansi.Strip(m.hintLine()))
	if strings.Contains(narrow, "settings") || strings.HasSuffix(narrow, "·") || !strings.HasPrefix(narrow, "enter play · e queue") {
		t.Fatalf("narrow hint should end at a key: %q", narrow)
	}
}

// The hint row fits on one line at every width, locale and screen.
func TestHintFitsWidth(t *testing.T) {
	defer i18n.SetLocale("en")
	for _, loc := range []string{"en", "fr", "ru", "es", "zh", "ar", "hi"} {
		i18n.SetLocale(loc)
		for s := range hints {
			m := interactionModel(s)
			for w := 20; w <= 220; w++ {
				m.width = w
				if got := lipgloss.Width(m.hintLine()); got > w {
					t.Fatalf("%s screen %v at width %d: hint is %d wide", loc, s, w, got)
				}
			}
		}
	}
}

func TestHelpViewScrollsWithinAvailableRows(t *testing.T) {
	m := &Model{}
	first := m.helpView(8, 0)
	if got := lipgloss.Height(first); got != 8 {
		t.Fatalf("help height = %d, want 8", got)
	}

	m.helpOffset = 5
	later := m.helpView(8, 0)
	if later == first || !strings.Contains(later, "6-12/") {
		t.Fatalf("help did not advance to the requested page: %q", later)
	}
}

// At 80x24 the help groups fit in columns, with no scroll.
func TestHelpFitsWithoutScrolling(t *testing.T) {
	m := interactionModel(screenHelp)
	l := m.layout()
	body := m.helpView(l.rows, l.main)
	if strings.Contains(body, "↑/↓") || lipgloss.Height(body) > l.rows || !strings.Contains(body, "quit") {
		t.Fatalf("help at 80x24 scrolls or is cut:\n%s", body)
	}
}

// The progress bar keeps its row whatever the status; from 30 rows a rule is under
// the header; under 8 rows the footer is progress and now playing only.
func TestBarRowFixed(t *testing.T) {
	m := interactionModel(screenList)
	m.ready = true
	m.width, m.height = 126, 30
	m.status = strings.Repeat("long status ", 30)
	rows := strings.Split(m.View(), "\n")
	if len(rows) != 30 || !strings.Contains(rows[27], "●") || !strings.Contains(rows[1], "───") {
		t.Fatalf("%d rows, rule %q, bar row %q", len(rows), rows[1], rows[27])
	}
	m.height = 6
	rows = strings.Split(m.View(), "\n")
	if len(rows) != 6 || !strings.Contains(rows[4], "●") || m.layout().footer != 2 {
		t.Fatalf("%d rows at height 6, row 4 %q", len(rows), rows[4])
	}
}

// A new status clears itself; an expiry for an older one does nothing.
func TestStatusExpires(t *testing.T) {
	m := interactionModel(screenList)
	if _, cmd := m.Update(statusMsg{text: "first"}); cmd == nil {
		t.Fatal("a new status started no expiry")
	}
	old := m.statusSeq
	_, _ = m.Update(statusMsg{text: "second"})
	_, _ = m.Update(statusExpiredMsg(old))
	if m.status != "second" {
		t.Fatalf("an old expiry cleared %q", m.status)
	}
	_, _ = m.Update(statusExpiredMsg(m.statusSeq))
	if m.status != "" {
		t.Fatalf("status %q after its expiry", m.status)
	}
}

// frameModel is a model sized w x h with fake tracks, a liked one and an account;
// playing adds a queue, shuffle and repeat, synced lyrics and a cover.
func frameModel(w, h int, playing bool) *Model {
	m := interactionModel(screenList)
	m.client = deezer.New("arl")
	m.ready = true
	m.width, m.height = w, h
	m.acct = deezer.Account{Name: "listener", Offer: "Deezer Free"}
	ts := testTracks(14)
	for i := range ts {
		ts[i].DurationMS = int64(150000 + i*7000)
		ts[i].Explicit = i%4 == 1
	}
	ts[0].Name = "A track title far too long to fit in any of the columns it is given"
	m.list.Title = "Liked Songs"
	setTrackList(m, ts)
	m.list.Select(3)
	m.likedIDs = map[string]bool{"a": true, "c": true, "e": true}
	m.searchReturn, m.plReturn = screenList, screenList
	if !playing {
		return m
	}
	m.q.Set(ts, 2)
	m.playing = true
	m.q.SetShuffle(true)
	m.q.SetRepeat(queue.RepeatAll)
	m.lyrics = &deezer.Lyrics{Synced: []deezer.LyricLine{
		{TimeMS: 0, Text: "First line of the song"}, {TimeMS: 4000, Text: "Second line"},
		{TimeMS: 8000, Text: "Third line, a little longer than the others"}, {TimeMS: 12000, Text: "Fourth"},
	}}
	m.lyricsTrack = "c"
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 4), 0x60, 0xFF})
		}
	}
	m.curImg = img
	return m
}

// frames renders every screen at each size, playing and idle, with and without a
// status, and calls check with each frame.
func frames(check func(name string, w, h int, frame string)) {
	sizes := [][2]int{{80, 24}, {120, 40}, {40, 12}, {59, 20}, {100, 30}, {160, 50}}
	for _, size := range sizes {
		for _, playing := range []bool{true, false} {
			for s := screenLoading; s <= screenSettings; s++ {
				for _, status := range []string{"", strings.Repeat("a long status line ", 12)} {
					m := frameModel(size[0], size[1], playing)
					switch s {
					case screenMenu:
						m.showMenu()
					case screenSettings:
						m.showSettings(0)
					}
					m.screen, m.status = s, status
					check(fmt.Sprintf("screen %v at %dx%d playing %v", s, size[0], size[1], playing), size[0], size[1], m.View())
				}
			}
		}
	}
}

// Every frame is exactly the terminal's height and no line is wider than it.
func TestFrameFitsEveryScreen(t *testing.T) {
	frames(func(name string, w, h int, frame string) {
		lines := strings.Split(frame, "\n")
		if len(lines) != h {
			t.Fatalf("%s: %d lines", name, len(lines))
		}
		for i, l := range lines {
			if lw := ansi.StringWidth(l); lw > w {
				t.Fatalf("%s: line %d is %d wide: %q", name, i, lw, ansi.Strip(l))
			}
		}
	})
}

// glyphs are the only non-ASCII characters a frame may show: those every terminal
// font carries as text (WGL4: box drawing, block elements and a few symbols).
const glyphs = "─━│▀▄▌▁▃▅▇▮▯►◄▲▼■●○♥♪♫≡•·…→←↑↓‖—−"

// Every non-ASCII character in the English frames is in glyphs.
func TestFramesUseOnlyTextGlyphs(t *testing.T) {
	frames(func(name string, _, _ int, frame string) {
		for _, r := range ansi.Strip(frame) {
			if r > 0x7F && !strings.ContainsRune(glyphs, r) {
				t.Fatalf("%s: %q (U+%04X) is not in the glyph allowlist", name, r, r)
			}
		}
	})
}

// The mark's corners are blank and its middle row is drawn; a cover cell is the mean
// of its source box.
func TestMarkAndCoverAveraging(t *testing.T) {
	lines := strings.Split(markView(6), "\n")
	if len(lines) != 6 || !strings.HasPrefix(lines[0], " ") || strings.TrimSpace(lines[3]) == "" {
		t.Fatalf("mark %q", lines)
	}
	for _, rows := range []int{6, 8} {
		mark, n := markImage{color.RGBA{0xA2, 0x38, 0xFF, 0xFF}, 16 * rows}, 2*rows
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				a, h, v := boxMean(mark, x, y, n, n).A, boxMean(mark, n-1-x, y, n, n).A, boxMean(mark, x, n-1-y, n, n).A
				if (a >= 128) != (h >= 128) || (a >= 128) != (v >= 128) {
					t.Fatalf("%d rows: disc not symmetric at cell %d,%d", rows, x, y)
				}
			}
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{0, 0, 0, 0xFF})
	img.Set(1, 0, color.RGBA{0xFF, 0xFF, 0xFF, 0xFF})
	img.Set(0, 1, color.RGBA{0, 0, 0, 0xFF})
	img.Set(1, 1, color.RGBA{0xFF, 0xFF, 0xFF, 0xFF})
	if c := boxMean(img, 0, 0, 1, 1); c.R != 0x7F || c.A != 0xFF {
		t.Fatalf("mean of black and white = %+v", c)
	}
}
