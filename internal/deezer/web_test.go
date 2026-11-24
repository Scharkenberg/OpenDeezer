package deezer

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// webFake answers like www.deezer.com: the gateway, the logged-in page naming
// the web player's script, and that script with its build number. It records
// every request.
type webFake struct {
	mu       sync.Mutex
	reqs     []*http.Request
	bodies   []string
	bundle   string // the script the page names
	release  string // the build number in it
	bundleGB int    // script downloads
}

func (f *webFake) client() *Client {
	c := New("dummy")
	c.http = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		var b []byte
		if r.Body != nil {
			b, _ = io.ReadAll(r.Body)
		}
		f.mu.Lock()
		f.reqs = append(f.reqs, r)
		f.bodies = append(f.bodies, string(b))
		f.mu.Unlock()
		switch {
		case strings.Contains(r.URL.RawQuery, "method=deezer.getUserData"):
			return jsonResponse([]byte(`{"error":{},"results":{"checkForm":"api-t","SERVER_TIMESTAMP":1795440000,` +
				`"USER":{"USER_ID":"7","OPTIONS":{"license_token":"lic"}},"OFFERS":[{"title":"P"}]}}`)), nil
		case strings.Contains(r.URL.RawQuery, "method="):
			return jsonResponse([]byte(`{"error":{},"results":true}`)), nil
		case r.URL.Host == "www.deezer.com":
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`<html><script src="` + f.bundle + `"></script></html>`))}, nil
		case r.URL.String() == f.bundle:
			f.mu.Lock()
			f.bundleGB++
			f.mu.Unlock()
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 5000) +
				`env:"production",release:"1.0.0+x",releaseNumeric:"` + f.release + `",bot:1`))}, nil
		}
		return nil, io.ErrUnexpectedEOF
	})}
	return c
}

func (f *webFake) requests() ([]*http.Request, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*http.Request(nil), f.reqs...), append([]string(nil), f.bodies...)
}

// Gateway calls go out as Deezer's web player sends them: a browser's
// User-Agent, its origin, the user header, and a random cid.
func TestGatewayCallsLikeWebPlayer(t *testing.T) {
	f := &webFake{bundle: "https://cdn-files.dzcdn.net/cache/js/app-web.aaaa.js", release: "10020261123132810"}
	c := f.client()
	if err := c.Login(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.gw("song.getData", `{"sng_id":"1"}`); err != nil {
		t.Fatal(err)
	}
	reqs, _ := f.requests()
	var gw *http.Request
	for _, r := range reqs {
		if r.URL.Query().Get("method") == "song.getData" {
			gw = r
		}
	}
	if gw == nil {
		t.Fatal("no gateway call")
	}
	q := gw.URL.Query()
	if q.Get("input") != "3" || q.Get("api_version") != "1.0" || q.Get("api_token") != "api-t" {
		t.Errorf("query %s", gw.URL.RawQuery)
	}
	if cid, err := strconv.Atoi(q.Get("cid")); err != nil || cid < 0 || cid >= 1e9 {
		t.Errorf("cid %q, want a random number below 1e9", q.Get("cid"))
	}
	if ua := gw.Header.Get("User-Agent"); !strings.HasPrefix(ua, "Mozilla/5.0 (") || !strings.Contains(ua, " Chrome/") || strings.Contains(ua, "OpenDeezer") {
		t.Errorf("User-Agent %q, want a browser's", ua)
	}
	if gw.Header.Get("Origin") != "https://www.deezer.com" || gw.Header.Get("Referer") != "https://www.deezer.com/" || gw.Header.Get("X-Deezer-User") != "7" {
		t.Errorf("headers %v", gw.Header)
	}
}

// After a login (LearnWebRelease) the client learns the web player's build number from the
// script the logged-in page names, and fetches that script again only when
// the page names another.
func TestLoginLearnsWebRelease(t *testing.T) {
	SetWebRelease("", "")
	t.Cleanup(func() { SetWebRelease("", "") })
	f := &webFake{bundle: "https://cdn-files.dzcdn.net/cache/js/app-web.aaaa.js", release: "10020261123150000"}
	if got, _ := WebRelease(); got != fallbackRelease {
		t.Fatalf("release before any login %q, want the fallback %q", got, fallbackRelease)
	}
	c := f.client()
	if err := c.Login(); err != nil {
		t.Fatal(err)
	}
	go c.LearnWebRelease()
	waitRelease := func(want string) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for {
			if got, _ := WebRelease(); got == want {
				return
			}
			if time.Now().After(deadline) {
				got, bundle := WebRelease()
				t.Fatalf("release %q (bundle %q), want %q", got, bundle, want)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	waitRelease("10020261123150000")
	c.LearnWebRelease()
	if f.bundleGB != 1 {
		t.Fatalf("%d script downloads for one script, want 1", f.bundleGB)
	}
	f.bundle, f.release = "https://cdn-files.dzcdn.net/cache/js/app-web.bbbb.js", "10020261124090000"
	go c.LearnWebRelease()
	waitRelease("10020261124090000")
}

// The listen log carries the web player's build as dev.v, as its own reports
// do; the client's clock follows Deezer's (SERVER_TIMESTAMP).
func TestListenLogCarriesWebRelease(t *testing.T) {
	SetWebRelease("https://cdn-files.dzcdn.net/cache/js/app-web.aaaa.js", "10020261123132810")
	t.Cleanup(func() { SetWebRelease("", "") })
	f := &webFake{bundle: "https://cdn-files.dzcdn.net/cache/js/app-web.aaaa.js", release: "10020261123132810"}
	c := f.client()
	if err := c.Login(); err != nil {
		t.Fatal(err)
	}
	if d := c.ServerNow().Unix() - 1795440000; d < -5 || d > 5 {
		t.Errorf("server clock %v, want about the login's SERVER_TIMESTAMP", c.ServerNow())
	}
	if err := c.LogListen(json.RawMessage(`{"lt":3}`), nil); err != nil {
		t.Fatal(err)
	}
	if err := c.LogListens([]json.RawMessage{json.RawMessage(`{"lt":4}`)}); err != nil {
		t.Fatal(err)
	}
	reqs, bodies := f.requests()
	var got []string
	for i, r := range reqs {
		if m := r.URL.Query().Get("method"); m == "log.listen" || m == "log.batch_listen" {
			got = append(got, bodies[i])
		}
	}
	want := []string{
		`{"params":{"dev":{"t":0,"v":"10020261123132810"},"lt":3}}`,
		`{"array":[{"dev":{"t":0,"v":"10020261123132810"},"lt":4}]}`,
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("listen log %q, want %q", got, want)
	}
}

// A login Deezer refuses (an invalid ARL), also the re-login of a call whose token
// went stale, marks the client refused until a login works.
func TestRefusedLoginMarksClient(t *testing.T) {
	refuse := false
	c := New("dummy")
	c.http = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		q := r.URL.RawQuery
		switch {
		case strings.Contains(q, "method=deezer.getUserData") && refuse:
			return jsonResponse([]byte(`{"error":{},"results":{"checkForm":"","USER":{"USER_ID":0}}}`)), nil
		case strings.Contains(q, "method=deezer.getUserData"):
			return jsonResponse([]byte(`{"error":{},"results":{"checkForm":"api-t","USER":{"USER_ID":"7","OPTIONS":{"license_token":"lic"}},"OFFERS":[]}}`)), nil
		}
		return jsonResponse([]byte(`{"error":{"VALID_TOKEN_REQUIRED":"Invalid CSRF token"},"results":{}}`)), nil
	})}
	if err := c.Login(); err != nil || c.Refused() {
		t.Fatalf("login %v, refused %v", err, c.Refused())
	}
	refuse = true
	if _, err := c.gw("song.getData", `{}`); !IsARLExpired(err) || !c.Refused() {
		t.Fatalf("gw %v, refused %v; want the ARL refused", err, c.Refused())
	}
	refuse = false
	if err := c.Login(); err != nil || c.Refused() {
		t.Fatalf("login again %v, refused %v", err, c.Refused())
	}
}
