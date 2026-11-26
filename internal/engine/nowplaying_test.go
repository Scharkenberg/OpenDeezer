package engine

import (
	"strings"
	"testing"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/control"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
)

// NowPlayingVersion moves exactly when NowPlayingJSON's payload changes: a
// new track, its metadata arriving, a routed device's track coming and going,
// the route ending; not for the same track again nor a pause.
func TestNowPlayingVersionFollowsPayload(t *testing.T) {
	clearRoutedRemote()
	t.Cleanup(clearRoutedRemote)
	t.Cleanup(func() { setCurrentTrack(deezer.Track{}) })
	setCurrentTrack(deezer.Track{})
	step := func(what string, change func(), moves bool, want string) {
		t.Helper()
		before := NowPlayingVersion()
		change()
		if got := NowPlayingVersion() != before; got != moves {
			t.Fatalf("%s: version moved %v, want %v", what, got, moves)
		}
		if js := NowPlayingJSON(); !strings.Contains(js, want) {
			t.Fatalf("%s: payload %s, want %s in it", what, js, want)
		}
	}
	var gen uint64
	step("a track", func() { gen = setCurrentTrack(deezer.Track{ID: "1"}) }, true, `"id":"1"`)
	step("the same track", func() { gen = setCurrentTrack(deezer.Track{ID: "1"}) }, false, `"id":"1"`)
	step("its metadata", func() { setCurrentTrackAt(gen, deezer.Track{ID: "1", Name: "One"}) }, true, `"name":"One"`)
	step("a pause", func() { Pause() }, false, `"name":"One"`)

	rc := control.NewClient("http://127.0.0.1:1", "", "")
	mu.Lock()
	remoteCli = rc
	mu.Unlock()
	step("a device's track", func() { setRemoteState(rc, control.State{State: "playing", Track: &control.Track{ID: "2"}}) }, true, `"id":"2"`)
	step("the device's same state", func() { setRemoteState(rc, control.State{State: "playing", Track: &control.Track{ID: "2"}}) }, false, `"id":"2"`)
	step("the device stopping", func() { setRemoteState(rc, control.State{State: "stopped"}) }, true, `{}`)
	step("the route ending", DisconnectDevice, true, `"name":"One"`)
}
