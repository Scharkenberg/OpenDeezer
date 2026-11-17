package audio

import "sync"

// pcmRing is a fixed-capacity circular FIFO of decoded interleaved s16 PCM. The
// decode goroutine writes (blocking while full, which paces decoding); the audio
// callback reads (non-blocking — a short read is an underrun the caller pads
// with silence). It is a true ring buffer: read/write touch only the bytes moved
// (no full-buffer memmove), so the lock the realtime audio callback contends for
// is held only briefly — the earlier slice-shift implementation held the lock
// for an O(buffer) copy on every callback and starved the producer, causing
// choppy playback. flush() drops buffered PCM and bumps a sequence so an
// in-flight write for the pre-seek position is discarded.
type pcmRing struct {
	mu       sync.Mutex
	cond     *sync.Cond
	buf      []byte
	head     int // read index
	size     int // bytes currently buffered
	flushSeq uint64
	closed   bool
}

func newPCMRing(capacity int) *pcmRing {
	r := &pcmRing{buf: make([]byte, capacity)}
	r.cond = sync.NewCond(&r.mu)
	return r
}

// write copies p into the ring, blocking until it all fits (or the ring is
// closed/flushed). Returns false if closed or flushed (seq changed) — caller
// stops/refreshes. Large p is written in capacity-sized waves as room frees.
func (r *pcmRing) write(p []byte, seq uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for len(p) > 0 {
		for r.size == len(r.buf) && !r.closed && seq == r.flushSeq {
			r.cond.Wait()
		}
		if r.closed || seq != r.flushSeq {
			return false
		}
		n := len(r.buf) - r.size // free space
		if n > len(p) {
			n = len(p)
		}
		tail := (r.head + r.size) % len(r.buf)
		c := copy(r.buf[tail:], p[:n]) // up to wrap
		if c < n {
			copy(r.buf, p[c:n]) // wrapped remainder
		}
		r.size += n
		p = p[n:]
		r.cond.Broadcast()
	}
	return true
}

// read copies up to len(p) bytes into p and returns the count (may be < len(p)).
func (r *pcmRing) read(p []byte) int {
	r.mu.Lock()
	n := r.size
	if n > len(p) {
		n = len(p)
	}
	if n > 0 {
		c := copy(p[:n], r.buf[r.head:]) // up to wrap
		if c < n {
			copy(p[c:n], r.buf) // wrapped remainder
		}
		r.head = (r.head + n) % len(r.buf)
		r.size -= n
		r.cond.Broadcast()
	}
	r.mu.Unlock()
	return n
}

// buffered reports how many PCM bytes are queued.
func (r *pcmRing) buffered() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.size
}

// flush empties the ring and returns the new sequence number.
func (r *pcmRing) flush() uint64 {
	r.mu.Lock()
	r.head, r.size = 0, 0
	r.flushSeq++
	s := r.flushSeq
	r.cond.Broadcast()
	r.mu.Unlock()
	return s
}

func (r *pcmRing) seq() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.flushSeq
}

func (r *pcmRing) close() {
	r.mu.Lock()
	r.closed = true
	r.cond.Broadcast()
	r.mu.Unlock()
}
