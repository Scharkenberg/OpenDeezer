package deezer

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// An episode listed without a picture shows its show's: the listing leaves
// picture_medium empty for many episodes, and the show's own is read once.
func TestPodcastEpisodes_MissingCoverTakesTheShows(t *testing.T) {
	shows := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/podcast/7/episodes":
			_, _ = w.Write([]byte(`{"data":[{"id":"1","title":"A","picture_medium":"own"},{"id":"2","title":"B","picture_medium":""}]}`))
		case "/podcast/7":
			shows++
			_, _ = w.Write([]byte(`{"id":"7","title":"Show","picture_medium":"show"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	c := New("dummy")
	c.restURLOverride = ts.URL
	eps, err := c.PodcastEpisodes("7")
	if err != nil || len(eps) != 2 || eps[0].ArtworkURL != "own" || eps[1].ArtworkURL != "show" || shows != 1 {
		t.Fatalf("episodes %+v, err %v, %d show reads; want own, show and one read", eps, err, shows)
	}
}