package deezer

import (
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

// Transport carries every request of the engine to Deezer: the gateway, the
// REST API, media URLs, streams, downloads and pictures share its connections,
// so a stream or a download reuses the connection a resolve opened. Dial and
// TLS timeouts guard connection setup and ResponseHeaderTimeout the first
// answer; a body may take as long as it lasts (a stalled body is closed by its
// reader's stall timer, and the caller's context ends it). A client takes it
// when made (HTTPClient, New). Tests replace it with SetTransport, also while
// a stream of an earlier test still makes its client.
func Transport() http.RoundTripper { return transport.Load().(roundTripper).RoundTripper }

// SetTransport makes rt the transport of every client made from now on and
// returns the one before.
func SetTransport(rt http.RoundTripper) http.RoundTripper {
	return transport.Swap(roundTripper{rt}).(roundTripper).RoundTripper
}

// roundTripper gives atomic.Value one concrete type to hold.
type roundTripper struct{ http.RoundTripper }

var transport = func() *atomic.Value {
	var v atomic.Value
	v.Store(roundTripper{&http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: time.Second,
		IdleConnTimeout:       90 * time.Second,
	}})
	return &v
}()

// HTTPClient returns a client on Transport; timeout bounds each whole request
// (0: none, for streams and downloads of any length).
func HTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Transport: Transport(), Timeout: timeout}
}
