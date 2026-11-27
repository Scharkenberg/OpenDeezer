// Package pictures keeps fetched cover art in the cache folder's "pictures" directory,
// shared by every app: a CDN picture never changes under its URL (which names its
// hash), so one fetched once comes back at once afterwards, also offline. The directory
// keeps the most recently used maxSize bytes.
package pictures

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/config"
)

// maxSize bounds the cache; past it, the least recently used pictures go until it holds
// three quarters of that.
const maxSize = 64 << 20

var cache struct {
	sync.Mutex
	counted bool
	size    int64
}

// path is where url's picture is kept, "" without a cache folder.
func path(url string) string {
	base, err := config.CacheBaseDir()
	if err != nil || base == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(url))
	return filepath.Join(base, "pictures", hex.EncodeToString(sum[:20]))
}

// Load returns url's picture when it is kept, marking it as recently used.
func Load(url string) ([]byte, bool) {
	p := path(url)
	if p == "" {
		return nil, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	now := time.Now()
	_ = os.Chtimes(p, now, now)
	return b, true
}

// Keep stores url's picture (written whole, then renamed) and prunes the cache. A
// picture that cannot be kept is only fetched again next time.
func Keep(url string, b []byte) {
	p := path(url)
	if p == "" {
		return
	}
	dir := filepath.Dir(p)
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, "*.part")
	if err != nil {
		return
	}
	_, werr := tmp.Write(b)
	if cerr := tmp.Close(); werr != nil || cerr != nil || os.Rename(tmp.Name(), p) != nil {
		_ = os.Remove(tmp.Name())
		return
	}
	cache.Lock()
	defer cache.Unlock()
	if !cache.counted {
		cache.counted, cache.size = true, prune(dir, maxSize)
	} else {
		cache.size += int64(len(b))
	}
	if cache.size > maxSize {
		cache.size = prune(dir, maxSize*3/4)
	}
}

// prune removes the least recently used pictures in dir until at most keep bytes
// remain, and returns what remains.
func prune(dir string, keep int64) int64 {
	entries, _ := os.ReadDir(dir)
	files := make([]os.FileInfo, 0, len(entries))
	var size int64
	for _, e := range entries {
		if fi, err := e.Info(); err == nil && !fi.IsDir() {
			files = append(files, fi)
			size += fi.Size()
		}
	}
	slices.SortFunc(files, func(a, b os.FileInfo) int { return a.ModTime().Compare(b.ModTime()) })
	for _, fi := range files {
		if size <= keep {
			break
		}
		if os.Remove(filepath.Join(dir, fi.Name())) == nil {
			size -= fi.Size()
		}
	}
	return size
}
