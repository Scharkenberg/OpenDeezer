package mediacache

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
)

func TestPutGetRoundtrip(t *testing.T) {
	dir := t.TempDir()
	c, err := New(dir, 10<<20)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("hello world this is some test payload for the cache 1234567890")
	wc, err := c.Put("test1.MP3_320")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wc.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := wc.Close(); err != nil {
		t.Fatal(err)
	}

	rc, sz, hit := c.Get("test1.MP3_320")
	if !hit {
		t.Fatal("expected hit")
	}
	if sz != int64(len(data)) {
		t.Fatalf("size=%d want %d", sz, len(data))
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("roundtrip data mismatch")
	}

	// stats
	n, b := c.Stats()
	if n != 1 || b != int64(len(data)) {
		t.Fatalf("stats=%d,%d", n, b)
	}
}

func TestAtomicVisibility(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir, 10<<20)

	wc, _ := c.Put("partial.MP3_128")
	_, _ = wc.Write([]byte("incomplete data so far"))
	// do NOT close

	if _, _, hit := c.Get("partial.MP3_128"); hit {
		t.Fatal("partial write must not be visible")
	}

	// now finish
	_, _ = wc.Write([]byte(" more"))
	if err := wc.Close(); err != nil {
		t.Fatal(err)
	}

	rc, _, hit := c.Get("partial.MP3_128")
	if !hit {
		t.Fatal("should be visible after close")
	}
	defer rc.Close()
	all, _ := io.ReadAll(rc)
	if !bytes.Contains(all, []byte("incomplete")) {
		t.Fatal("content wrong after commit")
	}
}

func TestLRUEviction(t *testing.T) {
	dir := t.TempDir()
	// limit = 90; three 30-byte entries = 90 (border), fourth will evict
	c, _ := New(dir, 90)

	payload := bytes.Repeat([]byte("x"), 30)
	for _, k := range []string{"A", "B", "C"} {
		wc, _ := c.Put(k + ".MP3")
		_, _ = wc.Write(payload)
		_ = wc.Close()
	}

	// touch A (bump recency)
	if rc, _, hit := c.Get("A.MP3"); hit {
		rc.Close()
	}

	// add D (30) -> temp total 120 >90 , evict oldest (B)
	wc, _ := c.Put("D.MP3")
	_, _ = wc.Write(payload)
	_ = wc.Close()

	n, _ := c.Stats()
	if n > 3 {
		t.Fatalf("too many entries: %d", n)
	}
	if rc, _, hit := c.Get("B.MP3"); hit {
		rc.Close()
		t.Error("B should have been evicted (oldest)")
	}
	// A (touched), C, D should remain. Close every reader so t.TempDir cleanup
	// does not trip over an open handle on Windows.
	for _, k := range []string{"A.MP3", "C.MP3", "D.MP3"} {
		rc, _, hit := c.Get(k)
		if !hit {
			t.Errorf("%s should still be present", k)
			continue
		}
		rc.Close()
	}
}

// Without index.json, recovery checks each file against the size in its name:
// a truncated file is dropped, an intact one kept, and a file with an older
// name (no size) is kept as it is.
func TestRecoverWithoutIndexChecksSizeInName(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir, 10<<20)
	for _, k := range []string{"intact.MP3", "cut.MP3"} {
		wc, _ := c.Put(k)
		_, _ = wc.Write(bytes.Repeat([]byte("x"), 1000))
		if err := wc.Close(); err != nil {
			t.Fatal(err)
		}
	}
	old := encodeKey("old.FLAC") + "-ff" // committed before sizes were in names
	if err := os.WriteFile(filepath.Join(dir, old), []byte("old body"), 0o644); err != nil {
		t.Fatal(err)
	}
	des, _ := os.ReadDir(dir)
	for _, de := range des {
		key, _, size, ok := parseFileName(de.Name())
		if ok && key == "cut.MP3" {
			if size != 1000 {
				t.Fatalf("size in the name of %s = %d, want 1000", de.Name(), size)
			}
			if err := os.Truncate(filepath.Join(dir, de.Name()), 400); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.Remove(filepath.Join(dir, "index.json")); err != nil {
		t.Fatal(err)
	}

	c.Close()
	c2, _ := New(dir, 10<<20)
	for key, want := range map[string]int64{"intact.MP3": 1000, "old.FLAC": 8} {
		rc, size, hit := c2.Get(key)
		if !hit || size != want {
			t.Fatalf("%s after recovery: hit %v size %d, want %d", key, hit, size, want)
		}
		rc.Close()
	}
	if rc, _, hit := c2.Get("cut.MP3"); hit {
		rc.Close()
		t.Fatal("the truncated file survived recovery")
	}
	if n, b := c2.Stats(); n != 2 || b != 1008 {
		t.Fatalf("Stats = %d / %d, want 2 / 1008", n, b)
	}
	des, _ = os.ReadDir(dir)
	for _, de := range des {
		if key, _, _, ok := parseFileName(de.Name()); ok && key == "cut.MP3" {
			t.Fatalf("the truncated file %s is still on disk", de.Name())
		}
	}
}

func TestRecoverDropsPartialAndCorrupt(t *testing.T) {
	dir := t.TempDir()

	// good: a valid file with a matching index entry (unique-filename scheme).
	goodFile := encodeKey("good.FLAC") + "-1"
	if err := os.WriteFile(filepath.Join(dir, goodFile), []byte("gooddata"), 0o644); err != nil {
		t.Fatal(err)
	}
	// bad: a file whose recorded index size does not match its on-disk size.
	badFile := encodeKey("bad.MP3") + "-2"
	if err := os.WriteFile(filepath.Join(dir, badFile), []byte("short"), 0o644); err != nil {
		t.Fatal(err)
	}
	// zero: a zero-length data file that no index entry references (orphan).
	zeroFile := encodeKey("zero.MP3") + "-3"
	if err := os.WriteFile(filepath.Join(dir, zeroFile), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	idx := indexData{
		Entries: map[string]indexEntry{
			"good.FLAC": {File: goodFile, Size: 8, AccessSeq: 2},
			"bad.MP3":   {File: badFile, Size: 999, AccessSeq: 1}, // size mismatch
		},
		NextSeq: 3,
		FileSeq: 3,
	}
	b, _ := json.Marshal(idx)
	if err := os.WriteFile(filepath.Join(dir, "index.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	// reopen -> should drop zero and bad (size mismatch), keep good.
	c2, _ := New(dir, 10<<20)
	n, _ := c2.Stats()
	if n != 1 {
		t.Fatalf("after recovery got %d entries, want 1 (only good)", n)
	}
	if rc, _, hit := c2.Get("bad.MP3"); hit {
		rc.Close()
		t.Error("bad should have been dropped (size mismatch)")
	}
	if rc, _, hit := c2.Get("zero.MP3"); hit {
		rc.Close()
		t.Error("zero should have been dropped")
	}
	rc, _, hit := c2.Get("good.FLAC")
	if !hit {
		t.Fatal("good should survive recovery")
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, []byte("gooddata")) {
		t.Fatalf("good bytes wrong after recovery: %q", got)
	}
	// The unreferenced/mismatched files must be reclaimed as orphans.
	for _, f := range []string{badFile, zeroFile} {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Errorf("orphan %s should have been reclaimed on recovery", f)
		}
	}
}

func TestConcurrentReadersAndWriter(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir, 1<<20)

	const key = "race.MP3_320"
	const payload = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

	var wg sync.WaitGroup
	start := make(chan struct{})

	// writer
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		wc, err := c.Put(key)
		if err != nil {
			return
		}
		for i := 0; i < 100; i++ {
			_, _ = wc.Write([]byte(payload))
		}
		_ = wc.Close()
	}()

	// many readers that also hammer Get
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 50; j++ {
				if rc, _, hit := c.Get(key); hit {
					_, _ = io.Copy(io.Discard, rc)
					rc.Close()
				}
				// also stats
				c.Stats()
			}
		}()
	}

	close(start)
	wg.Wait()

	// final check: if writer finished we should have data or partial (but test is racy ok)
	rc, sz, hit := c.Get(key)
	if hit {
		if sz == 0 {
			t.Error("got zero size entry")
		}
		rc.Close()
	}
}

// Adopt makes a complete NewTemp body an entry while its writer keeps the file
// open and reading; a body of another size than declared is refused.
func TestAdoptCommitAndRefuse(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir, 10<<20)

	good := []byte("good body data here")
	f, err := c.NewTemp()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, _ = f.Write(good)
	if err := c.Adopt("good.MP3", f.Name(), int64(len(good)), StreamMeta{}); err != nil {
		t.Fatalf("Adopt complete body: %v", err)
	}
	rc, sz, hit := c.Get("good.MP3")
	if !hit || sz != int64(len(good)) {
		t.Fatalf("adopted entry: hit %v size %d", hit, sz)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, good) {
		t.Fatalf("adopted entry = %q", got)
	}
	back := make([]byte, len(good)) // the writer's handle still reads the file
	if _, err := f.ReadAt(back, 0); err != nil || !bytes.Equal(back, good) {
		t.Fatalf("read through the writer's handle after Adopt: %v", err)
	}

	short, err := c.NewTemp()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = short.Write([]byte("partial"))
	if err := c.Adopt("short.MP3", short.Name(), 1000, StreamMeta{}); err == nil {
		t.Fatal("a short body was adopted")
	}
	_ = short.Close()
	if rc, _, hit := c.Get("short.MP3"); hit {
		rc.Close()
		t.Fatal("refused body became an entry")
	}
	if n, b := c.Stats(); n != 1 || b != int64(len(good)) {
		t.Fatalf("Stats = %d / %d, want 1 / %d", n, b, len(good))
	}
}

// Adopt stores the stream fields with the body, in the same index write, and
// they survive a restart.
func TestAdoptStoresMeta(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir, 1<<20)
	f, err := c.NewTemp()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte("body"))
	_ = f.Close()
	meta := StreamMeta{Format: "FLAC", Encrypted: true, GainDB: -2}
	if err := c.Adopt("9.FLAC", f.Name(), 4, meta); err != nil {
		t.Fatal(err)
	}
	if m, ok := c.GetMetaForTrack("9"); !ok || m != meta {
		t.Fatalf("meta after Adopt = %+v, %v", m, ok)
	}
	c.Close()
	c2, _ := New(dir, 1<<20)
	defer c2.Close()
	if m, ok := c2.GetMetaForTrack("9"); !ok || m != meta {
		t.Fatalf("meta after a restart = %+v, %v", m, ok)
	}
}

// An adopted entry can be evicted while its writer still has it open.
func TestEvictAdoptedWhileOpen(t *testing.T) {
	c, _ := New(t.TempDir(), 30)
	f, err := c.NewTemp()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, _ = f.Write(bytes.Repeat([]byte("a"), 20))
	if err := c.Adopt("a.MP3", f.Name(), 20, StreamMeta{}); err != nil {
		t.Fatal(err)
	}
	g, _ := c.NewTemp()
	_, _ = g.Write(bytes.Repeat([]byte("b"), 20))
	_ = g.Close()
	if err := c.Adopt("b.MP3", g.Name(), 20, StreamMeta{}); err != nil {
		t.Fatal(err)
	}
	if rc, _, hit := c.Get("a.MP3"); hit {
		rc.Close()
		t.Fatal("the open entry was not evicted")
	}
	if n, b := c.Stats(); n != 1 || b != 20 {
		t.Fatalf("Stats = %d / %d, want 1 / 20", n, b)
	}
}

// ageCache holds "old.MP3", last used 40 days ago, and "new.MP3", used now; the
// last use comes from the index when the cache is opened.
func ageCache(t *testing.T) (*Cache, string) {
	t.Helper()
	dir := t.TempDir()
	c, _ := New(dir, 1<<20)
	for _, k := range []string{"old.MP3", "new.MP3"} {
		wc, _ := c.Put(k)
		_, _ = wc.Write([]byte("body of " + k))
		if err := wc.Close(); err != nil {
			t.Fatal(err)
		}
	}
	c.mu.Lock()
	e := c.entries["old.MP3"]
	e.used = time.Now().Add(-40 * 24 * time.Hour)
	c.entries["old.MP3"] = e
	c.dirty = true
	c.mu.Unlock()
	c.Close()
	c2, _ := New(dir, 1<<20)
	return c2, e.path
}

// SetMaxAge removes at once the entries not used for longer than the age, keeps
// the recent ones, and 0 keeps everything.
func TestSetMaxAgeRemovesOldEntries(t *testing.T) {
	c, oldPath := ageCache(t)
	c.SetMaxAge(0)
	if n, _ := c.Stats(); n != 2 {
		t.Fatalf("max age 0 left %d entries, want 2", n)
	}
	c.SetMaxAge(30 * 24 * time.Hour)
	if rc, _, hit := c.Get("old.MP3"); hit {
		rc.Close()
		t.Fatal("an entry unused for 40 days survived a 30-day limit")
	}
	if rc, _, hit := c.Get("new.MP3"); !hit {
		t.Fatal("a recent entry was removed")
	} else {
		rc.Close()
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old entry's file: %v", err)
	}
}

// The last use is kept in the index, not in the file's mtime (which cannot be
// changed on Windows while the player has the file open): a track played now
// whose file's mtime is 40 days old survives a 30-day limit after a restart.
func TestLastUseSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir, 1<<20)
	putBody(t, c, "a.MP3", 10)
	rc, _, ok := c.Get("a.MP3")
	if !ok {
		t.Fatal("miss")
	}
	rc.Close()
	c.mu.Lock()
	p := c.entries["a.MP3"].path
	c.mu.Unlock()
	then := time.Now().Add(-40 * 24 * time.Hour)
	if err := os.Chtimes(p, then, then); err != nil {
		t.Fatal(err)
	}
	c.Close()
	c2, _ := New(dir, 1<<20)
	defer c2.Close()
	c2.SetMaxAge(30 * 24 * time.Hour)
	if !hasEntry(c2, "a.MP3") {
		t.Fatal("a track played now was removed as unused for 40 days")
	}
}

// While the cache runs, a commit or a recency flush removes old entries, at
// most once an hour.
func TestMaxAgeRunsWithCommitsHourly(t *testing.T) {
	c, _ := ageCache(t)
	c.mu.Lock()
	c.maxAge = 30 * 24 * time.Hour
	c.lastExpire = time.Now() // checked a moment ago
	c.mu.Unlock()
	putOne(t, c, "a.MP3")
	if n, _ := c.Stats(); n != 3 {
		t.Fatalf("a commit within the hour removed entries: %d left, want 3", n)
	}
	c.mu.Lock()
	c.lastExpire = time.Now().Add(-2 * time.Hour)
	c.mu.Unlock()
	putOne(t, c, "b.MP3")
	if rc, _, hit := c.Get("old.MP3"); hit {
		rc.Close()
		t.Fatal("a commit after the hour kept the old entry")
	}
	c.mu.Lock()
	c.lastExpire = time.Now().Add(-2 * time.Hour)
	e := c.entries["new.MP3"]
	e.used = time.Now().Add(-40 * 24 * time.Hour)
	c.entries["new.MP3"] = e
	c.mu.Unlock()
	c.flushRecency()
	if rc, _, hit := c.Get("new.MP3"); hit {
		rc.Close()
		t.Fatal("a recency flush after the hour kept an old entry")
	}
}

func putOne(t *testing.T, c *Cache, key string) {
	t.Helper()
	wc, _ := c.Put(key)
	_, _ = wc.Write([]byte("x"))
	if err := wc.Close(); err != nil {
		t.Fatal(err)
	}
}

// SetMax applies at once: a lower limit evicts the least recent entries, 0
// removes them all, and the index on disk follows.
func TestSetMaxEvictsAtOnce(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir, 100)
	for _, k := range []string{"a", "b", "c"} {
		wc, _ := c.Put(k + ".MP3")
		_, _ = wc.Write(bytes.Repeat([]byte(k), 30))
		_ = wc.Close()
	}
	c.SetMax(60)
	if n, b := c.Stats(); n != 2 || b != 60 {
		t.Fatalf("after SetMax(60): %d entries / %d bytes, want 2 / 60", n, b)
	}
	if rc, _, hit := c.Get("a.MP3"); hit {
		rc.Close()
		t.Fatal("the least recent entry survived a lower limit")
	}
	c.SetMax(0)
	if n, b := c.Stats(); n != 0 || b != 0 {
		t.Fatalf("after SetMax(0): %d entries / %d bytes, want 0 / 0", n, b)
	}
	c.Close()
	c2, _ := New(dir, 100)
	if n, _ := c2.Stats(); n != 0 {
		t.Fatalf("reopened after SetMax(0): %d entries", n)
	}
	des, _ := os.ReadDir(dir)
	for _, de := range des {
		if de.Name() != "index.json" && de.Name() != "lock" {
			t.Fatalf("file %s left after SetMax(0)", de.Name())
		}
	}
}

func TestClear(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir, 10<<20)

	wc, _ := c.Put("x.MP3")
	_, _ = wc.Write([]byte("abc"))
	_ = wc.Close()

	_ = c.Clear()
	n, b := c.Stats()
	if n != 0 || b != 0 {
		t.Errorf("clear stats %d %d", n, b)
	}
	// index should also be gone or empty
}

func TestNoEvictActiveWrite(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir, 50) // very small

	// start a long write for "active" (not yet committed)
	wc, _ := c.Put("active.MP3")
	_, _ = wc.Write(bytes.Repeat([]byte("A"), 30))

	// add others that would normally trigger eviction
	for i := 0; i < 5; i++ {
		w, _ := c.Put(string(rune('a'+i)) + ".MP3")
		_, _ = w.Write(bytes.Repeat([]byte("B"), 20))
		_ = w.Close()
	}

	// The active (in-progress) key must still be marked writing so eviction
	// never touches it while its Put is open.
	c.mu.Lock()
	_, busy := c.writing["active.MP3"]
	c.mu.Unlock()
	if !busy {
		t.Fatal("active write should still be protected (marked in-progress)")
	}

	// finish it
	if err := wc.Close(); err != nil {
		t.Fatal(err)
	}

	// It committed successfully despite the eviction pressure; as the most
	// recent entry it survives its own commit's eviction pass.
	rc, _, hit := c.Get("active.MP3")
	if !hit {
		t.Fatal("active entry should be present after commit")
	}
	rc.Close()
}

// A second body for an already-cached key that is abandoned (a stream that
// arrived short) must not destroy the previously-good entry: the old bytes stay
// visible until a replacement actually commits.
func TestAbandonedRewriteKeepsExistingEntry(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir, 10<<20)

	const key = "keep.MP3_320"
	orig := []byte("original good payload")
	wc, err := c.Put(key)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = wc.Write(orig)
	if err := wc.Close(); err != nil {
		t.Fatal(err)
	}

	// Second body for the same key that arrived short of its declared length.
	tmp, err := c.NewTemp()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = tmp.Write([]byte("replacement that never completes"))
	if err := c.Adopt(key, tmp.Name(), 1000, StreamMeta{}); err == nil {
		t.Fatal("a short body was adopted")
	}
	_ = tmp.Close()
	_ = os.Remove(tmp.Name())

	rc, sz, hit := c.Get(key)
	if !hit {
		t.Fatal("abandoned rewrite destroyed the existing entry")
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, orig) || sz != int64(len(orig)) {
		t.Fatalf("entry corrupted: got %q (size %d), want %q", got, sz, orig)
	}
	if n, b := c.Stats(); n != 1 || b != int64(len(orig)) {
		t.Fatalf("stats = %d entries, %d bytes; want 1, %d", n, b, len(orig))
	}

	// Third Put, closed without any writes (empty abandon) — same guarantee.
	wc3, _ := c.Put(key)
	_ = wc3.Close()
	if rc, _, hit := c.Get(key); !hit {
		t.Fatal("zero-write rewrite destroyed the existing entry")
	} else {
		rc.Close()
	}

	// A committed replacement swaps bytes and accounting atomically, onto a
	// brand-new on-disk file (never renaming over the previous one).
	repl := []byte("replacement payload, a bit longer than the original one")
	wc4, _ := c.Put(key)
	_, _ = wc4.Write(repl)
	if err := wc4.Close(); err != nil {
		t.Fatal(err)
	}
	rc, sz, hit = c.Get(key)
	if !hit {
		t.Fatal("committed replacement missing")
	}
	got, _ = io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, repl) || sz != int64(len(repl)) {
		t.Fatalf("replacement bytes wrong: got %q (size %d)", got, sz)
	}
	if n, b := c.Stats(); n != 1 || b != int64(len(repl)) {
		t.Fatalf("stats after replace = %d entries, %d bytes; want 1, %d", n, b, len(repl))
	}
}

// When a victim's file cannot be deleted (Windows sharing violation while a
// reader holds it open), eviction must keep the entry and its accounting —
// dropping only the bookkeeping would orphan the file on disk and let a
// restart's recover() blow past maxBytes. It skips that victim and goes on
// with the next one, and terminates rather than retry the same undeletable
// victim forever.
func TestEvictionKeepsEntryWhenRemoveFails(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir, 60)

	payload := bytes.Repeat([]byte("x"), 30)
	for _, k := range []string{"A", "B"} {
		wc, _ := c.Put(k + ".MP3")
		_, _ = wc.Write(payload)
		if err := wc.Close(); err != nil {
			t.Fatal(err)
		}
	}

	// Simulate the LRU victim (A) being busy: its removal fails. Each entry now
	// has a unique on-disk name, so resolve A's actual path from the index.
	c.mu.Lock()
	busy := c.entries["A.MP3"].path
	c.mu.Unlock()
	orig := removeFile
	removeFile = func(p string) error {
		if p == busy {
			return errors.New("sharing violation")
		}
		return os.Remove(p)
	}
	defer func() { removeFile = orig }()

	// Adding C (30) pushes total to 90 > 60: eviction picks A, fails to
	// delete it, keeps its accounting and evicts B instead. Dropping A's
	// bookkeeping would desync (total 60 with 90 bytes on disk); retrying A
	// in a loop would hang.
	wc, _ := c.Put("C.MP3")
	_, _ = wc.Write(payload)
	if err := wc.Close(); err != nil {
		t.Fatal(err)
	}

	n, b := c.Stats()
	if n != 2 || b != 60 || hasEntry(c, "B.MP3") {
		t.Fatalf("stats = %d entries, %d bytes, B kept %v; want 2, 60, B evicted past the busy A", n, b, hasEntry(c, "B.MP3"))
	}
	rc, _, hit := c.Get("A.MP3")
	if !hit {
		t.Fatal("busy victim was dropped from the index")
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, payload) {
		t.Fatal("busy victim's bytes corrupted")
	}

	// Once the file is deletable again, the next commit's eviction catches up.
	// (The Get above bumped A's recency, so B and C are now the LRU victims.)
	removeFile = os.Remove
	wc, _ = c.Put("D.MP3")
	_, _ = wc.Write(payload)
	if err := wc.Close(); err != nil {
		t.Fatal(err)
	}
	if n, b := c.Stats(); n != 2 || b != 60 {
		t.Fatalf("eviction did not resume after the victim became removable: %d entries, %d bytes", n, b)
	}
	for _, k := range []string{"B.MP3", "C.MP3"} {
		if rc, _, hit := c.Get(k); hit {
			rc.Close()
			t.Errorf("%s (LRU) should have been evicted once eviction resumed", k)
		}
	}
	if rc, _, hit := c.Get("A.MP3"); !hit {
		t.Error("A (recently touched) should have survived")
	} else {
		rc.Close()
	}
}

// go test -race should pass; the concurrent test above exercises it.
func TestMain(m *testing.M) {
	// nothing special
	os.Exit(m.Run())
}

// TestReplaceWhileReaderOpen is the exact Windows scenario the unique-filename
// scheme exists to survive: committing a replacement for a key while a reader
// still holds the previous file open. The old, same-name scheme did
// os.Rename(temp, key) here — on Windows that fails with "Access is denied"
// because the target file is open. With unique names the replacement renames
// onto a brand-new path and never touches the open file, so the commit
// succeeds on every OS, the open reader keeps reading the ORIGINAL bytes, and a
// fresh Get sees the replacement. (On Windows the best-effort delete of the old
// file fails while the reader is open and the file is left as a reclaimable
// orphan; the commit still returns nil. On Unix the delete succeeds but the
// open fd stays valid.)
func TestReplaceWhileReaderOpen(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir, 10<<20)

	const key = "replace.MP3_320"
	orig := []byte("original bytes held open by a reader across a replace")

	wc, err := c.Put(key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wc.Write(orig); err != nil {
		t.Fatal(err)
	}
	if err := wc.Close(); err != nil {
		t.Fatal(err)
	}

	// Open a reader on the original entry and deliberately keep it open across
	// the replacement (mimics Windows holding the file).
	rc, _, hit := c.Get(key)
	if !hit {
		t.Fatal("expected hit for the original entry")
	}
	defer rc.Close()

	// Commit a replacement for the same key while rc is still open. This must
	// NOT error (the whole point of the unique-filename scheme).
	repl := []byte("replacement bytes committed while a reader is open")
	wc2, err := c.Put(key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wc2.Write(repl); err != nil {
		t.Fatal(err)
	}
	if err := wc2.Close(); err != nil {
		t.Fatalf("replace while a reader holds the old file open must succeed: %v", err)
	}

	// The still-open reader must observe the ORIGINAL bytes in full.
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, orig) {
		t.Fatalf("open reader saw %q, want original %q", got, orig)
	}

	// A fresh Get returns the replacement.
	rc2, sz, hit := c.Get(key)
	if !hit {
		t.Fatal("expected hit after replacement")
	}
	got2, _ := io.ReadAll(rc2)
	rc2.Close()
	if !bytes.Equal(got2, repl) || sz != int64(len(repl)) {
		t.Fatalf("fresh reader saw %q (size %d), want %q", got2, sz, repl)
	}
	if n, b := c.Stats(); n != 1 || b != int64(len(repl)) {
		t.Fatalf("stats after replace = %d entries, %d bytes; want 1, %d", n, b, len(repl))
	}
}

// --- Phase 3 meta persistence for cached plans ---

func TestPutMetaGetMeta_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	c, err := New(dir, 10<<20)
	if err != nil {
		t.Fatal(err)
	}

	m := StreamMeta{
		Format:     "MP3_320",
		Encrypted:  true,
		GainDB:     -4.25,
		Preview:    false,
		DurationMS: 217000,
	}
	putBody(t, c, "42.MP3_320", 100) // GetMetaForTrack needs a body
	if err := c.PutMeta("42.MP3_320", m); err != nil {
		t.Fatalf("PutMeta: %v", err)
	}

	got, ok := c.GetMeta("42.MP3_320")
	if !ok {
		t.Fatal("GetMeta missed stored meta")
	}
	if got.Format != m.Format || got.Encrypted != m.Encrypted || got.GainDB != m.GainDB || got.Preview != m.Preview || got.DurationMS != m.DurationMS {
		t.Fatalf("meta roundtrip mismatch: %+v", got)
	}

	// also via track lookup
	got2, ok2 := c.GetMetaForTrack("42")
	if !ok2 || got2.Format != "MP3_320" {
		t.Fatalf("GetMetaForTrack: %+v ok=%v", got2, ok2)
	}
}

func TestGetMeta_CorruptToleratesAndKeepsEntries(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir, 10<<20)

	// body + meta
	data := []byte("payload-for-corrupt-meta-test")
	wc, _ := c.Put("corrupt.MP3")
	_, _ = wc.Write(data)
	_ = wc.Close()
	_ = c.PutMeta("corrupt.MP3", StreamMeta{Format: "MP3_128", Encrypted: false})

	// also a second meta-only
	_ = c.PutMeta("metaonly.FLAC", StreamMeta{Format: "FLAC", Encrypted: true, GainDB: 1.5})

	// Corrupt the index: make the metas value unmarshalable as StreamMeta map.
	// (top-level entries stay valid)
	idxp := filepath.Join(dir, "index.json")
	// Build using a name that will match the real file written by the Put above.
	// We know the impl uses encodeKey+seq; we will just force a minimal valid
	// entry and rely on load's file-stat validation to keep or drop.
	badIdx := []byte(`{"entries":{},"nextSeq":1,"fileSeq":0,"metas":{"corrupt.MP3":"this-is-not-an-object","metaonly.FLAC":123}}`)
	_ = os.WriteFile(idxp, badIdx, 0o644)

	// Reopen must succeed without panic and drop bad metas.
	c.Close()
	c2, err := New(dir, 10<<20)
	if err != nil {
		t.Fatalf("New after corrupt meta index: %v", err)
	}

	// No body in this index (we overwrote), but GetMeta must tolerate and return false.
	if _, ok := c2.GetMeta("corrupt.MP3"); ok {
		t.Error("corrupt meta should have been dropped")
	}
	if _, ok := c2.GetMeta("metaonly.FLAC"); ok {
		t.Error("bad-typed meta should have been dropped")
	}
}

func putBody(t *testing.T, c *Cache, key string, n int) {
	t.Helper()
	wc, err := c.Put(key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wc.Write(bytes.Repeat([]byte{1}, n)); err != nil {
		t.Fatal(err)
	}
	if err := wc.Close(); err != nil {
		t.Fatal(err)
	}
}

func hasEntry(c *Cache, key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.entries[key]
	return ok
}

// TestGetRecencySurvivesRestart: a Get-only bump is persisted, so after a
// restart the most recently played entry is not the eviction victim.
func TestGetRecencySurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir, 250)
	putBody(t, c, "old.MP3", 100)
	putBody(t, c, "new.MP3", 100)
	rc, _, ok := c.Get("old.MP3") // replayed last, no commit after it
	if !ok {
		t.Fatal("miss")
	}
	rc.Close()

	c.Close()
	c2, _ := New(dir, 250)
	putBody(t, c2, "third.MP3", 100)
	if !hasEntry(c2, "old.MP3") || hasEntry(c2, "new.MP3") {
		t.Errorf("after restart: old=%v new=%v, want old kept and new evicted",
			hasEntry(c2, "old.MP3"), hasEntry(c2, "new.MP3"))
	}
}

// With two formats cached, GetMetaForTrack answers the better one, every time.
func TestMetaForTrackPicksBestFormat(t *testing.T) {
	c, _ := New(t.TempDir(), 1<<20)
	for _, f := range []string{"MP3_128", "FLAC", "MP3_64"} {
		putBody(t, c, "5."+f, 100)
		_ = c.PutMeta("5."+f, StreamMeta{Format: f, Encrypted: true})
	}
	for range 20 { // map order varies between calls
		if m, ok := c.GetMetaForTrack("5"); !ok || m.Format != "FLAC" {
			t.Fatalf("GetMetaForTrack = %+v %v, want FLAC", m, ok)
		}
	}
}

// The cache deletes only its own files: data files, index.json and its temps.
// Other files in its folder survive opening, clearing, purging, a limit of 0
// and the age limit.
func TestForeignFilesSurvive(t *testing.T) {
	dir := t.TempDir()
	foreign := []string{"a.mp3", "x.tmp", "notes-1.txt", filepath.Join("sub", "b.mp3")}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-400 * 24 * time.Hour)
	for _, f := range foreign {
		p := filepath.Join(dir, f)
		if err := os.WriteFile(p, []byte("mine"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	check := func(step string) {
		t.Helper()
		for _, f := range foreign {
			if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
				t.Fatalf("%s after %s: %v", f, step, err)
			}
		}
	}
	c, err := New(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	check("New")
	putBody(t, c, "1.MP3", 10)
	c.SetMaxAge(24 * time.Hour)
	check("SetMaxAge")
	putBody(t, c, "2.MP3", 10)
	if err := c.Clear(); err != nil {
		t.Fatal(err)
	}
	check("Clear")
	putBody(t, c, "3.MP3", 10)
	c.SetMax(0)
	check("SetMax(0)")
	putBody(t, c, "4.MP3", 10)
	c.Close()
	if err := Purge(dir); err != nil {
		t.Fatal(err)
	}
	check("Purge")
	c, err = New(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	check("a second New")
}

// The cache older versions kept by default in <config dir>/mediacache (they
// kept no lock file) is removed with its folder; a file of someone else's
// keeps the folder.
func TestRemoveOldCaches(t *testing.T) {
	base := t.TempDir()
	config.SetBaseDir(base)
	defer config.SetBaseDir("")
	old := filepath.Join(base, "mediacache")
	oldCache := func() {
		t.Helper()
		c, err := New(old, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		putBody(t, c, "1.MP3", 10)
		c.Close()
		if err := os.Remove(filepath.Join(old, "lock")); err != nil {
			t.Fatal(err)
		}
		if err := config.SaveMedia(config.Media{}); err != nil { // not run yet
			t.Fatal(err)
		}
	}
	oldCache()
	RemoveOldCaches()
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old default folder after removal: %v", err)
	}
	if err := config.SaveMedia(config.Media{}); err != nil {
		t.Fatal(err)
	}
	RemoveOldCaches() // nothing there: no error, nothing created
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old default folder after a second removal: %v", err)
	}

	oldCache()
	mine := filepath.Join(old, "mine.txt")
	_ = os.WriteFile(mine, []byte("x"), 0o644)
	RemoveOldCaches()
	des, _ := os.ReadDir(old)
	if len(des) != 1 || des[0].Name() != "mine.txt" {
		var names []string
		for _, de := range des {
			names = append(names, de.Name())
		}
		t.Fatalf("old default folder holds %v, want only mine.txt", names)
	}
}

// readFolder maps each file directly in dir to its contents.
func readFolder(t *testing.T, dir string) map[string]string {
	t.Helper()
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, de := range des {
		if de.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, de.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[de.Name()] = string(b)
	}
	return files
}

// Files that look like cache files but are not (a date, upper-case hex), a
// foreign index.json, a lock, a temp name and a text file stay as they are
// through RemoveOldCaches, New, Purge, Clear and a folder change away and back.
// An old-layout cache beside such files is removed, and only its indexed
// files and index go. RemoveOldCaches runs once.
func TestOldCacheRemovalKeepsOtherFiles(t *testing.T) {
	base := t.TempDir()
	config.SetBaseDir(base)
	defer config.SetBaseDir("")
	foreign := map[string]string{
		"2024-11-17": "diary",
		"CAFE-01":    "menu",
		"index.json": `{"entries":{"a":{"file":"notes.txt","size":5}}}`,
		"lock":       "mine",
		".tmp-42":    "draft",
		"notes.txt":  "notes",
	}
	write := func(dir string, files map[string]string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, body := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	same := func(step, dir string, want map[string]string) {
		t.Helper()
		got := readFolder(t, dir)
		if len(got) != len(want) {
			t.Fatalf("after %s %s holds %v, want %v", step, dir, got, want)
		}
		for name, body := range want {
			if got[name] != body {
				t.Fatalf("after %s %s holds %v, want %v", step, dir, got, want)
			}
		}
	}

	picked := t.TempDir()
	write(picked, foreign)
	if err := config.SaveMedia(config.Media{MediaCacheMB: 1, MediaCacheDir: picked}); err != nil {
		t.Fatal(err)
	}
	// The default folder of older versions: a real cache with other files
	// beside it.
	old := filepath.Join(base, "mediacache")
	c, err := New(old, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	putBody(t, c, "1.MP3_128", 10)
	putBody(t, c, "2.FLAC", 10)
	c.Close()
	beside := map[string]string{}
	for name, body := range foreign {
		if name != "index.json" {
			beside[name] = body
		}
	}
	write(old, beside)

	RemoveOldCaches()
	same("RemoveOldCaches", picked, foreign)
	same("RemoveOldCaches", old, beside)

	// Run once: a cache put back in the default folder stays.
	c, err = New(old, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	putBody(t, c, "3.MP3_128", 10)
	c.Close()
	again := readFolder(t, old)
	RemoveOldCaches()
	same("a second RemoveOldCaches", old, again)

	// The cache in the picked folder lives in its subfolder.
	h := &Host{}
	if err := h.Open(); err != nil {
		t.Fatal(err)
	}
	putBody(t, h.Current(), "4.MP3_128", 10)
	if err := h.Clear(true); err != nil {
		t.Fatal(err)
	}
	putBody(t, h.Current(), "5.MP3_128", 10)
	if err := h.SetDir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := h.SetDir(picked); err != nil {
		t.Fatal(err)
	}
	if err := h.SetMB(0); err != nil {
		t.Fatal(err)
	}
	if err := Purge(filepath.Join(picked, "opendeezer-cache")); err != nil {
		t.Fatal(err)
	}
	same("the cache's use", picked, foreign)

	// In a cache's own folder too, files that only look like data files stay.
	lookalikes := map[string]string{"2024-11-17": "diary", "CAFE-01": "menu", "notes.txt": "notes"}
	dir := t.TempDir()
	write(dir, lookalikes)
	c, err = New(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	putBody(t, c, "6.MP3_128", 10)
	if err := c.Clear(); err != nil {
		t.Fatal(err)
	}
	putBody(t, c, "7.MP3_128", 10)
	c.SetMax(0)
	c.Close()
	if err := Purge(dir); err != nil {
		t.Fatal(err)
	}
	got := readFolder(t, dir)
	for name, body := range lookalikes {
		if got[name] != body {
			t.Fatalf("%s in a cache's folder holds %v, want %v kept", dir, got, lookalikes)
		}
	}
}

// The folder in use, picked again under another spelling of its path (another
// case on Windows), is the same folder: the cache stays open on it.
func TestSetDirSameFolderOtherSpelling(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("paths differing in case name one folder on Windows")
	}
	base := t.TempDir()
	config.SetBaseDir(base)
	defer config.SetBaseDir("")
	dir := filepath.Join(base, "Music")
	if err := config.SaveMedia(config.Media{MediaCacheMB: 1, MediaCacheDir: dir}); err != nil {
		t.Fatal(err)
	}
	h := &Host{}
	if err := h.Open(); err != nil {
		t.Fatal(err)
	}
	c := h.Current()
	defer func() { h.Current().Close() }()
	putBody(t, c, "1.MP3_128", 10)
	if err := h.SetDir(strings.ToUpper(dir)); err != nil {
		t.Fatalf("SetDir of the same folder in upper case: %v", err)
	}
	if h.Current() != c || !hasEntry(c, "1.MP3_128") {
		t.Fatal("the cache changed for the same folder")
	}
}

// One cache per folder: while one holds it, a second New on it fails (as in a
// second process, which then runs uncached) and leaves the first's files
// alone; once the first is closed the folder opens again.
func TestOneCachePerFolder(t *testing.T) {
	dir := t.TempDir()
	c, err := New(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	tmp, err := c.NewTemp()
	if err != nil {
		t.Fatal(err)
	}
	defer tmp.Close()
	if _, err := New(dir, 1<<20); err == nil {
		t.Fatal("a second cache opened a folder in use")
	}
	if _, err := os.Stat(tmp.Name()); err != nil {
		t.Fatalf("the first cache's temp file after a second New: %v", err)
	}
	c.Close()
	c2, err := New(dir, 1<<20)
	if err != nil {
		t.Fatalf("New after Close: %v", err)
	}
	c2.Close()
}

// After Close the cache refuses Get, Adopt and PutMeta, and a recency flush
// due from before does not overwrite the index of the cache now on the folder.
func TestClosedCacheRefuses(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir, 1<<20)
	putBody(t, c, "1.MP3", 10)
	if rc, _, ok := c.Get("1.MP3"); ok { // schedules a recency flush
		rc.Close()
	}
	tmp, err := c.NewTemp()
	if err != nil {
		t.Fatal(err)
	}
	defer tmp.Close()
	_, _ = tmp.Write([]byte("body"))
	c.Close()
	if rc, _, ok := c.Get("1.MP3"); ok {
		rc.Close()
		t.Fatal("a closed cache served an entry")
	}
	if err := c.Adopt("2.MP3", tmp.Name(), 4, StreamMeta{}); err == nil {
		t.Fatal("a closed cache adopted a body")
	}
	if err := c.PutMeta("1.MP3", StreamMeta{Format: "MP3"}); err == nil {
		t.Fatal("a closed cache stored meta")
	}
	if _, ok := c.GetMeta("1.MP3"); ok {
		t.Fatal("refused meta was kept")
	}

	now, err := New(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer now.Close()
	putBody(t, now, "3.MP3", 10)
	idx, _ := os.ReadFile(filepath.Join(dir, "index.json"))
	c.mu.Lock()
	c.dirty = true
	c.mu.Unlock()
	c.flushRecency()
	if after, _ := os.ReadFile(filepath.Join(dir, "index.json")); !bytes.Equal(after, idx) {
		t.Fatal("the closed cache rewrote the index")
	}
}

// A closed cache writes no file: Put refuses, and a Put begun before Close
// leaves nothing in the folder when it commits after it.
func TestClosedCacheWritesNothing(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir, 1<<20)
	putBody(t, c, "1.MP3", 10)
	wc, err := c.Put("2.MP3")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wc.Write([]byte("body")); err != nil {
		t.Fatal(err)
	}
	names := func() map[string]bool {
		des, _ := os.ReadDir(dir)
		m := map[string]bool{}
		for _, de := range des {
			m[de.Name()] = true
		}
		return m
	}
	before := names()
	c.Close()
	if err := wc.Close(); !errors.Is(err, errClosed) {
		t.Fatalf("a commit after Close returned %v, want errClosed", err)
	}
	if w, err := c.Put("3.MP3"); err == nil {
		_ = w.Close()
		t.Fatal("a closed cache began a Put")
	}
	after := names()
	for name := range after {
		if !before[name] {
			t.Fatalf("the closed cache wrote %s", name)
		}
	}
	for name := range before {
		if !after[name] && !strings.HasPrefix(name, ".tmp-") {
			t.Fatalf("the closed cache removed %s", name)
		}
	}
}

// CheckDir refuses only a relative path and a path that is a file.
func TestCheckDir(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{dir, filepath.Join(dir, "missing")} {
		if err := CheckDir(d); err != nil {
			t.Errorf("CheckDir(%s) = %v", d, err)
		}
	}
	for _, d := range []string{file, "relative"} {
		if err := CheckDir(d); err == nil {
			t.Errorf("CheckDir(%s) accepted", d)
		}
	}
}

// BenchmarkNew opens a cache of 2 000 entries (the startup cost in DZInit).
func BenchmarkNew(b *testing.B) {
	dir := b.TempDir()
	c, _ := New(dir, 1<<30)
	for i := range 2000 {
		w, _ := c.Put(fmt.Sprint(i, ".MP3_320"))
		_, _ = w.Write([]byte("ciphertext"))
		_ = w.Close()
	}
	c.Close()
	b.ResetTimer()
	for b.Loop() {
		c, _ := New(dir, 1<<30)
		if len(c.entries) != 2000 {
			b.Fatal(len(c.entries))
		}
		c.Close()
	}
}

// A hit does not rewrite index.json; the recency it records is written by the
// next flush.
func TestGetDoesNotRewriteIndex(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir, 1<<20)
	putBody(t, c, "a.MP3", 100)
	putBody(t, c, "b.MP3", 100)
	idx := filepath.Join(dir, "index.json")
	before, _ := os.ReadFile(idx)
	for range 5 {
		rc, _, ok := c.Get("a.MP3")
		if !ok {
			t.Fatal("miss")
		}
		rc.Close()
	}
	if after, _ := os.ReadFile(idx); !bytes.Equal(after, before) {
		t.Fatal("a hit rewrote index.json")
	}
	c.flushRecency()
	if after, _ := os.ReadFile(idx); bytes.Equal(after, before) {
		t.Fatal("the flush did not write the recency")
	}
}

// TestMetaRequiresBody: GetMetaForTrack answers only for a key with a live
// body, both for meta stored without a body and for a body dropped at load.
func TestMetaRequiresBody(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir, 1<<20)
	putBody(t, c, "7.MP3_320", 100)
	putBody(t, c, "8.MP3_320", 100)
	_ = c.PutMeta("7.MP3_320", StreamMeta{Format: "MP3_320", Encrypted: true})
	_ = c.PutMeta("9.MP3_320", StreamMeta{Format: "MP3_320", Encrypted: true}) // body never adopted
	if _, ok := c.GetMetaForTrack("9"); ok {
		t.Error("meta-only key reported as cached")
	}
	if _, ok := c.GetMetaForTrack("7"); !ok {
		t.Error("meta with a body not found")
	}

	c.mu.Lock()
	p := c.entries["7.MP3_320"].path
	c.mu.Unlock()
	if err := os.Truncate(p, 10); err != nil { // body truncated between runs
		t.Fatal(err)
	}
	c.Close()
	c2, _ := New(dir, 1<<20)
	if _, ok := c2.GetMetaForTrack("7"); ok {
		t.Error("meta survived the load that dropped its body")
	}
	if _, ok := c2.GetMeta("7.MP3_320"); ok {
		t.Error("load kept meta without a body")
	}
}
