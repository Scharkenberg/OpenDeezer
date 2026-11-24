package deezer

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// A REST answer over the request quota is retried once after a wait.
func TestRestQuotaRetriedOnce(t *testing.T) {
	prev := restQuotaWait
	restQuotaWait = 50 * time.Millisecond
	defer func() { restQuotaWait = prev }()
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"error":{"type":"Exception","message":"Quota limit exceeded","code":4}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"5","title":"T"}`))
	}))
	defer ts.Close()
	c := New("arl")
	c.restURLOverride = ts.URL
	b, err := c.restGet("/track/5")
	if err != nil || len(b) == 0 || calls.Load() != 2 {
		t.Fatalf("%d calls, %q, %v", calls.Load(), b, err)
	}
}
