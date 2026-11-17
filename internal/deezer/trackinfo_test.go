package deezer

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Fixtures follow the field names of gw song.getData and REST /track/{id} and
// /album/{id}; values are realistic but not captured from a live account.
const (
	gwSongDataFixture = `{"error":{},"results":{"SNG_ID":"3135556","SNG_TITLE":"Harder, Better, Faster, Stronger","VERSION":"(Live)",` +
		`"ART_ID":"27","ART_NAME":"Daft Punk",` +
		`"ARTISTS":[{"ART_ID":"27","ROLE_ID":"0","ARTISTS_SONGS_ORDER":"0","ART_NAME":"Daft Punk","ART_PICTURE":"f2bc007e9133c946ac3c3907ddc5d2ea","__TYPE__":"artist"},` +
		`{"ART_ID":"1234","ROLE_ID":"5","ARTISTS_SONGS_ORDER":"1","ART_NAME":"Guest Singer","ART_PICTURE":"aa11","__TYPE__":"artist"}],` +
		`"ALB_ID":"302127","ALB_TITLE":"Discovery","ALB_PICTURE":"2e018122cb56986277102d2041a592c8",` +
		`"DURATION":"224","TRACK_NUMBER":"4","DISK_NUMBER":"1","GAIN":"-9.8","ISRC":"GBDUW0000059",` +
		`"EXPLICIT_LYRICS":"1","PHYSICAL_RELEASE_DATE":"2001-03-07","DIGITAL_RELEASE_DATE":"2001-03-12",` +
		`"COPYRIGHT":"(P) 2001 Daft Life Ltd.","TRACK_TOKEN":"tok",` +
		`"SNG_CONTRIBUTORS":{"main_artist":["Daft Punk"],"composer":["Thomas Bangalter","Guy-Manuel de Homem-Christo","Edwin Birdsong"],` +
		`"author":["Edwin Birdsong"],"producer":["Daft Punk"],"mixer":["Thomas Bangalter"],"musicpublisher":["Because Editions"]},` +
		`"__TYPE__":"song"}}`

	restTrackFixture = `{"id":3135556,"readable":true,"title":"Harder, Better, Faster, Stronger (Live)","title_short":"Harder, Better, Faster, Stronger",` +
		`"title_version":"(Live)","isrc":"GBDUW0000059","duration":224,"track_position":4,"disk_number":1,"rank":900000,` +
		`"release_date":"2001-03-07","explicit_lyrics":false,"bpm":123.4,"gain":-9.8,` +
		`"contributors":[{"id":27,"name":"Daft Punk","picture_medium":"https://cdn.test/artist/27/250x250.jpg","picture_xl":"https://cdn.test/artist/27/1000x1000.jpg","type":"artist","role":"Main"},` +
		`{"id":1234,"name":"Guest Singer","picture_medium":"https://cdn.test/artist/1234/250x250.jpg","type":"artist","role":"Featured"}],` +
		`"artist":{"id":27,"name":"Daft Punk"},` +
		`"album":{"id":302127,"title":"Discovery","cover_medium":"https://cdn.test/cover/250x250.jpg","cover_xl":"https://cdn.test/cover/1000x1000.jpg","release_date":"2001-03-07","type":"album"},` +
		`"type":"track"}`

	restAlbumFixture = `{"id":302127,"title":"Discovery","upc":"724384960650","cover_xl":"https://cdn.test/cover/1000x1000.jpg",` +
		`"genre_id":113,"genres":{"data":[{"id":113,"name":"Dance","type":"genre"},{"id":106,"name":"Electro","type":"genre"}]},` +
		`"label":"Parlophone (France)","nb_tracks":14,"duration":3660,"release_date":"2001-03-07","record_type":"album","type":"album"}`
)

// trackInfoServer serves gw, REST track and REST album fixtures from one
// loopback httptest server; an empty body answers HTTP 500. The returned
// counters record how often each source was hit. delay holds every answer;
// hangAlbum leaves /album unanswered until the request is cancelled.
type trackInfoServer struct {
	gw, track, album       string
	gwHits, trackHits, alb atomic.Int32
	delay                  time.Duration
	hangAlbum              bool
}

func (s *trackInfoServer) client(t *testing.T) *Client {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(s.delay)
		var body string
		switch {
		case strings.HasSuffix(r.URL.Path, "gw-light.php") && r.URL.Query().Get("method") == "song.getData":
			s.gwHits.Add(1)
			body = s.gw
		case strings.HasPrefix(r.URL.Path, "/track/"):
			s.trackHits.Add(1)
			body = s.track
		case strings.HasPrefix(r.URL.Path, "/album/"):
			s.alb.Add(1)
			if s.hangAlbum {
				<-r.Context().Done()
				return
			}
			body = s.album
		}
		if body == "" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)
	target, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	base := ts.Client().Transport
	c := New("dummy")
	loggedInPremium(c)
	c.restURLOverride = ts.URL
	// Route the gw host to the loopback server as well.
	c.http = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.URL.Scheme, r.URL.Host, r.Host = target.Scheme, target.Host, target.Host
		return base.RoundTrip(r)
	})}
	return c
}

func TestTrackInfo_MergesAllSources(t *testing.T) {
	s := &trackInfoServer{gw: gwSongDataFixture, track: restTrackFixture, album: restAlbumFixture}
	ti, err := s.client(t).TrackInfo("3135556")
	if err != nil {
		t.Fatalf("TrackInfo: %v", err)
	}
	want := TrackInfo{
		ID:      "3135556",
		Title:   "Harder, Better, Faster, Stronger",
		Version: "Live",
		Artists: []TrackArtist{
			{ID: "27", Name: "Daft Punk", Role: "main", PictureURL: "https://cdn.test/artist/27/250x250.jpg"},
			{ID: "1234", Name: "Guest Singer", Role: "featured", PictureURL: "https://cdn.test/artist/1234/250x250.jpg"},
		},
		Album: TrackAlbum{
			ID: "302127", Title: "Discovery", CoverURL: "https://cdn.test/cover/1000x1000.jpg",
			ReleaseDate: "2001-03-07", Label: "Parlophone (France)", TrackCount: 14,
			Genres: []string{"Dance", "Electro"},
		},
		TrackNumber: 4,
		DiskNumber:  1,
		DurationMS:  224000,
		ReleaseDate: "2001-03-07",
		BPM:         123.4,
		Gain:        -9.8,
		ISRC:        "GBDUW0000059",
		Explicit:    true,
		Copyright:   "(P) 2001 Daft Life Ltd.",
		Contributors: []Contributor{
			{Role: "main_artist", Names: []string{"Daft Punk"}},
			{Role: "composer", Names: []string{"Thomas Bangalter", "Guy-Manuel de Homem-Christo", "Edwin Birdsong"}},
			{Role: "author", Names: []string{"Edwin Birdsong"}},
			{Role: "producer", Names: []string{"Daft Punk"}},
			{Role: "mixer", Names: []string{"Thomas Bangalter"}},
			{Role: "musicpublisher", Names: []string{"Because Editions"}},
		},
	}
	if !reflect.DeepEqual(ti, want) {
		t.Fatalf("TrackInfo mismatch\n got: %+v\nwant: %+v", ti, want)
	}
}

// Without the gateway (logged out, or gw down) REST alone still fills the page.
func TestTrackInfo_GatewayFailsKeepsREST(t *testing.T) {
	s := &trackInfoServer{track: restTrackFixture, album: restAlbumFixture}
	ti, err := s.client(t).TrackInfo("3135556")
	if err != nil {
		t.Fatalf("partial result must not be an error: %v", err)
	}
	if ti.Title != "Harder, Better, Faster, Stronger" || ti.Version != "Live" || ti.ISRC != "GBDUW0000059" ||
		ti.BPM != 123.4 || ti.Gain != -9.8 || ti.TrackNumber != 4 || ti.DiskNumber != 1 || ti.Explicit {
		t.Errorf("REST fields not merged: %+v", ti)
	}
	if ti.Album.Label != "Parlophone (France)" || len(ti.Artists) != 2 {
		t.Errorf("album/artists missing: %+v", ti)
	}
	if ti.Contributors != nil || ti.Copyright != "" {
		t.Errorf("gw-only fields must stay empty: %+v", ti)
	}
}

// Without REST track data the gateway supplies artists (by ROLE_ID), a
// 1000x1000 cover from the md5 and the album id for /album.
func TestTrackInfo_RESTTrackFailsKeepsGateway(t *testing.T) {
	s := &trackInfoServer{gw: gwSongDataFixture, album: restAlbumFixture}
	ti, err := s.client(t).TrackInfo("3135556")
	if err != nil {
		t.Fatalf("partial result must not be an error: %v", err)
	}
	wantArtists := []TrackArtist{
		{ID: "27", Name: "Daft Punk", Role: "main", PictureURL: gwImage("artist", "f2bc007e9133c946ac3c3907ddc5d2ea")},
		{ID: "1234", Name: "Guest Singer", Role: "featured", PictureURL: gwImage("artist", "aa11")},
	}
	if !reflect.DeepEqual(ti.Artists, wantArtists) {
		t.Errorf("artists = %+v, want %+v", ti.Artists, wantArtists)
	}
	if ti.Album.CoverURL != "https://cdn.test/cover/1000x1000.jpg" || ti.Album.TrackCount != 14 {
		t.Errorf("album not fetched via gw ALB_ID: %+v", ti.Album)
	}
	if ti.BPM != 0 || ti.ReleaseDate != "2001-03-07" || len(ti.Contributors) != 6 {
		t.Errorf("unexpected fields: %+v", ti)
	}
}

func TestTrackInfo_AlbumFailsUsesTrackAlbum(t *testing.T) {
	s := &trackInfoServer{gw: gwSongDataFixture, track: restTrackFixture}
	ti, err := s.client(t).TrackInfo("3135556")
	if err != nil {
		t.Fatalf("partial result must not be an error: %v", err)
	}
	want := TrackAlbum{ID: "302127", Title: "Discovery", CoverURL: "https://cdn.test/cover/1000x1000.jpg", ReleaseDate: "2001-03-07"}
	if !reflect.DeepEqual(ti.Album, want) {
		t.Errorf("album = %+v, want %+v", ti.Album, want)
	}
}

func TestTrackInfo_GatewayCoverFallbackIs1000px(t *testing.T) {
	s := &trackInfoServer{gw: gwSongDataFixture}
	ti, err := s.client(t).TrackInfo("3135556")
	if err != nil {
		t.Fatalf("TrackInfo: %v", err)
	}
	want := "https://e-cdns-images.dzcdn.net/images/cover/2e018122cb56986277102d2041a592c8/1000x1000-000000-80-0-0.jpg"
	if ti.Album.CoverURL != want {
		t.Errorf("cover = %q, want %q", ti.Album.CoverURL, want)
	}
}

func TestTrackInfo_BothPrimarySourcesFail(t *testing.T) {
	s := &trackInfoServer{album: restAlbumFixture}
	if _, err := s.client(t).TrackInfo("3135556"); err == nil {
		t.Fatal("want an error when gw and REST track both fail")
	}
	if n := s.alb.Load(); n != 0 {
		t.Errorf("album fetched %d times without an album id", n)
	}
}

func TestTrackInfo_UnknownTrackIsNotFound(t *testing.T) {
	s := &trackInfoServer{
		gw:    `{"error":{"DATA_ERROR":"song not found"},"results":{}}`,
		track: `{"error":{"type":"DataException","message":"no data","code":800}}`,
	}
	_, err := s.client(t).TrackInfo("999")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// gw sends an empty SNG_CONTRIBUTORS as [] rather than {}.
func TestTrackInfo_EmptyContributorsArray(t *testing.T) {
	gw := strings.Replace(gwSongDataFixture, `"SNG_CONTRIBUTORS":{"main_artist":["Daft Punk"],"composer":["Thomas Bangalter","Guy-Manuel de Homem-Christo","Edwin Birdsong"],`+
		`"author":["Edwin Birdsong"],"producer":["Daft Punk"],"mixer":["Thomas Bangalter"],"musicpublisher":["Because Editions"]}`, `"SNG_CONTRIBUTORS":[]`, 1)
	if gw == gwSongDataFixture {
		t.Fatal("fixture replacement did not apply")
	}
	s := &trackInfoServer{gw: gw, track: restTrackFixture, album: restAlbumFixture}
	ti, err := s.client(t).TrackInfo("3135556")
	if err != nil {
		t.Fatalf("TrackInfo: %v", err)
	}
	if ti.Contributors != nil || ti.ISRC == "" {
		t.Errorf("contributors = %+v, isrc = %q", ti.Contributors, ti.ISRC)
	}
}

// Complete results stay cached; a partial one (a source failed) is kept for
// five minutes, then asked again.
func TestTrackInfo_CachesPartialResultsForFiveMinutes(t *testing.T) {
	full := &trackInfoServer{gw: gwSongDataFixture, track: restTrackFixture, album: restAlbumFixture}
	c := full.client(t)
	for range 3 {
		if _, err := c.TrackInfo("3135556"); err != nil {
			t.Fatal(err)
		}
	}
	if full.gwHits.Load() != 1 || full.trackHits.Load() != 1 || full.alb.Load() != 1 {
		t.Errorf("complete result not cached: gw=%d track=%d album=%d", full.gwHits.Load(), full.trackHits.Load(), full.alb.Load())
	}

	partial := &trackInfoServer{gw: gwSongDataFixture, track: restTrackFixture}
	pc := partial.client(t)
	for range 2 {
		if _, err := pc.TrackInfo("3135556"); err != nil {
			t.Fatal(err)
		}
	}
	if partial.gwHits.Load() != 1 || partial.alb.Load() != 1 {
		t.Errorf("partial result not cached: gw hits = %d, album hits = %d, want 1 and 1", partial.gwHits.Load(), partial.alb.Load())
	}
	pc.infoMu.Lock()
	e := pc.infoCache["3135556"]
	if e.until.IsZero() || time.Until(e.until) > 5*time.Minute {
		t.Errorf("partial result kept until %v, want within five minutes", e.until)
	}
	e.until = time.Now().Add(-time.Second) // five minutes later
	pc.infoCache["3135556"] = e
	pc.infoMu.Unlock()
	if _, err := pc.TrackInfo("3135556"); err != nil {
		t.Fatal(err)
	}
	if partial.gwHits.Load() != 2 {
		t.Errorf("expired partial result not asked again: gw hits = %d, want 2", partial.gwHits.Load())
	}
}

// An album id of 0 (a track without an album) is not looked up.
func TestTrackInfo_AlbumIDZeroNotRequested(t *testing.T) {
	gw := strings.Replace(gwSongDataFixture, `"ALB_ID":"302127"`, `"ALB_ID":"0"`, 1)
	track := strings.Replace(restTrackFixture, `"album":{"id":302127`, `"album":{"id":0`, 1)
	s := &trackInfoServer{gw: gw, track: track, album: restAlbumFixture}
	if _, err := s.client(t).TrackInfo("3135556"); err != nil {
		t.Fatal(err)
	}
	if n := s.alb.Load(); n != 0 {
		t.Errorf("album 0 requested %d times", n)
	}
}

// Concurrent calls for one id share one fetch.
func TestTrackInfo_ConcurrentCallsFetchOnce(t *testing.T) {
	s := &trackInfoServer{gw: gwSongDataFixture, track: restTrackFixture, album: restAlbumFixture}
	s.delay = 100 * time.Millisecond
	c := s.client(t)
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if ti, err := c.TrackInfo("3135556"); err != nil || ti.ISRC == "" {
				t.Errorf("TrackInfo: %+v %v", ti, err)
			}
		})
	}
	wg.Wait()
	if s.gwHits.Load() != 1 || s.trackHits.Load() != 1 {
		t.Errorf("gw hits %d, track hits %d; want 1 each", s.gwHits.Load(), s.trackHits.Load())
	}
}

// A source that never answers is given up at the deadline; the rest fill
// the result.
func TestTrackInfo_Deadline(t *testing.T) {
	old := trackInfoTimeout
	trackInfoTimeout = 300 * time.Millisecond
	t.Cleanup(func() { trackInfoTimeout = old })
	s := &trackInfoServer{gw: gwSongDataFixture, track: restTrackFixture, album: restAlbumFixture, hangAlbum: true}
	start := time.Now()
	ti, err := s.client(t).TrackInfo("3135556")
	if err != nil || ti.ISRC == "" {
		t.Fatalf("TrackInfo: %+v %v", ti, err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("TrackInfo took %v", d)
	}
	if ti.Album.Label != "" {
		t.Errorf("hung album filled: %+v", ti.Album)
	}
}

func TestTrackInfo_CacheIsBounded(t *testing.T) {
	s := &trackInfoServer{gw: gwSongDataFixture, track: restTrackFixture, album: restAlbumFixture}
	c := s.client(t)
	for i := range trackInfoCacheSize + 1 {
		if _, err := c.TrackInfo(fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	c.infoMu.Lock()
	size := len(c.infoCache)
	_, oldestKept := c.infoCache["0"]
	c.infoMu.Unlock()
	if size != trackInfoCacheSize {
		t.Errorf("cache size = %d, want %d", size, trackInfoCacheSize)
	}
	if oldestKept {
		t.Error("oldest entry was not evicted")
	}
	before := s.gwHits.Load()
	if _, err := c.TrackInfo(fmt.Sprint(trackInfoCacheSize)); err != nil {
		t.Fatal(err)
	}
	if s.gwHits.Load() != before {
		t.Error("newest entry was evicted")
	}
}
