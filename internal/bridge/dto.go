// Package bridge defines the stable JSON wire types shared by the native
// bindings. Keep field order and JSON tags stable: native clients depend on
// this exact shape.
package bridge

import (
	"fmt"

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
	OwnerID    string `json:"ownerId,omitempty"`
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
// SNG_CONTRIBUTORS key.
type Contributor struct {
	RoleKey string   `json:"roleKey"`
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

// mapSlice converts each element with f. It returns a non-nil empty slice for
// nil input, so the JSON is [] rather than null.
func mapSlice[S, T any](in []S, f func(S) T) []T {
	out := make([]T, len(in))
	for i, v := range in {
		out[i] = f(v)
	}
	return out
}

// FromArtists converts Deezer artists to their native wire representation.
func FromArtists(artists []deezer.Artist) []Artist { return mapSlice(artists, FromArtist) }

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
func FromTracks(tracks []deezer.Track) []Track { return mapSlice(tracks, FromTrack) }

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
func FromAlbums(albums []deezer.Album) []Album { return mapSlice(albums, FromAlbum) }

// FromPlaylist converts a Deezer playlist to its native wire representation.
func FromPlaylist(playlist deezer.Playlist) Playlist {
	return Playlist{
		ID:         playlist.ID,
		Name:       playlist.Name,
		Owner:      playlist.Owner,
		TrackCount: playlist.TrackCount,
		ArtworkURL: playlist.ArtworkURL,
		OwnerID:    playlist.OwnerID,
	}
}

// FromPlaylists converts Deezer playlists to their native wire representation.
func FromPlaylists(playlists []deezer.Playlist) []Playlist { return mapSlice(playlists, FromPlaylist) }

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
// representation.
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
		out.Contributors = append(out.Contributors, Contributor{RoleKey: c.Role, Names: c.Names})
	}
	return out
}

// FromArtistInfos converts Deezer artist information to its native wire
// representation.
func FromArtistInfos(artists []deezer.ArtistInfo) []ArtistInfo {
	return mapSlice(artists, FromArtistInfo)
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
func FromPodcasts(podcasts []deezer.Podcast) []Podcast { return mapSlice(podcasts, FromPodcast) }

// Library reads the account's favorite albums, artists or podcasts by kind ("album",
// "artist", "podcast") as {"albums":[...]}, {"artists":[...]} or {"podcasts":[...]}.
func Library(c *deezer.Client, kind string) (any, error) {
	switch kind {
	case "album":
		v, err := c.FavoriteAlbums()
		return map[string]any{"albums": FromAlbums(v)}, err
	case "artist":
		v, err := c.FavoriteArtists()
		return map[string]any{"artists": FromArtistInfos(v)}, err
	case "podcast":
		v, err := c.FavoritePodcasts()
		return map[string]any{"podcasts": FromPodcasts(v)}, err
	}
	return nil, fmt.Errorf("library: unknown kind %q", kind)
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
func FromEpisodes(episodes []deezer.Episode) []Episode { return mapSlice(episodes, FromEpisode) }
