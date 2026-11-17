package audio

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
)

// pumpPCM pulls PCM from p as fast as it decodes until the test ends.
func pumpPCM(t *testing.T, p *Player) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
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
}

func heapInuse() uint64 {
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapInuse
}

// A 100 MB encrypted stream, MP3 and FLAC, plays through the player from a
// file: the heap grows by the PCM ring and the decoder's state, not by the
// stream.
func TestHeap100MBStream(t *testing.T) {
	if testing.Short() {
		t.Skip("streams 200 MB")
	}
	for _, tc := range []struct {
		format string
		make   func() []byte
		durMS  int64
	}{
		{"MP3_128", func() []byte { return makeMP3(251500) }, 251500 * 1152 * 1000 / 44100},
		{"FLAC", func() []byte { return makeFLAC(8900, 4096) }, 8900 * 4096 * 1000 / 44100},
	} {
		t.Run(tc.format, func(t *testing.T) {
			id := "heap100-" + tc.format
			path := filepath.Join(t.TempDir(), "stream.enc")
			b := tc.make()
			encryptStripe(id, b)
			size := len(b)
			if err := os.WriteFile(path, b, 0o600); err != nil {
				t.Fatal(err)
			}
			b = nil
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.ServeFile(w, r, path)
			}))
			defer ts.Close()

			p := newPlayer(&mockOutput{})
			go p.manage()
			defer p.Close()
			pumpPCM(t, p)
			before := heapInuse()
			plan := &deezer.StreamPlan{CDNURL: ts.URL + "/a", TrackID: id, Format: tc.format, Encrypted: true}
			if err := p.Play(plan, tc.durMS); err != nil {
				t.Fatal(err)
			}
			cur := p.cur.Load()
			deadline := time.Now().Add(time.Minute)
			for !sbDone(cur.sb) {
				if time.Now().After(deadline) {
					t.Fatal("the download did not finish")
				}
				time.Sleep(10 * time.Millisecond)
			}
			waitFor(t, "Playing", func() bool { return p.State() == Playing })
			after := heapInuse()
			growth := int64(after) - int64(before)
			t.Logf("%s %.1f MB stream: HeapInuse %.2f MB before, %.2f MB with the stream downloaded (growth %.2f MB)",
				tc.format, float64(size)/1e6, float64(before)/1e6, float64(after)/1e6, float64(growth)/1e6)
			// Usually 1.4-2.1 MB; the MP3 decoder's per-frame allocations in
			// flight and new OS threads reach about 6 MB in some runs.
			if growth > 8<<20 {
				t.Fatalf("HeapInuse grew %d bytes for a %d-byte stream, want under 8 MB", growth, size)
			}
			if e := p.LastError(); e != "" {
				t.Fatal(e)
			}
		})
	}
}

// A stream that cannot be written (no temp directory) fails the track with
// the reason.
func TestWriteErrorFailsTrack(t *testing.T) {
	for _, fx := range fixtures() {
		t.Run(fx.format, func(t *testing.T) {
			notDir := filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(notDir, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			prev, _ := config.CacheBaseDir()
			config.SetCacheBaseDir(notDir)
			defer config.SetCacheBaseDir(prev)
			cdn := newCountingCDN(t, fx.plain)

			p := newPlayer(&mockOutput{})
			go p.manage()
			defer p.Close()
			if err := p.Play(&deezer.StreamPlan{CDNURL: cdn.srv.URL, TrackID: "ep", Format: fx.format}, 10000); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "the failure", func() bool { return p.State() == Errored })
			if e := p.LastError(); !strings.Contains(e, "file") {
				t.Fatalf("LastError %q, want the write error", e)
			}
		})
	}
}
