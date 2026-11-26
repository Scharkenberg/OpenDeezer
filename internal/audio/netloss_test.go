package audio

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
)

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// netLoss serves fx's first half as a torn full response, every Range request in full,
// and answers every request after the first as a lost network while down reports true.
func netLoss(t *testing.T, fx fixture, down func() bool) string {
	t.Helper()
	half := len(fx.cipher) / 2
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rng := r.Header.Get("Range"); rng != "" {
			start := parseRangeStart(t, rng)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(fx.cipher)-1, len(fx.cipher)))
			w.Header().Set("Content-Length", strconv.Itoa(len(fx.cipher)-start))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(fx.cipher[start:])
			return
		}
		tornRawResponse(t, w, "200 OK", "", len(fx.cipher), fx.cipher[:half])
	}))
	t.Cleanup(srv.Close)
	orig := deezer.Transport()
	var first atomic.Bool
	deezer.SetTransport(rtFunc(func(r *http.Request) (*http.Response, error) {
		if first.Swap(true) && down() {
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no route", Name: r.URL.Host}}
		}
		return orig.RoundTrip(r)
	}))
	t.Cleanup(func() { deezer.SetTransport(orig) })
	return srv.URL
}

// A network that comes back within the wait: the download goes on and the track is whole.
func TestStreamWaitsForNetwork(t *testing.T) {
	for _, fx := range fixtures() {
		t.Run(fx.format, func(t *testing.T) {
			back := time.Now().Add(2 * time.Second)
			url := netLoss(t, fx, func() bool { return time.Now().Before(back) })
			s := newSource(&deezer.StreamPlan{CDNURL: url, TrackID: fx.id, Format: fx.format, Encrypted: true}, 10000)
			runDownload(t, s, fx)
			defer s.kill()
			if e := s.lastErr(); e != "" {
				t.Fatalf("download error after the network came back: %s", e)
			}
			if s.sb.Total() != int64(len(fx.cipher)) {
				t.Fatalf("stream total %d, want %d", s.sb.Total(), len(fx.cipher))
			}
		})
	}
}

// A network that stays away: the stream gives up after the wait, marked as a lost network.
func TestStreamGivesUpOnNetworkLoss(t *testing.T) {
	prev := deezer.NetDryWait
	deezer.NetDryWait = 300 * time.Millisecond
	defer func() { deezer.NetDryWait = prev }()
	for _, fx := range fixtures() {
		t.Run(fx.format, func(t *testing.T) {
			url := netLoss(t, fx, func() bool { return true })
			s := newSource(&deezer.StreamPlan{CDNURL: url, TrackID: fx.id, Format: fx.format, Encrypted: true}, 10000)
			runDownload(t, s, fx)
			defer s.kill()
			if s.lastErr() == "" || !s.errNet.Load() {
				t.Fatalf("error %q, network %v", s.lastErr(), s.errNet.Load())
			}
		})
	}
}

// A track whose stream failed after some audio ends Errored, its error kept with its mark.
func TestFailedStreamEndsErrored(t *testing.T) {
	p := newPlayer(&mockOutput{})
	go p.manage()
	defer p.Close()
	src := newSource(&deezer.StreamPlan{Format: "MP3"}, 1000)
	src.setErr(fmt.Errorf("%w: dial tcp", deezer.ErrNoNetwork))
	src.decoded.Store(50000)
	src.eof.Store(true)
	finished := make(chan bool, 1)
	p.SetOnFinish(func() { finished <- true })
	p.cur.Store(src)
	p.state.Store(int32(Playing))
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("onFinish was not called")
	}
	if p.State() != Errored || p.LastError() == "" || !p.LastErrorNetwork() {
		t.Fatalf("state %v, error %q, network %v", p.State(), p.LastError(), p.LastErrorNetwork())
	}
}

func runDownload(t *testing.T, s *source, fx fixture) {
	t.Helper()
	done := make(chan struct{})
	go func() { s.download(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		s.kill()
		t.Fatal("the download hung")
	}
}
