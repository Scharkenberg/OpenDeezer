package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/control"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// srModel is frameModel in screen-reader mode.
func srModel(w, h int, playing bool) *Model {
	m := frameModel(w, h, playing)
	m.sr = true
	return m
}

// srGlyphs are the drawing runes of the normal view; none may reach a screen reader.
const srGlyphs = "▀▄━─│▌●►‖♥▁▃▅▇"

// Every screen says itself in a few short plain lines.
func TestScreenReaderFramesArePlainAndShort(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {40, 12}} {
		for _, playing := range []bool{true, false} {
			for s := screenLoading; s <= screenSettings; s++ {
				for _, status := range []string{"", strings.Repeat("a long status line ", 12)} {
					m := srModel(size[0], size[1], playing)
					switch s {
					case screenMenu:
						m.showMenu()
					case screenSettings:
						m.showSettings(0)
					}
					m.screen, m.status = s, status
					frame := m.View()
					name := fmt.Sprintf("screen %v at %dx%d playing %v", s, size[0], size[1], playing)
					lines := strings.Split(frame, "\n")
					limit := 3
					switch s {
					case screenHelp:
						limit = size[1]
					case screenCredits:
						limit = 4
					}
					if len(lines) > limit {
						t.Fatalf("%s: %d lines:\n%s", name, len(lines), frame)
					}
					if strings.Contains(frame, "\x1b") {
						t.Fatalf("%s: escape sequence in %q", name, frame)
					}
					for i, l := range lines {
						if lw := ansi.StringWidth(l); lw > size[0] {
							t.Fatalf("%s: line %d is %d wide: %q", name, i, lw, l)
						}
						if strings.TrimSpace(l) == "" {
							t.Fatalf("%s: blank line %d", name, i)
						}
					}
					if strings.ContainsAny(frame, srGlyphs) {
						t.Fatalf("%s: drawing glyph in %q", name, frame)
					}
				}
			}
		}
	}
}

// The focus line names the list, the position and the selected row in words.
func TestScreenReaderFocusLine(t *testing.T) {
	m := srModel(80, 24, true)
	lines := strings.Split(m.View(), "\n")
	if want := "Liked Songs, 4 of 14: Track d, Artist d, 2:51"; lines[0] != want {
		t.Fatalf("focus line %q, want %q", lines[0], want)
	}
	if want := "Playing: Track c, Artist c"; len(lines) != 2 || lines[1] != want {
		t.Fatalf("event line %q, want %q", lines, want)
	}
	m.list.Select(2)
	if got := strings.Split(m.View(), "\n")[0]; got != "Liked Songs, 3 of 14: Track c, Artist c, 2:44, liked, playing" {
		t.Fatalf("playing row %q", got)
	}
	m.list.Select(1)
	if got := strings.Split(m.View(), "\n")[0]; !strings.HasSuffix(got, "explicit") {
		t.Fatalf("explicit row %q", got)
	}
	m.showMenu()
	if got := strings.Split(m.View(), "\n")[0]; !strings.HasPrefix(got, "Home, 1 of ") || !strings.HasSuffix(got, ": Liked Songs") {
		t.Fatalf("home %q", got)
	}
}

// Moving the selection changes the focus line and nothing else.
func TestScreenReaderMoveChangesOnlyFocus(t *testing.T) {
	for _, s := range []screen{screenList, screenQueue} {
		m := srModel(80, 24, true)
		m.screen = s
		before := strings.Split(m.View(), "\n")
		_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		after := strings.Split(m.View(), "\n")
		if len(before) != len(after) || before[0] == after[0] {
			t.Fatalf("screen %v: %q then %q", s, before, after)
		}
		for i := 1; i < len(after); i++ {
			if before[i] != after[i] {
				t.Fatalf("screen %v: line %d changed from %q to %q", s, i, before[i], after[i])
			}
		}
	}
}

// A tick that changes nothing leaves the frame as it was, and a status starts no
// expiry.
func TestScreenReaderTickKeepsFrame(t *testing.T) {
	for _, s := range []screen{screenList, screenNowPlaying, screenQueue, screenLyrics} {
		m := srModel(80, 24, true)
		m.playing = false
		m.screen = s
		before := m.View()
		_, _ = m.Update(tickMsg{})
		if after := m.View(); after != before {
			t.Fatalf("screen %v: tick changed %q to %q", s, before, after)
		}
	}
	m := srModel(80, 24, false)
	if _, cmd := m.Update(statusMsg{text: "Shuffle on"}); cmd != nil {
		t.Fatal("a status in screen-reader mode scheduled a command")
	}
}

// w says the position and state on the event line.
func TestScreenReaderWhere(t *testing.T) {
	m := srModel(80, 24, true)
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	lines := strings.Split(m.View(), "\n")
	want := "0:00 of 2:44, playing, Track c, Artist c, shuffle, repeat, Volume "
	if len(lines) != 2 || !strings.HasPrefix(lines[1], want) {
		t.Fatalf("after w: %q, want %q…", lines, want)
	}
}

// On the remote-control screen, w says the peer's position, state, track and modes.
func TestRemoteWhere(t *testing.T) {
	m := interactionModel(screenRemoteCtl)
	m.remote = control.NewClient("http://127.0.0.1:0", "", "acct") // no command runs
	m.remoteState = control.State{State: "paused", PositionMS: 62000, DurationMS: 222000, Volume: 0.5, Repeat: "one",
		Track: &control.Track{Title: "Peer song", Artist: "Peer artist"}}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	if want := "1:02 of 3:42, paused, Peer song, Peer artist, repeat 1, Volume 50%"; m.status != want {
		t.Fatalf("w on the remote screen: %q, want %q", m.status, want)
	}
}

// msgs runs a command and the commands of a sequence it returns.
func msgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Slice {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for i := 0; i < v.Len(); i++ {
		if c, ok := v.Index(i).Interface().(tea.Cmd); ok {
			out = append(out, msgs(c)...)
		}
	}
	return out
}

func sameMsgs(got []tea.Msg, want ...tea.Msg) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if reflect.TypeOf(got[i]) != reflect.TypeOf(want[i]) {
			return false
		}
	}
	return true
}

// Sign out device asks first: n keeps the saved ARL, y deletes it and quits.
func TestSignOutDevice(t *testing.T) {
	if err := SaveARL("secret"); err != nil {
		t.Fatal(err)
	}
	m := frameModel(80, 24, false)
	m.showSettings(0)
	m.screen = screenSettings
	m.list.Select(len(m.list.Items()) - 1)
	if r := m.list.SelectedItem().(row); r.title != "Sign out device" {
		t.Fatalf("last row %q", r.title)
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.signOutConfirm || !strings.Contains(m.View(), "Sign out this device?") {
		t.Fatal("enter did not ask")
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if m.signOutConfirm || LoadARL() != "secret" {
		t.Fatalf("n: asking %v, ARL %q", m.signOutConfirm, LoadARL())
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if LoadARL() != "" || !m.SignedOut() || !sameMsgs(msgs(cmd), tea.Quit()) {
		t.Fatalf("y: ARL %q, signed out %v, messages %#v", LoadARL(), m.SignedOut(), msgs(cmd))
	}
}

// The Settings row switches the mode at once, the terminal with it, and saves it.
func TestScreenReaderSettingToggles(t *testing.T) {
	m := frameModel(80, 24, false)
	m.showSettings(0)
	m.screen = screenSettings
	at := -1
	for i, it := range m.list.Items() {
		if it.(row).title == "Screen reader mode" {
			at = i
		}
	}
	if at < 0 {
		t.Fatal("no Screen reader mode row")
	}
	m.list.Select(at)
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.sr || !sameMsgs(msgs(cmd), tea.ExitAltScreen(), tea.DisableMouse(), tea.ClearScreen()) {
		t.Fatalf("on: sr %v, messages %#v", m.sr, msgs(cmd))
	}
	if LoadScreenReader() != "on" {
		t.Fatalf("saved %q", LoadScreenReader())
	}
	if got := strings.Split(m.View(), "\n")[0]; !strings.HasSuffix(got, ": Screen reader mode, on") {
		t.Fatalf("focus line %q", got)
	}
	_, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.sr || !sameMsgs(msgs(cmd), tea.EnterAltScreen(), tea.EnableMouseCellMotion()) {
		t.Fatalf("off: sr %v, messages %#v", m.sr, msgs(cmd))
	}
	if LoadScreenReader() != "off" || ScreenReaderAtStart() {
		t.Fatalf("saved %q", LoadScreenReader())
	}
}
