package audio

import (
	"math"
	"testing"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
)

// Deezer's Android app turns a track down to the account's target loudness
// only when it is louder than that, and leaves quieter tracks and tracks with
// no gain as they are.
func TestReplayGainFactor(t *testing.T) {
	cases := []struct {
		name               string
		gain, target, want float64
	}{
		{"no gain known", 0, -15, 1},
		{"louder than the target", -7.4, -15, math.Pow(10, -7.6/20)},
		{"louder still", -6.5, -15, math.Pow(10, -8.5/20)},
		{"at the target", -15, -15, 1},
		{"quieter than the target", -20, -15, 1},
		{"no target before a login", -7, 0, 1},
		{"another target", -7.4, -20, math.Pow(10, -12.6/20)},
		{"a positive gain", 3, -15, math.Pow(10, -18.0/20)},
	}
	for _, c := range cases {
		if got := replayGainFactor(c.gain, c.target); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: replayGainFactor(%v, %v) = %v, want %v", c.name, c.gain, c.target, got, c.want)
		}
	}
}

// The player applies the target it was given to every track's gain.
func TestPlayerReplayGainTarget(t *testing.T) {
	p := &Player{}
	p.SetReplayGainTarget(-15)
	if got := p.ReplayGainTarget(); got != -15 {
		t.Fatalf("target %v, want -15", got)
	}
	if got, want := p.rgFactor(&deezer.StreamPlan{GainDB: -7.4}), math.Pow(10, -7.6/20); math.Abs(got-want) > 1e-9 {
		t.Errorf("factor %v, want %v", got, want)
	}
}
