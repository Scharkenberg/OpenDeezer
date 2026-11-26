// Package audio streams, decrypts and decodes Deezer audio (MP3, FLAC) into a PCM ring
// drained by an output device, with seek, ReplayGain, gapless and crossfade. Streams are
// stored encrypted and decrypted on read; the output backend is chosen by build tag.
package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	pdlog "github.com/Scharkenberg/PurpleDisc/v4/internal/log"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/mediacache"
)

// output is the platform audio sink (output_malgo.go or output_oto.go by build tag).
// start() pulls PCM via read, which fills the buffer (zeroing any tail) and returns
// the bytes it wrote.
type output interface {
	start(read func(out []byte) int) error
	devices() ([]Device, error)
	setDevice(id string) error
	currentDevice() string
	// suspend releases (on=true) / restores (on=false) the OS output without
	// tearing down the backend context; safe to call from any goroutine.
	suspend(on bool) error
	// setLostHandler registers a callback the backend invokes when the selected
	// device disappears and cannot be recovered (no-op on backends that reroute
	// themselves, e.g. oto).
	setLostHandler(func(string))
	close()
	deviceDown() bool
	latencyFrames() int
}

// Device is an output device the user can pick (empty ID = system default).
type Device struct {
	ID        string
	Name      string
	IsDefault bool
}

// State is the player's lifecycle state.
type State int

const (
	Stopped State = iota
	Loading
	Playing
	Paused
	Errored
)

func (s State) String() string {
	switch s {
	case Loading:
		return "Loading"
	case Playing:
		return "Playing"
	case Paused:
		return "Paused"
	case Errored:
		return "Error"
	default:
		return "Stopped"
	}
}

const (
	sampleRate  = 44100
	channels    = 2
	frameBytes  = channels * 2 // s16 stereo
	bytesPerSec = sampleRate * frameBytes
	ringMax     = 4 * bytesPerSec // ~4s of decoded PCM buffered (headroom vs underrun)
	prebufferB  = 2 * bytesPerSec // fill ~2s before starting (clean intro, no underrun burst)
	decodeChunk = 16 * 1024

	fadeInFrames  = sampleRate * 12 / 1000 // ~12ms anti-click ramp after a discontinuity
	fadeOutFrames = sampleRate * 12 / 1000 // ~12ms anti-click ramp before Pause/Stop/skip/seek
	sleepFadeNS   = int64(10 * time.Second) // sleep-timer fade-out window before pausing

)

// monoBase is the start of the sleep timer's clock: its deadlines are durations since it,
// so a change of the wall clock moves none of them.
var monoBase = time.Now()


// pcmStream is a decoder yielding interleaved s16 PCM, seekable by PCM byte
// offset. *mp3Stream, *flacStream and *resampleStream all satisfy it.
type pcmStream interface {
	io.Reader
	Seek(offset int64, whence int) (int64, error)
}

// ErrOffline is returned when attempting to play a track offline (empty CDN URL)
// but the track is not present in the local StreamCache.
var ErrOffline = errors.New("offline: track not cached and no CDN URL available")

// stripeCipher is the decryptor of plan's stream, nil for a plain stream.
func stripeCipher(plan *deezer.StreamPlan) *deezer.StripeCipher {
	if plan.Encrypted {
		return deezer.NewStripeCipher(plan.CipherID())
	}
	return nil
}

// source is one track's pipeline: download -> streamFile -> decrypt -> decoder ->
// pcmRing. The download and decode each run on their own goroutine.
type source struct {
	// pl is the plan the source plays (read via plan(), also from the RT callback). A
	// cached plan whose entry is gone is replaced once, before the first byte, by a
	// network plan, possibly of another format.
	pl    atomic.Pointer[deezer.StreamPlan]
	durMS int64
	// owner is the player that publishes this source's format and gain while
	// it is current (nil for a source driven without one).
	owner *Player
	// cache is the optional raw-stream cache handed down from the Player
	// (nil = off). Immutable once download() starts.
	cache   StreamCache
	sb      *streamFile
	ring    *pcmRing
	eof     atomic.Bool  // decoder reached end and ring will not grow
	seekTo  atomic.Int64 // pending PCM-byte seek target, or -1
	dead    atomic.Bool
	errMsg  atomic.Value // string: download/decode error, if any
	errNet  atomic.Bool  // that error was a lost network
	netWait atomic.Bool  // the download waits for the network with no audio left
	decoded atomic.Int64 // PCM bytes successfully decoded and written to the ring
	// writeFailed is set when the stream could not be written (a full disk):
	// the track then ends Errored with that reason, however much of it played.
	writeFailed atomic.Bool

	// heard counts the PCM bytes of this source the device played (what a
	// listen reports; a seek does not move it). RT-incremented.
	heard atomic.Int64
	// speed is the speed the source plays at (SetSpeed; speedOf).
	speed atomic.Uint64
	// pauses and seeks count the pauses and seeks while this source played.
	pauses, seeks atomic.Int32

	// xfadeConsumed counts bytes of this source's ring drained by the crossfade
	// path while it is the incoming ("next") track, so the swap can resume it at
	// the position already played instead of restarting from 0. RT-incremented.
	xfadeConsumed atomic.Int64
	// decDurMS is the length the decoder read from the stream (FLAC
	// STREAMINFO), 0 when it has none; the near-end hook prefers it to durMS.
	decDurMS atomic.Int64
	// nearFired is set once the near-end hook ran for this source while it is
	// current, and cleared when the position leaves the hook's span.
	nearFired atomic.Bool

	// mu/cond let the decode goroutine park after decoder EOF and wake on a seek
	// or kill (so seeking back into a finished-decoding track still works).
	mu   sync.Mutex
	cond *sync.Cond

	// ctx is canceled by kill() to unblock a stalled Body.Read in download().
	ctx    context.Context
	cancel context.CancelFunc
}

type finishCallback struct {
	fn func()
}

// Listen is what was heard of one track the player played.
type Listen struct {
	TrackID string // the plan's
	Format  string // the format it played in
	HeardMS int64  // the audio played, apart from what pauses and seeks skipped
	Pauses  int
	Seeks   int
	Cached  bool // its stream came from the cache
	Preview bool // the 30-second preview
}

// listenOf is what was heard of s.
func listenOf(s *source) Listen {
	pl := s.plan()
	s.sb.mu.Lock()
	cached := s.sb.cached
	s.sb.mu.Unlock()
	return Listen{TrackID: pl.TrackID, Format: pl.Format, HeardMS: s.heard.Load() * 1000 / bytesPerSec,
		Pauses: int(s.pauses.Load()), Seeks: int(s.seeks.Load()), Cached: cached, Preview: pl.Preview}
}

// nearEndHook is what OnNearEnd registered.
type nearEndHook struct {
	lead func() time.Duration
	fn   func()
}

// stateCell holds the player State. Every Store wakes the manager, which
// waits on wake while nothing needs its tick.
type stateCell struct {
	atomic.Int32
	wake chan struct{}
}

func (c *stateCell) Store(v int32) {
	c.Int32.Store(v)
	c.kick()
}

func (c *stateCell) kick() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (s *source) plan() *deezer.StreamPlan { return s.pl.Load() }

func (s *source) setErr(err error) {
	if err != nil {
		s.errMsg.Store(err.Error())
		s.errNet.Store(deezer.IsNoNetwork(err))
	}
}

func (s *source) lastErr() string {
	v, _ := s.errMsg.Load().(string)
	return v
}

// Player owns the malgo context + one output device and plays a current source,
// optionally with a preloaded next source for gapless/crossfade.
type Player struct {
	out output

	// cur/next are accessed lock-free from the realtime audio callback.
	cur  atomic.Pointer[source]
	next atomic.Pointer[source]

	state       stateCell
	played      atomic.Int64 // PCM bytes the callback has consumed from cur (position)
	totalMS     atomic.Int64
	lastErr     atomic.Value // string
	lastNet     atomic.Bool  // lastErr came from a lost network
	format      atomic.Value // string
	volume      atomic.Uint64
	gainFac     atomic.Uint64
	rgOn        atomic.Bool
	rgTarget    atomic.Uint64 // float64 bits: the target loudness in dB
	onListenEnd atomic.Pointer[func(Listen)]
	gapless     atomic.Bool
	crossfadeMS atomic.Int64
	cbCount     atomic.Int64 // audio callbacks served (diagnostics)
	cbUnderrun  atomic.Int64 // callbacks with a short read (ring starvation)
	ticks       atomic.Int64 // manage ticks run (diagnostics)
	onFinish    atomic.Pointer[finishCallback]
	nearEnd     atomic.Pointer[nearEndHook]

	// streamCache is the optional raw-stream cache (see SetStreamCache) handed
	// to each new source; it may change during playback.
	streamCache atomic.Pointer[cacheRef]

	// fadeLeft is the frames still to ramp up (anti-click fade-in) after a start, resume
	// or seek. Read by the RT callback and stored by control calls; a stale read only
	// lengthens the fade slightly.
	fadeLeft atomic.Int64
	// fadeOut is the frames still to ramp down for Pause/Stop/skip/seek, played out by the
	// RT callback. fadeOutArmed keeps the output muted from arming until the control call's
	// deferred flip. Both are atomics only.
	fadeOut      atomic.Int64
	fadeOutArmed atomic.Bool
	// mix is a reusable scratch buffer for the crossfade path so the realtime
	// callback never allocates. Accessed only from readPCM (single goroutine).
	mix []byte

	// Sleep timer (core-owned so every client shares one implementation).
	sleepArmed atomic.Bool
	sleepEOT   atomic.Bool   // end-of-track mode (else duration mode)
	sleepAtNS  atomic.Int64  // deadline for duration mode, as time since monoBase
	sleepGain  atomic.Uint64 // float64 bits: fade-out envelope, 1.0 when not fading

	// pauseWanted is a pause asked for while Loading (Pause, TogglePause, sleep
	// deadline); manage then promotes Loading to Paused. Play and Resume clear it.
	pauseWanted atomic.Bool

	// playMu serializes Play with manage's swap to the preloaded next track, so cur and
	// its published duration, format and gain belong together.
	playMu sync.Mutex

	// Equalizer + mono downmix (core-owned DSP; see eq.go). eqCfg is rebuilt and
	// swapped whole by the setters; eqPrev/eqZ are filter memory touched only
	// from the RT callback.
	eqCfg       atomic.Pointer[eqConfig]
	eqPrev      *eqConfig
	eqZ         [channels][eqBands][2]float64
	eqSaveMu    sync.Mutex
	eqSaveTimer *time.Timer

	stopMgr chan struct{}
	mgrOnce sync.Once
}

// ---- ReplayGain / volume ----

// replayGainFactor is the amplitude factor for a track of gain gainDB (Deezer's GAIN):
// as in Deezer's Android app, a track louder than the target is turned down to it; a
// quieter one, or one without gain (0), plays as is.
func replayGainFactor(gainDB, targetDB float64) float64 {
	if gainDB == 0 || gainDB <= targetDB {
		return 1
	}
	return math.Pow(10, (targetDB-gainDB)/20)
}

// rgFactor is pl's ReplayGain factor at the player's target.
func (p *Player) rgFactor(pl *deezer.StreamPlan) float64 {
	return replayGainFactor(pl.GainDB, p.ReplayGainTarget())
}

// SetReplayGainTarget sets the loudness, in dB, tracks are normalized to (the
// account's GAIN.TARGET).
func (p *Player) SetReplayGainTarget(db float64) {
	p.rgTarget.Store(math.Float64bits(db))
	if p.rgOn.Load() {
		if cur := p.cur.Load(); cur != nil {
			p.gainFac.Store(math.Float64bits(p.rgFactor(cur.plan())))
		}
	}
}

// ReplayGainTarget returns the target loudness in dB (0 until one is set).
func (p *Player) ReplayGainTarget() float64 { return math.Float64frombits(p.rgTarget.Load()) }

func (p *Player) SetReplayGain(on bool) {
	p.rgOn.Store(on)
	if on {
		// Apply the current track's gain at once, so enabling ReplayGain mid-track takes
		// effect without waiting for the next Play().
		if cur := p.cur.Load(); cur != nil {
			p.gainFac.Store(math.Float64bits(p.rgFactor(cur.plan())))
		}
	} else {
		p.gainFac.Store(math.Float64bits(1))
	}
}
func (p *Player) ReplayGain() bool { return p.rgOn.Load() }

// volumeTaper maps a 0..1 slider position to a cubic amplitude gain, matching the
// roughly logarithmic perception of loudness. The 0..1 volume API is unchanged.
func volumeTaper(v float64) float64 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 1
	}
	return v * v * v
}

// sleepGainClamped reads the sleep-timer fade envelope (1.0 when not fading),
// treating an uninitialized/garbage value as full volume.
func (p *Player) sleepGainClamped() float64 {
	sg := math.Float64frombits(p.sleepGain.Load())
	if sg < 0 || sg > 1 { // uninitialized (0 bits) or garbage -> full
		return 1
	}
	return sg
}

func (p *Player) effectiveVolume() float64 {
	f := math.Float64frombits(p.gainFac.Load())
	if f == 0 {
		f = 1
	}
	return volumeTaper(p.Volume()) * f * p.sleepGainClamped()
}

// SetGapless enables/disables gapless transitions between tracks. A change
// runs the near-end hook again (RearmNearEnd), as whether a preload is
// wanted depends on it.
func (p *Player) SetGapless(on bool) {
	if p.gapless.Swap(on) != on {
		p.RearmNearEnd()
	}
}

// Gapless reports whether gapless transitions are enabled.
func (p *Player) Gapless() bool { return p.gapless.Load() }

// SetCrossfadeMS sets the crossfade length (0 = none). A change runs the
// near-end hook again, as with SetGapless.
func (p *Player) SetCrossfadeMS(ms int) {
	if ms < 0 {
		ms = 0
	}
	if p.crossfadeMS.Swap(int64(ms)) != int64(ms) {
		p.RearmNearEnd()
	}
}
func (p *Player) CrossfadeMS() int { return int(p.crossfadeMS.Load()) }

// ---- sleep timer ----

// SetSleepTimer arms the sleep timer: with endOfTrack the player pauses when the track
// ends (d ignored), else it fades out and pauses after d. d <= 0 without endOfTrack
// cancels.
func (p *Player) SetSleepTimer(d time.Duration, endOfTrack bool) {
	if !endOfTrack && d <= 0 {
		p.CancelSleepTimer()
		return
	}
	// Disarm first so a manage() tick can't observe armed with a half-updated
	// mode/deadline (e.g. armed && !EOT while sleepAtNS is still the old 0 from a
	// previous EOT arm), which would fire the expiry branch and pause instantly.
	p.sleepArmed.Store(false)
	p.sleepGain.Store(math.Float64bits(1))
	p.sleepEOT.Store(endOfTrack)
	if !endOfTrack {
		p.sleepAtNS.Store(int64(time.Since(monoBase) + d))
	} else {
		p.sleepAtNS.Store(0)
	}
	p.sleepArmed.Store(true) // arm last, once mode + deadline are consistent
	p.state.kick()           // the manager runs the timer, also while paused
}

// CancelSleepTimer disarms the sleep timer and restores full volume.
func (p *Player) CancelSleepTimer() { p.clearSleep(); p.sleepGain.Store(math.Float64bits(1)) }

func (p *Player) clearSleep() {
	p.sleepArmed.Store(false)
	p.sleepEOT.Store(false)
	p.sleepAtNS.Store(0)
}

// SleepActive reports whether a sleep timer is armed.
func (p *Player) SleepActive() bool { return p.sleepArmed.Load() }

// SleepEndOfTrack reports whether the armed timer is in end-of-track mode.
func (p *Player) SleepEndOfTrack() bool { return p.sleepArmed.Load() && p.sleepEOT.Load() }

// SleepRemainingMS returns the milliseconds until the timer fires: for duration
// mode the time left, for end-of-track mode the current track's
// remaining time. Returns 0 when no timer is armed.
func (p *Player) SleepRemainingMS() int64 {
	if !p.sleepArmed.Load() {
		return 0
	}
	if p.sleepEOT.Load() {
		if r := p.DurationMS() - p.PositionMS(); r > 0 {
			return r
		}
		return 0
	}
	if r := (p.sleepAtNS.Load() - int64(time.Since(monoBase))) / int64(time.Millisecond); r > 0 {
		return r
	}
	return 0
}

// ---- construction ----

// NewPlayer initializes the audio output (backend chosen by build tag).
func NewPlayer() (*Player, error) {
	out, err := newOutput()
	if err != nil {
		return nil, err
	}
	p := newPlayer(out)
	p.loadEQ()
	if err := p.out.start(p.readPCM); err != nil {
		p.out.close()
		return nil, err
	}
	go p.manage()
	return p, nil
}

// newPlayer builds a Stopped player on out at full volume with gapless on. A
// device the backend loses for good fails the player, so clients stop showing
// a frozen Playing state.
func newPlayer(out output) *Player {
	p := &Player{out: out, stopMgr: make(chan struct{})}
	p.state.wake = make(chan struct{}, 1)
	p.state.Store(int32(Stopped))
	p.setLastErr("", false)
	p.format.Store("")
	p.gainFac.Store(math.Float64bits(1))
	p.sleepGain.Store(math.Float64bits(1))
	p.gapless.Store(true)
	p.setVolume(1.0)
	p.out.setLostHandler(p.Fail)
	return p
}

// Fail puts the player in the Errored state with msg as LastError, for a
// failure outside the player (a lost output device).
func (p *Player) Fail(msg string) {
	p.setLastErr(msg, false)
	p.state.Store(int32(Errored))
}

// FailErr puts the player in the Errored state with err as LastError, for a track
// that could not be resolved; a lost network is marked as such.
func (p *Player) FailErr(err error) {
	p.setLastErr(err.Error(), deezer.IsNoNetwork(err))
	p.state.Store(int32(Errored))
}

// SetError records err as LastError without changing the state: a play that
// failed before it reached the player, while the old track goes on.
func (p *Player) SetError(err error) { p.setLastErr(err.Error(), deezer.IsNoNetwork(err)) }

func (p *Player) setLastErr(msg string, network bool) {
	p.lastErr.Store(msg)
	p.lastNet.Store(network)
}

// SetOutputSuspended releases (true) or restores (false) the OS audio output while
// keeping decoder, ring and position, e.g. on an iOS audio interruption. Safe from any
// goroutine.
func (p *Player) SetOutputSuspended(on bool) error { return p.out.suspend(on) }

// readPCM fills out with the next PCM for the device (zeroing any tail it can't
// produce) and returns the bytes actually written. Called from the backend's
// realtime pull (malgo callback / oto reader); must be fast + lock-free.
func (p *Player) readPCM(out []byte) int {
	for i := range out {
		out[i] = 0
	}
	if State(p.state.Load()) != Playing {
		return 0
	}
	cur := p.cur.Load()
	next := p.next.Load()
	xfadeMS := p.crossfadeMS.Load()
	if cur == nil {
		return 0
	}

	n := cur.ring.read(out)
	gainApplied := false

	// Crossfade: within the crossfade window of the end, with a next source
	// ready, mix in next (fading in) while cur fades out.
	if xfadeMS > 0 && next != nil {
		total := cur.durMS
		pos := p.played.Load() * 1000 / bytesPerSec
		if total > 0 && pos >= total-xfadeMS {
			// Reuse a scratch buffer instead of allocating on every RT callback.
			if cap(p.mix) < len(out) {
				p.mix = make([]byte, len(out))
			}
			mix := p.mix[:len(out)]
			for i := range mix {
				mix[i] = 0
			}
			m := next.ring.read(mix)
			// Remember how much of next we've already played so the swap resumes
			// it here instead of restarting (which double-books crossfadeMS).
			next.xfadeConsumed.Add(int64(m))
			next.heard.Add(int64(m))
			fade := float64(pos-(total-xfadeMS)) / float64(xfadeMS)
			if fade < 0 {
				fade = 0
			} else if fade > 1 {
				fade = 1
			}
			// Apply each track's own ReplayGain inside the window (the swap no
			// longer switches gains, which stepped the level between tracks whose
			// gains differ); the shared volume/sleep gain is folded in too.
			vs := volumeTaper(p.Volume()) * p.sleepGainClamped()
			curG, nextG := vs, vs
			if p.rgOn.Load() {
				curG *= p.rgFactor(cur.plan())
				nextG *= p.rgFactor(next.plan())
			}
			mixPCM(out[:n], out[:n], curG*(1-fade))
			mixPCM(mix[:m], mix[:m], nextG*fade)
			addPCM(out, mix)
			if m > n {
				n = m
			}
			gainApplied = true
		}
	}

	if !gainApplied {
		applyGain(out[:n], p.effectiveVolume())
	}
	curBytes := n // the position counts cur only
	// Gapless: cur ended inside this buffer, so fill the rest from next; manage's swap
	// resumes next from xfadeConsumed. Skipped when manage will not swap (end-of-track
	// sleep timer, or a cur that decoded nothing).
	if n < len(out) && next != nil && xfadeMS == 0 && p.gapless.Load() &&
		cur.eof.Load() && cur.seekTo.Load() < 0 &&
		!(p.sleepArmed.Load() && p.sleepEOT.Load()) && cur.decoded.Load() > 0 {
		m := next.ring.read(out[n:])
		next.xfadeConsumed.Add(int64(m))
		next.heard.Add(int64(m))
		g := volumeTaper(p.Volume()) * p.sleepGainClamped()
		if p.rgOn.Load() {
			g *= p.rgFactor(next.plan())
		}
		applyGain(out[n:n+m], g)
		n += m
	}
	// Equalizer + mono downmix (no-op when both are off). Linear DSP commutes
	// with the scalar gains above; runs before the fade-in ramp so the ramp
	// shapes the final signal.
	p.applyEQ(out[:n])
	// Anti-click: ramp the first few ms up after a discontinuity (start / resume /
	// seek) so the cut into a fresh waveform doesn't pop.
	if fl := p.fadeLeft.Load(); fl > 0 {
		p.fadeLeft.Store(applyFadeIn(out[:n], fl))
	}
	// Anti-click (out): a control op armed a down-ramp and waits for it before flipping
	// state. Ramp toward zero, then keep muting until that flip.
	if p.fadeOutArmed.Load() {
		if fo := p.fadeOut.Load(); fo > 0 {
			p.fadeOut.Store(applyFadeOut(out[:n], fo))
		} else {
			for i := 0; i < n; i++ {
				out[i] = 0
			}
		}
	}
	p.played.Add(int64(float64(curBytes) * speedOf(&cur.speed))) // media time
	cur.heard.Add(int64(curBytes))

	// Diagnostics: a short read means the ring didn't have a full callback's
	// worth of PCM ready (decode/producer starvation). Counted so we can tell
	// ring-underrun glitches from device/callback-jitter glitches.
	p.cbCount.Add(1)
	if n < len(out) {
		p.cbUnderrun.Add(1)
	}
	return n
}

// ---- volume ----

func (p *Player) setVolume(v float64) {
	if math.IsNaN(v) {
		return // ignore NaN (would corrupt every sample)
	}
	if v < 0 {
		v = 0
	} else if v > 1 {
		v = 1
	}
	p.volume.Store(math.Float64bits(v))
}
func (p *Player) Volume() float64 { return math.Float64frombits(p.volume.Load()) }

// SetVolume sets the absolute volume (clamped to 0..1).
func (p *Player) SetVolume(v float64) { p.setVolume(v) }
func (p *Player) AddVolume(delta float64) float64 {
	p.setVolume(p.Volume() + delta)
	return p.Volume()
}

// ---- accessors ----

func (p *Player) Format() string { s, _ := p.format.Load().(string); return s }
func (p *Player) SetOnFinish(fn func()) {
	if fn == nil {
		p.onFinish.Store(nil)
	} else {
		p.onFinish.Store(&finishCallback{fn: fn})
	}
}

// OnNearEnd registers fn to run once per track when its remaining time falls to lead()
// or less (at once when shorter or of unknown length); a seek back out re-arms it. fn
// runs on its own goroutine, never under the player's locks.
func (p *Player) OnNearEnd(lead func() time.Duration, fn func()) {
	p.nearEnd.Store(&nearEndHook{lead: lead, fn: fn})
}

// RearmNearEnd lets the near-end hook run again for the current track: on the
// next tick when the track is already within the hook's span.
func (p *Player) RearmNearEnd() {
	if cur := p.cur.Load(); cur != nil {
		cur.nearFired.Store(false)
	}
}

func (p *Player) State() State      { return State(p.state.Load()) }
func (p *Player) LastError() string { s, _ := p.lastErr.Load().(string); return s }

// LastErrorNetwork reports whether LastError came from a lost network.
func (p *Player) LastErrorNetwork() bool { return p.lastNet.Load() }
func (p *Player) PositionMS() int64 {
	played := p.played.Load()
	var latencyBytes int64
	if p.out != nil {
		latencyBytes = int64(p.out.latencyFrames() * frameBytes)
		if cur := p.cur.Load(); cur != nil {
			latencyBytes = int64(float64(latencyBytes) * speedOf(&cur.speed))
		}
	}
	bytes := played - latencyBytes
	if bytes < 0 {
		bytes = 0
	}
	return bytes * 1000 / bytesPerSec
}
func (p *Player) DurationMS() int64 { return p.totalMS.Load() }

// IsPreview reports whether the current track is Deezer's 30-second preview
// (the free-account fallback) rather than the full, entitled stream.
func (p *Player) IsPreview() bool {
	if cur := p.cur.Load(); cur != nil {
		return cur.plan().Preview
	}
	return false
}

// publish makes s's format and gain the player's. Callers hold playMu.
func (p *Player) publish(s *source) {
	pl := s.plan()
	p.format.Store(pl.Format)
	if p.rgOn.Load() {
		p.gainFac.Store(math.Float64bits(p.rgFactor(pl)))
	} else {
		p.gainFac.Store(math.Float64bits(1))
	}
}

// ---- playback ----

// fadeOutAndWait arms an anti-click down-ramp and waits, bounded, until the callback
// played it out; a no-op unless Playing. The caller clears the fade-out state
// (endFadeOut, or a Resume/Play re-arming fade-in) after its deferred flip.
func (p *Player) fadeOutAndWait() {
	if State(p.state.Load()) != Playing {
		return
	}
	// Publish the frame count before arming: atomics are sequentially consistent, so a
	// callback that sees fadeOutArmed also sees fadeOut and ramps instead of hard-zeroing.
	p.fadeOut.Store(fadeOutFrames)
	p.fadeOutArmed.Store(true)
	deadline := time.Now().Add(250 * time.Millisecond)
	for p.fadeOut.Load() > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
}

// endFadeOut clears the fade-out state so normal full-volume playback resumes.
func (p *Player) endFadeOut() {
	p.fadeOut.Store(0)
	p.fadeOutArmed.Store(false)
}

// ReplanIfGone returns the plan to hand to Play or Preload: a cached plan whose entry is
// gone is planned again over the network (maybe another format), keeping the cached plan
// if that fails. It can block for several requests, so call it before taking locks.
func (p *Player) ReplanIfGone(plan *deezer.StreamPlan) *deezer.StreamPlan {
	if plan.CDNURL != "" || plan.Refresh == nil {
		return plan
	}
	if c := p.cache(); c != nil {
		if f, _, ok := c.Get(plan.TrackID + "." + plan.Format); ok {
			_ = f.Close()
			return plan
		}
	}
	if np, err := plan.Refresh(); err == nil && np != nil && np.CDNURL != "" {
		return np
	}
	return plan
}

// Play starts a track immediately, replacing anything current.
func (p *Player) Play(plan *deezer.StreamPlan, durationMS int64) error {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	p.pauseWanted.Store(false)
	if p.out.deviceDown() {
		if err := p.out.setDevice(p.out.currentDevice()); err != nil {
			p.state.Store(int32(Errored))
			p.setLastErr(err.Error(), false)
			return err
		}
	}
	p.fadeOutAndWait() // ramp the outgoing track out before the swap (anti-click)
	p.stopSources()
	p.state.Store(int32(Loading))
	p.setLastErr("", false)
	p.played.Store(0)
	p.totalMS.Store(durationMS)
	p.endFadeOut()                 // clear the fade-out; the fresh track fades in below
	p.fadeLeft.Store(fadeInFrames) // anti-click ramp on the fresh track

	src := newSource(plan, durationMS)
	src.cache = p.cache()
	src.owner = p
	p.publish(src)
	go src.download()
	go src.decode()

	old := p.cur.Swap(src)
	oldNext := p.next.Swap(nil)
	if old != nil {
		old.kill()
	}
	if oldNext != nil {
		oldNext.kill()
	}
	// Stay Loading; manage flips to Playing once the ring is prebuffered, so the callback
	// never drains a half-filled ring and underruns at the start of a track.
	return nil
}

// Preload prepares the next track so the transition is gapless/crossfaded. It is
// a no-op when gapless and crossfade are both off.
func (p *Player) Preload(plan *deezer.StreamPlan, durationMS int64) {
	if !p.gapless.Load() && p.crossfadeMS.Load() == 0 {
		return
	}
	src := newSource(plan, durationMS)
	src.cache = p.cache()
	src.owner = p
	go src.download()
	go src.decode()
	if old := p.next.Swap(src); old != nil {
		old.kill()
	}
}

const (
	fastTick = 40 * time.Millisecond
	slowTick = 250 * time.Millisecond
	// slowTickLead is how far from the end of a track, or from a sleep timer's fade, the
	// manager goes back to fastTick: well before the gapless swap and the near-end hook.
	slowTickLead = 2 * time.Second
)

// tickFor is the manager's next period: slowTick through the middle of a playing track,
// where it only watches for the end; fastTick while loading, near the end, near or in a
// sleep timer's fade, and whenever the length is not known.
func (p *Player) tickFor() time.Duration {
	if p.State() != Playing {
		return fastTick
	}
	if p.sleepArmed.Load() && !p.sleepEOT.Load() &&
		p.sleepAtNS.Load()-int64(time.Since(monoBase)) <= sleepFadeNS+int64(slowTickLead) {
		return fastTick
	}
	cur := p.cur.Load()
	if cur == nil || cur.eof.Load() {
		return fastTick
	}
	dur := cur.decDurMS.Load()
	if dur <= 0 {
		dur = cur.durMS
	}
	left := time.Duration(dur-p.played.Load()*1000/bytesPerSec) * time.Millisecond
	if h := p.nearEnd.Load(); h != nil {
		left -= h.lead()
	}
	if dur <= 0 || left <= slowTickLead {
		return fastTick
	}
	return slowTick
}

// ClearPreload discards any preloaded next source. Call it when the upcoming track
// changes (e.g. shuffle or repeat toggled) so a stale preload is not swapped in.
func (p *Player) ClearPreload() {
	if old := p.next.Swap(nil); old != nil {
		old.kill()
	}
}

// manage advances to the preloaded next source when the current one is drained. It
// ticks every 40 ms while a track loads, nears its end or fades for the sleep timer, every
// 250 ms through the rest of a playing track (tickFor), and otherwise waits for a state
// change or, with a timed sleep timer armed, for its deadline.
func (p *Player) manage() {
	tick := fastTick
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		if st := p.State(); st != Loading && st != Playing {
			ticker.Stop()
			// Nothing plays: a timed sleep timer only has to clear at its deadline.
			var deadline <-chan time.Time
			var sleepTimer *time.Timer
			if p.sleepArmed.Load() && !p.sleepEOT.Load() {
				sleepTimer = time.NewTimer(time.Duration(max(0, p.sleepAtNS.Load()-int64(time.Since(monoBase)))))
				deadline = sleepTimer.C
			}
			select {
			case <-p.stopMgr:
				return
			case <-p.state.wake:
			case <-deadline:
				p.clearSleep()
				p.sleepGain.Store(math.Float64bits(1))
			}
			if sleepTimer != nil {
				sleepTimer.Stop()
			}
			tick = fastTick
			ticker.Reset(tick)
			continue
		}
		select {
		case <-p.stopMgr:
			return
		case <-ticker.C:
			if d := p.tickFor(); d != tick {
				tick = d
				ticker.Reset(d)
			}
			ticks := p.ticks.Add(1)
			// Sleep timer (duration mode): fade out over the last few seconds, then
			// pause exactly at the deadline. End-of-track mode is handled in the
			// finish branch below.
			if p.sleepArmed.Load() && !p.sleepEOT.Load() {
				remain := p.sleepAtNS.Load() - int64(time.Since(monoBase))
				switch {
				case remain <= 0:
					switch p.State() {
					case Playing:
						p.state.Store(int32(Paused))
						p.countPause()
					case Loading:
						if !p.pauseWanted.Swap(true) {
							p.countPause()
						}
					}
					p.clearSleep()
					p.sleepGain.Store(math.Float64bits(1))
				case remain <= sleepFadeNS:
					p.sleepGain.Store(math.Float64bits(float64(remain) / float64(sleepFadeNS)))
				}
			}
			// Diagnostics: log callback/underrun counts every ~5s while playing.
			if ticks%125 == 0 {
				if c := p.cbCount.Load(); c > 0 {
					var rb int
					if cur := p.cur.Load(); cur != nil {
						rb = cur.ring.buffered()
					}
					pdlog.Debug("audio: callbacks=%d underruns=%d ringBuf=%dKB state=%v",
						c, p.cbUnderrun.Load(), rb/1024, p.State())
				}
			}
			// The download waits for the network with nothing left to play: Loading, as at a start.
			if State(p.state.Load()) == Playing {
				if cur := p.cur.Load(); cur != nil && cur.netWait.Load() && cur.ring.buffered() == 0 {
					p.state.Store(int32(Loading))
				}
			}
			// Prebuffer: promote Loading -> Playing once the ring has filled (or
			// the track is short/decoded), so the callback starts from a healthy
			// ring instead of underrunning while it fills.
			if State(p.state.Load()) == Loading {
				if cur := p.cur.Load(); cur != nil &&
					(cur.ring.buffered() >= prebufferB || cur.eof.Load()) {
					if p.pauseWanted.Load() {
						p.state.Store(int32(Paused))
					} else {
						p.state.Store(int32(Playing))
					}
				}
			}
			// Near-end hook: run it once when the current track enters its span,
			// arm it again when a seek leaves the span.
			if h, cur := p.nearEnd.Load(), p.cur.Load(); h != nil && cur != nil {
				dur := cur.decDurMS.Load()
				if dur <= 0 {
					dur = cur.durMS
				}
				if dur <= 0 || dur-p.played.Load()*1000/bytesPerSec <= h.lead().Milliseconds() {
					if !cur.nearFired.Swap(true) {
						go h.fn()
					}
				} else {
					cur.nearFired.Store(false)
				}
			}
			if State(p.state.Load()) != Playing {
				continue
			}
			cur := p.cur.Load()
			next := p.next.Load()
			if cur == nil {
				continue
			}
			// Finished means decoder EOF, an empty ring and no pending seek. SeekMS empties the
			// ring at once but the decoder clears eof only after waking, so without the seekTo
			// check a tick in between would skip the track.
			if cur.eof.Load() && cur.ring.buffered() == 0 && cur.seekTo.Load() < 0 {
				// A stream that failed ends the track Errored (position and track stay); a lost
				// network is marked so the host does not take it for an unplayable track.
				if cur.lastErr() != "" {
					p.setLastErr(cur.lastErr(), cur.errNet.Load())
					if next != nil {
						if !p.next.CompareAndSwap(next, nil) {
							continue
						}
					}
					if !p.cur.CompareAndSwap(cur, nil) {
						if next != nil {
							next.kill()
						}
						continue
					}
					if next != nil {
						next.kill()
					}
					cur.kill()
					p.reportListen(cur)
					p.state.Store(int32(Errored))
					if cb := p.onFinish.Load(); cb != nil && cb.fn != nil {
						cb.fn()
					}
					continue
				}
				// End-of-track sleep timer: stop after this track finishes and do
				// not advance (don't fire onFinish, so the engine won't auto-next).
				if p.sleepArmed.Load() && p.sleepEOT.Load() {
					if next != nil {
						if !p.next.CompareAndSwap(next, nil) {
							continue
						}
					}
					if !p.cur.CompareAndSwap(cur, nil) {
						if next != nil {
							next.kill()
						}
						continue
					}
					if next != nil {
						next.kill()
					}
					cur.kill()
					p.reportListen(cur)
					p.state.Store(int32(Stopped))
					p.clearSleep()
					continue
				}
				if next != nil {
					// onFinish runs outside playMu: it may call Play.
					if p.advanceToNext(cur, next) {
						if cb := p.onFinish.Load(); cb != nil && cb.fn != nil {
							cb.fn()
						}
					}
				} else {
					// No preload: finish. Guard against a concurrent Play/Stop that
					// swapped in a new cur so we don't wedge it into Stopped.
					if !p.cur.CompareAndSwap(cur, nil) {
						continue
					}
					cur.kill() // release the decode goroutine parked after EOF
					p.reportListen(cur)
					p.state.Store(int32(Stopped))
					if cb := p.onFinish.Load(); cb != nil && cb.fn != nil {
						cb.fn()
					}
				}
			}
		}
	}
}

// advanceToNext swaps the drained cur for the preloaded next and publishes
// next's metadata. It reports whether next is playing.
func (p *Player) advanceToNext(cur, next *source) bool {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	// CAS so a concurrent Stop/ClearPreload that swapped these pointers (and
	// killed the sources) can't be clobbered: claim next, then install it as cur
	// only if cur is unchanged.
	if !p.next.CompareAndSwap(next, nil) {
		return false // preload was cleared/replaced under us
	}
	if !p.cur.CompareAndSwap(cur, next) {
		next.kill() // next is ours; cur changed, so release it
		return false
	}
	cur.kill()
	p.reportListen(cur)
	if p.cur.Load() != next { // a Stop got in between
		return false
	}
	// Crossfade or the gapless hand-over already played next.xfadeConsumed
	// bytes; resume from there.
	p.played.Store(next.xfadeConsumed.Load())
	p.totalMS.Store(next.durMS)
	p.publish(next)
	return true
}

// SetSpeed plays the current track at speed, from 0.5 to 2 (1 is normal),
// pitch not kept; a track starts at normal speed. It is for plain MP3 streams
// (episodes), which decode through the resampler. It takes effect at once.
func (p *Player) SetSpeed(speed float64) {
	cur := p.cur.Load()
	if cur == nil {
		return
	}
	speed = min(max(speed, 0.5), 2)
	if speedOf(&cur.speed) == speed {
		return
	}
	// The buffered PCM was decoded at the old speed: drop it, count its media time as
	// played and decode on at the new speed. No seek, since an MP3 still downloading may
	// wait for the rest of the stream.
	old := speedOf(&cur.speed)
	p.fadeOutAndWait()
	cur.speed.Store(math.Float64bits(speed))
	dropped := cur.ring.buffered()
	cur.ring.flush()
	p.played.Add(int64(float64(dropped) * old))
	p.endFadeOut()
	p.fadeLeft.Store(fadeInFrames)
}

func (p *Player) SeekMS(ms int64) {
	cur := p.cur.Load()
	if cur == nil {
		return
	}
	if ms < 0 {
		ms = 0
	}
	if total := p.totalMS.Load(); total > 0 && ms > total {
		ms = total
	}
	off := ms * bytesPerSec / 1000
	off -= off % frameBytes
	// Ramp out first (anti-click), then set the seek target and flush the ring so pre-seek
	// PCM stops at once. requestSeek comes before flush so the decoder does not refill the
	// ring with pre-seek PCM; fadeLeft then applies to the first post-seek PCM.
	p.fadeOutAndWait()
	cur.requestSeek(off)
	cur.ring.flush()
	p.played.Store(off)
	cur.seeks.Add(1)
	// Seeking back out of the crossfade (or gapless hand-over) window: rewind
	// next, whose intro was already drained into the mix. The length is the
	// decoder's, else the one passed; with neither, next is rewound.
	dur := cur.decDurMS.Load()
	if dur <= 0 {
		dur = cur.durMS
	}
	if next := p.next.Load(); next != nil && next.xfadeConsumed.Load() > 0 &&
		(dur <= 0 || ms < dur-p.crossfadeMS.Load()) {
		next.requestSeek(0)
		next.ring.flush()
		next.xfadeConsumed.Store(0)
		next.heard.Store(0)
	}
	p.endFadeOut()
	p.fadeLeft.Store(fadeInFrames) // anti-click ramp on the first post-seek PCM
}

func (p *Player) Pause() {
	switch p.State() {
	case Playing:
		p.fadeOutAndWait() // anti-click ramp before going silent
		// CAS: the track may have finished (Stopped/Errored) during the fade.
		if p.state.CompareAndSwap(int32(Playing), int32(Paused)) {
			p.countPause()
		}
		// Leave fadeOut/fadeOutArmed set: the state is Paused, so the callback returns silence;
		// Resume clears them and re-arms the fade-in.
	case Loading:
		if !p.pauseWanted.Swap(true) { // manage promotes Loading to Paused
			p.countPause()
		}
	}
}

// SetOnListenEnd sets fn to run with each track's Listen when it stops playing (ended,
// failed, replaced, stopped or handed to the next). fn runs on the goroutine that ended
// it and must not block.
func (p *Player) SetOnListenEnd(fn func(Listen)) { p.onListenEnd.Store(&fn) }

// reportListen hands what was heard of s, which stopped playing, to the
// OnListenEnd function.
func (p *Player) reportListen(s *source) {
	if fn := p.onListenEnd.Load(); fn != nil && *fn != nil {
		(*fn)(listenOf(s))
	}
}

// countPause counts a pause of the current track.
func (p *Player) countPause() {
	if cur := p.cur.Load(); cur != nil {
		cur.pauses.Add(1)
	}
}

func (p *Player) Resume() {
	p.pauseWanted.Store(false)
	if p.cur.Load() == nil {
		return
	}
	state := p.State()
	if state == Paused || state == Errored {
		if p.out.deviceDown() {
			if err := p.out.setDevice(p.out.currentDevice()); err != nil {
				p.state.Store(int32(Errored))
				p.setLastErr(err.Error(), false)
				return
			}
		}
		p.endFadeOut()
		p.fadeLeft.Store(fadeInFrames) // anti-click ramp on resume
		p.state.Store(int32(Playing))
	}
}
func (p *Player) TogglePause() {
	switch p.State() {
	case Playing:
		p.Pause()
	case Paused:
		p.Resume()
	case Loading:
		if p.pauseWanted.Load() {
			p.Resume()
		} else {
			p.Pause()
		}
	}
}

// Stop halts playback and releases sources.
func (p *Player) Stop() {
	p.fadeOutAndWait() // anti-click ramp before the hard stop
	p.stopSources()
	p.state.Store(int32(Stopped))
	p.endFadeOut()
}

func (p *Player) stopSources() {
	if cur := p.cur.Swap(nil); cur != nil {
		cur.kill()
		p.reportListen(cur)
	}
	if next := p.next.Swap(nil); next != nil {
		next.kill()
	}
}

// Close tears down sources and the output device.
func (p *Player) Close() {
	p.mgrOnce.Do(func() { close(p.stopMgr) })
	p.stopSources()
	// Flush a pending debounced EQ save so a change made just before quit
	// isn't lost.
	p.eqSaveMu.Lock()
	pending := p.eqSaveTimer != nil && p.eqSaveTimer.Stop()
	p.eqSaveMu.Unlock()
	if pending {
		p.saveEQNow()
	}
	if p.out != nil {
		p.out.close()
	}
}

// ---- output device selection (delegates to the backend) ----

// Devices lists available output devices.
func (p *Player) Devices() ([]Device, error) { return p.out.devices() }

// SetDevice switches output to the given device id ("" = system default).
func (p *Player) SetDevice(id string) error { return p.out.setDevice(id) }

// CurrentDevice returns the selected device id ("" = default).
func (p *Player) CurrentDevice() string { return p.out.currentDevice() }

// ---- source pipeline ----

func newSource(plan *deezer.StreamPlan, durMS int64) *source {
	s := &source{
		durMS: durMS,
		ring:  newPCMRing(ringMax),
	}
	s.pl.Store(plan)
	s.sb = newStreamFile(stripeCipher(plan))
	s.cond = sync.NewCond(&s.mu)
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.seekTo.Store(-1)
	return s
}

// download writes the CDN body as received into the stream file, resuming torn
// transfers with a Range GET and refreshing an expired URL (403/410). With a StreamCache,
// full encrypted tracks are read from or stored in the cache.
func (s *source) download() {
	pl := s.plan()
	url := pl.CDNURL
	refreshes := 0 // media-URL re-resolutions performed

	// Cache hit: the cached file is the stream, complete; no HTTP at all. A
	// damaged entry (another size than recorded) is a miss.
	cacheKey := s.cacheKey(pl)
	if cacheKey != "" {
		if f, size, ok := s.cache.Get(cacheKey); ok {
			b := s.sb
			b.mu.Lock()
			if b.closed {
				b.mu.Unlock()
				_ = f.Close()
				return
			}
			b.f, b.cached = f, true
			b.written, b.total, b.done = size, size, true
			b.cond.Broadcast()
			b.mu.Unlock()
			return
		}
	}
	if url == "" {
		var np *deezer.StreamPlan
		var err error
		if pl.Refresh != nil {
			refreshes++
			np, err = pl.Refresh()
		}
		if err != nil || np == nil || np.CDNURL == "" {
			s.setErr(ErrOffline)
			s.sb.finish(ErrOffline)
			return
		}
		pl, url = np, np.CDNURL
		s.sb.mu.Lock()
		s.sb.dec = stripeCipher(np)
		s.sb.mu.Unlock()
		s.pl.Store(pl)
		if p := s.owner; p != nil {
			p.playMu.Lock()
			if p.cur.Load() == s {
				p.publish(s)
			}
			p.playMu.Unlock()
		}
		cacheKey = s.cacheKey(pl)
	}

	// Cache fill: the stream goes into a cache temp file, which becomes the
	// entry once the whole declared body is in it.
	cacheFile := false
	if cacheKey != "" {
		if f, err := s.cache.NewTemp(); err == nil {
			b := s.sb
			b.mu.Lock()
			if b.closed {
				b.mu.Unlock()
				_ = f.Close()
				_ = os.Remove(f.Name())
				return
			}
			b.f = f
			b.mu.Unlock()
			cacheFile = true
		}
	}

	var dryAt time.Time // when the audio ran out while the network was gone
	_, _, err := deezer.Resume{
		Plan:        s.plan(),
		URL:         url,
		Refreshes:   refreshes,
		Pump:        s.pump,
		WaitNetwork: func(n int) bool { return s.waitNetwork(n, &dryAt) },
		Opened: func() {
			dryAt = time.Time{}
			s.netWait.Store(false)
		},
		Total: s.sb.setTotal,
		Fatal: func(error) bool { return s.writeFailed.Load() },
	}.Run(s.ctx)
	s.netWait.Store(false)
	switch {
	case s.dead.Load():
		s.sb.finish(nil)
	case err != nil:
		s.setErr(err)
		s.sb.finish(err)
	default:
		// The cache adopts the body only at its declared length. Once adopted the temp name is
		// gone, so close leaves the entry alone, and the next play uses the cache.
		if total := s.sb.Total(); cacheFile && total > 0 {
			meta := mediacache.StreamMeta{Format: pl.Format, AskedFormat: pl.AskedFormat, KeyID: pl.KeyID, Encrypted: true, GainDB: pl.GainDB}
			if err := s.cache.Adopt(cacheKey, s.sb.f.Name(), total, meta); err != nil {
				pdlog.Debug("stream %s: not cached: %v", pl.TrackID, err)
			}
		}
		s.sb.finish(nil)
	}
}

// cacheKey is pl's key in the raw-stream cache, "" when there is no cache or pl is not
// a full encrypted track (previews and plain streams are never cached).
func (s *source) cacheKey(pl *deezer.StreamPlan) string {
	if s.cache == nil || !pl.Encrypted || pl.Preview {
		return ""
	}
	return pl.TrackID + "." + pl.Format
}

// pump writes one response body into the stream file, advancing *consumed and skipping
// skip leading bytes. completed is true on a clean EOF; a read error is returned for a
// resume, a write error sets writeFailed (no resume).
func (s *source) pump(body io.Reader, buf []byte, consumed *int64, skip int64) (completed bool, err error) {
	for {
		n, rerr := body.Read(buf)
		if n > 0 {
			b := buf[:n]
			if skip > 0 {
				d := skip
				if d > int64(len(b)) {
					d = int64(len(b))
				}
				b = b[d:]
				skip -= d
			}
			if len(b) > 0 {
				if werr := s.sb.write(b); werr != nil {
					if !s.dead.Load() {
						s.writeFailed.Store(true)
					}
					return false, werr
				}
				*consumed += int64(len(b))
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				if s.sb.Total() > 0 && *consumed < s.sb.Total() {
					return false, io.ErrUnexpectedEOF
				}
				return true, nil
			}
			return false, rerr
		}
		if s.dead.Load() {
			return false, nil
		}
	}
}

// waitNetwork waits before the n-th network retry and reports whether to try again: always
// while audio is still buffered, for deezer.NetDryWait once it ran dry (dryAt marks when;
// the player shows Loading meanwhile).
func (s *source) waitNetwork(n int, dryAt *time.Time) bool {
	if s.ring.buffered() == 0 {
		if dryAt.IsZero() {
			*dryAt = time.Now()
		}
		if time.Since(*dryAt) > deezer.NetDryWait {
			return false
		}
		s.netWait.Store(true)
	}
	t := time.NewTimer(deezer.NetRetryDelays[min(n, len(deezer.NetRetryDelays)-1)])
	defer t.Stop()
	select {
	case <-s.ctx.Done():
		return false
	case <-t.C:
		return !s.dead.Load()
	}
}

// watermarkBytes estimates ~15 s of encoded audio from the format's bitrate, the amount
// buffered before decoding starts. At least 256 KB.
func watermarkBytes(format string) int {
	up := strings.ToUpper(format)
	kbps := 128
	switch {
	case strings.Contains(up, "FLAC"):
		kbps = 1024 // 16-bit/44.1k stereo FLAC compresses to roughly this
	case strings.Contains(up, "320"):
		kbps = 320
	case strings.Contains(up, "256"):
		kbps = 256
	case strings.Contains(up, "64"):
		kbps = 64
	}
	const seconds = 15
	n := kbps * 1000 / 8 * seconds
	if min := 256 * 1024; n < min {
		n = min
	}
	return n
}

// decode builds the decoder over a reader of the stream and pumps PCM into the
// ring, honoring seek requests.
func (s *source) decode() {
	// Start decoding on a watermark rather than the whole track; the stream's blocking Read
	// parks the decoder when it outruns the download. The plan is final after the first
	// byte, so the format is read after that.
	s.sb.waitWatermark(1)
	format := s.plan().Format
	s.sb.waitWatermark(int64(watermarkBytes(format)))
	if s.dead.Load() {
		return
	}
	var dec pcmStream
	var err error
	if strings.Contains(strings.ToUpper(format), "FLAC") {
		// The decoder reads only the metadata up front, so FLAC decodes progressively. Without a
		// SEEKTABLE the first seek reads to EOF, which blocks until the download completes.
		var fs *flacStream
		fs, err = newFLACStream(s.sb.reader())
		if err == nil {
			if n, rate := fs.info(); n > 0 && rate > 0 {
				s.decDurMS.Store(int64(n * 1000 / uint64(rate)))
			}
			dec = fs
		}
	} else {
		var ms *mp3Stream
		ms, err = newMP3Stream(s.sb)
		if err == nil {
			// The MP3 decoder emits 2ch s16 at the file's own rate while the output runs at 44100, so other
			// rates (podcast MP3s) go through a linear resampler. Plain streams (episodes) always
			// use it, for SetSpeed.
			if r := ms.SampleRate(); r != sampleRate || !s.plan().Encrypted {
				pdlog.Debug("mp3 decode: resampling %dHz -> %dHz format=%s", r, sampleRate, format)
				dec = newResampleStream(ms, r, &s.speed)
			} else {
				dec = ms
			}
		}
	}
	if err != nil {
		if s.lastErr() == "" {
			s.setErr(err)
		}
		s.eof.Store(true)
		// No decoder can service seeks, and manage() treats a pending seek as not finished, so
		// park here swallowing seek requests until the source is killed.
		for s.waitSeekOrDead() {
			s.seekTo.Store(-1)
		}
		return
	}
	buf := make([]byte, decodeChunk)
	seq := s.ring.seq()
	for {
		if s.dead.Load() {
			return
		}
		if s.seekTo.Load() >= 0 {
			// Clear eof before taking the seek target: manage() finishes on eof, an empty ring and
			// no pending seek, so taking seekTo first could let it skip the track.
			s.eof.Store(false) // seeking back into the track: PCM will grow again
			if to := s.seekTo.Swap(-1); to >= 0 {
				serr := func() (err error) {
					// A decoder may panic on a seek past its last frame.
					defer func() {
						if r := recover(); r != nil {
							err = fmt.Errorf("seek to %d: %v", to, r)
						}
					}()
					_, err = dec.Seek(to, io.SeekStart)
					return err
				}()
				seq = s.ring.flush()
				if serr != nil {
					// Past the decodable end (the duration is whole seconds):
					// finish the track instead of decoding on from the old spot.
					s.eof.Store(true)
					if !s.waitSeekOrDead() {
						return
					}
					continue
				}
			}
		}
		n, rerr := dec.Read(buf)
		if n > 0 {
			if !s.ring.write(buf[:n], seq) {
				// The ring was flushed or closed (a seek). Drop this chunk and loop so a pending seek is
				// handled before decoding more, keeping pre-seek PCM out of the flushed ring.
				if s.dead.Load() {
					return
				}
				seq = s.ring.seq()
				continue
			}
			s.decoded.Add(int64(n))
		}
		if rerr != nil {
			// Decoder EOF with the source alive: the user may still seek back. Mark eof so manage()
			// finishes once the ring drains, and park until a seek or kill.
			s.eof.Store(true)
			if !s.waitSeekOrDead() {
				return
			}
		}
	}
}

// waitSeekOrDead blocks until a seek is requested or the source is killed.
// Returns true if a seek is pending, false if the source is dead.
func (s *source) waitSeekOrDead() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.seekTo.Load() < 0 && !s.dead.Load() {
		s.cond.Wait()
	}
	return s.seekTo.Load() >= 0
}

func (s *source) requestSeek(pcmOffset int64) {
	s.seekTo.Store(pcmOffset)
	s.mu.Lock()
	s.cond.Broadcast() // wake the decode goroutine if it parked after EOF
	s.mu.Unlock()
}

func (s *source) kill() {
	s.dead.Store(true)
	s.eof.Store(true) // a killed source is finished: never let manage() wait on it
	s.cancel()        // unblock a stalled Body.Read in download()
	s.mu.Lock()
	s.cond.Broadcast() // wake the decode goroutine if it parked after EOF
	s.mu.Unlock()
	s.sb.close()
	s.ring.close()
}

func init() {
	// Compile-time or init-time endianness check that panics on big-endian platforms.
	// Since all supported targets are little-endian and we cast []byte to []int16,
	// big-endian platforms are not supported.
	var x uint16 = 0x0001
	p := unsafe.Pointer(&x)
	if *(*byte)(p) == 0x00 {
		panic("big-endian platform not supported by internal/audio")
	}
}

// pcm16 reinterprets a little-endian byte slice as int16 in place (unsafe.Slice; the
// host is checked to be little-endian at init). nil below 2 bytes; an odd last byte is
// ignored.
func pcm16(b []byte) []int16 {
	if len(b) < 2 {
		return nil
	}
	return unsafe.Slice((*int16)(unsafe.Pointer(&b[0])), len(b)/2)
}

// ---- PCM helpers ----

// applyGain scales interleaved s16 samples in place by g (0..1).
func applyGain(b []byte, g float64) {
	if g >= 0.999 {
		return
	}
	samples := pcm16(b)
	for i := range samples {
		samples[i] = int16(float64(samples[i]) * g)
	}
}

// applyFadeIn ramps interleaved s16 stereo frames up to unity over fadeInFrames,
// starting from `remaining` frames left, and returns the frames still to ramp.
// Runs in the RT callback; no allocation.
func applyFadeIn(b []byte, remaining int64) int64 {
	samples := pcm16(b)
	numFrames := len(samples) / channels
	for i := 0; i < numFrames && remaining > 0; i++ {
		g := float64(fadeInFrames-remaining) / float64(fadeInFrames)
		if g < 0 {
			g = 0
		} else if g > 1 {
			g = 1
		}
		off := i * channels
		for c := 0; c < channels; c++ {
			idx := off + c
			samples[idx] = int16(float64(samples[idx]) * g)
		}
		remaining--
	}
	return remaining
}

// applyFadeOut ramps interleaved s16 stereo frames down toward zero from `remaining`
// frames left and returns the frames still to ramp; past the ramp's end the buffer is
// zeroed. Runs in the RT callback without allocating.
func applyFadeOut(b []byte, remaining int64) int64 {
	samples := pcm16(b)
	numFrames := len(samples) / channels
	i := 0
	for ; i < numFrames && remaining > 0; i++ {
		g := float64(remaining) / float64(fadeOutFrames)
		if g < 0 {
			g = 0
		} else if g > 1 {
			g = 1
		}
		off := i * channels
		for c := 0; c < channels; c++ {
			idx := off + c
			samples[idx] = int16(float64(samples[idx]) * g)
		}
		remaining--
	}
	if remaining == 0 { // ramp spent: silence the rest of this buffer
		for j := i * channels; j < len(samples); j++ {
			samples[j] = 0
		}
		for j := len(samples) * 2; j < len(b); j++ {
			b[j] = 0
		}
	}
	return remaining
}

// mixPCM writes src*g into dst (same length); dst and src may alias.
func mixPCM(dst, src []byte, g float64) {
	dSamples := pcm16(dst)
	sSamples := pcm16(src)
	n := len(dSamples)
	if len(sSamples) < n {
		n = len(sSamples)
	}
	for i := 0; i < n; i++ {
		dSamples[i] = int16(float64(sSamples[i]) * g)
	}
}

// addPCM adds src into dst (saturating), in place.
func addPCM(dst, src []byte) {
	dSamples := pcm16(dst)
	sSamples := pcm16(src)
	n := len(dSamples)
	if len(sSamples) < n {
		n = len(sSamples)
	}
	for i := 0; i < n; i++ {
		s := int32(dSamples[i]) + int32(sSamples[i])
		if s > 32767 {
			s = 32767
		} else if s < -32768 {
			s = -32768
		}
		dSamples[i] = int16(s)
	}
}
