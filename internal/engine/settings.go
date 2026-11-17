package engine

// Playback settings the engine keeps for every host: the stream quality,
// gapless, crossfade and loudness normalization, in playback.json (shared
// with the terminal UI). They are applied when the player is made and at each
// login; every setter saves them. Until the file exists the player's and the
// engine's own values stand, and the first setter writes them with its change.

import (
	"sync/atomic"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/audio"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
	odlog "github.com/Cycl0o0/OpenDeezer/v3/internal/log"
)

// quality is the stream quality (0..2), kept also before a login.
var quality atomic.Int32

// applyPlayback applies playback.json, when it exists, to p and c (either may
// be nil); a client gets the quality in force either way.
func applyPlayback(p *audio.Player, c *deezer.Client) {
	if pb, ok := config.LoadPlayback(); ok {
		quality.Store(int32(clampQuality(pb.Quality)))
		if p != nil {
			p.SetGapless(pb.Gapless)
			p.SetCrossfadeMS(pb.CrossfadeMS)
			p.SetReplayGain(pb.ReplayGain)
		}
	}
	if c != nil {
		c.SetQuality(Quality())
	}
}

func clampQuality(q int) int { return min(max(q, deezer.QualityNormal), deezer.QualityLossless) }

// savePlayback saves playback.json with edit applied.
func savePlayback(edit func(*config.Playback)) {
	current := func() config.Playback {
		pb := config.Playback{Quality: Quality(), Gapless: true}
		if p := Player(); p != nil {
			pb.Gapless, pb.CrossfadeMS, pb.ReplayGain = p.Gapless(), p.CrossfadeMS(), p.ReplayGain()
		}
		return pb
	}
	if err := config.UpdatePlayback(current, edit); err != nil {
		odlog.Warn("playback.json: %v", err)
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
