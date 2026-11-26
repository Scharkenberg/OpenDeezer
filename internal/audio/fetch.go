package audio

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
)

// ErrWrite marks a fetch that failed because its stream could not be written
// (a full disk).
var ErrWrite = errors.New("the stream could not be written")

// Fetch downloads plan's stream into c as a play does (temp file, resume, URL refresh,
// adopted only at its declared size) without decoding it, and returns when the body is
// c's entry or the fetch failed. Canceling ctx stops it; a write failure is ErrWrite,
// one that ended on a lost network satisfies deezer.IsNoNetwork.
func Fetch(ctx context.Context, plan *deezer.StreamPlan, c StreamCache) error {
	s := &source{ring: newPCMRing(0), cache: c}
	s.pl.Store(plan)
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
		if s.errNet.Load() {
			return fmt.Errorf("%w: %s", deezer.ErrNoNetwork, e)
		}
		return errors.New(e)
	}
	f, _, ok := c.Get(s.cacheKey(s.plan()))
	if !ok {
		return errors.New("the stream ended before its declared length")
	}
	return f.Close()
}
