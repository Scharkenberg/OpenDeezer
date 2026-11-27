//go:build windows

package main

import (
	"errors"
	"image"
	"image/color"
	"slices"
	"strings"
	"syscall"
	"testing"
	"unicode"
	"unsafe"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/i18n"
)

// At the default line and at 1.5 and 2 times its height (a larger system font), from the
// minimum window up: every control inside the client, none overlapping.
func TestLayoutBoxesFitAndDoNotOverlap(t *testing.T) {
	if got := minHeight(16); got != 400 {
		t.Fatalf("minHeight(16) = %d, want 400", got)
	}
	for _, line := range []int{16, 24, 32} {
		for _, size := range [][2]int{{480, 400}, {639, 400}, {640, 400}, {920, 660}} {
			w, h := size[0], max(size[1], minHeight(line))
			boxes := layoutBoxes(w, h, line)
			if want := len(navIDs) + 7 + len(bottomIDs) + 2; len(boxes) != want {
				t.Fatalf("%dx%d: %d boxes, want %d", w, h, len(boxes), want)
			}
			for id, b := range boxes {
				if b.x < 0 || b.y < 0 || b.w <= 0 || b.h <= 0 || b.x+b.w > w || b.y+b.h > h {
					t.Errorf("line %d, %dx%d: control %d at %+v is outside the client area", line, w, h, id, b)
				}
				for other, o := range boxes {
					if other != id && b.x < o.x+o.w && o.x < b.x+b.w && b.y < o.y+o.h && o.y < b.y+b.h {
						t.Errorf("line %d, %dx%d: controls %d %+v and %d %+v overlap", line, w, h, id, b, other, o)
					}
				}
			}
			if v := boxes[idVolume]; v.w < 60 {
				t.Errorf("%dx%d: volume slider only %d wide", w, h, v.w)
			}
			if l := boxes[idList]; l.h < 100 {
				t.Errorf("line %d, %dx%d: list only %d high", line, w, h, l.h)
			}
		}
	}
	// Today's sizes at the default line.
	b := layoutBoxes(920, 660, 16)
	if b[idHome].h != 24 || b[idStatus].h != 17 || b[idPrev].h != 22 || b[idStatus].y != 660-80 {
		t.Errorf("default line: button %d, status %d at %d, bottom row %d", b[idHome].h, b[idStatus].h, b[idStatus].y, b[idPrev].h)
	}
}
func TestSeekMath(t *testing.T) {
	for _, c := range []struct{ pos, dur, bar, back int64 }{
		{0, 180000, 0, 0},
		{90000, 180000, 500, 90000},
		{180000, 180000, 1000, 180000},
		{200000, 180000, 1000, 180000},
		{5000, 0, 0, 0},
		{3_600_000, 7_200_000, 500, 3_600_000},
	} {
		if got := seekPos(c.pos, c.dur); got != c.bar {
			t.Errorf("seekPos(%d, %d) = %d, want %d", c.pos, c.dur, got, c.bar)
		}
		if got := seekMS(c.bar, c.dur); got != c.back {
			t.Errorf("seekMS(%d, %d) = %d, want %d", c.bar, c.dur, got, c.back)
		}
	}
}

func TestTimeLabel(t *testing.T) {
	for _, c := range []struct {
		pos, dur int64
		want     string
	}{
		{0, 0, "T    0:00 / 0:00"},
		{61_000, 245_999, "T    1:01 / 4:05"},
		{3_599_000, 3_600_000, "T    59:59 / 60:00"},
	} {
		if got := timeLabel("T", c.pos, c.dur); got != c.want {
			t.Errorf("timeLabel(%d, %d) = %q, want %q", c.pos, c.dur, got, c.want)
		}
	}
}

func TestRowCells(t *testing.T) {
	for _, c := range []struct {
		r             row
		playing, like bool
		first, second string
	}{
		{row{title: "Song", meta: "Artist"}, false, false, "Song", "Artist"},
		{row{title: "Song", meta: "Artist"}, true, true, "▶ ♥ Song", "Artist"},
		{row{title: "A lyric line"}, false, false, "A lyric line", ""},
		{row{kind: "header", title: "Albums"}, false, false, "ALBUMS", ""},
	} {
		if first, second := rowCells(c.r, c.playing, c.like); first != c.first || second != c.second {
			t.Errorf("rowCells(%+v) = %q, %q; want %q, %q", c.r, first, second, c.first, c.second)
		}
	}
}

// Every language: every button and the field have a tooltip, capitalized, and not the
// English one where the language has its own.
func TestTipTexts(t *testing.T) {
	defer i18n.SetLocale("en")
	i18n.SetLocale("en")
	english := tipTexts()
	for _, l := range i18n.Available() {
		i18n.SetLocale(l.Code)
		tips := tipTexts()
		for _, b := range buttons {
			tip := tips[b.id]
			if tip == "" || []rune(tip)[0] != unicode.ToUpper([]rune(tip)[0]) {
				t.Errorf("%s: button %d tooltip %q", l.Code, b.id, tip)
			}
			if l.Code != "en" && l.Code != "fr" && tip == english[b.id] {
				t.Errorf("%s: button %d tooltip %q is the English one", l.Code, b.id, tip)
			}
		}
		if tips[idSearch] == "" {
			t.Errorf("%s: no tooltip for the field", l.Code)
		}
	}
}

func TestLyricRows(t *testing.T) {
	synced := &deezer.Lyrics{Plain: "a\nb", Synced: []deezer.LyricLine{{TimeMS: 1000, Text: "a"}, {TimeMS: 4000, Text: "b"}}}
	for _, c := range []struct {
		name  string
		l     *deezer.Lyrics
		err   error
		kinds string
		times int
	}{
		{"synced", synced, nil, "lyric lyric", 2},
		{"plain", &deezer.Lyrics{Plain: "one\r\ntwo\nthree"}, nil, "lyric lyric lyric", 0},
		{"none on Deezer", &deezer.Lyrics{Plain: " \n"}, nil, "lyric", 0},
		{"failure here", nil, errors.New("timeout"), "lyric retryLyrics", 0},
	} {
		rows, times := lyricRows(c.l, c.err)
		var kinds []string
		for _, r := range rows {
			kinds = append(kinds, r.kind)
		}
		if strings.Join(kinds, " ") != c.kinds || len(times) != c.times {
			t.Errorf("%s: kinds %v, %d times", c.name, kinds, len(times))
		}
	}
	none, _ := lyricRows(&deezer.Lyrics{}, nil)
	failed, _ := lyricRows(nil, errors.New("timeout"))
	if none[0].title == failed[0].title || !strings.Contains(failed[0].title, "timeout") {
		t.Errorf("no lyrics %q and a failure %q must read differently", none[0].title, failed[0].title)
	}
}

func TestActiveLine(t *testing.T) {
	times := []int64{1000, 4000, 9000}
	for _, c := range []struct {
		pos  int64
		want int
	}{{0, -1}, {999, -1}, {1000, 0}, {3999, 0}, {4000, 1}, {8999, 1}, {9000, 2}, {600000, 2}} {
		if got := activeLine(times, c.pos); got != c.want {
			t.Errorf("activeLine(%d) = %d, want %d", c.pos, got, c.want)
		}
	}
	if got := activeLine(nil, 5000); got != -1 {
		t.Errorf("no lines: %d", got)
	}
}

func TestMenuIndex(t *testing.T) {
	titles := []string{"&Playback", "&Go", "A&pp", "Пере&ход"}
	for _, c := range []struct {
		ch   rune
		want int
	}{{'p', 0}, {'P', 0}, {'g', 1}, {'x', -1}, {'х', 3}, {'Х', 3}} {
		if got := menuIndex(titles, c.ch); got != c.want {
			t.Errorf("menuIndex(%q) = %d, want %d", c.ch, got, c.want)
		}
	}
}

func TestSection(t *testing.T) {
	if got := section("Albums", nil); got != nil {
		t.Errorf("empty section: %v", got)
	}
	if got := section("Albums", []row{{title: "A"}}); len(got) != 2 || got[0].kind != "header" {
		t.Errorf("section: %+v", got)
	}
}

// Every language: no two menu titles or buttons share a mnemonic, and a button that
// changes its text keeps its mnemonic.
func TestMnemonicsUnique(t *testing.T) {
	defer i18n.SetLocale("en")
	for _, l := range i18n.Available() {
		i18n.SetLocale(l.Code)
		seen := map[string]int{}
		for i, texts := range mnemonics(translated()) {
			k := strings.Index(texts[0], "&")
			if k < 0 {
				continue
			}
			key := strings.ToLower(string([]rune(texts[0][k+1:])[0]))
			if j, dup := seen[key]; dup {
				t.Errorf("%s: entries %d and %d share &%s", l.Code, j, i, key)
			}
			seen[key] = i
			for _, other := range texts[1:] {
				if m := strings.Index(other, "&"); m < 0 || strings.ToLower(string([]rune(other[m+1:])[0])) != key {
					t.Errorf("%s: %q and %q differ in mnemonic", l.Code, texts[0], other)
				}
			}
		}
		if l.Code == "en" && len(seen) != len(menuKeys)+len(buttons) {
			t.Errorf("en: %d of %d menus and buttons have a mnemonic", len(seen), len(menuKeys)+len(buttons))
		}
	}
}

func TestCoverURL(t *testing.T) {
	const u = "https://e-cdns-images.dzcdn.net/images/cover/abc/250x250-000000-80-0-0.jpg"
	for _, c := range []struct {
		px   int
		want string
	}{
		{36, "/56x56-"}, {56, "/56x56-"}, {57, "/120x120-"}, {300, "/500x500-"}, {5000, "/1000x1000-"},
	} {
		if got := coverURL(u, c.px); !strings.Contains(got, c.want) || strings.Contains(got, "250x250") {
			t.Errorf("coverURL(%d) = %s, want %s", c.px, got, c.want)
		}
	}
	if got := coverURL("https://example.test/a.jpg", 36); got != "https://example.test/a.jpg" {
		t.Errorf("a non-CDN URL changed: %s", got)
	}
}

// A 4x2 image: left half red, right half blue. Its centered square is the middle 2x2
// (one red, one blue column); scaled to 2x2 it keeps them, mirrored for right to left.
func TestThumbnail(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for y := range 2 {
		for x := range 4 {
			c := color.RGBA{255, 0, 0, 255}
			if x >= 2 {
				c = color.RGBA{0, 0, 255, 255}
			}
			img.Set(x, y, c)
		}
	}
	pix := thumbnail(img, 2, false)
	if len(pix) != 2*2*4 || pix[2] != 255 || pix[0] != 0 || pix[4] != 255 || pix[6] != 0 || pix[3] != 255 {
		t.Errorf("BGRA %v: want red then blue, opaque", pix[:8])
	}
	if flipped := thumbnail(img, 2, true); flipped[0] != 255 || flipped[2] != 0 {
		t.Errorf("mirrored %v: want blue first", flipped[:8])
	}
	if up := thumbnail(img, 6, false); len(up) != 6*6*4 {
		t.Errorf("scaling up: %d bytes", len(up))
	}
}

func TestPadRight(t *testing.T) {
	pix := []byte{1, 2, 3, 255, 4, 5, 6, 255, 7, 8, 9, 255, 10, 11, 12, 255} // 2x2
	out := padRight(pix, 2, 1)
	want := []byte{1, 2, 3, 255, 4, 5, 6, 255, 0, 0, 0, 0, 7, 8, 9, 255, 10, 11, 12, 255, 0, 0, 0, 0}
	if !slices.Equal(out, want) {
		t.Errorf("padRight = %v", out)
	}
}

// A 120 px cover as JPEG decodes it (YCbCr), scaled to the row and now-playing sizes
// at 150 %.
func BenchmarkThumbnail(b *testing.B) {
	img := image.NewYCbCr(image.Rect(0, 0, 120, 120), image.YCbCrSubsampleRatio420)
	for b.Loop() {
		thumbnail(img, 36, false)
		thumbnail(img, 54, false)
	}
}

// An item drawn in the UI font keeps its text on the menu, where screen readers read it,
// and the data that draws it.
func TestMenuItemsKeepTheirText(t *testing.T) {
	a := &app{}
	menu, _, _ := procCreatePopupMenu.Call()
	defer procDestroyMenu.Call(menu)
	a.appendItem(menu, mfChecked, idShuffle, "Shuffle\tCtrl+S")
	buf := make([]uint16, 64)
	n, _, _ := user32.NewProc("GetMenuStringW").Call(menu, 0, uintptr(unsafe.Pointer(&buf[0])), 64, mfByPosition)
	if got := syscall.UTF16ToString(buf[:n]); got != "Shuffle\tCtrl+S" {
		t.Errorf("the item's text is %q", got)
	}
	mi := menuItemInfo{Size: uint32(unsafe.Sizeof(menuItemInfo{})), Mask: 0x100 | 0x20 | 0x1} // MIIM_FTYPE | MIIM_DATA | MIIM_STATE
	user32.NewProc("GetMenuItemInfoW").Call(menu, 0, 1, uintptr(unsafe.Pointer(&mi)))
	if mi.Type&0x100 == 0 || mi.ItemData != menuItemBase || mi.State&mfChecked == 0 {
		t.Errorf("type %x, data %d, state %x", mi.Type, mi.ItemData, mi.State)
	}
	if text, keys := a.menuLabel(mi.ItemData); text != "Shuffle" || keys != "Ctrl+S" {
		t.Errorf("label %q, %q", text, keys)
	}
}
