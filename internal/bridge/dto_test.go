package bridge

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
)

func TestGoldenWireCompatibility(t *testing.T) {
	artist := deezer.Artist{ID: "artist-1", Name: "First Artist"}

	tests := []struct {
		name     string
		value    any
		wantJSON string
		wantKeys []string
	}{
		{
			name: "track",
			value: FromTrack(deezer.Track{
				ID: "track-1", Name: "Wire Song", DurationMS: 123456,
				Artists:   []deezer.Artist{artist, {ID: "artist-2", Name: "Guest Artist"}},
				AlbumName: "Wire Album", ArtworkURL: "https://img.example/track.jpg", Explicit: true,
			}),
			wantJSON: `{"id":"track-1","name":"Wire Song","durationMs":123456,"artists":[{"id":"artist-1","name":"First Artist"},{"id":"artist-2","name":"Guest Artist"}],"artistLine":"First Artist, Guest Artist","artistId":"artist-1","albumName":"Wire Album","artworkUrl":"https://img.example/track.jpg","explicit":true}`,
			wantKeys: []string{"albumName", "artistId", "artistLine", "artists", "artworkUrl", "durationMs", "explicit", "id", "name"},
		},
		{
			name: "album",
			value: FromAlbum(deezer.Album{
				ID: "album-1", Name: "Wire Album", Artists: []deezer.Artist{artist},
				ArtworkURL: "https://img.example/album.jpg",
			}),
			wantJSON: `{"id":"album-1","name":"Wire Album","artists":[{"id":"artist-1","name":"First Artist"}],"artworkUrl":"https://img.example/album.jpg"}`,
			wantKeys: []string{"artists", "artworkUrl", "id", "name"},
		},
		{
			name: "playlist",
			value: FromPlaylist(deezer.Playlist{
				ID: "playlist-1", Name: "Wire Mix", Owner: "Listener", TrackCount: 42,
				ArtworkURL: "https://img.example/playlist.jpg",
			}),
			wantJSON: `{"id":"playlist-1","name":"Wire Mix","owner":"Listener","trackCount":42,"artworkUrl":"https://img.example/playlist.jpg"}`,
			wantKeys: []string{"artworkUrl", "id", "name", "owner", "trackCount"},
		},
		{
			name:     "artist info",
			value:    FromArtistInfo(deezer.ArtistInfo{ID: "artist-1", Name: "First Artist", ArtworkURL: "https://img.example/artist.jpg", NbFans: 99}),
			wantJSON: `{"id":"artist-1","name":"First Artist","artworkUrl":"https://img.example/artist.jpg","nbFans":99}`,
			wantKeys: []string{"artworkUrl", "id", "name", "nbFans"},
		},
		{
			name:     "podcast",
			value:    FromPodcast(deezer.Podcast{ID: "podcast-1", Name: "Wire Cast", Description: "A show", ArtworkURL: "https://img.example/podcast.jpg", EpisodeCount: 7}),
			wantJSON: `{"id":"podcast-1","name":"Wire Cast","description":"A show","artworkUrl":"https://img.example/podcast.jpg","episodeCount":7}`,
			wantKeys: []string{"artworkUrl", "description", "episodeCount", "id", "name"},
		},
		{
			name: "episode",
			value: FromEpisode(deezer.Episode{
				ID: "episode-1", Title: "The Episode", Description: "An episode",
				ArtworkURL: "https://img.example/episode.jpg", DurationMS: 654321,
				ReleaseDate: "2026-07-11", PodcastName: "Wire Cast",
			}),
			wantJSON: `{"id":"episode-1","title":"The Episode","description":"An episode","artworkUrl":"https://img.example/episode.jpg","durationMs":654321,"releaseDate":"2026-07-11","podcastName":"Wire Cast"}`,
			wantKeys: []string{"artworkUrl", "description", "durationMs", "id", "podcastName", "releaseDate", "title"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tt.wantJSON {
				t.Fatalf("wire JSON changed\n got: %s\nwant: %s", got, tt.wantJSON)
			}
			assertJSONKeys(t, got, tt.wantKeys)
		})
	}
}

func TestArtistInfoWire(t *testing.T) {
	got, err := json.Marshal(FromArtistInfo(deezer.ArtistInfo{
		ID: "27", Name: "Daft Punk", ArtworkURL: "https://img.example/250.jpg", NbFans: 5,
	}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"id":"27","name":"Daft Punk","artworkUrl":"https://img.example/250.jpg","nbFans":5}`
	if string(got) != want {
		t.Fatalf("artist info wire JSON changed\n got: %s\nwant: %s", got, want)
	}
}

func TestTrackInfoWire(t *testing.T) {
	got, err := json.Marshal(FromTrackInfo(deezer.TrackInfo{
		ID: "3135556", Title: "Harder, Better, Faster, Stronger", Version: "Live",
		Artists: []deezer.TrackArtist{
			{ID: "27", Name: "Daft Punk", Role: "main", PictureURL: "https://img.example/a.jpg"},
			{ID: "1234", Name: "Guest", Role: "featured"},
		},
		Album: deezer.TrackAlbum{
			ID: "302127", Title: "Discovery", CoverURL: "https://img.example/1000.jpg",
			ReleaseDate: "2001-03-07", Label: "Parlophone", TrackCount: 14, Genres: []string{"Dance"},
		},
		TrackNumber: 4, DiskNumber: 1, DurationMS: 224000, ReleaseDate: "2001-03-07",
		BPM: 123.4, Gain: -9.8, ISRC: "GBDUW0000059", Explicit: true, Copyright: "(P) 2001 Daft Life Ltd.",
		Contributors: []deezer.Contributor{
			{Role: "composer", Names: []string{"Thomas Bangalter"}},
			{Role: "author", Names: []string{"Edwin Birdsong"}},
			{Role: "mixer", Names: []string{"Thomas Bangalter"}},
			{Role: "vocal_coach", Names: []string{"Someone"}},
		},
	}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"id":"3135556","title":"Harder, Better, Faster, Stronger","version":"Live",` +
		`"artists":[{"id":"27","name":"Daft Punk","role":"main","pictureUrl":"https://img.example/a.jpg"},{"id":"1234","name":"Guest","role":"featured"}],` +
		`"album":{"id":"302127","title":"Discovery","coverUrl":"https://img.example/1000.jpg","releaseDate":"2001-03-07","label":"Parlophone","trackCount":14,"genres":["Dance"]},` +
		`"trackNumber":4,"diskNumber":1,"durationMs":224000,"releaseDate":"2001-03-07","bpm":123.4,"gain":-9.8,"isrc":"GBDUW0000059","explicit":true,` +
		`"copyright":"(P) 2001 Daft Life Ltd.",` +
		`"contributors":[{"roleKey":"composer","role":"Composer","names":["Thomas Bangalter"]},{"roleKey":"author","role":"Lyricist","names":["Edwin Birdsong"]},` +
		`{"roleKey":"mixer","role":"Mixing engineer","names":["Thomas Bangalter"]},{"roleKey":"vocal_coach","role":"Vocal coach","names":["Someone"]}]}`
	if string(got) != want {
		t.Fatalf("track info wire JSON changed\n got: %s\nwant: %s", got, want)
	}
}

func TestTrackInfoWireOmitsUnknown(t *testing.T) {
	got, err := json.Marshal(FromTrackInfo(deezer.TrackInfo{ID: "1", Title: "Bare"}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"id":"1","title":"Bare","album":{}}`
	if string(got) != want {
		t.Fatalf("bare track info wire JSON changed\n got: %s\nwant: %s", got, want)
	}
}

func TestTrackWireCompatibilityWithoutArtists(t *testing.T) {
	got, err := json.Marshal(FromTrack(deezer.Track{ID: "track-empty"}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"id":"track-empty","name":"","durationMs":0,"artists":[],"artistLine":"","albumName":"","artworkUrl":"","explicit":false}`
	if string(got) != want {
		t.Fatalf("empty track wire JSON changed\n got: %s\nwant: %s", got, want)
	}
	assertJSONKeys(t, got, []string{"albumName", "artistLine", "artists", "artworkUrl", "durationMs", "explicit", "id", "name"})
}

func TestCollectionConvertersPreserveEmptyArrays(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{"artists", FromArtists(nil)},
		{"tracks", FromTracks(nil)},
		{"albums", FromAlbums(nil)},
		{"playlists", FromPlaylists(nil)},
		{"artist infos", FromArtistInfos(nil)},
		{"podcasts", FromPodcasts(nil)},
		{"episodes", FromEpisodes(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != "[]" {
				t.Fatalf("nil input marshaled as %s, want []", got)
			}
		})
	}
}

func assertJSONKeys(t *testing.T, data []byte, want []string) {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatalf("unmarshal object: %v", err)
	}
	got := make([]string, 0, len(object))
	for key := range object {
		got = append(got, key)
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wire keys changed: got %v, want %v", got, want)
	}
}
