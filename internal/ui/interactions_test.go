package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/audio"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/config"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/listens"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/mediacache"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/preload"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/queue"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/update"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// interactionModel builds a UI-only model (no client / audio device) with the
// production browse-list configuration, for exercising key handling. The zero
// audio.Player has no output: it reports Stopped and its non-output calls work.
func interactionModel(s screen) *Model {
	m := &Model{
		screen: s,
		search: textinput.New(),
		q:      queue.New(),
		player: &audio.Player{},
		caches: &mediacache.Host{},
		width:  80,
		height: 24,
	}
	m.list = newBrowseList(m)
	m.list.SetSize(80, 24)
	m.preloads = preload.New(func() *audio.Player { return m.player }, m.resolvePreload)
	m.listens = listens.New(func() listens.Sender { return nil })
	return m
}

func testTracks(n int) []deezer.Track {
	ts := make([]deezer.Track, n)
	for i := range ts {
		id := string(rune('a' + i))
		ts[i] = deezer.Track{
			ID: id, Name: "Track " + id,
			Artists:   []deezer.Artist{{ID: id, Name: "Artist " + id}},
			AlbumName: "Album " + id,
		}
	}
	return ts
}

func setTrackList(m *Model, ts []deezer.Track) {
	items := make([]list.Item, len(ts))
	for i, t := range ts {
		items[i] = trackRow(t)
	}
	m.browse = ts
	m.list.SetItems(items)
	m.list.ResetSelected()
}

func keyRunes(m *Model, r rune) {
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
}

// ---- ctrl+f local filter ----

func TestCtrlFStartsListFilterAndSlashStaysSearch(t *testing.T) {
	m := interactionModel(screenList)
	setTrackList(m, testTracks(3))

	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlF})
	if m.list.FilterState() != list.Filtering {
		t.Fatalf("ctrl+f should start the list filter, state=%v", m.list.FilterState())
	}
	// While filtering, plain keys type into the filter, not global bindings.
	keyRunes(m, 'q')
	if m.screen != screenList {
		t.Fatalf("typing into the filter changed screens: %v", m.screen)
	}

	// "/" (outside filtering) still opens Deezer search.
	m2 := interactionModel(screenList)
	setTrackList(m2, testTracks(3))
	keyRunes(m2, '/')
	if m2.screen != screenSearch {
		t.Fatalf("/ should open Deezer search, got screen %v", m2.screen)
	}
}

func TestCtrlFIgnoredOnOverlays(t *testing.T) {
	m := interactionModel(screenHelp)
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlF})
	if m.list.FilterState() == list.Filtering {
		t.Fatal("ctrl+f on an overlay must not start filtering the hidden list")
	}
}

func TestEscClearsAppliedFilterBeforeNavigating(t *testing.T) {
	m := interactionModel(screenList)
	setTrackList(m, testTracks(3))
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlF})
	keyRunes(m, 'a')
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter}) // apply the filter
	if m.list.FilterState() != list.FilterApplied {
		t.Fatalf("filter not applied: %v", m.list.FilterState())
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.list.FilterState() != list.Unfiltered || m.screen != screenList {
		t.Fatalf("first esc should only clear the filter (state=%v screen=%v)",
			m.list.FilterState(), m.screen)
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.screen != screenMenu {
		t.Fatalf("second esc should navigate back, got %v", m.screen)
	}
}

func TestFilterValueIncludesArtistAndAlbum(t *testing.T) {
	r := trackRow(deezer.Track{
		Name:      "Song",
		Artists:   []deezer.Artist{{Name: "Some Artist"}},
		AlbumName: "Great Album",
	})
	fv := r.FilterValue()
	if !strings.Contains(fv, "Some Artist") || !strings.Contains(fv, "Great Album") {
		t.Fatalf("FilterValue should include artist and album: %q", fv)
	}
	if sectionRow("Top tracks").FilterValue() != "" {
		t.Fatal("section headers must not match filters")
	}
}

// ---- artist page ----

func TestArtistPageBuildsSectionedList(t *testing.T) {
	m := interactionModel(screenLoading)
	page := &deezer.ArtistPage{
		Artist: deezer.ArtistInfo{ID: "1", Name: "Artist"},
		Top:    testTracks(2),
		Albums: []deezer.Album{{ID: "al1", Name: "LP", Artists: []deezer.Artist{{Name: "Artist"}}}},
		Related: []deezer.ArtistInfo{
			{ID: "2", Name: "Friend"},
		},
	}
	_, _ = m.Update(artistPageMsg{page: page})

	if m.screen != screenList {
		t.Fatalf("screen = %v, want list", m.screen)
	}
	items := m.list.Items()
	// 3 headers (the later two after a blank row) + 2 tracks + 1 album + 1 related.
	if len(items) != 9 {
		t.Fatalf("items = %d, want 9", len(items))
	}
	kinds := make([]rowKind, len(items))
	for i, it := range items {
		kinds[i] = it.(row).kind
	}
	want := []rowKind{rowSection, rowTrack, rowTrack, rowSection, rowSection, rowAlbum, rowSection, rowSection, rowArtist}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("item %d kind = %v, want %v (%v)", i, kinds[i], want[i], kinds)
		}
	}
	if items[3].(row).title != "" || items[4].(row).title == "" {
		t.Fatalf("no blank row above the second section: %+v %+v", items[3], items[4])
	}
	if len(m.browse) != 2 || m.browse[0].ID != "a" {
		t.Fatalf("browse should hold only the top tracks: %+v", m.browse)
	}
	if m.list.Index() != 1 {
		t.Fatalf("selection should start on the first entry, not the header: %d", m.list.Index())
	}
	// Enter on the album row opens it (returns a fetch command).
	m.list.Select(5)
	_, cmd := m.activate()
	if cmd == nil {
		t.Fatal("activating an album row should return a command")
	}
	// Section headers do nothing.
	m.list.Select(4)
	_, cmd = m.activate()
	if cmd != nil {
		t.Fatal("activating a section header should be a no-op")
	}
}

// ---- like toggle ----

func TestLikeToggleFlipsCachedSetOptimistically(t *testing.T) {
	m := interactionModel(screenList)
	ts := testTracks(2)
	setTrackList(m, ts)
	m.likedIDs = map[string]bool{"a": true}

	// Selected row "a" is liked -> f unlikes it (optimistically removed).
	keyRunes(m, 'f')
	if m.likedIDs["a"] {
		t.Fatal("unlike should remove the id from the cached set")
	}
	// A failed round-trip rolls the flip back.
	_, _ = m.Update(favToggleMsg{id: "a", name: "Track a", liked: false, err: errFake})
	if !m.likedIDs["a"] {
		t.Fatal("failed unlike should restore the cached id")
	}

	// Selected row "b" is not liked -> f likes it.
	m.list.Select(1)
	keyRunes(m, 'f')
	if !m.likedIDs["b"] {
		t.Fatal("like should add the id to the cached set")
	}
}

func TestFavoritesViewRefreshesLikedCache(t *testing.T) {
	m := interactionModel(screenMenu)
	m.likedIDs = map[string]bool{"stale": true}
	_, _ = m.Update(tracksMsg{title: "Liked", tracks: testTracks(2), favorites: true})
	if m.likedIDs["stale"] || !m.likedIDs["a"] || !m.likedIDs["b"] {
		t.Fatalf("favorites view should rebuild the liked cache: %v", m.likedIDs)
	}
	if m.ownPlaylists {
		t.Fatal("a tracks view must clear the own-playlists flag")
	}
}

// ---- add to playlist / playlist CRUD ----

func TestAddToPlaylistPickerFlow(t *testing.T) {
	m := interactionModel(screenList)
	setTrackList(m, testTracks(2))

	// The picker opens over the browse list (snapshot + restore like devices).
	_, _ = m.Update(playlistPickMsg{
		playlists: []deezer.Playlist{{ID: "p1", Name: "Mix"}},
		track:     m.browse[0],
	})
	if m.screen != screenPlaylistPick {
		t.Fatalf("screen = %v, want playlist picker", m.screen)
	}
	items := m.list.Items()
	if len(items) != 2 || items[0].(row).action != actNewPlaylist || items[1].(row).kind != rowPlaylist {
		t.Fatalf("picker items wrong: %+v", items)
	}
	// Enter on a playlist adds and restores the browse list.
	m.list.Select(1)
	_, cmd := m.activate()
	if cmd == nil {
		t.Fatal("picking a playlist should return the add command")
	}
	if m.screen != screenList || len(m.list.Items()) != 2 || m.list.Items()[0].(row).kind != rowTrack {
		t.Fatalf("picker should restore the browse list: screen=%v items=%d", m.screen, len(m.list.Items()))
	}
}

func TestNewPlaylistEntryOpensTitlePrompt(t *testing.T) {
	m := interactionModel(screenList)
	setTrackList(m, testTracks(1))
	_, _ = m.Update(playlistPickMsg{playlists: nil, track: m.browse[0]})
	_, _ = m.activate() // index 0 = "New playlist…"
	if m.screen != screenPlaylistPrompt || m.plPrompt != plCreateWithTrack {
		t.Fatalf("new-playlist entry: screen=%v prompt=%v", m.screen, m.plPrompt)
	}
	// Enter with a title dispatches the create and leaves the prompt.
	m.search.SetValue("Road trip")
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || m.screen == screenPlaylistPrompt {
		t.Fatalf("prompt enter should create and close (screen=%v)", m.screen)
	}
}

func TestPlaylistOpsKeysOnOwnPlaylistsScreen(t *testing.T) {
	m := interactionModel(screenMenu)
	_, _ = m.Update(playlistsMsg{title: "My Playlists", playlists: []deezer.Playlist{{ID: "p1", Name: "Mix"}}})
	if !m.ownPlaylists || m.screen != screenList {
		t.Fatalf("playlists view: own=%v screen=%v", m.ownPlaylists, m.screen)
	}

	// R opens the rename prompt prefilled with the current title.
	keyRunes(m, 'R')
	if m.screen != screenPlaylistPrompt || m.plPrompt != plRename || m.search.Value() != "Mix" {
		t.Fatalf("rename prompt: screen=%v prompt=%v value=%q", m.screen, m.plPrompt, m.search.Value())
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.screen != screenList {
		t.Fatalf("prompt esc should return to the playlists screen, got %v", m.screen)
	}

	// X asks for confirmation; n cancels, y deletes.
	keyRunes(m, 'X')
	if !m.plConfirm {
		t.Fatal("X should arm the delete confirmation")
	}
	keyRunes(m, 'n') // must cancel, NOT skip to the next track
	if m.plConfirm {
		t.Fatal("n should cancel the delete confirmation")
	}
	keyRunes(m, 'X')
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if m.plConfirm || cmd == nil {
		t.Fatal("y should dispatch the delete command")
	}

	// N opens the create prompt.
	m.loading = false
	keyRunes(m, 'N')
	if m.screen != screenPlaylistPrompt || m.plPrompt != plCreateEmpty {
		t.Fatalf("N should open the new-playlist prompt: screen=%v prompt=%v", m.screen, m.plPrompt)
	}
}

func TestPlaylistOpsIgnoredOffPlaylistsScreen(t *testing.T) {
	m := interactionModel(screenList)
	setTrackList(m, testTracks(1)) // a plain browse list (ownPlaylists = false)
	keyRunes(m, 'N')
	if m.screen == screenPlaylistPrompt {
		t.Fatal("N must be inert outside the playlists screen")
	}
	keyRunes(m, 'X')
	if m.plConfirm {
		t.Fatal("X must not arm a delete outside the playlists screen")
	}
}

// An available update is the home menu's first row, added when the check answers.
func TestUpdateNoticeIsFirstMenuRow(t *testing.T) {
	useTempConfig(t)
	m := interactionModel(screenMenu)
	m.showMenu()
	_, _ = m.Update(updateCheckMsg{info: update.Info{Latest: "9.9.9", HasUpdate: true, URL: "https://example.invalid"}})
	r := m.list.Items()[0].(row)
	if r.action != actUpdate || !strings.Contains(r.title, "9.9.9") {
		t.Fatalf("first menu row %+v", r)
	}
}

// ---- interactive queue + queue building ----

func TestQueueScreenNavigationAndEditing(t *testing.T) {
	m := interactionModel(screenList)
	ts := testTracks(4)
	m.q.Set(ts, 0)

	keyRunes(m, 'u') // open the queue view with the cursor on the playing row
	if m.screen != screenQueue || m.queueSel != 0 {
		t.Fatalf("queue open: screen=%v sel=%d", m.screen, m.queueSel)
	}
	keyRunes(m, 'j')
	keyRunes(m, 'j')
	if m.queueSel != 2 {
		t.Fatalf("j j -> sel %d, want 2", m.queueSel)
	}
	keyRunes(m, 'k')
	if m.queueSel != 1 {
		t.Fatalf("k -> sel %d, want 1", m.queueSel)
	}

	// x removes the selected (non-playing) row.
	keyRunes(m, 'x')
	if m.q.Len() != 3 || m.q.Tracks()[1].ID != "c" {
		t.Fatalf("x should remove row 1: len=%d tracks[1]=%q", m.q.Len(), m.q.Tracks()[1].ID)
	}
	// x on the playing row is refused.
	m.queueSel = 0
	keyRunes(m, 'x')
	if m.q.Len() != 3 {
		t.Fatal("x must not remove the playing track")
	}

	// J/K move the selected row, cursor following.
	m.queueSel = 1 // "c"
	keyRunes(m, 'J')
	if m.q.Tracks()[2].ID != "c" || m.queueSel != 2 {
		t.Fatalf("J: tracks=%v sel=%d", m.q.Tracks(), m.queueSel)
	}
	keyRunes(m, 'K')
	if m.q.Tracks()[1].ID != "c" || m.queueSel != 1 {
		t.Fatalf("K: tracks=%v sel=%d", m.q.Tracks(), m.queueSel)
	}

	// enter jumps playback to the selected row.
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.q.Index() != 1 || cmd == nil {
		t.Fatalf("enter should jump to row 1 and start a play: idx=%d", m.q.Index())
	}

	// g / G jump the cursor.
	keyRunes(m, 'G')
	if m.queueSel != m.q.Len()-1 {
		t.Fatalf("G -> sel %d", m.queueSel)
	}
	keyRunes(m, 'g')
	if m.queueSel != 0 {
		t.Fatalf("g -> sel %d", m.queueSel)
	}
}

func TestBrowsePlayNextAndAddToEnd(t *testing.T) {
	m := interactionModel(screenList)
	ts := testTracks(3)
	setTrackList(m, ts)
	m.q.Set([]deezer.Track{{ID: "z", Name: "Playing"}}, 0)

	// E on a selected track row queues it right after the current track.
	m.list.Select(1) // track "b"
	keyRunes(m, 'E')
	if m.q.Len() != 2 || m.q.Tracks()[1].ID != "b" || m.q.Index() != 0 {
		t.Fatalf("play-next: tracks=%v idx=%d", m.q.Tracks(), m.q.Index())
	}
	// e appends to the end.
	m.list.Select(2) // track "c"
	keyRunes(m, 'e')
	if m.q.Len() != 3 || m.q.Tracks()[2].ID != "c" {
		t.Fatalf("append: tracks=%v", m.q.Tracks())
	}
	// n is the next track, also with a track row selected.
	before := m.q.Index()
	keyRunes(m, 'n')
	if m.q.Index() != before+1 || m.q.Len() != 3 {
		t.Fatalf("n should advance playback: %d -> %d, queue %d", before, m.q.Index(), m.q.Len())
	}
}

// ---- control queue snapshot cache ----

func TestCtrlQueueSnapshotRebuiltOnlyOnVersionChange(t *testing.T) {
	m := interactionModel(screenList)
	if m.ctrlQueueSnapshot() != nil {
		t.Fatal("empty queue should snapshot to nil")
	}
	m.q.Set(testTracks(3), 0)
	s1 := m.ctrlQueueSnapshot()
	if len(s1) != 3 || s1[0].ID != "a" {
		t.Fatalf("snapshot = %+v", s1)
	}
	s2 := m.ctrlQueueSnapshot()
	if &s1[0] != &s2[0] {
		t.Fatal("unchanged queue must reuse the cached snapshot slice")
	}
	// Cursor moves don't bump the version: still cached.
	m.q.Next()
	s3 := m.ctrlQueueSnapshot()
	if &s1[0] != &s3[0] {
		t.Fatal("cursor move must not rebuild the snapshot")
	}
	// A queue edit does.
	m.q.Append(deezer.Track{ID: "x", Name: "X"})
	s4 := m.ctrlQueueSnapshot()
	if len(s4) != 4 || s4[3].ID != "x" {
		t.Fatalf("snapshot after append = %+v", s4)
	}
}

// ---- mouse ----

func TestMouseWheelScrollsListAndClickSelectsRow(t *testing.T) {
	m := interactionModel(screenList)
	setTrackList(m, testTracks(5))
	m.list.SetSize(80, 20)

	_, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if m.list.Index() != 1 {
		t.Fatalf("wheel down should move the cursor, index=%d", m.list.Index())
	}
	_, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	if m.list.Index() != 0 {
		t.Fatalf("wheel up should move the cursor back, index=%d", m.list.Index())
	}

	// Row n of a track list is under the header and the column header, at 80 and at
	// 120 columns (where the side panel starts).
	for _, w := range []int{80, 120} {
		m.width = w
		m.list.Select(0)
		l := m.layout()
		if (w == 120) != (l.panel > 0) || l.head != 1 {
			t.Fatalf("width %d: panel %d, head %d", w, l.panel, l.head)
		}
		y := l.top + l.head + 2
		_, _ = m.Update(tea.MouseMsg{X: 4, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		if m.list.Index() != 2 {
			t.Fatalf("width %d: click should select row 2, index=%d", w, m.list.Index())
		}
		// The title row, the column header and below the last item do nothing.
		for _, y := range []int{0, l.top, l.top + l.head + 5} {
			_, _ = m.Update(tea.MouseMsg{X: 4, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			if m.list.Index() != 2 || m.screen != screenList {
				t.Fatalf("width %d: click on row %d must be ignored, index=%d", w, y, m.list.Index())
			}
		}
	}
	// A second click on the selected row activates it (play => queue commit).
	m.width = 80
	_, cmd := m.Update(tea.MouseMsg{X: 4, Y: 4, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if cmd == nil || m.q.Len() != 5 || m.q.Index() != 2 {
		t.Fatalf("click-on-selected should activate: qlen=%d idx=%d", m.q.Len(), m.q.Index())
	}
}

// A click on a header view opens it; Browse goes back to the list. A click on the
// queue's row n selects entry n. A click in the side panel opens Now Playing.
func TestMouseClickHeaderViewsQueueRowsAndPanel(t *testing.T) {
	m := interactionModel(screenList)
	setTrackList(m, testTracks(3))
	m.q.Set(testTracks(4), 0)
	click := func(x, y int) {
		_, _ = m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	}
	tabs := m.layout().tabs
	if len(tabs) != len(views) {
		t.Fatal("no view switches at 80 columns")
	}
	click(tabs[2][0], 0)
	if m.screen != screenQueue {
		t.Fatalf("click on Queue opened %v", m.screen)
	}
	click(2, 4) // under the header and the column header: entry 2 ("c")
	if m.queueSel != 2 {
		t.Fatalf("queue click selected %d, want 2", m.queueSel)
	}
	click(tabs[0][0], 0)
	if m.screen != screenList {
		t.Fatalf("click on Browse went to %v", m.screen)
	}
	m.width = 120
	click(110, 5)
	if m.screen != screenNowPlaying {
		t.Fatalf("click in the panel opened %v", m.screen)
	}
}

// The progress bar is the footer's first row: position, the bar with its knob, length.
// A click at a quarter of the bar asks a seek to a quarter of the track.
func TestProgressBarRowAndSeek(t *testing.T) {
	m := interactionModel(screenList)
	m.ready = true
	l := m.layout()
	bar := ansi.Strip(strings.Split(m.View(), "\n")[l.bar])
	if !strings.HasPrefix(bar, " 0:00 ●─") || !strings.HasSuffix(bar, "─ 0:00") {
		t.Fatalf("bar row %q", bar)
	}
	if ms, ok := l.seekAt(l.barX+l.barW/4, 240000); !ok || ms != 60000 {
		t.Fatalf("a click at 25%% seeks to %d (%v)", ms, ok)
	}
	if _, ok := l.seekAt(l.barX-1, 240000); ok {
		t.Fatal("a click on the position seeks")
	}
}

func TestMouseWheelScrollsHelpAndQueue(t *testing.T) {
	m := interactionModel(screenHelp)
	_, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if m.helpOffset != 1 {
		t.Fatalf("wheel should scroll help, offset=%d", m.helpOffset)
	}
	q := interactionModel(screenQueue)
	q.q.Set(testTracks(3), 0)
	_, _ = q.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if q.queueSel != 1 {
		t.Fatalf("wheel should move the queue cursor, sel=%d", q.queueSel)
	}
}

// errFake is a sentinel error for round-trip failure tests.
var errFake = errFakeT{}

type errFakeT struct{}

func (errFakeT) Error() string { return "fake failure" }

var keyEnter = tea.KeyMsg{Type: tea.KeyEnter}

func curID(m *Model) string {
	t, _ := m.q.Current()
	return t.ID
}

func useTempConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
}

// The ARL, playback and theme files are replaced whole, with no temp file left.
func TestSettingsFilesWrittenAtomically(t *testing.T) {
	useTempConfig(t)
	for range 2 {
		if err := SaveARL(" arl-value "); err != nil {
			t.Fatal(err)
		}
		if err := config.SavePlayback(config.Playback{Quality: 2}); err != nil {
			t.Fatal(err)
		}
		if err := SaveTheme("nord"); err != nil {
			t.Fatal(err)
		}
	}
	if pb, _ := config.LoadPlayback(); LoadARL() != "arl-value" || pb.Quality != 2 || LoadTheme() != "nord" {
		t.Fatalf("read back %q %d %q", LoadARL(), pb.Quality, LoadTheme())
	}
	dir, _ := config.Dir()
	if tmps, _ := filepath.Glob(filepath.Join(dir, "*.tmp*")); len(tmps) != 0 {
		t.Fatalf("temp files left: %v", tmps)
	}
}

// A queue edit must not change what Enter on a row plays.
func TestEnterPlaysRowTrackAfterQueueRemoval(t *testing.T) {
	m := interactionModel(screenList)
	setTrackList(m, testTracks(3))
	_, _ = m.handleKey(keyEnter) // queue a b c, playing a
	m.queueRemove(1)             // drop b from the queue
	m.list.Select(1)             // row b
	_, _ = m.handleKey(keyEnter)
	if curID(m) != "b" {
		t.Fatalf("Enter on row b played %q", curID(m))
	}
}

// A row whose track is not in the browse slice plays that track alone.
func TestEnterOnRowMissingFromBrowsePlaysRowTrack(t *testing.T) {
	m := interactionModel(screenList)
	setTrackList(m, testTracks(2))
	m.browse = testTracks(1)
	m.list.Select(1)
	_, _ = m.handleKey(keyEnter)
	if curID(m) != "b" || m.q.Len() != 1 {
		t.Fatalf("played %q (queue %d), want b alone", curID(m), m.q.Len())
	}
}

// A new list clears the filter of the previous one.
func TestNewListClearsFilter(t *testing.T) {
	m := interactionModel(screenList)
	setTrackList(m, testTracks(3))
	m.list.SetFilterText("Track a")
	_, _ = m.Update(tracksMsg{title: "Album", tracks: testTracks(2)})
	if m.list.FilterState() != list.Unfiltered || len(m.list.VisibleItems()) != 2 {
		t.Fatalf("filter state %v, %d visible", m.list.FilterState(), len(m.list.VisibleItems()))
	}
}

// Enter, g/G, list keys and D on an overlay leave the hidden list alone.
func TestOverlayKeysDoNotActOnHiddenList(t *testing.T) {
	for _, s := range []screen{screenNowPlaying, screenHelp, screenCredits} {
		m := interactionModel(s)
		setTrackList(m, testTracks(3))
		m.acct.Premium = true
		_, _ = m.handleKey(keyEnter)
		if m.q.Len() != 0 {
			t.Fatalf("screen %v: Enter replaced the queue", s)
		}
		keyRunes(m, 'G')
		_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
		if m.list.Index() != 0 {
			t.Fatalf("screen %v: list cursor moved to %d", s, m.list.Index())
		}
		keyRunes(m, 'D')
		if m.downloading {
			t.Fatalf("screen %v: D downloaded the hidden row", s)
		}
	}
}

// G with a filter applied selects the last visible row.
func TestGWithFilterSelectsLastVisible(t *testing.T) {
	m := interactionModel(screenList)
	setTrackList(m, []deezer.Track{{ID: "1", Name: "Alpha"}, {ID: "2", Name: "Beta"}, {ID: "3", Name: "Gamma"}})
	m.list.SetFilterText("Beta")
	keyRunes(m, 'G')
	if it, ok := m.list.SelectedItem().(row); !ok || it.track.ID != "2" {
		t.Fatalf("G selected %v", m.list.SelectedItem())
	}
}

// Control-API queue edits keep the queue view's cursor on its track.
func TestControlQueueEditsKeepQueueCursor(t *testing.T) {
	m := interactionModel(screenQueue)
	m.q.Set(testTracks(5), 0)
	m.queueSel = 3 // d
	reply := make(chan error, 1)
	_, _ = m.Update(controlQueueEditMsg{kind: "remove", i: 1, reply: reply})
	if <-reply != nil || m.q.Tracks()[m.queueSel].ID != "d" {
		t.Fatalf("after remove the cursor is on %q", m.q.Tracks()[m.queueSel].ID)
	}
	_, _ = m.Update(controlQueueEditMsg{kind: "move", i: 3, j: 1, reply: reply}) // e before d
	if <-reply != nil || m.q.Tracks()[m.queueSel].ID != "d" {
		t.Fatalf("after move the cursor is on %q", m.q.Tracks()[m.queueSel].ID)
	}
}

// A track change on the lyrics screen loads the new track's lyrics.
func TestTrackChangeOnLyricsScreenReloadsLyrics(t *testing.T) {
	m := interactionModel(screenLyrics)
	ts := testTracks(2)
	m.lyricsTrack = "a"
	cmd := m.onTrackChanged(ts[1])
	if m.lyricsTrack != "b" || cmd == nil {
		t.Fatalf("lyricsTrack = %q, cmd nil = %v", m.lyricsTrack, cmd == nil)
	}
}

// A track start only records the next track: nothing is resolved until the
// player's near-end hook runs, which arms the queue's next track once.
func TestPreloadWaitsForNearEnd(t *testing.T) {
	m := interactionModel(screenList)
	ts := testTracks(3)
	m.q.Set(ts, 0)
	var resolved []string
	m.player.SetGapless(true)
	m.preloads = preload.New(func() *audio.Player { return m.player }, func(req deezer.Track) (*deezer.StreamPlan, deezer.Track, error) {
		resolved = append(resolved, req.ID)
		return &deezer.StreamPlan{TrackID: req.ID}, req, nil
	})
	_, _ = m.Update(playStartedMsg{track: ts[0], seq: m.pickSeq.Load()})
	if len(resolved) != 0 {
		t.Fatalf("a track start resolved %v", resolved)
	}
	m.preloads.Resolve() // the near-end hook
	if len(resolved) != 1 || resolved[0] != "b" {
		t.Fatalf("near end resolved %v, want [b]", resolved)
	}
	m.preloads.Resolve()
	if len(resolved) != 1 {
		t.Fatalf("a second run of the hook resolved %v", resolved)
	}
}

// A track ending while a pick resolves must not advance past the pick.
func TestFinishDuringPendingPickDoesNotAdvance(t *testing.T) {
	m := interactionModel(screenList)
	m.q.Set(testTracks(3), 0)
	_ = m.playCurrent()
	_, _ = m.Update(trackFinishedMsg{swapped: false})
	if curID(m) != "a" {
		t.Fatalf("finish advanced over the pending pick to %q", curID(m))
	}
}

// The delete prompt is visible, and is dropped by a mouse action or a screen
// change, so a later y cannot delete.
func TestPlaylistDeletePromptVisibleAndDisarmed(t *testing.T) {
	m := interactionModel(screenMenu)
	m.ready = true
	_, _ = m.Update(playlistsMsg{title: "My Playlists", playlists: []deezer.Playlist{{ID: "p1", Name: "Mix"}}})
	keyRunes(m, 'X')
	m.status = "something else"
	if !strings.Contains(m.View(), "Delete playlist Mix?") {
		t.Fatal("delete prompt not rendered")
	}
	_, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if m.plConfirm {
		t.Fatal("mouse action left the delete armed")
	}
	keyRunes(m, 'X')
	_, _ = m.Update(tracksMsg{title: "Album", tracks: testTracks(1)})
	if m.plConfirm {
		t.Fatal("list change left the delete armed")
	}
}

// Ctrl+C quits from every input, including a pending confirm and an active
// filter.
func TestCtrlCAlwaysQuits(t *testing.T) {
	ctrlC := tea.KeyMsg{Type: tea.KeyCtrlC}
	for _, s := range []screen{screenSearch, screenRemoteInput, screenPlaylistPrompt, screenList} {
		m := interactionModel(s)
		m.plConfirm = s == screenList
		_, cmd := m.handleKey(ctrlC)
		if cmd == nil {
			t.Fatalf("screen %v: ctrl+c did nothing", s)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("screen %v: ctrl+c did not quit", s)
		}
	}
	m := interactionModel(screenList)
	setTrackList(m, testTracks(2))
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlF})
	if _, cmd := m.handleKey(ctrlC); cmd == nil {
		t.Fatal("ctrl+c while filtering did nothing")
	} else if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c while filtering did not quit")
	}
}

// A list that arrives after the user has moved on does not switch the screen,
// so the keys typed meanwhile stay in the search box.
func TestLateResultDoesNotSwitchScreen(t *testing.T) {
	m := interactionModel(screenList)
	m.list.SetItems([]list.Item{albumRow(deezer.Album{ID: "al", Name: "LP"})})
	_, _ = m.Update(keyEnter) // request the album
	gen := m.nav
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	_, _ = m.Update(navMsg{gen: gen, msg: tracksMsg{title: "LP", tracks: testTracks(2)}})
	if m.screen != screenSearch {
		t.Fatalf("late result switched the screen to %v", m.screen)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if m.search.Value() != "q" {
		t.Fatalf("q did not reach the search box (value %q)", m.search.Value())
	}
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("q quit")
		}
	}
	// A current result is shown.
	_, _ = m.Update(navMsg{gen: m.nav, msg: tracksMsg{title: "LP", tracks: testTracks(2)}})
	if m.screen != screenList {
		t.Fatalf("current result not shown: screen %v", m.screen)
	}
}

// A stale result keeps the spinner of a newer request; a result made stale
// only by leaving the screen stops it.
func TestStaleResultKeepsNewerRequestLoading(t *testing.T) {
	m := interactionModel(screenMenu)
	first := m.navCmd(func() tea.Msg { return tracksMsg{title: "A"} })
	_ = m.navCmd(func() tea.Msg { return tracksMsg{title: "B"} })
	m.loading = true
	_, _ = m.Update(first())
	if !m.loading {
		t.Fatal("a stale result stopped the newer request's spinner")
	}
	m.nav++ // the user leaves the screen: the newer result is stale too
	_, _ = m.Update(navMsg{gen: m.navReq, msg: tracksMsg{title: "B"}})
	if m.loading {
		t.Fatal("the last request's stale result left the spinner on")
	}
}

// The resume offset applies only to its own track.
func TestResumeOffsetStaysWithItsTrack(t *testing.T) {
	m := interactionModel(screenList)
	m.q.Set(testTracks(2), 0)
	m.resumeID, m.resumeMS = "a", 5000
	_ = m.playCurrent()
	if m.resumeMS != 5000 {
		t.Fatal("playCurrent dropped the offset of its own track")
	}
	m.q.Next()
	_ = m.playCurrent()
	if m.resumeID != "" || m.resumeMS != 0 {
		t.Fatalf("offset leaked onto the next track: %q %d", m.resumeID, m.resumeMS)
	}
}

// Shutdown writes the resume state once (at position 0 it clears a saved one);
// a second call does nothing.
func TestShutdownIdempotent(t *testing.T) {
	useTempConfig(t)
	m := interactionModel(screenList)
	m.q.Set(testTracks(1), 0)
	m.playing = true
	saved := func() bool {
		r := LoadResume()
		return r != nil
	}
	if err := SaveResume(testTracks(1)[0], 5000, false); err != nil {
		t.Fatal(err)
	}
	m.Shutdown()
	if saved() {
		t.Fatal("Shutdown at position 0 kept the old resume state")
	}
	if err := SaveResume(testTracks(1)[0], 5000, false); err != nil {
		t.Fatal(err)
	}
	m.Shutdown()
	if !saved() {
		t.Fatal("a second Shutdown wrote the resume state again")
	}
}

// Resuming a podcast episode restores episode mode.
func TestResumeRestoresEpisodeMode(t *testing.T) {
	useTempConfig(t)
	ep := deezer.Track{ID: "ep1", Name: "Episode", DurationMS: 600000}
	if err := SaveResume(ep, 5000, true); err != nil {
		t.Fatal(err)
	}
	if r := LoadResume(); r == nil || !r.Episode {
		t.Fatalf("resume state %+v lost the episode flag", r)
	}
	m := interactionModel(screenMenu)
	m.showMenu()
	_, _ = m.Update(keyEnter) // the Resume row
	if !m.episodeMode || curID(m) != "ep1" || m.resumeMS != 5000 {
		t.Fatalf("episodeMode %v, current %q, resume %d", m.episodeMode, curID(m), m.resumeMS)
	}
}

// The spinner ticks only while loading.
func TestSpinnerTicksOnlyWhileLoading(t *testing.T) {
	m := interactionModel(screenList)
	m.spinning = true
	if _, cmd := m.Update(spinner.TickMsg{}); cmd != nil || m.spinning {
		t.Fatal("idle spinner kept ticking")
	}
	m.q.Set(testTracks(1), 0)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}) // starts a load
	if cmd == nil || !m.spinning {
		t.Fatal("loading did not restart the spinner")
	}
}

// The UI tick runs only while a track is loaded or a peer is driven.
func TestTickOnlyWhilePlaying(t *testing.T) {
	m := interactionModel(screenList)
	m.ticking = true
	if _, cmd := m.Update(tickMsg{}); cmd != nil || m.ticking {
		t.Fatal("tick kept running with nothing playing")
	}
	_, _ = m.Update(playStartedMsg{track: testTracks(1)[0], seq: m.pickSeq.Load()})
	if !m.playing || !m.ticking {
		t.Fatal("a play did not start the tick")
	}
	// The zero player reports Stopped: the tick closes the play and ends.
	if _, _ = m.Update(tickMsg{}); m.playing || m.ticking {
		t.Fatal("tick kept running after the player stopped")
	}
}

// The token editor takes tokens longer than a search.
func TestTokenEditorHasNoLengthLimit(t *testing.T) {
	useTempConfig(t)
	m := interactionModel(screenWebRemote)
	m.search.CharLimit = searchCharLimit
	keyRunes(m, 't')
	tok := strings.Repeat("x", 200)
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tok)})
	if m.search.Value() != tok {
		t.Fatalf("token cut to %d characters", len(m.search.Value()))
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.search.CharLimit != searchCharLimit {
		t.Fatalf("search limit %d after editing", m.search.CharLimit)
	}
}

// Play and SetDevice run in commands (the zero player has no output and would
// panic inside Update). A play command whose pick a stop superseded does not
// play, and a stream resolved for it is dropped.
func TestBlockingPlayerCallsLeaveUpdate(t *testing.T) {
	m := interactionModel(screenList)
	m.q.Set(testTracks(2), 0)
	_ = m.playCurrent()
	ready := streamReadyMsg{plan: &deezer.StreamPlan{}, track: testTracks(1)[0], seq: m.pickSeq.Load()}
	_, play := m.Update(ready)
	if play == nil {
		t.Fatal("stream ready did not start a play command")
	}
	_, _ = m.Update(playStartedMsg{track: ready.track, seq: ready.seq})
	if !m.playing || m.resolving || m.loading {
		t.Fatalf("play started: playing %v, resolving %v, loading %v", m.playing, m.resolving, m.loading)
	}

	_ = m.playCurrent()
	ready.seq = m.pickSeq.Load()
	_, play = m.Update(ready)
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if msg := play(); msg != nil {
		t.Fatalf("superseded play command ran Play: %v", msg)
	}
	if _, cmd := m.Update(ready); cmd != nil || m.loading {
		t.Fatal("stop did not cancel the pending play")
	}

	d := interactionModel(screenDevices)
	d.prevScreen = screenList
	d.list.SetItems([]list.Item{deviceRow("x", "X", false)})
	if _, cmd := d.Update(keyEnter); cmd == nil || d.screen != screenList {
		t.Fatalf("device switch: command %v, screen %v; want a command and the list back", cmd != nil, d.screen)
	}
}

// The same track picked twice in a row plays once: the first pick's resolve
// is stale once the second pick is made.
func TestSameTrackPickedTwicePlaysOnce(t *testing.T) {
	m := interactionModel(screenList)
	m.q.Set(testTracks(2), 0)
	_ = m.playCurrent()
	first := m.pickSeq.Load()
	_ = m.playCurrent()
	m.spinning = true // no spinner tick in the returned commands
	track := testTracks(1)[0]
	if _, cmd := m.Update(streamReadyMsg{plan: &deezer.StreamPlan{}, track: track, seq: first}); cmd != nil {
		t.Fatal("the first pick's resolve started a play")
	}
	if _, cmd := m.Update(streamReadyMsg{plan: &deezer.StreamPlan{}, track: track, seq: m.pickSeq.Load()}); cmd == nil {
		t.Fatal("the second pick's resolve did not start a play")
	}
}

// Tracks that top up a radio queue are appended only while that radio still
// plays.
func TestRadioMoreAppendsToItsQueueOnly(t *testing.T) {
	m := interactionModel(screenList)
	m.q.Set(testTracks(3), 0)
	flow := listens.Context{Type: "dynamic_page_user_radio", ID: "7"}
	m.playCtx = flow
	_, _ = m.Update(radioMoreMsg{ctx: listens.Context{Type: "track_mix_contextual", ID: "1"}, tracks: []deezer.Track{{ID: "z"}}})
	_, _ = m.Update(radioMoreMsg{ctx: flow, tracks: []deezer.Track{{ID: "x"}, {ID: "y"}}})
	if ts := m.q.Tracks(); len(ts) != 5 || ts[3].ID != "x" || ts[4].ID != "y" {
		t.Fatalf("queue %v", ts)
	}
}

func activateMenuRow(m *Model, r row) tea.Cmd {
	m.list.SetItems([]list.Item{r})
	m.list.ResetSelected()
	_, cmd := m.activate()
	return cmd
}

// Flow is one tap: choosing it requests a stream that plays at once, with no
// tracklist in between; choosing it again while it plays toggles play/pause.
func TestFlowPlaysAtOnceAndTogglesWhenPlaying(t *testing.T) {
	m := interactionModel(screenMenu)
	flowRow := row{kind: rowMenu, action: actFlow}
	if cmd := activateMenuRow(m, flowRow); cmd == nil || !m.loading || m.screen != screenMenu || m.q.Len() != 0 {
		t.Fatalf("flow: cmd %v, loading %v, screen %v, queue %d", cmd != nil, m.loading, m.screen, m.q.Len())
	}

	flow := listens.Context{Type: "dynamic_page_user_radio", ID: "7"}
	m.spinning = true
	if _, cmd := m.Update(navMsg{gen: m.nav, msg: playNowMsg{tracks: testTracks(3), ctx: flow}}); cmd == nil {
		t.Fatal("the Flow batch did not start playing")
	}
	if curID(m) != "a" || m.playCtx != flow || !m.resolving {
		t.Fatalf("playing %q from %v, resolving %v", curID(m), m.playCtx, m.resolving)
	}

	m.playing, m.resolving, m.loading = true, false, false
	seq := m.pickSeq.Load()
	if cmd := activateMenuRow(m, flowRow); cmd != nil || m.loading || m.pickSeq.Load() != seq || m.q.Len() != 3 {
		t.Fatalf("flow while playing restarted it: cmd %v, loading %v", cmd != nil, m.loading)
	}
}

// A mood starts its own stream unless that mood is what plays.
func TestFlowMoodTogglesOnlyItself(t *testing.T) {
	m := interactionModel(screenList)
	m.q.Set(testTracks(2), 0)
	m.playing = true
	m.playCtx = listens.Context{Type: "dynamic_page_flow_config", ID: "chill"}
	if cmd := activateMenuRow(m, row{kind: rowMenu, action: actFlowMood, id: "chill"}); cmd != nil || m.loading {
		t.Fatal("the playing mood was restarted")
	}
	if cmd := activateMenuRow(m, row{kind: rowMenu, action: actFlowMood, id: "focus"}); cmd == nil || !m.loading {
		t.Fatal("another mood did not start")
	}
	m.loading = false
	if cmd := activateMenuRow(m, row{kind: rowMenu, action: actFlow}); cmd == nil || !m.loading {
		t.Fatal("Flow did not start while a mood played")
	}
}

// The moods list offers each mood as a row that starts it.
func TestFlowMoodsList(t *testing.T) {
	m := interactionModel(screenMenu)
	_, _ = m.Update(flowMoodsMsg{moods: []deezer.FlowMood{{ID: "chill", Title: "Chill"}, {ID: "focus"}}})
	items := m.list.Items()
	if m.screen != screenList || len(items) != 2 {
		t.Fatalf("screen %v, %d rows", m.screen, len(items))
	}
	for i, want := range []row{{title: "Chill", id: "chill"}, {title: "focus", id: "focus"}} {
		r := items[i].(row)
		if r.kind != rowMenu || r.action != actFlowMood || r.id != want.id || r.title != want.title {
			t.Fatalf("row %d: %+v", i, r)
		}
	}
	found := false
	for _, it := range m.menuRows() {
		found = found || it.(row).action == actFlowMoods
	}
	if !found {
		t.Fatal("no Flow moods row on the home menu")
	}
}

// The sleep timer cycles through the apps' presets.
func TestSleepTimerPresets(t *testing.T) {
	m := interactionModel(screenMenu)
	var got []string
	for range len(sleepMinutes) {
		m.cycleSleepTimer()
		switch p := m.player; {
		case !p.SleepActive():
			got = append(got, "off")
		case p.SleepEndOfTrack():
			got = append(got, "eot")
		default:
			got = append(got, fmtMS((p.SleepRemainingMS()+59999)/60000*60000))
		}
	}
	want := "5:00 10:00 20:00 30:00 45:00 60:00 eot off"
	if s := strings.Join(got, " "); s != want {
		t.Fatalf("cycle %q, want %q", s, want)
	}
}

// The format label names the codec only.
func TestCodecLabel(t *testing.T) {
	for raw, want := range map[string]string{
		"": "", "MP3_128": "MP3", "MP3_320": "MP3", "mp3_64": "MP3", "FLAC": "FLAC", "AAC_96": "AAC", "OPUS": "OPUS",
	} {
		if got := codecLabel(raw); got != want {
			t.Errorf("codecLabel(%q) = %q, want %q", raw, got, want)
		}
	}
}
