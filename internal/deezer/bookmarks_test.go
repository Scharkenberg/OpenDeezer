package deezer

import (
	"reflect"
	"strings"
	"testing"
)

// Bookmarks are read from the web player's user menu; a bookmark write and a
// played mark send the web player's gateway calls.
func TestEpisodeBookmarks(t *testing.T) {
	var sent []string
	f := &pipeFake{gwReply: func(method, body string) string {
		sent = append(sent, method+" "+body)
		if method == "deezer.userMenu" {
			return `{"error":{},"results":{"EPISODE_BOOKMARKS":{"data":[{"EPISODE_ID":"11","OFFSET":"754","BOOKMARK_TIME":"2026-11-20 10:00:00","IS_HEARD":"0"},{"EPISODE_ID":12,"OFFSET":0,"IS_HEARD":"1"}],"count":2,"total":2}}}`
		}
		return `{"error":{},"results":true}`
	}}
	c := f.client()
	got, err := c.EpisodeBookmarks()
	want := map[string]EpisodeBookmark{"11": {OffsetS: 754}, "12": {Heard: true}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v, %v", got, err)
	}
	if err := c.SetEpisodeBookmark("11", 790, 3600, false); err != nil {
		t.Fatal(err)
	}
	if err := c.MarkEpisodePlayed("11", true); err != nil {
		t.Fatal(err)
	}
	if err := c.MarkEpisodePlayed("12", false); err != nil {
		t.Fatal(err)
	}
	wantSent := []string{
		`deezer.userMenu {"checksum":true,"checksums":{}}`,
		`episode.bookmarkSet {"DURATION":3600,"EPISODE_ID":"11","ISHEARD":false,"OFFSET":790}`,
		`episode.markAsPlayed {"EPISODE_ID":"11"}`,
		`episode.markAsNotPlayed {"EPISODE_ID":"12"}`,
	}
	if strings.Join(sent, "\n") != strings.Join(wantSent, "\n") {
		t.Fatalf("sent\n%s", strings.Join(sent, "\n"))
	}
}

// An episode counts as heard from 90 % of its length when at most two
// minutes are left, as both Deezer players count it.
func TestEpisodeHeard(t *testing.T) {
	for _, c := range []struct {
		pos, dur int
		heard    bool
	}{
		{0, 0, false},
		{899, 1000, false},  // below 90 %
		{900, 1000, true},   // 90 %, 100 s left
		{3240, 3600, false}, // 90 % but 6 min left
		{3480, 3600, true},  // two minutes left
		{3479, 3600, false}, // just over two minutes left
		{1000, 1000, true},  // the end
	} {
		if got := EpisodeHeard(c.pos, c.dur); got != c.heard {
			t.Errorf("EpisodeHeard(%d, %d) = %v", c.pos, c.dur, got)
		}
	}
}
