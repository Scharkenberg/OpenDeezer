package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/mediacache"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/mediacache/mctest"
)

// cacheProfile points the config and cache directories at a fresh folder for
// the test.
func cacheProfile(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	config.SetBaseDir(base)
	config.SetCacheBaseDir(base)
	t.Cleanup(func() {
		config.SetBaseDir("")
		config.SetCacheBaseDir("")
		resetOfflineGlobals()
	})
	resetOfflineGlobals()
	return base
}

// dataFiles lists the cache's data files in dir (index.json and lock aside).
func dataFiles(t *testing.T, dir string) []string {
	t.Helper()
	des, _ := os.ReadDir(dir)
	var out []string
	for _, de := range des {
		if de.Name() != "index.json" && de.Name() != "lock" {
			out = append(out, de.Name())
		}
	}
	return out
}

// What Init runs for a budget in media.json opens the cache.
func TestInitCacheFromMedia(t *testing.T) {
	cacheProfile(t)
	if err := config.SaveMedia(config.Media{MediaCacheMB: 1}); err != nil {
		t.Fatal(err)
	}
	if err := caches.Open(); err != nil {
		t.Fatal(err)
	}
	if caches.Current() == nil {
		t.Fatal("no cache opened from media.json")
	}
}

// The budget applies at once: a cache opens for a budget, and 0 removes its
// tracks and detaches it.
func TestMediaCacheBudgetAppliesAtOnce(t *testing.T) {
	base := cacheProfile(t)
	if caches.SetMB(1) != nil {
		t.Fatal("SetMB(1) failed")
	}
	mc := caches.Current()
	if mc == nil {
		t.Fatal("no cache after a budget was set")
	}
	mctest.PutBody(t, mc, "1.MP3_128")
	st := caches.Stats()
	want := mediacache.Stats{Entries: 1, Bytes: int64(len("ciphertext")), MaxBytes: 1 << 20, Dir: base}
	if st != want {
		t.Fatalf("stats = %+v, want %+v", st, want)
	}
	if caches.SetMB(0) != nil {
		t.Fatal("SetMB(0) failed")
	}
	if caches.Current() != nil {
		t.Fatal("a cache is still attached with a budget of 0")
	}
	if f := dataFiles(t, filepath.Join(base, "opendeezer-cache")); len(f) != 0 {
		t.Fatalf("files left with a budget of 0: %v", f)
	}
	if st := caches.Stats(); st.Entries != 0 || st.Bytes != 0 || st.MaxBytes != 0 || st.Dir != want.Dir {
		t.Fatalf("stats while off = %+v", st)
	}
}

// A budget of 0 also removes the tracks an earlier session left in the
// folder when no cache was opened in this one.
func TestMediaCacheZeroPurgesUnopenedFolder(t *testing.T) {
	base := cacheProfile(t)
	dir := filepath.Join(base, "opendeezer-cache")
	earlier, err := mediacache.New(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	mctest.PutBody(t, earlier, "1.MP3_128")
	earlier.Close()
	if caches.Current() != nil {
		t.Fatal("a cache is open before the test")
	}
	if caches.SetMB(0) != nil {
		t.Fatal("SetMB(0) failed")
	}
	if f := dataFiles(t, dir); len(f) != 0 {
		t.Fatalf("files left in the folder: %v", f)
	}
	if caches.Current() != nil {
		t.Fatal("a cache is attached with a budget of 0")
	}
}

// oldTrackInFolder leaves in dir a cache holding "old.MP3_128", last used 40 days ago,
// and "new.MP3_128", just used. Its index is gone, so the last use comes from the
// files' mtimes.
func oldTrackInFolder(t *testing.T, dir string) {
	t.Helper()
	earlier, err := mediacache.New(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	mctest.PutBody(t, earlier, "old.MP3_128")
	old := dataFiles(t, dir)
	mctest.PutBody(t, earlier, "new.MP3_128")
	earlier.Close()
	if err := os.Remove(filepath.Join(dir, "index.json")); err != nil {
		t.Fatal(err)
	}
	then := time.Now().Add(-40 * 24 * time.Hour)
	for _, f := range old {
		if err := os.Chtimes(filepath.Join(dir, f), then, then); err != nil {
			t.Fatal(err)
		}
	}
}

// The age limit in media.json is applied when the cache opens.
func TestMediaCacheMaxAgeReadAtOpen(t *testing.T) {
	base := cacheProfile(t)
	oldTrackInFolder(t, filepath.Join(base, "opendeezer-cache"))
	if err := config.SaveMedia(config.Media{MediaCacheMaxAgeDays: 30}); err != nil {
		t.Fatal(err)
	}
	if got := config.LoadMedia().MediaCacheMaxAgeDays; got != 30 {
		t.Fatalf("MediaCacheMaxAgeDays = %d, want 30", got)
	}
	_ = caches.SetMB(1)
	if st := caches.Stats(); st.Entries != 1 {
		t.Fatalf("entries after opening with a 30-day limit = %d, want the recent one", st.Entries)
	}
}

// Setting the age limit applies it at once; 0 keeps every track.
func TestSetMediaCacheMaxAgeAppliesAtOnce(t *testing.T) {
	base := cacheProfile(t)
	oldTrackInFolder(t, filepath.Join(base, "opendeezer-cache"))
	_ = caches.SetMB(1)
	if caches.SetMaxAgeDays(0) != nil || caches.Stats().Entries != 2 {
		t.Fatal("an age limit of 0 removed tracks")
	}
	if caches.SetMaxAgeDays(30) != nil {
		t.Fatal("SetMaxAgeDays failed")
	}
	if st := caches.Stats(); st.Entries != 1 {
		t.Fatalf("entries after setting a 30-day limit = %d, want 1", st.Entries)
	}
	if got := config.LoadMedia().MediaCacheMaxAgeDays; got != 30 {
		t.Fatalf("persisted age %d, want 30", got)
	}
}

// A clear removes every cached track and keeps the cache on.
func TestClearMediaCache(t *testing.T) {
	cacheProfile(t)
	_ = caches.SetMB(1)
	mctest.PutBody(t, caches.Current(), "1.MP3_128")
	if caches.Clear(true) != nil {
		t.Fatal("Clear failed")
	}
	if st := caches.Stats(); st.Entries != 0 || st.Bytes != 0 || st.MaxBytes != 1<<20 {
		t.Fatalf("stats after a clear = %+v", st)
	}
}

// A new cache folder takes over empty, in its subfolder opendeezer-cache; the
// old cache is cleared. A relative path or a file is refused and the old
// folder stays in use.
func TestSetMediaCacheDir(t *testing.T) {
	base := cacheProfile(t)
	_ = caches.SetMB(1)
	mctest.PutBody(t, caches.Current(), "1.MP3_128")

	file := filepath.Join(base, "file")
	_ = os.WriteFile(file, []byte("x"), 0o600)
	for _, dir := range []string{file, "relative"} {
		if err := caches.SetDir(dir); err == nil {
			t.Fatalf("caches.SetDir(%q) accepted", dir)
		}
	}
	if st := caches.Stats(); st.Dir != base || st.Entries != 1 {
		t.Fatalf("after refusals stats = %+v, want the old folder with its track", st)
	}

	next := filepath.Join(base, "next")
	if err := caches.SetDir(next); err != nil {
		t.Fatal(err)
	}
	if st := caches.Stats(); st.Dir != next || st.Entries != 0 || st.MaxBytes != 1<<20 {
		t.Fatalf("after the move stats = %+v", st)
	}
	if f := dataFiles(t, filepath.Join(base, "opendeezer-cache")); len(f) != 0 {
		t.Fatalf("old cache not cleared: %v", f)
	}
	mctest.PutBody(t, caches.Current(), "2.MP3_128")
	if f := dataFiles(t, filepath.Join(next, "opendeezer-cache")); len(f) != 1 {
		t.Fatalf("new cache holds %v, want the new track", f)
	}
	if err := caches.SetDir(""); err != nil {
		t.Fatal(err)
	}
	if st := caches.Stats(); st.Dir != base {
		t.Fatalf("after the reset stats = %+v", st)
	}
}

// A folder of the user's own files can be picked: the cache goes in its
// subfolder, and every file of the user's survives the cache filling,
// clearing, a budget of 0 and a switch away.
func TestMediaCacheDirKeepsForeignFiles(t *testing.T) {
	base := cacheProfile(t)
	music := filepath.Join(base, "Music")
	foreign := []string{"a.mp3", "x.tmp", "index.json", filepath.Join("sub", "b.mp3")}
	_ = os.MkdirAll(filepath.Join(music, "sub"), 0o700)
	for _, f := range foreign {
		_ = os.WriteFile(filepath.Join(music, f), []byte(`{"mine":true}`), 0o600)
	}
	check := func(step string) {
		t.Helper()
		for _, f := range foreign {
			if _, err := os.Stat(filepath.Join(music, f)); err != nil {
				t.Fatalf("%s after %s: %v", f, step, err)
			}
		}
	}
	_ = caches.SetMB(1)
	if err := caches.SetDir(music); err != nil {
		t.Fatal(err)
	}
	if st := caches.Stats(); st.Dir != music {
		t.Fatalf("stats dir = %s, want the picked folder %s", st.Dir, music)
	}
	mctest.PutBody(t, caches.Current(), "1.MP3_128")
	if f := dataFiles(t, filepath.Join(music, "opendeezer-cache")); len(f) != 1 {
		t.Fatalf("cache subfolder holds %v, want the track", f)
	}
	_ = caches.Clear(true)
	check("a clear")
	mctest.PutBody(t, caches.Current(), "2.MP3_128")
	_ = caches.SetMB(0)
	check("a budget of 0")
	_ = caches.SetMB(1)
	mctest.PutBody(t, caches.Current(), "3.MP3_128")
	if err := caches.SetDir(""); err != nil {
		t.Fatal(err)
	}
	check("a switch away")
}
