package engine

import (
	"testing"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/audio"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
)

// usePlayer makes p the engine's player for the test.
func usePlayer(t *testing.T, p *audio.Player) {
	t.Helper()
	mu.Lock()
	prev := player
	player = p
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		player = prev
		mu.Unlock()
	})
}

// The playback setters apply and save quality, gapless, crossfade and
// normalization in playback.json; the getters read them back.
func TestPlaybackSettingsSaved(t *testing.T) {
	cacheProfile(t)
	usePlayer(t, &audio.Player{})
	SetQuality(2)
	SetGapless(false)
	SetCrossfadeMS(4000)
	SetReplayGain(true)
	want := config.Playback{Quality: 2, Gapless: false, CrossfadeMS: 4000, ReplayGain: true}
	if pb, ok := config.LoadPlayback(); !ok || pb != want {
		t.Fatalf("playback.json %+v %v, want %+v", pb, ok, want)
	}
	if Quality() != 2 || Gapless() || CrossfadeMS() != 4000 || !ReplayGain() {
		t.Fatalf("getters %d %v %d %v", Quality(), Gapless(), CrossfadeMS(), ReplayGain())
	}
}

// Until playback.json exists the values in force stand (a host applied its
// own); the first setter writes them with its change.
func TestFirstPlaybackSetterMigrates(t *testing.T) {
	cacheProfile(t)
	p := &audio.Player{}
	p.SetGapless(true)
	p.SetReplayGain(true)
	usePlayer(t, p)
	if _, ok := config.LoadPlayback(); ok {
		t.Fatal("playback.json exists before any setter")
	}
	SetCrossfadeMS(3000)
	want := config.Playback{Quality: Quality(), Gapless: true, CrossfadeMS: 3000, ReplayGain: true}
	if pb, _ := config.LoadPlayback(); pb != want {
		t.Fatalf("playback.json %+v, want %+v", pb, want)
	}
}

// With no playback.json, the terminal UI's older files (quality.txt,
// gapless.txt, crossfade.txt, replaygain.txt) are moved into it when the
// engine loads its settings, whichever client starts first.
func TestPlaybackLoadMigratesTUIFiles(t *testing.T) {
	cacheProfile(t)
	dir, _ := config.Dir()
	for name, v := range map[string]string{"quality.txt": "hifi\n", "gapless.txt": "0\n", "crossfade.txt": "6000\n", "replaygain.txt": "1\n"} {
		if err := config.WriteFile(name, v); err != nil {
			t.Fatal(err)
		}
	}
	p := &audio.Player{}
	p.SetGapless(true)
	applyPlayback(p, nil)
	want := config.Playback{Quality: 2, Gapless: false, CrossfadeMS: 6000, ReplayGain: true}
	if pb, ok := config.LoadPlayback(); !ok || pb != want {
		t.Fatalf("playback.json %+v %v in %s, want the migrated %+v", pb, ok, dir, want)
	}
	if Quality() != 2 || p.Gapless() || p.CrossfadeMS() != 6000 || !p.ReplayGain() {
		t.Fatalf("applied %d %v %d %v", Quality(), p.Gapless(), p.CrossfadeMS(), p.ReplayGain())
	}
}

// The engine applies playback.json to a new player and to the client of each
// login.
func TestPlaybackFileApplied(t *testing.T) {
	fd := offlineEngine(t, "1001")
	_ = fd
	if err := config.SavePlayback(config.Playback{Quality: 2, Gapless: false, CrossfadeMS: 5000, ReplayGain: true}); err != nil {
		t.Fatal(err)
	}
	p := &audio.Player{}
	p.SetGapless(true)
	applyPlayback(p, nil)
	if p.Gapless() || p.CrossfadeMS() != 5000 || !p.ReplayGain() {
		t.Fatalf("player %v %d %v", p.Gapless(), p.CrossfadeMS(), p.ReplayGain())
	}
	t.Cleanup(Logout)
	if !Init("1001") || Client().Quality() != 2 || Quality() != 2 {
		t.Fatalf("client quality %d, engine %d after a login", Client().Quality(), Quality())
	}
}
