package mediacache

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
)

// pin pins key with pins, keeping every other entry's pins.
func pin(c *Cache, key string, pins ...string) {
	c.mu.Lock()
	all := map[string][]string{}
	for k, e := range c.entries {
		if len(e.pins) > 0 {
			all[k] = e.pins
		}
	}
	c.mu.Unlock()
	if len(pins) == 0 {
		delete(all, key)
	} else {
		all[key] = pins
	}
	c.SetPins(all, nil)
}

// Eviction never removes a pinned entry, also when the pinned bytes alone
// exceed the limit; pinned bytes do not count against it and are reported.
func TestEvictionNeverRemovesPinnedEntry(t *testing.T) {
	c, _ := New(t.TempDir(), 250)
	defer c.Close()
	putBody(t, c, "1.MP3", 100)
	pin(c, "1.MP3", "album:1@u")
	for _, k := range []string{"2.MP3", "3.MP3", "4.MP3"} {
		putBody(t, c, k, 100)
	}
	if !hasEntry(c, "1.MP3") {
		t.Fatal("the pinned entry was evicted")
	}
	if n, b := c.Stats(); n != 3 || b != 300 || c.PinnedBytes() != 100 {
		t.Fatalf("stats %d entries %d bytes %d pinned, want 3 / 300 / 100", n, b, c.PinnedBytes())
	}

	// Pinned bytes above the limit: nothing pinned goes, and unpinned entries
	// still fit beside them.
	pin(c, "4.MP3", "track:4@u")
	c.SetMax(50)
	putBody(t, c, "5.MP3", 10)
	for _, k := range []string{"1.MP3", "4.MP3", "5.MP3"} {
		if !hasEntry(c, k) {
			t.Fatalf("%s gone with 200 pinned bytes over a 50-byte limit", k)
		}
	}
	if c.PinnedBytes() != 200 {
		t.Fatalf("pinned bytes %d, want 200", c.PinnedBytes())
	}
}

// A limit of 0 removes every unpinned entry and keeps the pinned ones
// readable.
func TestLimitZeroKeepsPinnedEntriesReadable(t *testing.T) {
	c, _ := New(t.TempDir(), 1<<20)
	defer c.Close()
	putBody(t, c, "1.MP3", 100)
	putBody(t, c, "2.MP3", 100)
	pin(c, "1.MP3", "track:1@u")
	c.SetMax(0)
	if hasEntry(c, "2.MP3") {
		t.Fatal("an unpinned entry survived a limit of 0")
	}
	f, size, ok := c.Get("1.MP3")
	if !ok || size != 100 {
		t.Fatalf("pinned entry at a limit of 0: ok %v size %d", ok, size)
	}
	_ = f.Close()
}

// The age limit never removes a pinned entry.
func TestExpiryNeverRemovesPinnedEntry(t *testing.T) {
	c, _ := ageCache(t)
	pin(c, "old.MP3", "playlist:9@u")
	c.SetMaxAge(30 * 24 * time.Hour)
	if !hasEntry(c, "old.MP3") {
		t.Fatal("the age limit removed a pinned entry")
	}
}

// Clear(includePinned=0) keeps the pinned files and their accounting.
func TestClearUnpinnedKeepsPinnedFilesAndAccounting(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir, 1<<20)
	putBody(t, c, "1.MP3", 100)
	putBody(t, c, "2.MP3", 50)
	pin(c, "1.MP3", "album:1@u")
	if err := c.ClearUnpinned(); err != nil {
		t.Fatal(err)
	}
	if n, b := c.Stats(); n != 1 || b != 100 || c.PinnedBytes() != 100 {
		t.Fatalf("after the clear %d entries %d bytes %d pinned, want 1 / 100 / 100", n, b, c.PinnedBytes())
	}
	c.Close()
	c, _ = New(dir, 1<<20)
	defer c.Close()
	if !hasEntry(c, "1.MP3") || c.PinnedBytes() != 100 {
		t.Fatal("the pinned entry did not survive the clear on disk")
	}
	if hasEntry(c, "2.MP3") {
		t.Fatal("the unpinned entry survived the clear")
	}
}

// Unpinning the last pin makes the entry evictable; another pin keeps it.
func TestUnpinningLastPinMakesEntryEvictable(t *testing.T) {
	c, _ := New(t.TempDir(), 1<<20)
	defer c.Close()
	putBody(t, c, "1.MP3", 100)
	pin(c, "1.MP3", "album:1@u", "track:1@u")
	c.SetMax(0)
	pin(c, "1.MP3", "album:1@u")
	if !hasEntry(c, "1.MP3") {
		t.Fatal("the entry went while a pin was left")
	}
	pin(c, "1.MP3")
	if hasEntry(c, "1.MP3") || c.PinnedBytes() != 0 {
		t.Fatal("the entry stayed past a limit of 0 with no pin left")
	}
}

// Pins are kept in index.json.
func TestPinsKeptInIndex(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir, 1<<20)
	putBody(t, c, "1.MP3", 100)
	pin(c, "1.MP3", "album:1@u")
	c.Close()
	c, _ = New(dir, 0)
	defer c.Close()
	if !hasEntry(c, "1.MP3") || c.PinnedBytes() != 100 {
		t.Fatal("the pin did not survive a reopen")
	}
}

// A body fetched for a pin is adopted pinned, also at a limit of 0; an
// unpinned body is not adopted at 0 (it would be evicted at once) and the
// caller keeps its file.
func TestAdoptPinnedAtLimitZero(t *testing.T) {
	c, _ := New(t.TempDir(), 0)
	defer c.Close()
	write := func() string {
		f, err := c.NewTemp()
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write(make([]byte, 64))
		_ = f.Close()
		return f.Name()
	}
	if err := c.AdoptPinned("1.MP3", write(), 64, StreamMeta{Format: "MP3"}, []string{"track:1@u"}); err != nil {
		t.Fatal(err)
	}
	if !hasEntry(c, "1.MP3") || c.PinnedBytes() != 64 {
		t.Fatal("the pinned body was not kept")
	}
	path := write()
	if err := c.Adopt("2.MP3", path, 64, StreamMeta{Format: "MP3"}); err == nil {
		t.Fatal("an unpinned body was adopted at a limit of 0")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the refused body's file: %v", err)
	}
}

// A body that replaces a pinned entry of the same key keeps its pins.
func TestReplacingPinnedEntryKeepsPins(t *testing.T) {
	c, _ := New(t.TempDir(), 1<<20)
	defer c.Close()
	putBody(t, c, "1.MP3", 100)
	pin(c, "1.MP3", "album:1@u")
	putBody(t, c, "1.MP3", 80)
	c.SetMax(0)
	if !hasEntry(c, "1.MP3") || c.PinnedBytes() != 80 {
		t.Fatal("the replaced entry lost its pin")
	}
}

// SetPins fills the stream meta of an entry whose index lost it.
func TestSetPinsFillsMissingMeta(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir, 1<<20)
	putBody(t, c, "1.MP3_128", 100)
	c.Close()
	if err := os.Remove(filepath.Join(dir, "index.json")); err != nil {
		t.Fatal(err)
	}
	c, _ = New(dir, 1<<20)
	defer c.Close()
	meta := StreamMeta{Format: "MP3_128", Encrypted: true, GainDB: -4}
	c.SetPins(map[string][]string{"1.MP3_128": {"track:1@u"}}, map[string]StreamMeta{"1.MP3_128": meta})
	if m, ok := c.GetMetaForTrack("1"); !ok || m != meta || c.PinnedBytes() != 100 {
		t.Fatalf("meta %+v %v pinned %d after SetPins", m, ok, c.PinnedBytes())
	}
}

// hostProfile points the config at a fresh folder with a 1 MB cache.
func hostProfile(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	config.SetBaseDir(base)
	config.SetCacheBaseDir(base)
	t.Cleanup(func() {
		config.SetBaseDir("")
		config.SetCacheBaseDir("")
	})
	if err := config.SaveMedia(config.Media{MediaCacheMB: 1}); err != nil {
		t.Fatal(err)
	}
	return base
}

// Changing the cache folder copies the pinned entries, with their pins and
// meta, switches, then deletes the old ones; unpinned entries are not copied.
func TestSetDirCopiesPinnedEntries(t *testing.T) {
	base := hostProfile(t)
	h := &Host{}
	if err := h.Open(); err != nil {
		t.Fatal(err)
	}
	old := h.Current()
	putBody(t, old, "1.MP3_128", 100)
	putBody(t, old, "2.MP3_128", 50)
	meta := StreamMeta{Format: "MP3_128", Encrypted: true, GainDB: -2}
	old.SetPins(map[string][]string{"1.MP3_128": {"album:1@u"}}, map[string]StreamMeta{"1.MP3_128": meta})
	next := filepath.Join(base, "next")
	if err := h.SetDir(next); err != nil {
		t.Fatal(err)
	}
	c := h.Current()
	defer c.Close()
	if c == old || !hasEntry(c, "1.MP3_128") || hasEntry(c, "2.MP3_128") || c.PinnedBytes() != 100 {
		t.Fatal("the new folder does not hold exactly the pinned entry")
	}
	if m, ok := c.GetMeta("1.MP3_128"); !ok || m != meta {
		t.Fatalf("copied meta %+v %v", m, ok)
	}
	f, size, ok := c.Get("1.MP3_128")
	if !ok || size != 100 {
		t.Fatal("the copied entry does not read")
	}
	_ = f.Close()
	if left := dataNames(t, filepath.Join(base, "opendeezer-cache")); len(left) != 0 {
		t.Fatalf("old folder still holds %v", left)
	}
}

// When the copy fails the old folder stays in use, its pinned entry intact,
// and the error is returned.
func TestSetDirCopyFailureKeepsOldFolder(t *testing.T) {
	base := hostProfile(t)
	h := &Host{}
	if err := h.Open(); err != nil {
		t.Fatal(err)
	}
	old := h.Current()
	defer old.Close()
	putBody(t, old, "1.MP3_128", 100)
	pin(old, "1.MP3_128", "album:1@u")
	// The pinned file cannot be read: its data file is replaced by a folder.
	old.mu.Lock()
	p := old.entries["1.MP3_128"].path
	old.mu.Unlock()
	saved, _ := os.ReadFile(p)
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(p, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := h.SetDir(filepath.Join(base, "next")); err == nil {
		t.Fatal("SetDir succeeded without copying the pinned entry")
	}
	_ = os.Remove(p)
	_ = os.WriteFile(p, saved, 0o600)
	if h.Current() != old || !hasEntry(old, "1.MP3_128") {
		t.Fatal("the old folder is no longer in use")
	}
	if st := h.Stats(); st.Dir != base {
		t.Fatalf("stats dir %s after a failed move, want %s", st.Dir, base)
	}
}

// A pinned entry whose file is gone when the copy reaches it (an unpin
// removed it after the copy listed it) is skipped: the move succeeds with the
// other pinned entries.
func TestSetDirSkipsRemovedSource(t *testing.T) {
	base := hostProfile(t)
	h := &Host{}
	if err := h.Open(); err != nil {
		t.Fatal(err)
	}
	old := h.Current()
	putBody(t, old, "1.MP3_128", 100)
	putBody(t, old, "2.MP3_128", 50)
	pin(old, "1.MP3_128", "album:1@u")
	pin(old, "2.MP3_128", "album:1@u")
	old.mu.Lock()
	p := old.entries["2.MP3_128"].path
	old.mu.Unlock()
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := h.SetDir(filepath.Join(base, "next")); err != nil {
		t.Fatalf("SetDir: %v", err)
	}
	c := h.Current()
	defer c.Close()
	if c == old || !hasEntry(c, "1.MP3_128") || hasEntry(c, "2.MP3_128") {
		t.Fatal("the new folder does not hold exactly the entry still on disk")
	}
}

// The host keeps a cache open while something is pinned, also at a size of
// 0, and reports the pinned bytes.
func TestHostKeepsPinnedCacheOpenAtZero(t *testing.T) {
	hostProfile(t)
	pinned := true
	h := &Host{Pinned: func() bool { return pinned }}
	if err := h.Open(); err != nil {
		t.Fatal(err)
	}
	c := h.Current()
	putBody(t, c, "1.MP3_128", 100)
	putBody(t, c, "2.MP3_128", 100)
	pin(c, "1.MP3_128", "track:1@u")
	if err := h.SetMB(0); err != nil {
		t.Fatal(err)
	}
	if h.Current() != c || !hasEntry(c, "1.MP3_128") || hasEntry(c, "2.MP3_128") {
		t.Fatal("a size of 0 closed the cache or dropped the pinned entry")
	}
	if st := h.Stats(); st.PinnedBytes != 100 || st.Bytes != 100 || st.MaxBytes != 0 {
		t.Fatalf("stats %+v", st)
	}
	if err := h.Clear(false); err != nil || !hasEntry(c, "1.MP3_128") {
		t.Fatalf("Clear(false): %v, pinned entry kept %v", err, hasEntry(c, "1.MP3_128"))
	}
	pinned = false
	if err := h.Clear(true); err != nil {
		t.Fatal(err)
	}
	if err := h.Open(); err != nil {
		t.Fatal(err)
	}
	if h.Current() != nil {
		t.Fatal("the cache stayed open at a size of 0 with nothing pinned")
	}
}

// A pinned entry whose index.json is lost keeps its file and its pin when the
// host opens with a maximum age it is older than: the pins come before the age.
func TestHostOpenLostIndexPinnedOverMaxAge(t *testing.T) {
	base := hostProfile(t)
	if err := config.SaveMedia(config.Media{MediaCacheMB: 1, MediaCacheMaxAgeDays: 7}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "opendeezer-cache")
	c, _ := New(dir, 1<<20)
	putBody(t, c, "1.MP3_128", 100)
	c.mu.Lock()
	p := c.entries["1.MP3_128"].path
	c.mu.Unlock()
	c.Close()
	if err := os.Remove(filepath.Join(dir, "index.json")); err != nil {
		t.Fatal(err)
	}
	then := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(p, then, then); err != nil {
		t.Fatal(err)
	}
	h := &Host{
		Attach: func(c *Cache) {
			if c != nil {
				c.SetPins(map[string][]string{"1.MP3_128": {"track:1@u"}}, nil)
			}
		},
		Pinned: func() bool { return true },
	}
	if err := h.Open(); err != nil {
		t.Fatal(err)
	}
	defer h.Current().Close()
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("the pinned file: %v", err)
	}
	if !h.Current().Pinned("1.MP3_128") {
		t.Fatal("the entry lost its pin")
	}
}

// dataNames lists the data files in dir.
func dataNames(t *testing.T, dir string) []string {
	t.Helper()
	des, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	var out []string
	for _, de := range des {
		if _, _, _, ok := parseFileName(de.Name()); ok {
			out = append(out, de.Name())
		}
	}
	return out
}
