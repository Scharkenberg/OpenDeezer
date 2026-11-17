package history

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	return New(filepath.Join(t.TempDir(), "history.jsonl"))
}

func entry(id, title, artist string, at int64, secs int64) Entry {
	return Entry{TrackID: id, Title: title, Artist: artist, StartedAt: at, DurationPlayedSec: secs}
}

func TestRecordAndRecent(t *testing.T) {
	s := testStore(t)
	for i := 1; i <= 5; i++ {
		e := entry(fmt.Sprint(i), fmt.Sprintf("Song %d", i), "Artist", int64(1000+i), 60)
		if err := s.Record(e); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	got, err := s.Recent(3)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(got) != 3 || got[0].TrackID != "5" || got[2].TrackID != "3" {
		t.Fatalf("Recent(3) = %+v, want newest-first 5,4,3", got)
	}
	all, _ := s.Recent(0)
	if len(all) != 5 {
		t.Fatalf("Recent(0) = %d entries, want 5", len(all))
	}
}

func TestRecentReloadsFromDisk(t *testing.T) {
	s := testStore(t)
	if err := s.Record(entry("1", "A", "X", 100, 30)); err != nil {
		t.Fatal(err)
	}
	// A fresh store on the same file must lazily rebuild the index.
	s2 := New(s.Path())
	got, err := s2.Recent(10)
	if err != nil || len(got) != 1 || got[0].Title != "A" {
		t.Fatalf("reload got %+v err %v", got, err)
	}
}

func TestCorruptLineSkipped(t *testing.T) {
	s := testStore(t)
	if err := s.Record(entry("1", "A", "X", 100, 30)); err != nil {
		t.Fatal(err)
	}
	// Simulate a torn write from a crash.
	f, err := os.OpenFile(s.Path(), os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"trackId":"2","tit`); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	s2 := New(s.Path())
	got, err := s2.Recent(0)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(got) != 1 || got[0].TrackID != "1" {
		t.Fatalf("got %+v, want just the intact entry", got)
	}
}

func TestStartedAtDefaultsToNow(t *testing.T) {
	s := testStore(t)
	before := time.Now().Unix()
	if err := s.Record(Entry{TrackID: "1", Title: "A", Artist: "X", DurationPlayedSec: 5}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Recent(1)
	if len(got) != 1 || got[0].StartedAt < before {
		t.Fatalf("StartedAt not defaulted: %+v", got)
	}
}

func TestTopTracksAndArtists(t *testing.T) {
	s := testStore(t)
	now := time.Now().Unix()
	plays := []Entry{
		entry("1", "Alpha", "Ann", now-100, 200),
		entry("1", "Alpha", "Ann", now-90, 200),
		entry("2", "Beta", "Bob", now-80, 500),
		entry("2", "Beta", "Bob", now-70, 500),
		entry("3", "Gamma", "Ann", now-60, 100),
		// Old play outside the "since" window.
		entry("9", "Old", "Old Guy", now-100000, 999),
	}
	for _, e := range plays {
		if err := s.Record(e); err != nil {
			t.Fatal(err)
		}
	}
	since := time.Unix(now-1000, 0)

	tracks, err := s.TopTracks(since, 10)
	if err != nil {
		t.Fatalf("TopTracks: %v", err)
	}
	if len(tracks) != 3 {
		t.Fatalf("TopTracks = %+v, want 3", tracks)
	}
	// Ties on plays (Alpha vs Beta, 2 each) break by TotalSec desc.
	if tracks[0].TrackID != "2" || tracks[0].Plays != 2 || tracks[0].TotalSec != 1000 {
		t.Errorf("top track = %+v, want Beta 2 plays 1000s", tracks[0])
	}
	if tracks[1].TrackID != "1" || tracks[2].TrackID != "3" {
		t.Errorf("order = %v %v", tracks[1].TrackID, tracks[2].TrackID)
	}

	artists, err := s.TopArtists(since, 10)
	if err != nil {
		t.Fatalf("TopArtists: %v", err)
	}
	if len(artists) != 2 {
		t.Fatalf("TopArtists = %+v, want 2", artists)
	}
	if artists[0].Artist != "Ann" || artists[0].Plays != 3 || artists[0].TotalSec != 500 {
		t.Errorf("top artist = %+v, want Ann 3 plays 500s", artists[0])
	}

	total, err := s.TotalListenedSec(since)
	if err != nil || total != 1500 {
		t.Errorf("TotalListenedSec(since) = %d err %v, want 1500", total, err)
	}
	all, err := s.TotalListenedSec(time.Time{})
	if err != nil || all != 2499 {
		t.Errorf("TotalListenedSec(zero) = %d err %v, want 2499", all, err)
	}

	top1, _ := s.TopTracks(since, 1)
	if len(top1) != 1 {
		t.Errorf("TopTracks(n=1) = %+v", top1)
	}
}

func TestRotationCapsEntries(t *testing.T) {
	s := testStore(t)
	s.cap = 10
	// Rotation waits for cap + cap/10 entries (11), so the 12th record rotates.
	for i := 0; i < 12; i++ {
		if err := s.Record(entry(fmt.Sprint(i), "T", "A", int64(i+1), 1)); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := s.Recent(0)
	if len(got) != 10 || got[0].TrackID != "11" || got[9].TrackID != "2" {
		t.Fatalf("after rotation Recent(0) = %d entries first=%v last=%v, want 10/11/2",
			len(got), got[0].TrackID, got[len(got)-1].TrackID)
	}
	// The file itself must have been trimmed too.
	b, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Count(string(b), "\n")
	if lines != 10 {
		t.Fatalf("file has %d lines, want 10", lines)
	}
	// And a fresh store sees the same trimmed view.
	s2 := New(s.Path())
	got2, _ := s2.Recent(0)
	if len(got2) != 10 || got2[0].TrackID != "11" {
		t.Fatalf("reloaded after rotation = %d entries first=%v", len(got2), got2[0].TrackID)
	}
}

// The TUI and the GUI share the file: one's rotation keeps what the other appended.
func TestRotationKeepsOtherWritersEntries(t *testing.T) {
	a := testStore(t)
	a.cap = 10
	b := New(a.Path())
	b.cap = 10
	for i := 0; i < 11; i++ {
		if err := a.Record(entry(fmt.Sprint(i), "T", "A", int64(i+1), 1)); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Record(entry("other", "T", "A", 100, 1)); err != nil {
		t.Fatal(err)
	}
	if err := a.Record(entry("last", "T", "A", 101, 1)); err != nil { // rotates
		t.Fatal(err)
	}
	got, _ := New(a.Path()).Recent(0)
	found := false
	for _, e := range got {
		found = found || e.TrackID == "other"
	}
	if len(got) != 10 || got[0].TrackID != "last" || !found {
		t.Fatalf("after rotation: %d entries, first %v, other writer's entry kept: %v", len(got), got[0].TrackID, found)
	}
}

// TestKindRoundTripsAndExcludesEpisodesFromMusicStats proves an entry's media
// Kind survives a write + reload from disk, that a legacy line with no "kind"
// field decodes as a song (Kind==""), and that podcast episodes are kept out of
// the music-only TopTracks/TopArtists charts while still counting toward the
// overall TotalListenedSec.
func TestKindRoundTripsAndExcludesEpisodesFromMusicStats(t *testing.T) {
	s := testStore(t)
	now := time.Now().Unix()
	song := Entry{TrackID: "1", Title: "Song", Artist: "Band", StartedAt: now - 10, DurationPlayedSec: 100}
	ep := Entry{TrackID: "e1", Title: "Ep 1", Artist: "The Show", StartedAt: now - 5, DurationPlayedSec: 900, Kind: KindEpisode}
	for _, e := range []Entry{song, ep} {
		if err := s.Record(e); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	// Append a legacy line written before Kind existed (no "kind" field).
	f, err := os.OpenFile(s.Path(), os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	legacy := fmt.Sprintf(`{"trackId":"old","title":"Legacy","artist":"Band","startedAt":%d,"durationPlayedSec":50}`, now-2)
	if _, err := f.WriteString(legacy + "\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	// Fresh store: the Kind must survive the reload, and the legacy line decodes
	// as a song ("").
	s2 := New(s.Path())
	got, err := s2.Recent(0)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	kinds := map[string]string{}
	for _, e := range got {
		kinds[e.TrackID] = e.Kind
	}
	if kinds["e1"] != KindEpisode {
		t.Errorf("episode Kind = %q, want %q", kinds["e1"], KindEpisode)
	}
	if kinds["1"] != "" {
		t.Errorf("song Kind = %q, want empty", kinds["1"])
	}
	if kinds["old"] != "" {
		t.Errorf("legacy line Kind = %q, want empty (backward compatible)", kinds["old"])
	}

	// Music stats exclude the episode entirely.
	tracks, err := s2.TopTracks(time.Time{}, 10)
	if err != nil {
		t.Fatalf("TopTracks: %v", err)
	}
	for _, ts := range tracks {
		if ts.TrackID == "e1" {
			t.Fatalf("episode leaked into TopTracks: %+v", tracks)
		}
	}
	if len(tracks) != 2 { // the song + the legacy song
		t.Fatalf("TopTracks = %+v, want 2 songs", tracks)
	}
	artists, err := s2.TopArtists(time.Time{}, 10)
	if err != nil {
		t.Fatalf("TopArtists: %v", err)
	}
	for _, a := range artists {
		if a.Artist == "The Show" {
			t.Fatalf("episode's show leaked into TopArtists: %+v", artists)
		}
	}
	// But total listening time still counts the episode's 900s.
	total, err := s2.TotalListenedSec(time.Time{})
	if err != nil || total != 100+900+50 {
		t.Fatalf("TotalListenedSec = %d err %v, want %d (all media)", total, err, 100+900+50)
	}
}

// A Deezer CDN artwork URL is written as its image kind and hash and read back
// as the same URL; old lines with the full URL and other URLs read unchanged.
func TestArtworkStoredAsHash(t *testing.T) {
	const (
		hash    = "0123456789abcdef0123456789abcdef"
		gwURL   = "https://e-cdns-images.dzcdn.net/images/cover/" + hash + "/250x250-000000-80-0-0.jpg"
		restURL = "https://cdn-images.dzcdn.net/images/cover/" + hash + "/250x250-000000-80-0-0.jpg"
		epURL   = "https://cdn-images.dzcdn.net/images/talk/" + hash + "/250x250-000000-80-0-0.jpg"
		bigURL  = "https://e-cdns-images.dzcdn.net/images/cover/" + hash + "/1000x1000-000000-80-0-0.jpg"
		other   = "https://img.example/cover.jpg"
	)
	s := testStore(t)
	want := map[string]string{"gw": gwURL, "rest": restURL, "ep": epURL, "big": bigURL, "other": other, "none": ""}
	for _, id := range []string{"gw", "rest", "ep", "big", "other", "none"} {
		e := entry(id, "T", "A", 100, 1)
		e.ArtworkURL = want[id]
		if id == "ep" {
			e.Kind = KindEpisode
		}
		if err := s.Record(e); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.OpenFile(s.Path(), os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"trackId":"old","title":"T","artist":"A","startedAt":100,"durationPlayedSec":1,"artworkUrl":"` + gwURL + `"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	want["old"] = gwURL

	b, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	for i, sub := range []string{
		`"cover":"cover/` + hash + `"}`,
		`"cover":"cover/` + hash + `","cdn":"cdn-images"}`,
		`"cover":"talk/` + hash + `","cdn":"cdn-images"}`,
	} {
		if !strings.Contains(lines[i], sub) || strings.Contains(lines[i], "artworkUrl") {
			t.Errorf("line %d = %s, want %s and no artworkUrl", i, lines[i], sub)
		}
	}
	if !strings.Contains(lines[3], bigURL) || !strings.Contains(lines[4], other) {
		t.Errorf("other URLs not kept as is:\n%s\n%s", lines[3], lines[4])
	}

	check := func(name string, got []Entry) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: %d entries, want %d", name, len(got), len(want))
		}
		for _, e := range got {
			if e.ArtworkURL != want[e.TrackID] {
				t.Errorf("%s: %s artworkUrl = %q, want %q", name, e.TrackID, e.ArtworkURL, want[e.TrackID])
			}
		}
	}
	all, err := New(s.Path()).Recent(0)
	if err != nil {
		t.Fatal(err)
	}
	check("file", all)
	recent, err := s.Recent(10)
	if err != nil {
		t.Fatal(err)
	}
	check("memory", recent)
	// Record adds to the loaded window.
	if err := s.Record(Entry{TrackID: "new", ArtworkURL: epURL, Kind: KindEpisode, StartedAt: 101}); err != nil {
		t.Fatal(err)
	}
	want["new"] = epURL
	recent, err = s.Recent(10)
	if err != nil {
		t.Fatal(err)
	}
	check("recorded", recent)
}

// A play appends without loading a long history, and a query keeps only the
// newest entries in memory.
func TestLongHistoryNotHeldInMemory(t *testing.T) {
	s := testStore(t)
	var b strings.Builder
	for i := 0; i < DefaultCap; i++ {
		fmt.Fprintf(&b, `{"trackId":"%d","title":"Title %d","artist":"Artist %d","album":"Album","startedAt":%d,"durationPlayedSec":200,"artworkUrl":"https://e-cdns-images.dzcdn.net/images/cover/0123456789abcdef0123456789abcdef/250x250-000000-80-0-0.jpg","durationMs":200000}`+"\n", i, i, i%300, 1000+i)
	}
	if err := os.WriteFile(s.Path(), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	b = strings.Builder{}
	retained := func() float64 {
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		return float64(ms.HeapAlloc) / 1e6
	}
	base := retained()
	if err := s.Record(entry("new", "New", "A", 999999, 1)); err != nil {
		t.Fatal(err)
	}
	afterRecord := retained() - base
	got, err := s.Recent(3)
	if err != nil || len(got) != 3 || got[0].TrackID != "new" || got[1].TrackID != fmt.Sprint(DefaultCap-1) {
		t.Fatalf("Recent(3) = %+v err %v", got, err)
	}
	afterQuery := retained() - base
	t.Logf("retained after Record %.2f MB, after Recent %.2f MB", afterRecord, afterQuery)
	if afterRecord > 1 || afterQuery > 1 {
		t.Fatalf("history held in memory: %.2f MB after Record, %.2f MB after Recent", afterRecord, afterQuery)
	}
	runtime.KeepAlive(s)
}

func TestConcurrentWriterAndReaders(t *testing.T) {
	s := testStore(t)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	// Concurrent readers.
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, _ = s.Recent(5)
				_, _ = s.TopTracks(time.Time{}, 3)
				_, _ = s.TopArtists(time.Time{}, 3)
				_, _ = s.TotalListenedSec(time.Time{})
			}
		}()
	}
	// One writer.
	for i := 0; i < 200; i++ {
		if err := s.Record(entry(fmt.Sprint(i%7), "T", "A", int64(i+1), 1)); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	got, err := s.Recent(0)
	if err != nil || len(got) != 200 {
		t.Fatalf("got %d entries err %v, want 200", len(got), err)
	}
}
