package audio

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/mediacache"
)

// Compile-time check: the real on-disk cache satisfies the audio-side
// interface (the product code only ever depends on StreamCache).
var _ StreamCache = (*mediacache.Cache)(nil)

// makeCipher builds deterministic pseudo-ciphertext of n bytes.
func makeCipher(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((i*11 + 5) % 253)
	}
	return b
}

// countingCDN serves body with HTTP Range support, counting every request and
// recording the start offset of the most recent Range request (-1 = none).
type countingCDN struct {
	srv       *httptest.Server
	hits      atomic.Int32
	lastRange atomic.Int64
}

func newCountingCDN(t *testing.T, body []byte) *countingCDN {
	t.Helper()
	c := &countingCDN{}
	c.lastRange.Store(-1)
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.hits.Add(1)
		if rng := r.Header.Get("Range"); rng != "" {
			start := parseRangeStart(t, rng)
			c.lastRange.Store(int64(start))
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(body)-1, len(body)))
			w.Header().Set("Content-Length", strconv.Itoa(len(body)-start))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(body[start:])
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func newTestCache(t *testing.T) *mediacache.Cache {
	t.Helper()
	mc, err := mediacache.New(t.TempDir(), 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	return mc
}

// TestStreamCacheHitSkipsHTTP verifies that after one full play the second play
// of the same track+format is served entirely from the cache — zero HTTP
// requests — and yields byte-identical decrypted output.
func TestStreamCacheHitSkipsHTTP(t *testing.T) {
	const trackID = "3135556"
	cipher := makeCipher(300000)
	want, err := deezer.DecryptTrack(trackID, cipher)
	if err != nil {
		t.Fatal(err)
	}
	mc := newTestCache(t)
	cdn := newCountingCDN(t, cipher)
	plan := &deezer.StreamPlan{CDNURL: cdn.srv.URL, TrackID: trackID, Format: "MP3_320", Encrypted: true}

	s1 := newSource(plan, 10000)
	s1.cache = mc
	s1.download()
	if got := drainStreamFile(s1.sb); !bytes.Equal(got, want) {
		t.Fatalf("first play mismatch: got %d bytes, want %d", len(got), len(want))
	}
	if h := cdn.hits.Load(); h != 1 {
		t.Fatalf("first play made %d HTTP requests, want 1", h)
	}

	s2 := newSource(plan, 10000)
	s2.cache = mc
	s2.download()
	if got := drainStreamFile(s2.sb); !bytes.Equal(got, want) {
		t.Fatalf("cached play output differs from the network play")
	}
	if h := cdn.hits.Load(); h != 1 {
		t.Fatalf("cached play performed HTTP requests: total %d, want still 1", h)
	}
	if e := s2.lastErr(); e != "" {
		t.Fatalf("unexpected error on cached play: %s", e)
	}
	// The cached size is the stream's total.
	if total := s2.sb.Total(); total != int64(len(cipher)) {
		t.Fatalf("cached play content length = %d, want %d", total, len(cipher))
	}
}

// TestStreamCacheStoresExactCiphertext verifies that after a full first play
// the cache holds exactly the raw bytes the server served (ciphertext at rest,
// never the decrypted output).
func TestStreamCacheStoresExactCiphertext(t *testing.T) {
	const trackID = "3135556"
	cipher := makeCipher(260000)
	mc := newTestCache(t)
	cdn := newCountingCDN(t, cipher)

	s := newSource(&deezer.StreamPlan{CDNURL: cdn.srv.URL, TrackID: trackID, Format: "FLAC", Encrypted: true}, 10000)
	s.cache = mc
	s.download()
	drainStreamFile(s.sb)

	rc, size, ok := mc.Get(trackID + ".FLAC")
	if !ok {
		t.Fatal("cache miss after a full play")
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len(cipher)) || !bytes.Equal(got, cipher) {
		t.Fatalf("cached entry is not the served ciphertext: got %d bytes (size %d), want %d", len(got), size, len(cipher))
	}
	if n, b := mc.Stats(); n != 1 || b != int64(len(cipher)) {
		t.Fatalf("Stats = %d entries / %d bytes, want 1 / %d", n, b, len(cipher))
	}
}

// TestStreamCacheResumedDownloadCached verifies that a torn first download
// (mid-body drop + Range resume) still ends as the whole body in the stream
// file, which the cache adopts: the entry is exactly the served ciphertext and
// plays back as the plaintext's PCM.
func TestStreamCacheResumedDownloadCached(t *testing.T) {
	for _, fx := range fixtures() {
		t.Run(fx.format, func(t *testing.T) {
			mc := newTestCache(t)
			srv := flakyServer(t, fx.cipher, len(fx.cipher)/2+7)
			defer srv.Close()

			s := newSource(&deezer.StreamPlan{CDNURL: srv.URL, TrackID: fx.id, Format: fx.format, Encrypted: true}, 10000)
			s.cache = mc
			s.download()
			defer s.kill()
			if e := s.lastErr(); e != "" {
				t.Fatalf("unexpected error after successful resume: %s", e)
			}
			if got, want := pcmOf(t, fx.format, s.sb.reader()), pcmOf(t, fx.format, bytes.NewReader(fx.plain)); !bytes.Equal(got, want) {
				t.Fatalf("resumed play PCM %d bytes, want %d", len(got), len(want))
			}
			rc, size, ok := mc.Get(fx.id + "." + fx.format)
			if !ok {
				t.Fatal("resumed complete download was not cached")
			}
			got, _ := io.ReadAll(rc)
			rc.Close()
			if size != int64(len(fx.cipher)) || !bytes.Equal(got, fx.cipher) {
				t.Fatalf("cached entry is not the served ciphertext: %d bytes (size %d)", len(got), size)
			}
		})
	}
}

// TestStreamCacheShortBodyNotAdopted verifies that a body that never arrives
// whole (every resume torn too) leaves no cache entry and no file.
func TestStreamCacheShortBodyNotAdopted(t *testing.T) {
	for _, fx := range fixtures() {
		t.Run(fx.format, func(t *testing.T) {
			mc := newTestCache(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				start := 0
				if rng := r.Header.Get("Range"); rng != "" {
					start = parseRangeStart(t, rng)
					hdr := fmt.Sprintf("Content-Range: bytes %d-%d/%d\r\n", start, len(fx.cipher)-1, len(fx.cipher))
					tornRawResponse(t, w, "206 Partial Content", hdr, len(fx.cipher)-start, fx.cipher[start:start+10])
					return
				}
				tornRawResponse(t, w, "200 OK", "", len(fx.cipher), fx.cipher[:len(fx.cipher)/2])
			}))
			defer srv.Close()

			s := newSource(&deezer.StreamPlan{CDNURL: srv.URL, TrackID: fx.id, Format: fx.format, Encrypted: true}, 10000)
			s.cache = mc
			s.download()
			if n, b := mc.Stats(); n != 0 || b != 0 {
				t.Fatalf("Stats after a short body = %d entries / %d bytes, want 0 / 0", n, b)
			}
			name := s.sb.f.Name()
			s.kill()
			if _, err := os.Stat(name); !os.IsNotExist(err) {
				t.Fatalf("stream file after kill: %v", err)
			}
		})
	}
}

// TestStreamCacheSkipsPreviewAndPassthrough verifies that plain passthrough
// streams (podcasts) and preview plans never create cache entries.
func TestStreamCacheSkipsPreviewAndPassthrough(t *testing.T) {
	mc := newTestCache(t)

	// Plain passthrough (podcast-style, Encrypted=false).
	body := makeCipher(120000)
	cdn := newCountingCDN(t, body)
	s := newSource(&deezer.StreamPlan{CDNURL: cdn.srv.URL, TrackID: "ep1", Format: "MP3", Encrypted: false}, 10000)
	s.cache = mc
	s.download()
	if got := drainStreamFile(s.sb); !bytes.Equal(got, body) {
		t.Fatalf("passthrough play mismatch: got %d bytes, want %d", len(got), len(body))
	}

	// Preview (the 30-second free fallback) — excluded even when flagged
	// encrypted, so a preview can never shadow the full track under its key.
	cipher := makeCipher(80000)
	cdn2 := newCountingCDN(t, cipher)
	s2 := newSource(&deezer.StreamPlan{CDNURL: cdn2.srv.URL, TrackID: "3135556", Format: "MP3_128", Encrypted: true, Preview: true}, 10000)
	s2.cache = mc
	s2.download()
	drainStreamFile(s2.sb)

	if n, b := mc.Stats(); n != 0 || b != 0 {
		t.Fatalf("preview/passthrough created cache entries: %d entries / %d bytes", n, b)
	}
}

// TestStreamCacheTornHitFallsBackToHTTP verifies that a cached entry whose
// file was cut short on disk is a miss: the track streams over the network
// from the start, plays as the plaintext's PCM, and is cached whole again.
func TestStreamCacheTornHitFallsBackToHTTP(t *testing.T) {
	for _, fx := range fixtures() {
		t.Run(fx.format, func(t *testing.T) {
			dir := t.TempDir()
			mc, err := mediacache.New(dir, 1<<30)
			if err != nil {
				t.Fatal(err)
			}
			key := fx.id + "." + fx.format
			w, _ := mc.Put(key)
			_, _ = w.Write(fx.cipher)
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			des, _ := os.ReadDir(dir)
			for _, de := range des {
				if de.Name() != "index.json" && de.Name() != "lock" {
					if err := os.Truncate(filepath.Join(dir, de.Name()), 100000); err != nil {
						t.Fatal(err)
					}
				}
			}
			cdn := newCountingCDN(t, fx.cipher)

			s := newSource(&deezer.StreamPlan{CDNURL: cdn.srv.URL, TrackID: fx.id, Format: fx.format, Encrypted: true}, 10000)
			s.cache = mc
			s.download()
			defer s.kill()
			if got, want := pcmOf(t, fx.format, s.sb.reader()), pcmOf(t, fx.format, bytes.NewReader(fx.plain)); !bytes.Equal(got, want) {
				t.Fatalf("torn-cache play PCM %d bytes, want %d", len(got), len(want))
			}
			if h, start := cdn.hits.Load(), cdn.lastRange.Load(); h != 1 || start != -1 {
				t.Fatalf("HTTP fallback: %d requests, last Range start %d; want one full GET", h, start)
			}
			if _, size, ok := mc.Get(key); !ok || size != int64(len(fx.cipher)) {
				t.Fatalf("entry after the fallback: ok %v size %d, want the whole body", ok, size)
			}
		})
	}
}

// TestStreamCacheHitReadsCacheFile verifies that a cached play reads the
// cache entry's file itself (no copy, no HTTP), decodes to the plaintext's PCM,
// and leaves the entry in place when the source goes.
func TestStreamCacheHitReadsCacheFile(t *testing.T) {
	for _, fx := range fixtures() {
		t.Run(fx.format, func(t *testing.T) {
			mc := newTestCache(t)
			key := fx.id + "." + fx.format
			w, _ := mc.Put(key)
			_, _ = w.Write(fx.cipher)
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			s := newSource(&deezer.StreamPlan{TrackID: fx.id, Format: fx.format, Encrypted: true}, 10000)
			s.cache = mc
			s.download()
			if !s.sb.cached {
				t.Fatal("the cached play does not read the cache file")
			}
			if got, want := pcmOf(t, fx.format, s.sb.reader()), pcmOf(t, fx.format, bytes.NewReader(fx.plain)); !bytes.Equal(got, want) {
				t.Fatalf("cached play PCM %d bytes, want %d", len(got), len(want))
			}
			s.kill()
			if _, size, ok := mc.Get(key); !ok || size != int64(len(fx.cipher)) {
				t.Fatalf("entry after the cached play: ok %v size %d", ok, size)
			}
		})
	}
}

// The cache can be swapped while tracks start; each source keeps the cache it
// started with (run with -race).
func TestSetStreamCacheDuringPlayback(t *testing.T) {
	p := newPlayer(&mockOutput{})
	defer p.Close()
	mc := newTestCache(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			if i%2 == 0 {
				p.SetStreamCache(mc)
			} else {
				p.SetStreamCache(nil)
			}
		}
	}()
	for i := 0; i < 50; i++ {
		_ = p.Play(&deezer.StreamPlan{TrackID: "1", Format: "MP3_128", Encrypted: true}, 1000)
		p.Preload(&deezer.StreamPlan{TrackID: "2", Format: "MP3_128", Encrypted: true}, 1000)
	}
	<-done
	p.SetStreamCache(mc)
	_ = p.Play(&deezer.StreamPlan{TrackID: "3", Format: "MP3_128", Encrypted: true}, 1000)
	if c := p.cur.Load().cache; c != StreamCache(mc) {
		t.Fatal("a track started after SetStreamCache does not use the new cache")
	}
}

// The stream fields are stored with the body when the cache adopts it, so a
// track skipped after its download completed plays from the cache next time
// with no network.
func TestAdoptStoresMeta(t *testing.T) {
	for _, fx := range fixtures() {
		t.Run(fx.format, func(t *testing.T) {
			mc := newTestCache(t)
			cdn := newCountingCDN(t, fx.cipher)
			s := newSource(&deezer.StreamPlan{CDNURL: cdn.srv.URL, TrackID: fx.id, Format: fx.format, AskedFormat: "FLAC", Encrypted: true, GainDB: -4.5}, 10000)
			s.cache = mc
			s.download()
			s.kill() // skipped: the track never finishes
			m, ok := mc.GetMetaForTrack(fx.id)
			if !ok || m.Format != fx.format || m.AskedFormat != "FLAC" || !m.Encrypted || m.GainDB != -4.5 || m.Preview {
				t.Fatalf("meta after the download = %+v, %v; want %s asked as FLAC, encrypted, gain -4.5", m, ok, fx.format)
			}
		})
	}
}

// A source that started with a cache since closed (the folder changed, or the
// cache went off and on again) neither reads nor fills it: the track streams,
// the cache now on that folder gets no entry and keeps its index, and the
// temp file goes with the source.
func TestClosedCacheNotWritten(t *testing.T) {
	for _, fx := range fixtures() {
		t.Run(fx.format, func(t *testing.T) {
			dir := t.TempDir()
			old, err := mediacache.New(dir, 1<<30)
			if err != nil {
				t.Fatal(err)
			}
			cdn := newCountingCDN(t, fx.cipher)
			s := newSource(&deezer.StreamPlan{CDNURL: cdn.srv.URL, TrackID: fx.id, Format: fx.format, Encrypted: true}, 10000)
			s.cache = old
			old.Close()
			now, err := mediacache.New(dir, 1<<30)
			if err != nil {
				t.Fatal(err)
			}
			defer now.Close()
			idx, _ := os.ReadFile(filepath.Join(dir, "index.json"))

			s.download()
			if got, want := pcmOf(t, fx.format, s.sb.reader()), pcmOf(t, fx.format, bytes.NewReader(fx.plain)); !bytes.Equal(got, want) {
				t.Fatalf("PCM %d bytes, want %d", len(got), len(want))
			}
			if h := cdn.hits.Load(); h != 1 {
				t.Fatalf("%d requests, want the track streamed once", h)
			}
			name := s.sb.f.Name()
			s.kill()
			if n, _ := now.Stats(); n != 0 {
				t.Fatalf("the cache now on the folder has %d entries, want 0", n)
			}
			if after, _ := os.ReadFile(filepath.Join(dir, "index.json")); !bytes.Equal(after, idx) {
				t.Fatal("the closed cache rewrote the folder's index")
			}
			if _, err := os.Stat(name); !os.IsNotExist(err) {
				t.Fatalf("temp file after the source went: %v", err)
			}
		})
	}
}

// TestSetStreamCacheNilSafe covers the setter contract: nil-safe, installable
// and clearable before playback starts.
func TestSetStreamCacheNilSafe(t *testing.T) {
	p := &Player{}
	p.SetStreamCache(nil) // must not panic
	mc := newTestCache(t)
	p.SetStreamCache(mc)
	if p.cache() == nil {
		t.Fatal("SetStreamCache did not install the cache")
	}
	p.SetStreamCache(nil)
	if p.cache() != nil {
		t.Fatal("SetStreamCache(nil) did not clear the cache")
	}
}

// TestStreamCacheOfflineHit verifies that an offline play (plan.CDNURL == "")
// with a cache hit successfully decodes byte-identical PCM data with zero HTTP requests.
func TestStreamCacheOfflineHit(t *testing.T) {
	const trackID = "123456"
	cipher := makeCipher(100000)
	want, err := deezer.DecryptTrack(trackID, cipher)
	if err != nil {
		t.Fatal(err)
	}

	mc := newTestCache(t)
	// Seed the cache directly
	cacheKey := trackID + ".MP3_320"
	w, err := mc.Put(cacheKey)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(cipher)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// Create plan with empty CDNURL
	plan := &deezer.StreamPlan{
		CDNURL:    "",
		TrackID:   trackID,
		Format:    "MP3_320",
		Encrypted: true,
	}

	s := newSource(plan, 10000)
	s.cache = mc
	s.download()

	if got := drainStreamFile(s.sb); !bytes.Equal(got, want) {
		t.Fatalf("offline hit play mismatch: got %d bytes, want %d", len(got), len(want))
	}
	if e := s.lastErr(); e != "" {
		t.Fatalf("unexpected error on offline play: %s", e)
	}

	// Check that size handling worked correctly using the cached size
	if total := s.sb.Total(); total != int64(len(cipher)) {
		t.Fatalf("offline play content length = %d, want %d", total, len(cipher))
	}
}

// A plan from the cache whose entry went before the track opened (evicted,
// expired or cleared) streams over the network through its Refresh, and the
// body is cached again.
func TestCachedPlanMissStreams(t *testing.T) {
	for _, fx := range fixtures() {
		t.Run(fx.format, func(t *testing.T) {
			mc := newTestCache(t)
			cdn := newCountingCDN(t, fx.cipher)
			plan := &deezer.StreamPlan{TrackID: fx.id, Format: fx.format, Encrypted: true,
				Refresh: func() (*deezer.StreamPlan, error) {
					return &deezer.StreamPlan{CDNURL: cdn.srv.URL, TrackID: fx.id, Format: fx.format, Encrypted: true}, nil
				}}
			s := newSource(plan, 10000)
			s.cache = mc
			s.download()
			defer s.kill()
			if e := s.lastErr(); e != "" {
				t.Fatalf("a cached plan with its entry gone failed: %s", e)
			}
			if got, want := pcmOf(t, fx.format, s.sb.reader()), pcmOf(t, fx.format, bytes.NewReader(fx.plain)); !bytes.Equal(got, want) {
				t.Fatalf("PCM %d bytes, want %d", len(got), len(want))
			}
			if h := cdn.hits.Load(); h != 1 {
				t.Fatalf("%d requests, want 1", h)
			}
			if _, size, ok := mc.Get(fx.id + "." + fx.format); !ok || size != int64(len(fx.cipher)) {
				t.Fatalf("entry after the stream: ok %v size %d", ok, size)
			}
		})
	}
}

// drainRing reads a source's ring until its decoder has finished, bypassing
// the player's gain.
func drainRing(t *testing.T, s *source) []byte {
	t.Helper()
	var out []byte
	buf := make([]byte, 4096)
	deadline := time.Now().Add(10 * time.Second)
	for {
		n := s.ring.read(buf)
		out = append(out, buf[:n]...)
		if n > 0 {
			continue
		}
		if s.eof.Load() && s.ring.buffered() == 0 {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatal("the decoder did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

// A plan from the cache whose entry went before the source opened, and whose
// Refresh answers another format, plays that format: its decoder, its key for
// the cache and the meta stored with it, and the player publishes its format
// and gain while the source is current. The other fixture's format stands for
// the format the cache held.
func TestCachedPlanMissOtherFormat(t *testing.T) {
	fxs := fixtures()
	for i, fx := range fxs {
		other := fxs[1-i].format
		t.Run(other+"->"+fx.format, func(t *testing.T) {
			mc := newTestCache(t)
			cdn := newCountingCDN(t, fx.cipher)
			plan := &deezer.StreamPlan{TrackID: fx.id, Format: other, Encrypted: true, GainDB: -2,
				Refresh: func() (*deezer.StreamPlan, error) {
					return &deezer.StreamPlan{CDNURL: cdn.srv.URL, TrackID: fx.id, Format: fx.format, AskedFormat: "FLAC", Encrypted: true, GainDB: -6}, nil
				}}
			p := newPlayer(&mockOutput{})
			defer p.Close()
			p.SetStreamCache(mc)
			p.SetReplayGain(true)
			if err := p.Play(plan, 10000); err != nil {
				t.Fatal(err)
			}
			s := p.cur.Load()
			got := drainRing(t, s)
			if e := s.lastErr(); e != "" {
				t.Fatalf("the source failed: %s", e)
			}
			if want := pcmOf(t, fx.format, bytes.NewReader(fx.plain)); !bytes.Equal(got, want) {
				t.Fatalf("PCM %d bytes, want the %s plaintext's %d", len(got), fx.format, len(want))
			}
			if f := p.Format(); f != fx.format {
				t.Fatalf("published format %q, want %q", f, fx.format)
			}
			if g, want := math.Float64frombits(p.gainFac.Load()), dbToFactor(-6); g != want {
				t.Fatalf("published gain %v, want %v", g, want)
			}
			if f := s.plan().rgFac; f != dbToFactor(-6) {
				t.Fatalf("source gain %v, want %v", f, dbToFactor(-6))
			}
			waitFor(t, "the entry", func() bool {
				_, _, ok := mc.Get(fx.id + "." + fx.format)
				return ok
			})
			if m, ok := mc.GetMetaForTrack(fx.id); !ok || m.Format != fx.format || m.AskedFormat != "FLAC" || m.GainDB != -6 {
				t.Fatalf("meta %+v, %v; want %s asked as FLAC, gain -6", m, ok, fx.format)
			}
			if _, _, ok := mc.Get(fx.id + "." + other); ok {
				t.Fatalf("an entry under the cached plan's key %s", other)
			}
		})
	}
}

// A plan from the cache whose Refresh falls back to the unencrypted preview
// plays the preview as it is (no decryption), reports it as a preview and
// caches nothing.
func TestCachedPlanMissPreview(t *testing.T) {
	plain := makeMP3(120)
	mc := newTestCache(t)
	cdn := newCountingCDN(t, plain)
	plan := &deezer.StreamPlan{TrackID: "fx-FLAC", Format: "FLAC", Encrypted: true,
		Refresh: func() (*deezer.StreamPlan, error) {
			return &deezer.StreamPlan{CDNURL: cdn.srv.URL, TrackID: "fx-FLAC", Format: "MP3_128", Preview: true}, nil
		}}
	p := newPlayer(&mockOutput{})
	defer p.Close()
	p.SetStreamCache(mc)
	if err := p.Play(plan, 30000); err != nil {
		t.Fatal(err)
	}
	s := p.cur.Load()
	got := drainRing(t, s)
	if e := s.lastErr(); e != "" {
		t.Fatalf("the source failed: %s", e)
	}
	if want := pcmOf(t, "MP3_128", bytes.NewReader(plain)); !bytes.Equal(got, want) {
		t.Fatalf("PCM %d bytes, want %d", len(got), len(want))
	}
	if !p.IsPreview() || p.Format() != "MP3_128" {
		t.Fatalf("IsPreview %v, format %q; want a preview in MP3_128", p.IsPreview(), p.Format())
	}
	if n, _ := mc.Stats(); n != 0 {
		t.Fatalf("%d entries, want none for a preview", n)
	}
}

// A preloaded source whose plan changes at its open does not publish while
// another track is current; its promotion publishes the plan it plays.
func TestCachedPlanMissPreloadPublishesAtPromotion(t *testing.T) {
	fx := fixtures()[1]
	mc := newTestCache(t)
	cdn := newCountingCDN(t, fx.cipher)
	p := newPlayer(&mockOutput{})
	defer p.Close()
	p.SetStreamCache(mc)
	if err := p.Play(&deezer.StreamPlan{TrackID: "cur", Format: "MP3_128"}, 10000); err != nil {
		t.Fatal(err)
	}
	cur := p.cur.Load()
	p.Preload(&deezer.StreamPlan{TrackID: fx.id, Format: "MP3_128", Encrypted: true,
		Refresh: func() (*deezer.StreamPlan, error) {
			return &deezer.StreamPlan{CDNURL: cdn.srv.URL, TrackID: fx.id, Format: fx.format, Encrypted: true}, nil
		}}, 10000)
	next := p.next.Load()
	waitFor(t, "the preload's plan", func() bool { return next.plan().Format == fx.format })
	if f := p.Format(); f != "MP3_128" {
		t.Fatalf("published format %q while the preload is not current, want MP3_128", f)
	}
	if !p.advanceToNext(cur, next) {
		t.Fatal("the preload was not promoted")
	}
	if f := p.Format(); f != fx.format {
		t.Fatalf("published format %q after the promotion, want %q", f, fx.format)
	}
}

// TestStreamCacheOfflineMiss verifies that an offline play (plan.CDNURL == "")
// with a cache miss fails immediately with ErrOffline, without panic or HTTP dialing.
func TestStreamCacheOfflineMiss(t *testing.T) {
	mc := newTestCache(t)
	// Create plan with empty CDNURL and no entry in cache
	plan := &deezer.StreamPlan{
		CDNURL:    "",
		TrackID:   "not_cached_track",
		Format:    "MP3_320",
		Encrypted: true,
	}

	s := newSource(plan, 10000)
	s.cache = mc
	s.download()

	// Ensure the source error was set to ErrOffline
	gotErr := s.lastErr()
	if gotErr != ErrOffline.Error() {
		t.Fatalf("expected error %q, got %q", ErrOffline.Error(), gotErr)
	}

	// Reading the stream file should return the same error
	_, err := s.sb.reader().Read(make([]byte, 100))
	if err == nil {
		t.Fatal("expected read error from the stream file, got nil")
	}
	if !errors.Is(err, ErrOffline) {
		t.Fatalf("expected stream file error errors.Is(ErrOffline), got %v", err)
	}
}
