package deezer

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// gwRecorder is a logged-in client whose gateway calls are recorded as
// method -> bodies.
func gwRecorder(t *testing.T) (*Client, map[string][]string) {
	t.Helper()
	calls := map[string][]string{}
	c := New("dummy")
	loggedInPremium(c)
	c.http = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		m := r.URL.Query().Get("method")
		calls[m] = append(calls[m], string(b))
		return jsonResponse([]byte(`{"error":{},"results":false}`)), nil
	})}
	return c, calls
}

// log.listen carries the finished listen as params and the track that starts
// as next_media, as Deezer's players send them; either may be missing.
func TestLogListenBody(t *testing.T) {
	c, calls := gwRecorder(t)
	params := json.RawMessage(`{"media":{"id":"7","type":"song","format":"MP3_320"},"lt":42}`)
	if err := c.LogListen(params, &ListenMedia{ID: "8", Type: "song"}); err != nil {
		t.Fatal(err)
	}
	if err := c.LogListen(nil, &ListenMedia{ID: "9", Type: "episode"}); err != nil {
		t.Fatal(err)
	}
	if err := c.LogListen(params, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{
		`{"params":{"dev":{"t":0,"v":"` + fallbackRelease + `"},"lt":42,"media":{"id":"7","type":"song","format":"MP3_320"}},"next_media":{"media":{"id":"8","type":"song"}}}`,
		`{"next_media":{"media":{"id":"9","type":"episode"}}}`,
		`{"params":{"dev":{"t":0,"v":"` + fallbackRelease + `"},"lt":42,"media":{"id":"7","type":"song","format":"MP3_320"}}}`,
	}
	got := calls["log.listen"]
	if len(got) != len(want) {
		t.Fatalf("log.listen bodies %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("body %d %s, want %s", i, got[i], want[i])
		}
	}
}

// Listens that could not be sent go together in log.batch_listen.
func TestLogListensBatch(t *testing.T) {
	c, calls := gwRecorder(t)
	if err := c.LogListens([]json.RawMessage{json.RawMessage(`{"lt":1}`), json.RawMessage(`{"lt":2}`)}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(calls["log.batch_listen"], "|"); got != `{"array":[{"dev":{"t":0,"v":"`+fallbackRelease+`"},"lt":1},{"dev":{"t":0,"v":"`+fallbackRelease+`"},"lt":2}]}` {
		t.Fatalf("log.batch_listen %s", got)
	}
}

// With reporting turned off nothing is sent.
func TestLogListenOff(t *testing.T) {
	c, calls := gwRecorder(t)
	c.SetAdsDisabled(true)
	_ = c.LogListen(json.RawMessage(`{}`), &ListenMedia{ID: "8", Type: "song"})
	_ = c.LogListens([]json.RawMessage{json.RawMessage(`{}`)})
	if len(calls["log.listen"])+len(calls["log.batch_listen"]) != 0 {
		t.Fatalf("calls %v with reporting off", calls)
	}
}
