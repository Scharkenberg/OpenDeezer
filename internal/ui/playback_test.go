package ui

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/audio"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/config"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	tea "github.com/charmbracelet/bubbletea"
)

// The TUI plays with playback.json's settings, shared with the engine; while
// that file does not exist, its own older files are migrated into it once.
func TestPlaybackMigratesTUIFilesOnce(t *testing.T) {
	useTempConfig(t)
	dir, _ := config.Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, v := range map[string]string{"quality.txt": "hifi\n", "gapless.txt": "0\n", "crossfade.txt": "6000\n", "replaygain.txt": "1\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want := config.Playback{Quality: 2, Gapless: false, CrossfadeMS: 6000, ReplayGain: true}
	if pb := loadPlayback(); pb != want {
		t.Fatalf("loaded %+v, want %+v", pb, want)
	}
	if pb, ok := config.LoadPlayback(); !ok || pb != want {
		t.Fatalf("playback.json %+v %v, want the migrated %+v", pb, ok, want)
	}
	_ = os.WriteFile(filepath.Join(dir, "quality.txt"), []byte("normal\n"), 0o600)
	if pb := loadPlayback(); pb.Quality != 2 {
		t.Fatalf("quality %d: the old file won over playback.json", pb.Quality)
	}
}

// A login hands the account's target loudness (GAIN.TARGET) to the player and
// playback.json; the next start applies the saved one before any login.
func TestLoginAppliesGainTarget(t *testing.T) {
	useTempConfig(t)
	prev := deezer.SetTransport(roundTrip(func(r *http.Request) (*http.Response, error) {
		body := `{"error":{},"results":{"checkForm":"tok","USER":{"USER_ID":"7","BLOG_NAME":"u","OPTIONS":{"license_token":"lic"}},` +
			`"OFFERS":[{"title":"Premium"}],"GAIN":{"TARGET":"-15","ADS":"-13"}}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	}))
	t.Cleanup(func() { deezer.SetTransport(prev) })
	m := interactionModel(screenLoading)
	m.client = deezer.New("arl")
	m.Update(m.loginCmd()())
	if got := m.player.ReplayGainTarget(); got != -15 {
		t.Fatalf("player target %v after the login, want -15", got)
	}
	if pb, ok := config.LoadPlayback(); !ok || pb.GainTarget != -15 {
		t.Fatalf("playback.json %+v %v, want gainTarget -15", pb, ok)
	}
	p := &audio.Player{}
	New(deezer.New("arl"), p)
	if got := p.ReplayGainTarget(); got != -15 {
		t.Fatalf("a new model's player target %v, want the saved -15", got)
	}
}

// roundTrip is an http.RoundTripper made of a function.
type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The TUI's playback settings save into playback.json.
func TestPlaybackSettingsSaveSharedFile(t *testing.T) {
	useTempConfig(t)
	if err := config.SavePlayback(config.Playback{Gapless: true}); err != nil {
		t.Fatal(err)
	}
	m := interactionModel(screenList)
	m.client = deezer.New("arl")
	for _, label := range []string{"Crossfade", "Audio quality", "ReplayGain", "Gapless"} {
		changeSetting(t, m, label)
	}
	pb, ok := config.LoadPlayback()
	if !ok || pb.CrossfadeMS != 3000 || pb.Quality != 1 || !pb.ReplayGain || pb.Gapless != m.player.Gapless() {
		t.Fatalf("playback.json %+v %v after the settings", pb, ok)
	}
}

// changeSetting opens Settings with ',' and presses enter on the row labeled label; the
// cursor stays on that row.
func changeSetting(t *testing.T, m *Model, label string) {
	t.Helper()
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}})
	for i, s := range m.settings() {
		if s.label != label {
			continue
		}
		m.list.Select(i)
		m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
		if m.list.Index() != i || m.list.SelectedItem().(row).right != m.settings()[i].value() {
			t.Fatalf("%s: cursor %d, row %+v after enter", label, m.list.Index(), m.list.SelectedItem())
		}
		return
	}
	t.Fatalf("no setting %q", label)
}

// After a login the TUI learns the web player's build and keeps it for the
// next start (web-release.json); a new model starts from the kept one.
func TestLoginKeepsWebRelease(t *testing.T) {
	useTempConfig(t)
	t.Cleanup(func() { deezer.SetWebRelease("", "") })
	deezer.SetWebRelease("", "")
	const bundle = "https://cdn-files.dzcdn.net/cache/js/app-web.cafe.js"
	prev := deezer.SetTransport(roundTrip(func(r *http.Request) (*http.Response, error) {
		body := `{"error":{},"results":{"checkForm":"tok","USER":{"USER_ID":"7","BLOG_NAME":"u","OPTIONS":{"license_token":"lic"}},"OFFERS":[{"title":"Premium"}]}}`
		switch {
		case r.URL.Host == "www.deezer.com" && r.URL.Path == "/":
			body = `<script src="` + bundle + `"></script>`
		case r.URL.String() == bundle:
			body = `releaseNumeric:"10020261124120000",`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	}))
	t.Cleanup(func() { deezer.SetTransport(prev) })
	m := interactionModel(screenLoading)
	m.client = deezer.New("arl")
	ttl := statusTTL
	statusTTL = 0 // runCmds runs the status's expiry tick too
	t.Cleanup(func() { statusTTL = ttl })
	_, cmd := m.Update(m.loginCmd()())
	runCmds(cmd)
	if b, release := config.LoadWebRelease(); b != bundle || release != "10020261124120000" {
		t.Fatalf("web-release.json %q %q", b, release)
	}
	deezer.SetWebRelease("", "")
	New(deezer.New("arl"), &audio.Player{})
	if release, _ := deezer.WebRelease(); release != "10020261124120000" {
		t.Fatalf("a new model starts from release %q", release)
	}
}

// runCmds runs cmd and the commands its batches hold, on this goroutine.
func runCmds(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if b, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range b {
			runCmds(c)
		}
	}
}
