// Package mediacache is an opt-in, on-disk LRU cache of raw CDN stream bodies keyed
// "trackID.format", stored as served (ciphertext). Entries commit atomically under
// unique file names; pinned entries are never evicted. Safe for concurrent use.
package mediacache

import (
	"cmp"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/config"
	pdlog "github.com/Scharkenberg/PurpleDisc/v4/internal/log"
)

// removeFile is os.Remove, indirected so tests can simulate removal failures
// (e.g. a Windows sharing violation while a reader still has the file open).
var removeFile = os.Remove

// fileSep separates the hex parts of a data file name; since both sides are hex, no
// data name can collide with "index.json", "index.json.tmp*" or ".tmp-*".
const fileSep = "-"

// encodeKey hex-encodes an arbitrary cache key into a filesystem-safe token.
// Hex is fully reversible, so a filesystem-only rebuild (when index.json is
// absent or corrupt) can recover the original key from the filename.
func encodeKey(key string) string { return hex.EncodeToString([]byte(key)) }

// parseFileName reverses newFilePath's "<hexKey>-<hexSeq>-<hexSize>" (lower-case hex,
// key "<trackID>.<format>"); "<hexKey>-<hexSeq>" names have size -1. ok is false for
// names that are not the cache's.
func parseFileName(name string) (key string, seq uint64, size int64, ok bool) {
	parts := strings.Split(name, fileSep)
	if len(parts) < 2 || len(parts) > 3 {
		return "", 0, 0, false
	}
	for _, p := range parts {
		if p == "" || strings.Trim(p, "0123456789abcdef") != "" {
			return "", 0, 0, false
		}
	}
	kb, err := hex.DecodeString(parts[0])
	if err != nil {
		return "", 0, 0, false
	}
	id, format, dot := strings.Cut(string(kb), ".")
	if !dot || id == "" || format == "" ||
		strings.Trim(id, "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_-") != "" ||
		strings.Trim(format, "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ_") != "" {
		return "", 0, 0, false
	}
	seq, err = strconv.ParseUint(parts[1], 16, 64)
	if err != nil {
		return "", 0, 0, false
	}
	size = -1
	if len(parts) == 3 {
		if size, err = strconv.ParseInt(parts[2], 16, 64); err != nil || size < 0 {
			return "", 0, 0, false
		}
	}
	return string(kb), seq, size, true
}

// newFilePath allocates a unique path for a commit of key with size bytes (the size
// lets a recovery check the file). It never names an existing file, so a rename onto
// it never hits an open file.
func (c *Cache) newFilePath(key string, size int64) string {
	enc := encodeKey(key)
	for {
		c.mu.Lock()
		c.fileSeq++
		seq := c.fileSeq
		c.mu.Unlock()
		p := filepath.Join(c.dir, enc+fileSep+strconv.FormatUint(seq, 16)+fileSep+strconv.FormatInt(size, 16))
		if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
			return p
		}
		// Extremely unlikely collision with a surviving orphan from a prior
		// run whose index was lost: bump the sequence and try again.
	}
}

// Cache is an on-disk LRU cache of raw stream payloads.
type Cache struct {
	dir     string
	max     int64
	lock    *os.File // the folder's lock file, held until Close
	mu      sync.Mutex
	closed  bool // set by Close: nothing more is read or written
	entries map[string]entry
	total   int64
	pinned  int64 // bytes of the pinned entries, part of total
	nextSeq uint64
	fileSeq uint64              // monotonic; makes every committed file name unique
	writing map[string]struct{} // keys with active Put writers (protect from eviction)
	metas   map[string]StreamMeta
	dirty   bool        // entries or recency changed since index.json was written
	flush   *time.Timer // pending index write, nil when none
	flushAt time.Time   // when flush fires
	// maxAge removes entries not used for longer (0 = never); lastExpire is
	// when that was last checked.
	maxAge     time.Duration
	lastExpire time.Time
}

// recencyFlush is how long a hit's recency may wait in memory for the next
// index write.
const recencyFlush = 30 * time.Second

// installFlush is how long an installed entry may wait in memory for the index write:
// a pin of many tracks writes the index about once a second, not once per track. A var
// for tests.
var installFlush = time.Second

// expireEvery is how often, at most, a commit or a recency flush removes the
// entries older than maxAge.
const expireEvery = time.Hour

type entry struct {
	path      string
	size      int64
	accessSeq uint64    // higher = more recent (precise LRU in this process)
	used      time.Time // last write or hit
	pins      []string  // sorted; none for an entry kept only as a cache
}

type indexData struct {
	Entries map[string]indexEntry `json:"entries"`
	NextSeq uint64                `json:"nextSeq"`
	FileSeq uint64                `json:"fileSeq"`
	Metas   map[string]StreamMeta `json:"metas,omitempty"`
}

type indexEntry struct {
	File      string `json:"file"` // on-disk basename (unique per commit)
	Size      int64  `json:"size"`
	AccessSeq uint64 `json:"accessSeq"`
	// Used is the last write or hit, in Unix milliseconds; 0 in an index
	// written before it was kept, when the file's mtime stands in.
	Used int64    `json:"used,omitempty"`
	Pins []string `json:"pins,omitempty"`
}

// StreamMeta is the minimal subset of StreamPlan fields persisted per cache
// key so that a cached ciphertext can yield a fully playable plan with no
// PrepareStream / token / get_url network round-trips.
type StreamMeta struct {
	Format string
	// AskedFormat is the format asked for when the copy was fetched; Format
	// is lower when Deezer had nothing better. "" when unknown.
	AskedFormat string `json:",omitempty"`
	// KeyID is the id of the track whose stream the body is, when Deezer
	// served another version of the track; its key decrypts the body.
	KeyID      string `json:",omitempty"`
	Encrypted  bool
	GainDB     float64
	Preview    bool
	DurationMS int64 // milliseconds; 0 if unknown (playback callers often supply separately)
}

// New creates or opens the cache in dir. maxBytes is the soft limit (entries
// are evicted on Put commits to stay at or below it). dir is created if needed.
// Partial, zero-length, and temp files are cleaned; a corrupt index is ignored.
func New(dir string, maxBytes int64) (*Cache, error) {
	if dir == "" {
		return nil, errors.New("mediacache: empty dir")
	}
	if maxBytes < 0 {
		return nil, errors.New("mediacache: maxBytes must be >= 0")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	// One cache per folder: the lock is held until Close (or the process
	// ends), so a second process on the same folder runs uncached instead of
	// deleting this one's temps and overwriting its index.
	lock, err := openShared(dir, "lock", os.O_RDWR|os.O_CREATE)
	if err != nil {
		return nil, err
	}
	if err := lockExclusive(lock); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("mediacache: %s is in use by another cache: %w", dir, err)
	}
	c := &Cache{
		dir:     dir,
		max:     maxBytes,
		lock:    lock,
		entries: make(map[string]entry),
		writing: make(map[string]struct{}),
		metas:   make(map[string]StreamMeta),
	}
	c.recover()
	return c, nil
}

// Close writes the pending index (installs and recency) and releases the folder. Afterward Get,
// Put, Adopt and PutMeta refuse and an open Put commits nothing; a stream started with
// this cache plays uncached.
func (c *Cache) Close() {
	c.mu.Lock()
	if c.dirty {
		c.saveIndexLocked()
	}
	if c.flush != nil {
		c.flush.Stop()
		c.flush = nil
	}
	c.closed = true
	lock := c.lock
	c.lock = nil
	c.mu.Unlock()
	if lock != nil {
		_ = lock.Close()
	}
}

// errClosed is the refusal of a closed cache.
var errClosed = errors.New("mediacache: closed")

func (c *Cache) recover() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// One directory listing serves the whole recovery: it finds the cache's temps to
	// remove and the data files' sizes and mtimes, without a stat per file.
	files := map[string]os.FileInfo{}
	des, _ := os.ReadDir(c.dir)
	for _, de := range des {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		rest, temp := strings.CutPrefix(name, ".tmp-")
		if !temp {
			rest, temp = strings.CutPrefix(name, "index.json.tmp")
		}
		temp = temp && rest != "" && strings.Trim(rest, "0123456789") == ""
		switch {
		case temp:
			_ = os.Remove(filepath.Join(c.dir, name))
		case name != "index.json":
			if fi, err := de.Info(); err == nil {
				files[name] = fi
			}
		}
	}

	if ents, ms, seq, fseq, ok := c.loadIndexLocked(files); ok && len(ents) > 0 {
		c.entries = ents
		c.total, c.pinned = 0, 0
		for _, e := range ents {
			c.total += e.size
			if len(e.pins) > 0 {
				c.pinned += e.size
			}
		}
		c.nextSeq = seq
		c.fileSeq = fseq
		if ms != nil {
			c.metas = ms
		}
	} else {
		// Fallback: rebuild from filesystem using mtimes for initial recency
		// order (index.json missing or corrupt).
		c.rebuildFromFSLocked(files)
	}

	// Any data file not referenced by a live entry is an orphan: a leftover
	// from a crashed replace, or a file whose delete a reader was blocking.
	c.reclaimOrphansLocked(files)
}

// reclaimOrphansLocked tries to delete every data file in the listing that no index
// entry references; other files stay. An undeletable orphan (open on Windows) is
// retried by the next New.
func (c *Cache) reclaimOrphansLocked(files map[string]os.FileInfo) {
	live := make(map[string]struct{}, len(c.entries))
	var maxSeq uint64
	for _, e := range c.entries {
		live[filepath.Base(e.path)] = struct{}{}
	}
	for name := range files {
		_, seq, _, ours := parseFileName(name)
		if !ours {
			continue
		}
		// Keep fileSeq above any sequence still present on disk so a future
		// commit can never regenerate a name that collides with an orphan.
		if seq > maxSeq {
			maxSeq = seq
		}
		if _, ok := live[name]; ok {
			continue
		}
		// Orphan: try to delete it. If a reader holds it open (Windows) the remove fails and
		// the next New retries.
		_ = removeFile(filepath.Join(c.dir, name))
	}
	if maxSeq >= c.fileSeq {
		c.fileSeq = maxSeq
	}
}

// loadIndexLocked reads index.json, keeping the entries whose file is in the
// listing with the recorded size.
func (c *Cache) loadIndexLocked(files map[string]os.FileInfo) (ents map[string]entry, metas map[string]StreamMeta, nextSeq, fileSeq uint64, ok bool) {
	p := filepath.Join(c.dir, "index.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, nil, 0, 0, false
	}
	// Use raw for metas so a corrupt meta entry (bad type or value) does not
	// nuke the entire index; we drop only the bad meta(s).
	var raw struct {
		Entries map[string]indexEntry      `json:"entries"`
		NextSeq uint64                     `json:"nextSeq"`
		FileSeq uint64                     `json:"fileSeq"`
		Metas   map[string]json.RawMessage `json:"metas"`
	}
	if json.Unmarshal(b, &raw) != nil || raw.Entries == nil {
		return nil, nil, 0, 0, false
	}
	valid := make(map[string]entry)
	maxSeq := raw.NextSeq
	maxFileSeq := raw.FileSeq
	for k, ie := range raw.Entries {
		// Only a data file name is taken: an entry never makes the cache
		// delete a file that is not its own.
		_, seq, _, ours := parseFileName(ie.File)
		if k == "" || !ours {
			continue
		}
		pth := filepath.Join(c.dir, ie.File)
		fi, listed := files[ie.File]
		if !listed || fi.Size() != ie.Size {
			// Missing or size-mismatched: drop the entry. The file is then unreferenced and
			// reclaimOrphansLocked deletes it.
			continue
		}
		used := time.UnixMilli(ie.Used)
		if ie.Used == 0 {
			used = fi.ModTime()
		}
		valid[k] = entry{path: pth, size: ie.Size, accessSeq: ie.AccessSeq, used: used, pins: sortedPins(ie.Pins)}
		if ie.AccessSeq > maxSeq {
			maxSeq = ie.AccessSeq
		}
		if seq > maxFileSeq {
			maxFileSeq = seq
		}
	}
	validMetas := make(map[string]StreamMeta)
	for k, rm := range raw.Metas {
		if _, body := valid[k]; !body {
			continue // meta without a usable body
		}
		var m StreamMeta
		if json.Unmarshal(rm, &m) == nil {
			validMetas[k] = m
		}
		// corrupt meta for this k is dropped; body (if any) remains usable
	}
	return valid, validMetas, maxSeq + 1, maxFileSeq, true
}

// rebuildFromFSLocked builds the entries from the listing's data files.
func (c *Cache) rebuildFromFSLocked(files map[string]os.FileInfo) {
	c.entries = make(map[string]entry)
	c.metas = make(map[string]StreamMeta)
	c.total, c.pinned = 0, 0
	c.nextSeq = 1
	c.fileSeq = 0

	type cand struct {
		e   entry
		mt  time.Time
		seq uint64
	}
	// Multiple files may map to the same key (orphans from a crashed replace).
	// Keep the newest (highest sequence) per key; the rest stay unreferenced
	// and are reclaimed by reclaimOrphansLocked.
	best := make(map[string]cand)
	for name, fi := range files {
		key, seq, size, ok := parseFileName(name)
		if !ok {
			continue // not one of ours; reclaimOrphansLocked handles it
		}
		if seq > c.fileSeq {
			c.fileSeq = seq
		}
		pth := filepath.Join(c.dir, name)
		// A file whose size differs from the one in its name is damaged; an
		// older name carries no size, so only an empty one is refused.
		if fi.Size() == 0 || (size >= 0 && fi.Size() != size) {
			continue // leave for reclaimOrphansLocked
		}
		if ex, seen := best[key]; !seen || seq > ex.seq {
			best[key] = cand{
				e:   entry{path: pth, size: fi.Size(), used: fi.ModTime()},
				mt:  fi.ModTime(),
				seq: seq,
			}
		}
	}

	// sort oldest (smallest mtime) first so we can assign low seqs to LRU
	cands := make([]cand, 0, len(best))
	for _, cd := range best {
		cands = append(cands, cd)
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].mt.Before(cands[j].mt) })

	for i, cd := range cands {
		key, _, _, _ := parseFileName(filepath.Base(cd.e.path))
		cd.e.accessSeq = uint64(i + 1)
		c.entries[key] = cd.e
		c.total += cd.e.size
	}
	if len(cands) > 0 {
		c.nextSeq = uint64(len(cands)) + 1
	}
}

// writeWithinLocked marks the index changed and has it written within d: a pending
// write due later is brought forward, never pushed back.
func (c *Cache) writeWithinLocked(d time.Duration) {
	c.dirty = true
	if c.flush != nil && time.Until(c.flushAt) <= d {
		return
	}
	if c.flush != nil {
		c.flush.Stop()
	}
	c.flushAt = time.Now().Add(d)
	c.flush = time.AfterFunc(d, c.flushRecency)
}

// Flush writes index.json now when anything changed since the last write: the end
// of a batch of installs (a pin's fetch).
func (c *Cache) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dirty {
		c.saveIndexLocked()
	}
}

// flushRecency removes entries older than the age limit when that is due, and
// writes index.json if anything changed since the last write.
func (c *Cache) flushRecency() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flush = nil
	if c.closed {
		return // a timer that fired as Close ran
	}
	if time.Since(c.lastExpire) >= expireEvery {
		c.expireLocked()
	}
	if c.dirty {
		c.saveIndexLocked()
	}
}

// SetMaxAge sets the age limit and applies it at once: entries whose last
// write or hit is older than d are removed; 0 keeps entries of any age.
func (c *Cache) SetMaxAge(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maxAge = d
	c.expireLocked()
	if c.dirty {
		c.saveIndexLocked()
	}
}

// expireLocked removes the entries not used within maxAge (none when it is 0),
// skipping keys being written and files that cannot be deleted; it marks the
// index dirty when it removed any.
func (c *Cache) expireLocked() {
	now := time.Now()
	c.lastExpire = now
	if c.maxAge <= 0 {
		return
	}
	for k, e := range c.entries {
		if _, busy := c.writing[k]; busy || len(e.pins) > 0 || now.Sub(e.used) <= c.maxAge {
			continue
		}
		if err := removeFile(e.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			continue // the next check retries it
		}
		c.forgetLocked(k, e)
		c.dirty = true
	}
}

// forgetLocked drops key's entry, its accounting and its meta.
func (c *Cache) forgetLocked(k string, e entry) {
	c.total -= e.size
	if len(e.pins) > 0 {
		c.pinned -= e.size
	}
	delete(c.entries, k)
	delete(c.metas, k)
}

// sortedPins is a sorted copy of pins, nil when there are none.
func sortedPins(pins []string) []string {
	if len(pins) == 0 {
		return nil
	}
	out := slices.Clone(pins)
	slices.Sort(out)
	return slices.Compact(out)
}

func (c *Cache) saveIndexLocked() {
	if c.closed {
		return // the folder may belong to another cache
	}
	d := indexData{
		Entries: make(map[string]indexEntry, len(c.entries)),
		NextSeq: c.nextSeq,
		FileSeq: c.fileSeq,
		Metas:   make(map[string]StreamMeta, len(c.metas)),
	}
	for k, e := range c.entries {
		d.Entries[k] = indexEntry{File: filepath.Base(e.path), Size: e.size, AccessSeq: e.accessSeq, Used: e.used.UnixMilli(), Pins: e.pins}
	}
	for k, m := range c.metas {
		d.Metas[k] = m
	}
	if err := c.writeIndex(d); err != nil {
		pdlog.Warn("mediacache: index.json not written: %v", err)
		return
	}
	c.dirty = false
}

// writeIndex writes d as index.json through a synced temp file.
func (c *Cache) writeIndex(d indexData) error {
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.dir, "index.json.tmp*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	// The index reaches the disk before it replaces the previous one, and
	// after the data files it names were synced.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, filepath.Join(c.dir, "index.json"))
}

// Get opens a cached entry's file and returns it, its size and true; the caller closes
// it. A file of the wrong size is dropped as a miss. A hit becomes most recent, its use
// written with the next index write.
func (c *Cache) Get(key string) (*os.File, int64, bool) {
	if key == "" {
		return nil, 0, false
	}
	c.mu.Lock()
	e, ok := c.entries[key]
	if !ok || c.closed {
		c.mu.Unlock()
		return nil, 0, false
	}
	sz := e.size
	// Bump recency in memory; index.json gets it with the next commit or
	// within recencyFlush.
	c.nextSeq++
	e.accessSeq = c.nextSeq
	e.used = time.Now()
	c.entries[key] = e
	c.writeWithinLocked(recencyFlush)
	pth := e.path
	c.mu.Unlock()

	f, err := openShared(c.dir, filepath.Base(pth), os.O_RDONLY)
	if err == nil {
		if fi, serr := f.Stat(); serr != nil || fi.Size() != sz {
			_ = f.Close()
			err = errors.New("mediacache: entry file changed size")
		}
	}
	if err != nil {
		// raced with delete, vanished or damaged; drop from index
		c.mu.Lock()
		if cur, still := c.entries[key]; still && cur.path == pth {
			c.forgetLocked(key, cur)
		}
		c.mu.Unlock()
		_ = os.Remove(pth)
		return nil, 0, false
	}
	return f, sz, true
}

// Put returns a WriteCloser writing to a temp file. Only when Close returns nil is the
// entry renamed into place and visible to Get; otherwise no partial entry is left.
func (c *Cache) Put(key string) (io.WriteCloser, error) {
	if key == "" {
		return nil, errors.New("mediacache: empty key")
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errClosed
	}
	if _, busy := c.writing[key]; busy {
		c.mu.Unlock()
		return nil, fmt.Errorf("mediacache: put already in progress for %s", key)
	}
	c.writing[key] = struct{}{}
	c.mu.Unlock()

	// A previous entry for this key stays visible until the replacement commits under a
	// new file name; the old file is never overwritten, so a failed rewrite loses nothing.
	// The writing marker keeps the old entry safe from eviction.

	tmp, err := os.CreateTemp(c.dir, ".tmp-")
	if err != nil {
		c.unmarkWriting(key)
		return nil, err
	}

	return &putWriter{
		c:   c,
		key: key,
		tmp: tmp,
	}, nil
}

type putWriter struct {
	c       *Cache
	key     string
	tmp     *os.File
	closed  bool
	written int64
}

func (w *putWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, errors.New("mediacache: write after close")
	}
	n, err := w.tmp.Write(p)
	w.written += int64(n)
	return n, err
}

func (w *putWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	defer w.c.unmarkWriting(w.key)

	name := w.tmp.Name()
	_ = w.tmp.Close()

	if w.written == 0 {
		_ = os.Remove(name)
		return nil
	}

	if err := w.c.install(w.key, name, w.written, nil, nil); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// install makes the complete body at tmp the entry for key, with meta (if not nil) and
// pins (plus those of the replaced entry): renamed to a new unique name, indexed, then
// trimmed to the limit.
func (c *Cache) install(key, tmp string, size int64, meta *StreamMeta, pins []string) error {
	// The body reaches the disk before its name says it is complete.
	f, err := openShared(c.dir, filepath.Base(tmp), os.O_RDWR)
	if err != nil {
		return err
	}
	err = f.Sync()
	_ = f.Close()
	if err != nil {
		return err
	}
	// Because final never names an existing file, this rename can never
	// overwrite (and thus never fail against) a file a concurrent reader holds
	// open.
	final := c.newFilePath(key, size)
	if err := os.Rename(tmp, final); err != nil {
		return err
	}

	// Swap the index pointer to the new file under lock; move the accounting. A cache
	// closed meanwhile takes nothing: its folder may belong to another cache.
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = os.Remove(final)
		return errClosed
	}
	old, hadOld := c.entries[key]
	if hadOld {
		c.total -= old.size
		if len(old.pins) > 0 {
			c.pinned -= old.size
		}
		pins = append(slices.Clone(pins), old.pins...)
	}
	pins = sortedPins(pins)
	c.total += size
	if len(pins) > 0 {
		c.pinned += size
	}
	c.nextSeq++
	c.entries[key] = entry{path: final, size: size, accessSeq: c.nextSeq, used: time.Now(), pins: pins}
	if meta != nil {
		c.metas[key] = *meta
	}
	c.mu.Unlock()

	// Try to remove the old file, which no entry references. If a foreign reader holds it (Windows),
	// it stays as an orphan for the next New; the commit never fails on it.
	if hadOld && old.path != final {
		_ = removeFile(old.path)
	}

	c.evictIfNeeded()
	c.mu.Lock()
	if time.Since(c.lastExpire) >= expireEvery {
		c.expireLocked()
	}
	c.writeWithinLocked(installFlush)
	c.mu.Unlock()
	return nil
}

// openShared opens name in dir. On Windows the handle allows others to
// delete and rename the file, so evicting or adopting a file that a player
// still reads does not fail.
func openShared(dir, name string, flag int) (*os.File, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.OpenFile(name, flag, 0o600)
}

// NewTemp creates an empty file in the cache directory, open for reading and
// writing, to receive a body that Adopt may later make an entry. A crash
// leaves it to the next New, which deletes it.
func (c *Cache) NewTemp() (*os.File, error) { return CreateTemp(c.dir, ".tmp-") }

// CreateTemp creates a new file in dir, named as os.CreateTemp names it,
// open for reading and writing. On Windows too the file can be deleted or
// renamed while the handle is open.
func CreateTemp(dir, pattern string) (*os.File, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, err
	}
	name := f.Name()
	_ = f.Close()
	shared, err := openShared(dir, filepath.Base(name), os.O_RDWR)
	if err != nil {
		_ = os.Remove(name)
		return nil, err
	}
	return shared, nil
}

// Adopt makes the NewTemp file at path, holding size bytes, the entry for key with meta,
// in one index write. Another size, or a limit of 0, is refused and the caller keeps
// the file. The caller may go on reading it.
func (c *Cache) Adopt(key, path string, size int64, meta StreamMeta) error {
	return c.AdoptPinned(key, path, size, meta, nil)
}

// AdoptPinned is Adopt for a body kept for offline use: the entry carries
// pins from the start, so it is kept at any limit.
func (c *Cache) AdoptPinned(key, path string, size int64, meta StreamMeta, pins []string) error {
	if key == "" {
		return errors.New("mediacache: empty key")
	}
	c.mu.Lock()
	closed, unkept := c.closed, c.max == 0 && len(pins) == 0
	c.mu.Unlock()
	if closed {
		return errClosed
	}
	if unkept {
		return errors.New("mediacache: size 0 keeps only pinned entries")
	}
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if size <= 0 || fi.Size() != size {
		return fmt.Errorf("mediacache: body of %d bytes, declared %d", fi.Size(), size)
	}
	return c.install(key, path, size, &meta, pins)
}

func (c *Cache) unmarkWriting(key string) {
	c.mu.Lock()
	delete(c.writing, key)
	c.mu.Unlock()
}

// evictIfNeeded removes least-recent unpinned entries (by accessSeq) until the
// unpinned bytes are under the limit. It skips keys that are currently being
// written and files that cannot be deleted.
func (c *Cache) evictIfNeeded() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.total-c.pinned <= c.max {
		return
	}
	// The evictable entries once, least recent first.
	victims := make([]string, 0, len(c.entries))
	for k, e := range c.entries {
		if _, busy := c.writing[k]; !busy && len(e.pins) == 0 {
			victims = append(victims, k)
		}
	}
	slices.SortFunc(victims, func(a, b string) int { return cmp.Compare(c.entries[a].accessSeq, c.entries[b].accessSeq) })
	for _, victim := range victims {
		if c.total-c.pinned <= c.max {
			break
		}
		e := c.entries[victim]
		if err := removeFile(e.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			// The file could not be deleted (Windows sharing violation). Keep the entry and its
			// accounting so the index matches the disk, and go on with the next victim; the next
			// commit retries this one.
			continue
		}
		c.forgetLocked(victim, e)
	}
	// the caller writes the index
}

// CheckDir reports whether dir can be chosen as the folder a cache goes in:
// an absolute path that is not a file. The cache itself lives in a subfolder
// of it and deletes nothing outside that.
func CheckDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("%s is not an absolute path", dir)
	}
	fi, err := os.Stat(dir)
	if err == nil && !fi.IsDir() {
		return fmt.Errorf("%s is a file", dir)
	}
	return nil
}

// Purge removes the entries of the cache in dir, when there is one, as
// SetMax(0) does on an open cache. For a cache that is not open in this
// process.
func Purge(dir string) error {
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	c, err := New(dir, 0)
	if err != nil {
		return err
	}
	c.SetMax(0)
	c.Close()
	return nil
}

// RemoveOldCaches removes caches kept outside an purpledisc-cache folder: the default
// <config dir>/mediacache and one directly in the chosen folder. It runs once
// (recorded in media.json), at startup before a cache opens.
func RemoveOldCaches() {
	m := config.LoadMedia()
	if m.OldCachesRemoved {
		return
	}
	if base, err := config.Dir(); err == nil {
		dir := filepath.Join(base, "mediacache")
		removeOldCache(dir)
		_ = os.Remove(dir) // only when empty
	}
	if filepath.IsAbs(m.MediaCacheDir) {
		removeOldCache(m.MediaCacheDir)
	}
	m.OldCachesRemoved = true
	_ = config.SaveMedia(m)
}

// removeOldCache removes the files an old cache's index.json in dir names, then the
// index, only when every entry names a data file of its key; otherwise dir is left
// alone.
func removeOldCache(dir string) {
	index := filepath.Join(dir, "index.json")
	b, err := os.ReadFile(index)
	if err != nil {
		return
	}
	var idx struct {
		Entries map[string]indexEntry `json:"entries"`
	}
	if json.Unmarshal(b, &idx) != nil || idx.Entries == nil {
		return
	}
	for k, ie := range idx.Entries {
		if key, _, _, ok := parseFileName(ie.File); !ok || key != k {
			return
		}
	}
	for _, ie := range idx.Entries {
		_ = os.Remove(filepath.Join(dir, ie.File))
	}
	_ = os.Remove(index)
}

// SetMax sets the limit and applies it at once: the least recent entries are
// evicted until the cache fits; 0 removes every entry not being written.
func (c *Cache) SetMax(maxBytes int64) {
	c.mu.Lock()
	c.max = maxBytes
	c.mu.Unlock()
	c.evictIfNeeded()
	c.mu.Lock()
	c.saveIndexLocked()
	c.mu.Unlock()
}

// Stats returns the current number of entries and total bytes stored, pinned
// ones included.
func (c *Cache) Stats() (entries int, bytes int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries), c.total
}

// PinnedBytes returns the bytes of the pinned entries.
func (c *Cache) PinnedBytes() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pinned
}

// SetPins makes pins[key] each entry's pins (absent keys: none) and gives entries
// without stream meta theirs from metas, in one index write. The limit then applies to
// unpinned entries.
func (c *Cache) SetPins(pins map[string][]string, metas map[string]StreamMeta) {
	c.mu.Lock()
	changed := false
	for k, e := range c.entries {
		want := sortedPins(pins[k])
		if !slices.Equal(e.pins, want) {
			if len(e.pins) > 0 {
				c.pinned -= e.size
			}
			if len(want) > 0 {
				c.pinned += e.size
			}
			e.pins = want
			c.entries[k] = e
			changed = true
		}
		if m, ok := metas[k]; ok {
			if _, has := c.metas[k]; !has {
				c.metas[k] = m
				changed = true
			}
		}
	}
	c.mu.Unlock()
	if !changed {
		return
	}
	c.evictIfNeeded()
	c.mu.Lock()
	c.saveIndexLocked()
	c.mu.Unlock()
}

// Path returns the file of key's entry, for a reader that opens it by name (a
// stored picture shown by a host). It changes only when the entry is written
// again.
func (c *Cache) Path(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || c.closed {
		return "", false
	}
	return e.path, true
}

// Pinned reports whether key's entry carries a pin.
func (c *Cache) Pinned(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries[key].pins) > 0
}

// copyPinned copies every pinned entry, with its meta and pins, into dst.
func (c *Cache) copyPinned(dst *Cache) error {
	type item struct {
		key  string
		e    entry
		meta StreamMeta
	}
	c.mu.Lock()
	var items []item
	for k, e := range c.entries {
		if len(e.pins) > 0 {
			items = append(items, item{k, e, c.metas[k]})
		}
	}
	c.mu.Unlock()
	for _, it := range items {
		if err := copyEntry(it.e.path, dst, it.key, it.e.size, it.meta, it.e.pins); err != nil {
			return err
		}
	}
	return nil
}

// copyEntry copies the body at src into a temp file of dst and adopts it
// there. A src gone meanwhile (unpinned and removed) is skipped.
func copyEntry(src string, dst *Cache, key string, size int64, meta StreamMeta, pins []string) error {
	in, err := os.Open(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := dst.NewTemp()
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = dst.AdoptPinned(key, out.Name(), size, meta, pins)
	}
	if err != nil {
		_ = os.Remove(out.Name())
	}
	return err
}

// ClearUnpinned removes every entry not pinned, and the data files no entry
// names; the pinned entries stay with their accounting.
func (c *Cache) ClearUnpinned() error {
	c.mu.Lock()
	var paths []string
	live := map[string]bool{}
	for k, e := range c.entries {
		if len(e.pins) > 0 {
			live[filepath.Base(e.path)] = true
			continue
		}
		paths = append(paths, e.path)
		c.forgetLocked(k, e)
	}
	des, _ := os.ReadDir(c.dir)
	for _, de := range des {
		if _, _, _, ours := parseFileName(de.Name()); ours && !de.IsDir() && !live[de.Name()] {
			paths = append(paths, filepath.Join(c.dir, de.Name()))
		}
	}
	c.mu.Unlock()
	for _, p := range paths {
		_ = os.Remove(p)
	}
	c.mu.Lock()
	c.saveIndexLocked()
	c.mu.Unlock()
	return nil
}

// Clear removes all entries, pinned ones too (files that cannot be deleted stay), and
// resets the cache. It does not remove the directory itself.
func (c *Cache) Clear() error {
	c.mu.Lock()
	paths := make([]string, 0, len(c.entries))
	for _, e := range c.entries {
		paths = append(paths, e.path)
	}
	c.entries = make(map[string]entry)
	c.metas = make(map[string]StreamMeta)
	c.total, c.pinned = 0, 0
	c.nextSeq = 1
	// Data files no entry names go too; temps belong to writers in progress,
	// and any other file is not the cache's.
	des, _ := os.ReadDir(c.dir)
	for _, de := range des {
		if _, _, _, ours := parseFileName(de.Name()); ours && !de.IsDir() {
			paths = append(paths, filepath.Join(c.dir, de.Name()))
		}
	}
	c.mu.Unlock()

	for _, p := range paths {
		_ = os.Remove(p)
	}
	_ = os.Remove(filepath.Join(c.dir, "index.json"))

	c.mu.Lock()
	c.saveIndexLocked()
	c.mu.Unlock()
	return nil
}

// PutMeta stores or overwrites the stream metadata for a key and writes index.json at
// once, with or without a body entry. Store the meta current when the body is cached.
func (c *Cache) PutMeta(key string, m StreamMeta) error {
	if key == "" {
		return errors.New("mediacache: empty key")
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errClosed
	}
	if c.metas == nil {
		c.metas = make(map[string]StreamMeta)
	}
	c.metas[key] = m
	c.saveIndexLocked()
	c.mu.Unlock()
	return nil
}

// GetMeta returns stored metadata for the key and whether it was present.
func (c *Cache) GetMeta(key string) (StreamMeta, bool) {
	if key == "" {
		return StreamMeta{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	m, ok := c.metas[key]
	return m, ok
}

// FormatRank orders Deezer stream formats by quality: FLAC above MP3_320
// above MP3_128 above MP3_64; anything else ranks lowest.
func FormatRank(format string) int {
	switch format {
	case "FLAC":
		return 4
	case "MP3_320":
		return 3
	case "MP3_128":
		return 2
	case "MP3_64":
		return 1
	}
	return 0
}

// GetMetaForTrack returns the metadata of the best format cached with a body
// under "<trackID>." (so callers do not need to know the exact format under
// which a track was cached). Returns ok=false when absent.
func (c *Cache) GetMetaForTrack(trackID string) (StreamMeta, bool) {
	if trackID == "" {
		return StreamMeta{}, false
	}
	pref := trackID + "."
	c.mu.Lock()
	defer c.mu.Unlock()
	var best StreamMeta
	found := false
	for k, m := range c.metas {
		if _, body := c.entries[k]; body && strings.HasPrefix(k, pref) &&
			(!found || FormatRank(m.Format) > FormatRank(best.Format)) {
			best, found = m, true
		}
	}
	return best, found
}
