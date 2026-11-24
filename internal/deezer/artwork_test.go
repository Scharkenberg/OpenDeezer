package deezer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A cover download whose body never comes ends after the artwork timeout.
func TestArtworkFetchStallEnds(t *testing.T) {
	prev := artworkTimeout
	artworkTimeout = 300 * time.Millisecond
	defer func() { artworkTimeout = prev }()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	start := time.Now()
	if _, err := defaultArtworkFetcher(context.Background(), srv.URL+"/cover.jpg"); err == nil {
		t.Fatal("a stalled cover download returned no error")
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("the stalled cover download took %v", took)
	}
}
