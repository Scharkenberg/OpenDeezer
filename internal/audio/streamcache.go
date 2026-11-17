package audio

import (
	"os"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/mediacache"
)

// StreamCache is an optional on-disk cache of raw CDN stream bodies, keyed by
// "trackID.format" (e.g. "12345.MP3_320"). The stored bytes are exactly what
// the CDN served (still stripe-encrypted ciphertext for tracks), so nothing
// decrypted ever lands on disk; the player decrypts as it reads.
// *mediacache.Cache satisfies this interface; the audio package uses only
// the interface and mediacache's StreamMeta, so the cache stays a pluggable
// opt-in.
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

// SetStreamCache attaches an optional raw-stream cache used by every source
// created afterwards (Play/Preload). nil (the default) disables caching.
// Only full encrypted track streams are cached; previews and plain passthrough
// streams (podcasts) never touch the cache. Safe at any time, also while a
// track plays: that track keeps the cache it started with.
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
