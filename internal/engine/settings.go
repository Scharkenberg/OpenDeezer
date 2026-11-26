package engine

// Playback settings kept for every host in playback.json (shared with the TUI):
// quality, gapless, crossfade, loudness normalization and single-device playback.
// Applied when the player is made and at each login; every setter saves them.

import (
	"sync/atomic"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/audio"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/config"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	pdlog "github.com/Scharkenberg/PurpleDisc/v4/internal/log"
)

// quality is the stream quality (0..2), kept also before a login.
var quality atomic.Int32

// applyPlayback applies playback.json, when it exists, to p and c (either may be nil).
// The client always gets the quality in force; the player always gets the account's
// target loudness, which is then saved when playback.json exists and holds another.
func applyPlayback(p *audio.Player, c *deezer.Client) {
	pb, ok := config.LoadPlayback()
	if ok {
		quality.Store(int32(clampQuality(pb.Quality)))
		playAlongside.Store(pb.PlayAlongside)
		if p != nil {
			p.SetGapless(pb.Gapless)
			p.SetCrossfadeMS(pb.CrossfadeMS)
			p.SetReplayGain(pb.ReplayGain)
			p.SetReplayGainTarget(pb.GainTarget)
		}
	}
	if c != nil {
		c.SetQuality(Quality())
		target := c.GainTarget()
		withPlayer(func(p *audio.Player) { p.SetReplayGainTarget(target) })
		if ok && pb.GainTarget != target {
			savePlayback(func(pb *config.Playback) { pb.GainTarget = target })
		}
	}
}

func clampQuality(q int) int { return min(max(q, deezer.QualityNormal), deezer.QualityLossless) }

// savePlayback saves playback.json with edit applied.
func savePlayback(edit func(*config.Playback)) {
	current := func() config.Playback {
		pb := config.Playback{Quality: Quality(), Gapless: true, PlayAlongside: playAlongside.Load()}
		if p := Player(); p != nil {
			pb.Gapless, pb.CrossfadeMS, pb.ReplayGain, pb.GainTarget = p.Gapless(), p.CrossfadeMS(), p.ReplayGain(), p.ReplayGainTarget()
		}
		return pb
	}
	if err := config.UpdatePlayback(current, edit); err != nil {
		pdlog.Warn("playback.json: %v", err)
	}
}

// SetQuality sets and saves the stream quality: 0 MP3 128, 1 MP3 320, 2 FLAC
// (MP3 where the account or the track has no FLAC).
func SetQuality(q int) {
	q = clampQuality(q)
	quality.Store(int32(q))
	if c := Client(); c != nil {
		c.SetQuality(q)
	}
	savePlayback(func(pb *config.Playback) { pb.Quality = q })
}

// Quality returns the stream quality (0..2).
func Quality() int { return int(quality.Load()) }

// SetGapless turns gapless playback on or off and saves it.
func SetGapless(on bool) {
	withPlayer(func(p *audio.Player) { p.SetGapless(on) })
	savePlayback(func(pb *config.Playback) { pb.Gapless = on })
}

// Gapless reports whether gapless playback is on (true before Init).
func Gapless() bool {
	p := Player()
	return p == nil || p.Gapless()
}

// SetCrossfadeMS sets the crossfade length (0 = off) and saves it.
func SetCrossfadeMS(ms int) {
	withPlayer(func(p *audio.Player) { p.SetCrossfadeMS(ms) })
	savePlayback(func(pb *config.Playback) { pb.CrossfadeMS = ms })
}

// CrossfadeMS returns the crossfade length (0 before Init).
func CrossfadeMS() int {
	v := 0
	withPlayer(func(p *audio.Player) { v = p.CrossfadeMS() })
	return v
}

// SetReplayGain turns loudness normalization on or off and saves it.
func SetReplayGain(on bool) {
	withPlayer(func(p *audio.Player) { p.SetReplayGain(on) })
	savePlayback(func(pb *config.Playback) { pb.ReplayGain = on })
}

// ReplayGain reports whether loudness normalization is on.
func ReplayGain() bool {
	p := Player()
	return p != nil && p.ReplayGain()
}
