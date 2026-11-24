package deezer

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// favIDsPage is a song.getFavoriteIds page of ids from..from+n-1.
func favIDsPage(from, n, total int, checksum string) string {
	var sb strings.Builder
	sb.WriteString(`{"error":{},"results":{"data":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, `{"SNG_ID":"%d","DATE_FAVORITE":"1"}`, from+i)
	}
	fmt.Fprintf(&sb, `],"total":%d,"checksum":%q,"count":%d}}`, total, checksum, n)
	return sb.String()
}

// Liked-song ids come in pages of 10 000; the next read sends the list's
// checksum, and "Not modified" returns the ids already read.
func TestFavoriteIDsPagesAndChecksum(t *testing.T) {
	var bodies []string
	f := &pipeFake{gwReply: func(method, body string) string {
		if method != "song.getFavoriteIds" {
			t.Errorf("method %s", method)
		}
		bodies = append(bodies, body)
		var q struct {
			Start    int `json:"start"`
			Checksum any `json:"checksum"`
		}
		_ = json.Unmarshal([]byte(body), &q)
		if q.Checksum == "cs1" {
			return `{"error":{},"results":"Not modified"}`
		}
		if q.Start == 0 {
			return favIDsPage(1, favPage, favPage+3, "cs1")
		}
		return favIDsPage(1+favPage, 3, favPage+3, "")
	}}
	c := f.client()
	ids, err := c.FavoriteIDs()
	if err != nil || len(ids) != favPage+3 || ids[0] != "1" || ids[favPage+2] != fmt.Sprint(favPage+3) {
		t.Fatalf("first read: %d ids, %v", len(ids), err)
	}
	if !strings.Contains(bodies[0], `"checksum":null`) || !strings.Contains(bodies[1], fmt.Sprintf(`"start":%d`, favPage)) {
		t.Fatalf("bodies %q", bodies)
	}
	again, err := c.FavoriteIDs()
	if err != nil || !reflect.DeepEqual(again, ids) || len(bodies) != 3 || !strings.Contains(bodies[2], `"checksum":"cs1"`) {
		t.Fatalf("second read: %d ids, %d calls, %v", len(again), len(bodies), err)
	}
}

// Liked songs are listed in full, past the first page.
func TestFavoritesAllPages(t *testing.T) {
	f := &pipeFake{gwReply: func(method, body string) string {
		if strings.Contains(body, `"start":0`) {
			return string(gwTracksPage(1, favPage))
		}
		return string(gwTracksPage(1+favPage, 5))
	}}
	got, err := f.client().Favorites()
	if err != nil || len(got) != favPage+5 {
		t.Fatalf("%d tracks, %v", len(got), err)
	}
}

// Favorite albums and artists come from the profile page's tabs.
func TestFavoriteAlbumsAndArtists(t *testing.T) {
	f := &pipeFake{gwReply: func(method, body string) string {
		switch {
		case strings.Contains(body, `"tab":"albums"`):
			return `{"error":{},"results":{"TAB":{"albums":{"data":[{"ALB_ID":"5","ALB_TITLE":"Al","ALB_PICTURE":"p","ART_ID":"9","ART_NAME":"Ar"}]}}}}`
		case strings.Contains(body, `"tab":"artists"`):
			return `{"error":{},"results":{"TAB":{"artists":{"data":[{"ART_ID":"9","ART_NAME":"Ar","ART_PICTURE":"q","NB_FAN":1234}]}}}}`
		}
		return `{"error":{"X":"bad"}}`
	}}
	c := f.client()
	al, err := c.FavoriteAlbums()
	if err != nil || len(al) != 1 || al[0].ID != "5" || al[0].Name != "Al" || al[0].Artists[0].Name != "Ar" || al[0].ArtworkURL == "" {
		t.Fatalf("albums %+v, %v", al, err)
	}
	ar, err := c.FavoriteArtists()
	if err != nil || len(ar) != 1 || ar[0].ID != "9" || ar[0].NbFans != 1234 {
		t.Fatalf("artists %+v, %v", ar, err)
	}
}

// Favorites, recommendation bans and search history send the web player's
// Pipe operations with their id variables.
func TestPipeLibraryMutations(t *testing.T) {
	f := &pipeFake{ttl: time.Hour, reply: func(string, map[string]any) string { return `{"data":{}}` }}
	c := f.client()
	steps := []struct {
		run  func() error
		op   string
		vars map[string]any
	}{
		{func() error { return c.SetFavorite(FavAlbum, "1", true) }, "AddAlbumToFavorite", map[string]any{"albumId": "1"}},
		{func() error { return c.SetFavorite(FavArtist, "2", false) }, "RemoveArtistFromFavorite", map[string]any{"artistId": "2"}},
		{func() error { return c.SetFavorite(FavPodcast, "3", true) }, "AddPodcastToFavorite", map[string]any{"podcastId": "3"}},
		{func() error { return c.SetRecommendationBan("track", "4", true) }, "BanTrackFromRecommendation", map[string]any{"trackId": "4"}},
		{func() error { return c.SetRecommendationBan("artist", "5", false) }, "UnbanArtistFromRecommendation", map[string]any{"artistId": "5"}},
		{func() error { return c.AddSearchHistory(SearchHistoryItem{Type: "PodcastEpisode", ID: "6"}) }, "AddPodcastEpisodeInSearchHistory", map[string]any{"podcastEpisodeId": "6"}},
		{func() error { return c.ClearSearchHistory() }, "ClearSearchHistory", map[string]any{}},
	}
	for i, s := range steps {
		if err := s.run(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if f.ops[i] != s.op || !reflect.DeepEqual(f.vars[i], s.vars) {
			t.Fatalf("step %d: %s %v, want %s %v", i, f.ops[i], f.vars[i], s.op, s.vars)
		}
	}
	if c.SetFavorite("song", "1", true) == nil || c.SetRecommendationBan("album", "1", true) == nil ||
		c.AddSearchHistory(SearchHistoryItem{Type: "User", ID: "1"}) == nil {
		t.Fatal("unknown kinds must fail")
	}
}

// The search history is read newest first, whatever the item type.
func TestSearchHistory(t *testing.T) {
	f := &pipeFake{ttl: time.Hour, reply: func(string, map[string]any) string {
		return `{"data":{"me":{"searchHistory":{"successResults":{"edges":[{"node":{"__typename":"Artist","id":"1"}},{"node":{"__typename":"Track","id":"2"}}]}}}}}`
	}}
	got, err := f.client().SearchHistory(20)
	want := []SearchHistoryItem{{Type: "Artist", ID: "1"}, {Type: "Track", ID: "2"}}
	if err != nil || !reflect.DeepEqual(got, want) || f.vars[0]["first"] != float64(20) {
		t.Fatalf("%+v, %v, vars %v", got, err, f.vars)
	}
}

// Changing the explicit-content level sends it and keeps it.
func TestSetExplicitLevel(t *testing.T) {
	var sent string
	f := &pipeFake{gwReply: func(method, body string) string {
		sent = method + " " + body
		return `{"error":{},"results":true}`
	}}
	c := f.client()
	if err := c.SetExplicitLevel(ExplicitHide); err != nil {
		t.Fatal(err)
	}
	if sent != `user.updateExplicitContentLevel {"EXPLICIT_CONTENT_LEVEL":"explicit_hide"}` || c.Explicit().Level != ExplicitHide {
		t.Fatalf("sent %q, level %q", sent, c.Explicit().Level)
	}
}

// Playlist edits and the favorite state send the web player's calls: Pipe
// UpdatePlaylist with only the fields changed, gw playlist.updateOrder, and an
// item query by kind; a best result is read whatever its kind.
func TestPlaylistEditsAndBestResult(t *testing.T) {
	var gwSent string
	f := &pipeFake{ttl: time.Hour,
		reply: func(op string, vars map[string]any) string {
			switch op {
			case "BestResult":
				return `{"data":{"instantSearch":{"bestResult":{"__typename":"InstantSearchArtistBestResult","artist":{"__typename":"Artist","id":"27","name":"Daft Punk","picture":{"small":["p"]}}}}}}`
			case "IsFavorite":
				return `{"data":{"item":{"id":"5","isFavorite":true}}}`
			}
			return `{"data":{}}`
		},
		gwReply: func(method, body string) string {
			gwSent = method + " " + body
			return `{"error":{},"results":true}`
		}}
	c := f.client()
	title, private := "New", true
	if err := c.UpdatePlaylist("9", PlaylistUpdate{Title: &title, IsPrivate: &private}); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"input": map[string]any{"playlistId": "9", "title": "New", "isPrivate": true}}
	if f.ops[0] != "UpdatePlaylist" || !reflect.DeepEqual(f.vars[0], want) {
		t.Fatalf("%s %v", f.ops[0], f.vars[0])
	}
	collab := true
	if c.UpdatePlaylist("9", PlaylistUpdate{IsPrivate: &private, IsCollaborative: &collab}) == nil {
		t.Fatal("private and collaborative at once must fail")
	}
	if err := c.ReorderPlaylist("9", []string{"3", "1"}); err != nil || gwSent != `playlist.updateOrder {"order":["3","1"],"playlist_id":"9","position":"0"}` {
		t.Fatalf("%v %s", err, gwSent)
	}
	if fav, err := c.IsFavorite(FavAlbum, "5"); err != nil || !fav || f.vars[1]["id"] != "5" {
		t.Fatalf("%v %v %v", fav, err, f.vars[1])
	}
	best, err := c.BestResult("daft punk")
	if err != nil || best == nil || *best != (SearchHistoryItem{Type: "Artist", ID: "27", Title: "Daft Punk", Picture: "p"}) {
		t.Fatalf("%+v %v", best, err)
	}
}

// Home pins are read with their pictures, and a pin Home has no room for is
// ErrPinLimit; an unpin sends the item's type.
func TestPinned(t *testing.T) {
	full := false
	f := &pipeFake{ttl: time.Hour, reply: func(op string, vars map[string]any) string {
		switch op {
		case "PinnedItems":
			return `{"data":{"me":{"quickAccess":{"pinnedItems":[{"item":{"__typename":"Album","id":"5","displayTitle":"Al","cover":{"small":["c"]}}}]}}}}`
		case "PinItem":
			if full {
				return `{"data":{"quickAccessAppendItem":{"__typename":"QuickAccessAppendItemMutationError","isLimitExceed":true}}}`
			}
			return `{"data":{"quickAccessAppendItem":{"__typename":"QuickAccessAppendItemMutationOutput"}}}`
		}
		return `{"data":{}}`
	}}
	c := f.client()
	got, err := c.Pinned()
	if err != nil || len(got) != 1 || got[0] != (SearchHistoryItem{Type: "Album", ID: "5", Title: "Al", Picture: "c"}) {
		t.Fatalf("%+v %v", got, err)
	}
	if err := c.SetPinned("Artist", "27", true); err != nil {
		t.Fatal(err)
	}
	full = true
	if err := c.SetPinned("Artist", "27", true); err != ErrPinLimit {
		t.Fatalf("a full Home: %v", err)
	}
	if err := c.SetPinned("Track", "3", false); err != nil || f.ops[3] != "UnpinItem" || f.vars[3]["type"] != "TRACK" || f.vars[3]["context"] != "HOMEPAGE" {
		t.Fatalf("%v %v %v", err, f.ops, f.vars)
	}
}

// A page of liked songs that fails fails the list: a partial list would read
// as the rest unliked.
func TestFavoritesFailedPageFails(t *testing.T) {
	f := &pipeFake{gwReply: func(method, body string) string {
		if strings.Contains(body, `"start":0`) {
			return string(gwTracksPage(1, favPage))
		}
		return `{"error":{"DATA_ERROR":"try later"},"results":{}}`
	}}
	if got, err := f.client().Favorites(); err == nil {
		t.Fatalf("%d tracks and no error after a failed page", len(got))
	}
}

// Favorite podcasts come with their title, description and cover in one Pipe query.
func TestFavoritePodcastsOneQuery(t *testing.T) {
	f := &pipeFake{ttl: time.Hour, reply: func(op string, _ map[string]any) string {
		if op != "FavoritePodcasts" {
			return `{"errors":[{"message":"unexpected"}]}`
		}
		return `{"data":{"me":{"userFavorites":{"podcasts":{"pageInfo":{"hasNextPage":false,"endCursor":null},"edges":[` +
			`{"node":{"id":"11","displayTitle":"One","description":"First","cover":{"urls":["https://cdn.test/11.jpg"]}}},` +
			`{"node":{"id":"12","displayTitle":"Two","description":"","cover":null}}]}}}}}`
	}}
	got, err := f.client().FavoritePodcasts()
	want := []Podcast{{ID: "11", Name: "One", Description: "First", ArtworkURL: "https://cdn.test/11.jpg"}, {ID: "12", Name: "Two"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("FavoritePodcasts = %+v, %v", got, err)
	}
	if len(f.ops) != 1 {
		t.Fatalf("ops %v, want one FavoritePodcasts", f.ops)
	}
}
