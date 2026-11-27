package engine

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// A picture is downloaded once: the second Fetch and one in offline mode come from the
// cache folder, and a picture never fetched is still refused offline.
func TestFetchKeepsPictures(t *testing.T) {
	base := cacheProfile(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("picture " + r.URL.Path))
	}))
	defer srv.Close()

	for range 2 {
		b, err := Fetch(srv.URL + "/a.jpg")
		if err != nil || string(b) != "picture /a.jpg" {
			t.Fatalf("Fetch = %q, %v", b, err)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("server hit %d times, want 1", n)
	}
	if entries, _ := os.ReadDir(filepath.Join(base, "pictures")); len(entries) != 1 || strings.HasSuffix(entries[0].Name(), ".part") {
		t.Fatalf("pictures folder holds %v", entries)
	}

	SetOfflineMode(true)
	defer SetOfflineMode(false)
	if b, err := Fetch(srv.URL + "/a.jpg"); err != nil || string(b) != "picture /a.jpg" {
		t.Fatalf("offline, kept picture: %q, %v", b, err)
	}
	if _, err := Fetch(srv.URL + "/b.jpg"); err != ErrOfflineMode {
		t.Fatalf("offline, new picture: %v, want ErrOfflineMode", err)
	}
}

