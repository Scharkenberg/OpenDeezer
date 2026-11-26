package audio

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/mediacache"
)

// A fetch for offline use streams the body into the cache through the stream
// file, resuming a torn transfer, and the cache adopts it whole with its meta.
func TestFetchStoresResumedBody(t *testing.T) {
	for _, fx := range fixtures() {
		t.Run(fx.format, func(t *testing.T) {
			mc := newTestCache(t)
			srv := flakyServer(t, fx.cipher, len(fx.cipher)/2+7)
			defer srv.Close()
			plan := &deezer.StreamPlan{CDNURL: srv.URL, TrackID: fx.id, Format: fx.format, AskedFormat: "FLAC", Encrypted: true, GainDB: -3}
			if err := Fetch(context.Background(), plan, mc); err != nil {
				t.Fatal(err)
			}
			rc, size, ok := mc.Get(fx.id + "." + fx.format)
			if !ok {
				t.Fatal("the fetched body is not in the cache")
			}
			got, _ := io.ReadAll(rc)
			rc.Close()
			if size != int64(len(fx.cipher)) || !bytes.Equal(got, fx.cipher) {
				t.Fatalf("cached %d bytes, want the served %d", len(got), len(fx.cipher))
			}
			if m, ok := mc.GetMeta(fx.id + "." + fx.format); !ok || m.AskedFormat != "FLAC" || m.GainDB != -3 || !m.Encrypted {
				t.Fatalf("meta %+v %v", m, ok)
			}
		})
	}
}

// A body that never arrives whole is not stored, and the fetch says so.
func TestFetchShortBodyFails(t *testing.T) {
	mc := newTestCache(t)
	body := makeCipher(100000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := 0
		if rng := r.Header.Get("Range"); rng != "" {
			start = parseRangeStart(t, rng)
		}
		tornRawResponse(t, w, "200 OK", "", len(body)-start, body[start:start+10])
	}))
	defer srv.Close()
	plan := &deezer.StreamPlan{CDNURL: srv.URL, TrackID: "7", Format: "MP3_128", Encrypted: true}
	if err := Fetch(context.Background(), plan, mc); err == nil {
		t.Fatal("a short body fetched without an error")
	}
	if n, _ := mc.Stats(); n != 0 {
		t.Fatalf("%d entries after a short body", n)
	}
}

// readOnlyTemps is a cache whose temp files cannot be written, as on a full
// disk.
type readOnlyTemps struct{ *mediacache.Cache }

func (c readOnlyTemps) NewTemp() (*os.File, error) {
	f, err := c.Cache.NewTemp()
	if err != nil {
		return nil, err
	}
	name := f.Name()
	_ = f.Close()
	return os.Open(name)
}

// A stream that cannot be written fails the fetch with ErrWrite and the
// reason.
func TestFetchWriteFailure(t *testing.T) {
	mc := newTestCache(t)
	cdn := newCountingCDN(t, makeCipher(100000))
	plan := &deezer.StreamPlan{CDNURL: cdn.srv.URL, TrackID: "7", Format: "MP3_128", Encrypted: true}
	err := Fetch(context.Background(), plan, readOnlyTemps{mc})
	if !errors.Is(err, ErrWrite) || err.Error() == ErrWrite.Error() {
		t.Fatalf("Fetch = %v, want ErrWrite with the reason", err)
	}
}

// A fetch that ends on a lost network says so.
func TestFetchNetworkLossIsNoNetwork(t *testing.T) {
	prev := deezer.NetDryWait
	deezer.NetDryWait = 300 * time.Millisecond
	defer func() { deezer.NetDryWait = prev }()
	orig := deezer.SetTransport(rtFunc(func(r *http.Request) (*http.Response, error) {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	}))
	defer deezer.SetTransport(orig)
	plan := &deezer.StreamPlan{CDNURL: "http://127.0.0.1:1/a", TrackID: "7", Format: "MP3_128", Encrypted: true}
	if err := Fetch(context.Background(), plan, newTestCache(t)); !deezer.IsNoNetwork(err) {
		t.Fatalf("Fetch = %v, want a lost network", err)
	}
}

// Canceling the context stops a fetch that waits on the network.
func TestFetchCancel(t *testing.T) {
	mc := newTestCache(t)
	body := makeCipher(100000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body[:1000])
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Fetch(ctx, &deezer.StreamPlan{CDNURL: srv.URL, TrackID: "7", Format: "MP3_128", Encrypted: true}, mc)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Fetch = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the fetch did not stop")
	}
	if n, _ := mc.Stats(); n != 0 {
		t.Fatalf("%d entries after a cancel", n)
	}
}
