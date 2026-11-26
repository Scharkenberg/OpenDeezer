package audio

import (
	"os"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/mediacache"
)

// StreamCache is an optional on-disk cache of raw CDN bodies keyed "trackID.format",
// stored as served (ciphertext for tracks). *mediacache.Cache implements it.
type StreamCache interface {
	// Get opens the cached body's file and returns it with its size when key
	// is present. The caller must close the file and must not delete it.
	Get(key string) (*os.File, int64, bool)
	// NewTemp creates an empty file in the cache, open for reading and
	// writing, for a stream to be downloaded into.
	NewTemp() (*os.File, error)
	// Adopt makes the NewTemp file at path the entry for key, with meta as
	// its stream fields, when it holds exactly size bytes, else refuses it.
	// The caller's handle keeps working.
	Adopt(key, path string, size int64, meta mediacache.StreamMeta) error
}

// cacheRef holds the attached StreamCache for an atomic.Pointer.
type cacheRef struct{ c StreamCache }

// SetStreamCache sets the raw-stream cache for sources created afterwards (nil turns it
// off). Only full encrypted tracks are cached. A playing track keeps the cache it
// started with.
func (p *Player) SetStreamCache(c StreamCache) {
	if c == nil {
		p.streamCache.Store(nil)
	} else {
		p.streamCache.Store(&cacheRef{c})
	}
}

// cache returns the attached StreamCache, nil when none.
func (p *Player) cache() StreamCache {
	if r := p.streamCache.Load(); r != nil {
		return r.c
	}
	return nil
}
