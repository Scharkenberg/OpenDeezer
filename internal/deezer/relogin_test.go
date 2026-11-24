package deezer

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
)

// Parallel calls that all meet a stale token log in once, and each retries
// with the new one.
func TestStaleTokenLogsInOnce(t *testing.T) {
	var logins atomic.Int32
	c := New("arl")
	c.mu.Lock()
	c.apiToken = "stale"
	c.mu.Unlock()
	c.http = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		q := r.URL.Query()
		if q.Get("method") == "deezer.getUserData" {
			logins.Add(1)
			return jsonResponse([]byte(`{"error":{},"results":{"checkForm":"fresh","USER":{"USER_ID":"7","OPTIONS":{"license_token":"l"}}}}`)), nil
		}
		if q.Get("api_token") != "fresh" {
			return jsonResponse([]byte(`{"error":{"VALID_TOKEN_REQUIRED":"Invalid CSRF token"},"results":{}}`)), nil
		}
		return jsonResponse([]byte(`{"error":{},"results":{}}`)), nil
	})}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.gw("song.getData", "{}"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := logins.Load(); n != 1 {
		t.Fatalf("%d logins for 8 calls on a stale token, want 1", n)
	}
}
