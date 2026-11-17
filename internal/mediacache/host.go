package mediacache

import (
	"errors"
	"os"
	"sync"
	"sync/atomic"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
)

// Host runs the media cache as the engine hosts do (the desktop library, the
// mobile binding and the terminal UI): media.json's size, age and folder
// applied at once, and the cache in use handed to Attach whenever it changes.
type Host struct {
	// Attach receives the cache to use from now on, nil when it closes.
	Attach func(*Cache)
	// Pinned reports whether anything is pinned for offline use: the cache
	// then stays open at a size of 0. nil: nothing is ever pinned.
	Pinned func() bool

	mu     sync.Mutex // serializes the settings
	moveMu sync.Mutex // serializes SetDir, whose copy runs outside mu
	cur    atomic.Pointer[Cache]
}

// Current is the cache in use, nil while the size is 0 and nothing is pinned.
func (h *Host) Current() *Cache { return h.cur.Load() }

// Open applies media.json's size and the pins: the cache is open while the
// size is above 0 or something is pinned. Call it at startup and whenever the
// pins come or go.
func (h *Host) Open() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.applyLocked(config.LoadMedia().MediaCacheMB)
}

func (h *Host) pinned() bool { return h.Pinned != nil && h.Pinned() }

// SetMB saves a size of mb megabytes and applies it at once: a lower size
// evicts the least recent tracks; 0 (or less) removes every track not pinned,
// also from a folder not opened in this session, and stops caching.
func (h *Host) SetMB(mb int) error {
	mb = max(mb, 0)
	h.mu.Lock()
	defer h.mu.Unlock()
	m := config.LoadMedia()
	m.MediaCacheMB = mb
	if err := config.SaveMedia(m); err != nil {
		return err
	}
	wasOpen := h.cur.Load() != nil
	if err := h.applyLocked(mb); err != nil {
		return err
	}
	if mb == 0 && !wasOpen {
		dir, err := m.CacheStoreDir()
		if err != nil {
			return err
		}
		return Purge(dir)
	}
	return nil
}

// applyLocked makes the cache match a size of mb megabytes: it opens one (in
// media.json's folder, with its age) when needed, applies the limit to an
// open one, and with 0 removes its unpinned entries, and closes and detaches
// it when nothing is pinned.
func (h *Host) applyLocked(mb int) error {
	c := h.cur.Load()
	want := mb > 0 || h.pinned()
	switch {
	case c != nil && want:
		c.SetMax(int64(mb) << 20)
	case c != nil:
		c.SetMax(0)
		c.Close()
		h.use(nil)
	case want:
		m := config.LoadMedia()
		dir, err := m.CacheStoreDir()
		if err != nil {
			return err
		}
		if c, err = New(dir, int64(mb)<<20); err != nil {
			return err
		}
		h.use(c) // Attach gives the entries their pins, which the age never removes
		c.SetMaxAge(m.CacheMaxAge())
	}
	return nil
}

// use makes c the cache in use and hands it to Attach.
func (h *Host) use(c *Cache) {
	h.cur.Store(c)
	if h.Attach != nil {
		h.Attach(c)
	}
}

// SetMaxAgeDays saves the age limit (0 or less keeps tracks at any age) and
// applies it at once.
func (h *Host) SetMaxAgeDays(days int) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	m := config.LoadMedia()
	m.MediaCacheMaxAgeDays = max(days, 0)
	if err := config.SaveMedia(m); err != nil {
		return err
	}
	if c := h.cur.Load(); c != nil {
		c.SetMaxAge(m.CacheMaxAge())
	}
	return nil
}

// Clear removes every cached track not pinned, and with includePinned the
// pinned ones too.
func (h *Host) Clear(includePinned bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.cur.Load()
	switch {
	case c == nil:
		return nil
	case includePinned:
		return c.Clear()
	default:
		return c.ClearUnpinned()
	}
}

// Stats is the cache's state for the hosts' settings pages.
type Stats struct {
	Entries     int    `json:"entries"`
	Bytes       int64  `json:"bytes"`       // every stored track, pinned ones included
	PinnedBytes int64  `json:"pinnedBytes"` // tracks kept for offline use
	MaxBytes    int64  `json:"maxBytes"`    // the size; 0 = off
	Dir         string `json:"dir"`         // the folder picked, also while off
}

// Stats reports the cache's state.
func (h *Host) Stats() Stats {
	h.mu.Lock()
	defer h.mu.Unlock()
	m := config.LoadMedia()
	st := Stats{MaxBytes: int64(m.MediaCacheMB) << 20}
	st.Dir, _ = m.CacheDir()
	if c := h.cur.Load(); c != nil {
		st.Entries, st.Bytes = c.Stats()
		st.PinnedBytes = c.PinnedBytes()
	}
	return st
}

// SetDir moves the cache to the subfolder opendeezer-cache of dir, an
// absolute path ("" = the default folder). The pinned entries are copied
// there; then the old cache is cleared, so the new one holds only them. A
// relative path or a file is refused; on any error (a copy too) the old
// folder stays in use. The copy runs without holding the other settings:
// Stats, Clear, SetMB and the pins' Open answer meanwhile, on the old folder.
func (h *Host) SetDir(dir string) error {
	h.moveMu.Lock()
	defer h.moveMu.Unlock()
	// Under the settings lock: check the folder and open the new cache.
	var prev, next *Cache
	err := func() error {
		h.mu.Lock()
		defer h.mu.Unlock()
		m := config.LoadMedia()
		oldDir, err := m.CacheDir()
		if err != nil {
			return err
		}
		m.MediaCacheDir = dir
		newDir, err := m.CacheDir()
		if err != nil {
			return err
		}
		// The same folder under another spelling (case, a link) changes nothing.
		oldFi, oldErr := os.Stat(oldDir)
		newFi, newErr := os.Stat(newDir)
		if oldErr == nil && newErr == nil && os.SameFile(oldFi, newFi) {
			return config.SaveMedia(m)
		}
		if err := CheckDir(newDir); err != nil {
			return err
		}
		if prev = h.cur.Load(); prev == nil {
			return config.SaveMedia(m)
		}
		store, err := m.CacheStoreDir()
		if err != nil {
			return err
		}
		next, err = New(store, int64(m.MediaCacheMB)<<20)
		return err
	}()
	if err != nil || next == nil {
		return err
	}
	err = prev.copyPinned(next)
	h.mu.Lock()
	defer h.mu.Unlock()
	if err == nil && h.cur.Load() != prev {
		err = errors.New("mediacache: the cache closed during the move")
	}
	m := config.LoadMedia() // the size and age as set now, also during the copy
	m.MediaCacheDir = dir
	if err == nil {
		err = config.SaveMedia(m)
	}
	if err != nil {
		_ = next.Clear()
		next.Close()
		return err
	}
	next.SetMax(int64(m.MediaCacheMB) << 20)
	next.SetMaxAge(m.CacheMaxAge())
	_ = prev.Clear()
	prev.Close()
	h.use(next)
	return nil
}
