package audio

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
)

// ErrWrite marks a fetch that failed because its stream could not be written
// (a full disk).
var ErrWrite = errors.New("the stream could not be written")

// Fetch downloads plan's stream into c the way a play does (into a cache
// temp file, resuming a torn transfer, refreshing an expired URL, adopted only
// at its declared size), without decoding it, and returns once the body is
// c's entry or the fetch failed. Cancelling ctx stops it. A write failure is
// ErrWrite with the reason.
func Fetch(ctx context.Context, plan *deezer.StreamPlan, c StreamCache) error {
	s := &source{ring: newPCMRing(0), cache: c}
	s.pl.Store(newPlayPlan(plan))
	s.sb = newStreamFile(nil) // nothing reads it back: no decryptor
	s.cond = sync.NewCond(&s.mu)
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.seekTo.Store(-1)
	if s.cacheKey(s.plan()) == "" {
		return errors.New("only full encrypted tracks are stored (a preview or a plain stream is not)")
	}
	stop := context.AfterFunc(ctx, s.kill)
	s.download()
	stop()
	s.kill() // closes the stream file: a body not adopted goes with it
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.writeFailed.Load() {
		return fmt.Errorf("%w: %s", ErrWrite, s.lastErr())
	}
	if e := s.lastErr(); e != "" {
		return errors.New(e)
	}
	f, _, ok := c.Get(s.cacheKey(s.plan()))
	if !ok {
		return errors.New("the stream ended before its declared length")
	}
	return f.Close()
}
