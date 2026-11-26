package deezer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The genre list starts with "All" (id 0) whatever REST lists, and a genre's charts ask
// /chart/<id>.
func TestGenresAndGenreCharts(t *testing.T) {
	var asked string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/genre":
			_, _ = w.Write([]byte(`{"data":[{"id":0,"name":"Tous"},{"id":132,"name":"Pop"},{"id":116,"name":"Rap"}]}`))
		default:
			asked = r.URL.Path
			_, _ = w.Write([]byte(`{"tracks":{"data":[{"id":1,"title":"A","album":{"id":7,"title":"X"}}]}}`))
		}
	}))
	defer ts.Close()
	c := New("dummy_arl")
	c.restURLOverride = ts.URL
	gs, err := c.Genres()
	if err != nil || len(gs) != 3 || gs[0] != (Genre{"0", "All"}) || gs[1] != (Genre{"132", "Pop"}) {
		t.Fatalf("genres %+v, %v", gs, err)
	}
	ch, err := c.Charts("132")
	if err != nil || asked != "/chart/132" || len(ch.Tracks) != 1 {
		t.Fatalf("charts asked %q, %+v, %v", asked, ch, err)
	}
}

// The artist page reads every album page while REST names a next one.
func TestArtistAlbumsFollowNext(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/artist/5/albums" && r.URL.Query().Get("index") == "0":
			_, _ = w.Write([]byte(`{"data":[{"id":1,"title":"One"},{"id":2,"title":"Two"}],"next":"https://api.deezer.com/artist/5/albums?limit=100&index=100"}`))
		case r.URL.Path == "/artist/5/albums" && r.URL.Query().Get("index") == "100":
			_, _ = w.Write([]byte(`{"data":[{"id":3,"title":"Three"}]}`))
		case r.URL.Path == "/artist/5":
			_, _ = w.Write([]byte(`{"id":5,"name":"Five"}`))
		default:
			_, _ = w.Write([]byte(`{"data":[]}`))
		}
	}))
	defer ts.Close()
	c := New("dummy_arl")
	c.restURLOverride = ts.URL
	page, err := c.ArtistProfile("5")
	if err != nil || len(page.Albums) != 3 || page.Albums[2].Name != "Three" {
		t.Fatalf("albums %+v, %v", page.Albums, err)
	}
}

// A track read from REST and one read from the gateway both carry their album's id.
func TestTracksCarryAlbumID(t *testing.T) {
	var rest restTrackDTO
	if err := json.Unmarshal([]byte(`{"id":1,"title":"A","album":{"id":302127,"title":"Discovery"}}`), &rest); err != nil {
		t.Fatal(err)
	}
	var gw gwTrackDTO
	if err := json.Unmarshal([]byte(`{"SNG_ID":"2","SNG_TITLE":"B","ALB_ID":"302127","ALB_TITLE":"Discovery"}`), &gw); err != nil {
		t.Fatal(err)
	}
	for name, tr := range map[string]Track{"rest": rest.toTrack(), "gw": gw.toTrack()} {
		if tr.AlbumID != "302127" {
			t.Errorf("%s track album id %q, want 302127", name, tr.AlbumID)
		}
	}
}
