package audio

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/config"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/mediacache"
)

// readWindow is each reader's buffer: the only copy of the stream in memory.
const readWindow = 64 << 10

// streamFile holds one track's stream in a file as received (ciphertext when encrypted).
// One writer appends; readers read it as decrypted io.ReadSeekers, blocking until their
// bytes arrive, the writer finishes or fails, or the file closes.
type streamFile struct {
	mu   sync.Mutex
	cond *sync.Cond
	dec  *deezer.StripeCipher // nil for a plain stream
	f    *os.File             // created by the first write, or a cache file
	// cached is set when f is a cache entry opened for reading: close leaves
	// the file in place.
	cached bool
	// total is the full stream length when known (Content-Length or
	// Content-Range), else 0: a stream that ends short of it is not complete.
	total   int64
	written int64
	done    bool
	err     error
	closed  bool
}

func newStreamFile(dec *deezer.StripeCipher) *streamFile {
	b := &streamFile{dec: dec}
	b.cond = sync.NewCond(&b.mu)
	return b
}

// tempDir is where streams not backed by the cache are written:
// <cache base dir>/tmp.
func tempDir() (string, error) {
	dir, err := config.CacheBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tmp"), nil
}

// RemoveTempFiles deletes the stream files a crashed process left in the temp
// directory, and leftover podcast spill files in the system
// temp directory. Call it once at startup, before any playback.
func RemoveTempFiles() {
	if dir, err := tempDir(); err == nil {
		des, _ := os.ReadDir(dir)
		for _, de := range des {
			_ = os.Remove(filepath.Join(dir, de.Name()))
		}
	}
	old, _ := filepath.Glob(filepath.Join(os.TempDir(), "opdeezer-podcast-*.tmp"))
	for _, p := range old {
		_ = os.Remove(p)
	}
}

// write appends p at the end of the stream, creating the backing file in
// tempDir on the first write. A write after close is dropped.
func (b *streamFile) write(p []byte) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	if b.f == nil {
		dir, err := tempDir()
		if err == nil {
			err = os.MkdirAll(dir, 0o700)
		}
		if err == nil {
			// Deletable while open, so close removes it even when a reader's
			// ReadAt still holds the handle.
			b.f, err = mediacache.CreateTemp(dir, "*.part")
		}
		if err != nil {
			b.mu.Unlock()
			return err
		}
	}
	f, off := b.f, b.written
	b.mu.Unlock()
	// The only writer: the offset cannot move meanwhile, and readers read only
	// below written.
	n, err := f.WriteAt(p, off)
	b.mu.Lock()
	b.written += int64(n)
	b.cond.Broadcast()
	b.mu.Unlock()
	return err
}

// setTotal records the full stream length once it is known.
func (b *streamFile) setTotal(n int64) {
	b.mu.Lock()
	b.total = n
	b.mu.Unlock()
}

func (b *streamFile) Total() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.total
}

// finish marks the stream complete; err (nil for a clean end) is what a
// reader gets past the last byte. A first error is never replaced.
func (b *streamFile) finish(err error) {
	b.mu.Lock()
	b.done = true
	if b.err == nil {
		b.err = err
	}
	b.cond.Broadcast()
	b.mu.Unlock()
}

// close wakes every waiting reader (they get io.ErrClosedPipe), and closes
// and deletes the file (a cache entry stays).
func (b *streamFile) close() {
	b.mu.Lock()
	b.closed = true
	b.cond.Broadcast()
	f, cached := b.f, b.cached
	b.mu.Unlock()
	if f != nil {
		_ = f.Close()
		if !cached {
			_ = os.Remove(f.Name())
		}
	}
}

// waitWatermark blocks until n bytes have arrived, or the stream finished or
// closed. The decoder starts on this margin instead of the whole stream.
func (b *streamFile) waitWatermark(n int64) {
	b.mu.Lock()
	for b.written < n && !b.done && !b.closed {
		b.cond.Wait()
	}
	b.mu.Unlock()
}

// waitDone blocks until the whole stream has arrived (or it closed).
func (b *streamFile) waitDone() {
	b.mu.Lock()
	for !b.done && !b.closed {
		b.cond.Wait()
	}
	b.mu.Unlock()
}

// streamReader reads a streamFile from its own position through a window of
// readWindow decrypted bytes.
type streamReader struct {
	b   *streamFile
	pos int64
	buf []byte
	off int64 // stream offset of buf[0]
	n   int   // valid bytes in buf
}

func (b *streamFile) reader() io.ReadSeeker {
	return &streamReader{b: b, buf: make([]byte, readWindow)}
}

func (r *streamReader) Read(p []byte) (int, error) {
	if r.pos < r.off || r.pos >= r.off+int64(r.n) {
		b := r.b
		b.mu.Lock()
		// end is how far the stream can be read: an encrypted chunk decrypts
		// only whole, so up to the last full chunk unless the stream arrived
		// complete (no error, and all of a known length).
		var end int64
		for {
			end = b.written
			complete := b.done && b.err == nil && (b.total == 0 || b.written >= b.total)
			if b.dec != nil && !complete {
				end -= end % deezer.StripeChunk
			}
			if end > r.pos || b.done || b.closed {
				break
			}
			b.cond.Wait()
		}
		f, closed, err := b.f, b.closed, b.err
		b.mu.Unlock()
		if closed {
			return 0, io.ErrClosedPipe
		}
		if r.pos >= end {
			if err != nil {
				return 0, err
			}
			return 0, io.EOF
		}
		start := r.pos - r.pos%deezer.StripeChunk
		n := int(min(int64(len(r.buf)), end-start))
		got, rerr := f.ReadAt(r.buf[:n], start)
		if got < n {
			r.n = 0
			b.mu.Lock()
			closed = b.closed
			b.mu.Unlock()
			if closed {
				return 0, io.ErrClosedPipe
			}
			if rerr == io.EOF {
				rerr = io.ErrUnexpectedEOF
			}
			return 0, rerr
		}
		if b.dec != nil {
			b.dec.DecryptAt(r.buf[:n], start)
		}
		r.off, r.n = start, n
	}
	c := copy(p, r.buf[r.pos-r.off:r.n])
	r.pos += int64(c)
	return c, nil
}

// Size is the stream's declared length, or what arrived once it is complete; 0 when
// not known yet.
func (r *streamReader) Size() int64 {
	b := r.b
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.total > 0 {
		return b.total
	}
	if b.done && b.err == nil {
		return b.written
	}
	return 0
}

// Seek blocks until the target has arrived (or the stream ended or closed);
// a target past the end resolves to the end. io.SeekEnd is refused: dr_flac's
// seek from the end is answered from Size by its source (flac_cgo.go) and never
// reaches here.
func (r *streamReader) Seek(off int64, whence int) (int64, error) {
	b := r.b
	b.mu.Lock()
	defer b.mu.Unlock()
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = off
	case io.SeekCurrent:
		abs = r.pos + off
	default:
		return r.pos, errors.New("audio: stream seek from the end")
	}
	if abs < 0 {
		abs = 0
	}
	for b.written < abs && !b.done && !b.closed {
		b.cond.Wait()
	}
	if abs > b.written {
		abs = b.written
	}
	r.pos = abs
	return abs, nil
}
