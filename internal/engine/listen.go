package engine

// Deezer's listen log for local playback: every local start (play, auto-advance,
// promote, episode) is reported with the listen that ended before it (SetOnListenEnd).
// Connect devices log their own plays.

import (
	"sync/atomic"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/history"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/listens"
)

var (
	listenLog = listens.New(func() listens.Sender {
		if c := netClient(); c != nil {
			return c
		}
		return nil
	})

	// listenCtx is the list the host plays from (SetListenContext).
	listenCtx atomic.Pointer[listens.Context]
)

// SetListenContext sets the page type ("playlist_page", "album_page", ...) and id the
// listen log gets for every start until the next call; "" for both clears it.
func SetListenContext(pageType, id string) {
	listenCtx.Store(&listens.Context{Type: pageType, ID: id})
}

// learnRelease runs after each login; tests replace it, as it reaches deezer.com.
var learnRelease = deezer.KeepWebRelease

// listenStarted reports the start of the playing track to the listen log.
func listenStarted() {
	t, kind := currentTrackSnapshot()
	var ctx listens.Context
	if c := listenCtx.Load(); c != nil {
		ctx = *c
	}
	queueMu.Lock()
	shuffle, repeat := engineQ.Shuffle(), int(engineQ.Repeat())
	queueMu.Unlock()
	listenLog.Started(listens.Start{ID: t.ID, Episode: kind == history.KindEpisode, Context: ctx, Shuffle: shuffle, Repeat: repeat})
}
