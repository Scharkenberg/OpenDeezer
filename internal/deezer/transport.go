package deezer

import (
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

// Transport carries every request of the engine to Deezer, so streams reuse the
// connections resolves opened. Timeouts cover dialing, TLS and the response header, not
// the body. Tests swap it with SetTransport.
func Transport() http.RoundTripper { return transport.Load().(roundTripper).RoundTripper }

// SetTransport makes rt the transport of every client made after the call and
// returns the previous one.
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
		// An HTTP/2 connection that received nothing for 15 s is pinged and dropped when the
		// ping goes unanswered for 5 s, so one left dead by a sleep does not hang a request.
		HTTP2: &http.HTTP2Config{SendPingTimeout: 15 * time.Second, PingTimeout: 5 * time.Second},
	}})
	return &v
}()

// HTTPClient returns a client on Transport; timeout bounds each whole request
// (0: none, for streams and downloads of any length).
func HTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Transport: Transport(), Timeout: timeout}
}
