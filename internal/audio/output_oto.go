//go:build darwin

package audio

import (
	"fmt"

	"github.com/ebitengine/oto/v3"
)

// otoOutput plays through oto (AudioToolbox via purego) on all macOS builds, where
// malgo's CoreAudio callback plays choppy. oto has no output-device selection; Linux
// and Windows use malgo.
type otoOutput struct {
	ctx    *oto.Context
	player *oto.Player
}

func newOutput() (output, error) {
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   sampleRate,
		ChannelCount: channels,
		Format:       oto.FormatSignedInt16LE,
	})
	if err != nil {
		return nil, fmt.Errorf("audio init: %w", err)
	}
	<-ready
	return &otoOutput{ctx: ctx}, nil
}

func (o *otoOutput) start(read func(out []byte) int) error {
	o.player = o.ctx.NewPlayer(&otoReader{read: read})
	o.player.Play()
	return nil
}

// otoReader adapts the player's pull function to oto's io.Reader. read fills the whole
// buffer (zeroing any tail) and never returns EOF, so oto plays silence when idle.
type otoReader struct{ read func(out []byte) int }

func (r *otoReader) Read(p []byte) (int, error) {
	r.read(p)
	return len(p), nil
}

// oto plays the system default device only.
func (o *otoOutput) devices() ([]Device, error) {
	return []Device{{ID: "", Name: "System default", IsDefault: true}}, nil
}
func (o *otoOutput) setDevice(string) error { return nil }
func (o *otoOutput) currentDevice() string  { return "" }
func (o *otoOutput) deviceDown() bool       { return false }

// suspend releases/restores the audio context (used on audio-focus loss, e.g.
// iOS interruptions) without dropping the queued player.
func (o *otoOutput) suspend(on bool) error {
	if o.ctx == nil {
		return nil
	}
	if on {
		return o.ctx.Suspend()
	}
	return o.ctx.Resume()
}

// oto follows the system default device on a device change itself, so there is
// nothing for the player to recover from here.
func (o *otoOutput) setLostHandler(func(string)) {}

func (o *otoOutput) latencyFrames() int {
	return sampleRate / 2
}

func (o *otoOutput) close() {
	if o.player != nil {
		_ = o.player.Close()
	}
}
