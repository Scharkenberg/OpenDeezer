// Package bridge defines the stable JSON wire types shared by the native
// bindings. Keep field order and JSON tags stable: native clients depend on
// this exact shape.
package bridge

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
)

// Artist is a track or album credit on the native JSON wire.
type Artist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Track is a track on the native JSON wire.
type Track struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	DurationMS int64    `json:"durationMs"`
	Artists    []Artist `json:"artists"`
	ArtistLine string   `json:"artistLine"`
	ArtistID   string   `json:"artistId,omitempty"`
	AlbumName  string   `json:"albumName"`
	ArtworkURL string   `json:"artworkUrl"`
	Explicit   bool     `json:"explicit"`
}

// Album is an album on the native JSON wire.
type Album struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Artists    []Artist `json:"artists"`
	ArtworkURL string   `json:"artworkUrl"`
}

// Playlist is a playlist on the native JSON wire.
type Playlist struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Owner      string `json:"owner"`
	TrackCount int    `json:"trackCount"`
	ArtworkURL string `json:"artworkUrl"`
}

// ArtistInfo is an artist search result or profile on the native JSON wire.
type ArtistInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ArtworkURL string `json:"artworkUrl"`
	NbFans     int    `json:"nbFans"`
}

// TrackArtist is an artist credited on a track overview. Role is "main" or
// "featured".
type TrackArtist struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Role       string `json:"role,omitempty"`
	PictureURL string `json:"pictureUrl,omitempty"`
}

// TrackAlbum is the album block of a track overview.
type TrackAlbum struct {
	ID          string   `json:"id,omitempty"`
	Title       string   `json:"title,omitempty"`
	CoverURL    string   `json:"coverUrl,omitempty"` // 1000x1000
	ReleaseDate string   `json:"releaseDate,omitempty"`
	Label       string   `json:"label,omitempty"`
	TrackCount  int      `json:"trackCount,omitempty"`
	Genres      []string `json:"genres,omitempty"`
}

// Contributor is one credit role on a track overview: RoleKey is Deezer's
// SNG_CONTRIBUTORS key, Role its English display name.
type Contributor struct {
	RoleKey string   `json:"roleKey"`
	Role    string   `json:"role"`
	Names   []string `json:"names"`
}

// TrackInfo is a track overview on the native JSON wire. Fields Deezer does
// not provide are omitted.
type TrackInfo struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Version      string        `json:"version,omitempty"`
	Artists      []TrackArtist `json:"artists,omitempty"`
	Album        TrackAlbum    `json:"album"`
	TrackNumber  int           `json:"trackNumber,omitempty"`
	DiskNumber   int           `json:"diskNumber,omitempty"`
	DurationMS   int64         `json:"durationMs,omitempty"`
	ReleaseDate  string        `json:"releaseDate,omitempty"`
	BPM          float64       `json:"bpm,omitempty"`
	Gain         float64       `json:"gain,omitempty"`
	ISRC         string        `json:"isrc,omitempty"`
	Explicit     bool          `json:"explicit,omitempty"`
	Copyright    string        `json:"copyright,omitempty"`
	Contributors []Contributor `json:"contributors,omitempty"`
}

// contributorRoles maps common SNG_CONTRIBUTORS keys to English role names.
// Other keys are shown with underscores as spaces and a capital first letter.
var contributorRoles = map[string]string{
	"main_artist":         "Main artist",
	"featuring":           "Featured artist",
	"composer":            "Composer",
	"author":              "Lyricist",
	"lyricist":            "Lyricist",
	"writer":              "Songwriter",
	"producer":            "Producer",
	"co-producer":         "Co-producer",
	"mixer":               "Mixing engineer",
	"engineer":            "Engineer",
	"mastering":           "Mastering engineer",
	"arranger":            "Arranger",
	"conductor":           "Conductor",
	"musicpublisher":      "Music publisher",
	"publisher":           "Publisher",
	"associatedperformer": "Associated performer",
}

// Podcast is a podcast on the native JSON wire.
type Podcast struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	ArtworkURL   string `json:"artworkUrl"`
	EpisodeCount int    `json:"episodeCount"`
}

// Episode is a podcast episode on the native JSON wire.
type Episode struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	ArtworkURL  string `json:"artworkUrl"`
	DurationMS  int64  `json:"durationMs"`
	ReleaseDate string `json:"releaseDate"`
	PodcastName string `json:"podcastName"`
}

// FromArtist converts a Deezer artist to its native wire representation.
func FromArtist(a deezer.Artist) Artist {
	return Artist{ID: a.ID, Name: a.Name}
}

// FromArtists converts Deezer artists to their native wire representation.
// It deliberately returns a non-nil empty slice for nil input so JSON remains
// [] rather than null, matching the original binding implementations.
func FromArtists(artists []deezer.Artist) []Artist {
	out := make([]Artist, len(artists))
	for i, artist := range artists {
		out[i] = FromArtist(artist)
	}
	return out
}

// FromTrack converts a Deezer track to its native wire representation.
func FromTrack(track deezer.Track) Track {
	artistID := ""
	if len(track.Artists) > 0 {
		artistID = track.Artists[0].ID
	}
	return Track{
		ID:         track.ID,
		Name:       track.Name,
		DurationMS: track.DurationMS,
		Artists:    FromArtists(track.Artists),
		ArtistLine: track.ArtistLine(),
		ArtistID:   artistID,
		AlbumName:  track.AlbumName,
		ArtworkURL: track.ArtworkURL,
		Explicit:   track.Explicit,
	}
}

// FromTracks converts Deezer tracks to their native wire representation.
func FromTracks(tracks []deezer.Track) []Track {
	out := make([]Track, len(tracks))
	for i, track := range tracks {
		out[i] = FromTrack(track)
	}
	return out
}

// FromAlbum converts a Deezer album to its native wire representation.
func FromAlbum(album deezer.Album) Album {
	return Album{
		ID:         album.ID,
		Name:       album.Name,
		Artists:    FromArtists(album.Artists),
		ArtworkURL: album.ArtworkURL,
	}
}

// FromAlbums converts Deezer albums to their native wire representation.
func FromAlbums(albums []deezer.Album) []Album {
	out := make([]Album, len(albums))
	for i, album := range albums {
		out[i] = FromAlbum(album)
	}
	return out
}

// FromPlaylist converts a Deezer playlist to its native wire representation.
func FromPlaylist(playlist deezer.Playlist) Playlist {
	return Playlist{
		ID:         playlist.ID,
		Name:       playlist.Name,
		Owner:      playlist.Owner,
		TrackCount: playlist.TrackCount,
		ArtworkURL: playlist.ArtworkURL,
	}
}

// FromPlaylists converts Deezer playlists to their native wire representation.
func FromPlaylists(playlists []deezer.Playlist) []Playlist {
	out := make([]Playlist, len(playlists))
	for i, playlist := range playlists {
		out[i] = FromPlaylist(playlist)
	}
	return out
}

// FromArtistInfo converts Deezer artist information to its native wire
// representation.
func FromArtistInfo(artist deezer.ArtistInfo) ArtistInfo {
	return ArtistInfo{
		ID:         artist.ID,
		Name:       artist.Name,
		ArtworkURL: artist.ArtworkURL,
		NbFans:     artist.NbFans,
	}
}

// FromTrackInfo converts a Deezer track overview to its native wire
// representation, naming contributor roles in English.
func FromTrackInfo(ti deezer.TrackInfo) TrackInfo {
	out := TrackInfo{
		ID:      ti.ID,
		Title:   ti.Title,
		Version: ti.Version,
		Album: TrackAlbum{
			ID:          ti.Album.ID,
			Title:       ti.Album.Title,
			CoverURL:    ti.Album.CoverURL,
			ReleaseDate: ti.Album.ReleaseDate,
			Label:       ti.Album.Label,
			TrackCount:  ti.Album.TrackCount,
			Genres:      ti.Album.Genres,
		},
		TrackNumber: ti.TrackNumber,
		DiskNumber:  ti.DiskNumber,
		DurationMS:  ti.DurationMS,
		ReleaseDate: ti.ReleaseDate,
		BPM:         ti.BPM,
		Gain:        ti.Gain,
		ISRC:        ti.ISRC,
		Explicit:    ti.Explicit,
		Copyright:   ti.Copyright,
	}
	for _, a := range ti.Artists {
		out.Artists = append(out.Artists, TrackArtist{ID: a.ID, Name: a.Name, Role: a.Role, PictureURL: a.PictureURL})
	}
	for _, c := range ti.Contributors {
		role, ok := contributorRoles[c.Role]
		if !ok {
			role = strings.ReplaceAll(c.Role, "_", " ")
			if r, size := utf8.DecodeRuneInString(role); size > 0 {
				role = string(unicode.ToUpper(r)) + role[size:]
			}
		}
		out.Contributors = append(out.Contributors, Contributor{RoleKey: c.Role, Role: role, Names: c.Names})
	}
	return out
}

// FromArtistInfos converts Deezer artist information to its native wire
// representation.
func FromArtistInfos(artists []deezer.ArtistInfo) []ArtistInfo {
	out := make([]ArtistInfo, len(artists))
	for i, artist := range artists {
		out[i] = FromArtistInfo(artist)
	}
	return out
}

// FromPodcast converts a Deezer podcast to its native wire representation.
func FromPodcast(podcast deezer.Podcast) Podcast {
	return Podcast{
		ID:           podcast.ID,
		Name:         podcast.Name,
		Description:  podcast.Description,
		ArtworkURL:   podcast.ArtworkURL,
		EpisodeCount: podcast.EpisodeCount,
	}
}

// FromPodcasts converts Deezer podcasts to their native wire representation.
func FromPodcasts(podcasts []deezer.Podcast) []Podcast {
	out := make([]Podcast, len(podcasts))
	for i, podcast := range podcasts {
		out[i] = FromPodcast(podcast)
	}
	return out
}

// FromEpisode converts a Deezer podcast episode to its native wire
// representation.
func FromEpisode(episode deezer.Episode) Episode {
	return Episode{
		ID:          episode.ID,
		Title:       episode.Title,
		Description: episode.Description,
		ArtworkURL:  episode.ArtworkURL,
		DurationMS:  episode.DurationMS,
		ReleaseDate: episode.ReleaseDate,
		PodcastName: episode.PodcastName,
	}
}

// FromEpisodes converts Deezer podcast episodes to their native wire
// representation.
func FromEpisodes(episodes []deezer.Episode) []Episode {
	out := make([]Episode, len(episodes))
	for i, episode := range episodes {
		out[i] = FromEpisode(episode)
	}
	return out
}
