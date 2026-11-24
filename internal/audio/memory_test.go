package audio

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
	"golang.org/x/crypto/blowfish"
)

// encryptStripe is the inverse of deezer.DecryptTrack, in place: every third
// full 2 048-byte chunk is Blowfish-CBC encrypted with the fixed IV.
func encryptStripe(trackID string, b []byte) {
	c, err := blowfish.NewCipher(deezer.BlowfishKey(trackID))
	if err != nil {
		panic(err)
	}
	for i := 0; (i+1)*2048 <= len(b); i += 3 {
		chunk := b[i*2048 : (i+1)*2048]
		prev := [8]byte{0, 1, 2, 3, 4, 5, 6, 7}
		for off := 0; off < len(chunk); off += 8 {
			b := chunk[off : off+8]
			for k := range b {
				b[k] ^= prev[k]
			}
			c.Encrypt(b, b)
			copy(prev[:], b)
		}
	}
}

// TestHeapAfterThreeFullPlays plays a 30 MB encrypted FLAC to the end three
// times through the player, pulling PCM as fast as it decodes, and checks the
// heap the runtime took from the OS against the live heap while a track plays.
func TestHeapAfterThreeFullPlays(t *testing.T) {
	if testing.Short() {
		t.Skip("plays 90 MB of audio")
	}
	const id = "heap1"
	const frames = 2670 // ~30 MB, 248 s
	durMS := int64(frames * 4096 * 1000 / 44100)
	// HeapSys never shrinks, so the plays run in a fresh process that serves
	// the stream from a file: its heap holds neither the fixture nor what the
	// package's other tests leave behind.
	path := os.Getenv("OD_HEAP_STREAM")
	if path == "" {
		path = filepath.Join(t.TempDir(), "stream.flac.enc")
		b := makeFLAC(frames, 4096)
		encryptStripe(id, b)
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestHeapAfterThreeFullPlays$", "-test.v")
		cmd.Env = append(os.Environ(), "OD_HEAP_STREAM="+path)
		out, err := cmd.CombinedOutput()
		t.Logf("%s", out)
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, path)
	}))
	defer ts.Close()

	p := newPlayer(&mockOutput{})
	go p.manage()
	defer p.Close()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		buf := make([]byte, 4096)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if p.readPCM(buf) == 0 {
				time.Sleep(100 * time.Microsecond)
			}
		}
	}()
	finished := make(chan struct{}, 1)
	p.SetOnFinish(func() { finished <- struct{}{} })

	var live uint64
	for i := 0; i < 3; i++ {
		plan := &deezer.StreamPlan{CDNURL: ts.URL + "/a", TrackID: id, Format: "FLAC", Encrypted: true}
		if err := p.Play(plan, durMS); err != nil {
			t.Fatal(err)
		}
		if i == 2 {
			// The live heap mid-track, once the whole stream has arrived.
			cur := p.cur.Load()
			waitFor(t, "the download", func() bool { return sbDone(cur.sb) })
			runtime.GC()
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			live = ms.HeapAlloc
		}
		select {
		case <-finished:
		case <-time.After(2 * time.Minute):
			t.Fatalf("play %d did not finish (state %v)", i+1, p.State())
		}
		if p.State() != Stopped || p.LastError() != "" {
			t.Fatalf("play %d ended %v %q", i+1, p.State(), p.LastError())
		}
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	t.Logf("live mid-track %.1f MB, HeapSys after third play %.1f MB (%.2fx), TotalAlloc %.0f MB, NumGC %d",
		float64(live)/1e6, float64(ms.HeapSys)/1e6, float64(ms.HeapSys)/float64(live),
		float64(ms.TotalAlloc)/1e6, ms.NumGC)
	// The stream is read through one 64 KB window, so the live heap is about 1.5 MB and
	// HeapSys (8-12 MB) is mostly the runtime's floor. The live heap must stay under 8 MB,
	// which a stream held in memory (30 MB) would exceed.
	if live > 8<<20 {
		t.Fatalf("live heap mid-track %d > 8 MB", live)
	}
	if ms.HeapSys > 5*live && ms.HeapSys > 16<<20 {
		t.Fatalf("HeapSys %d > 5 x live %d and > 16 MB", ms.HeapSys, live)
	}
}
