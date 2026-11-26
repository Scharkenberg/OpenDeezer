package deezer

import (
	"encoding/json"
	"fmt"
	"sync"
)

// ---- shared REST DTOs ----

type restAlbumDTO struct {
	ID          json.Number `json:"id"`
	Title       string      `json:"title"`
	Artist      restArtist  `json:"artist"`
	CoverMedium string      `json:"cover_medium"`
}

func (a restAlbumDTO) toAlbum() Album {
	return Album{
		ID:         a.ID.String(),
		Name:       a.Title,
		Artists:    []Artist{{ID: a.Artist.ID.String(), Name: a.Artist.Name}},
		ArtworkURL: a.CoverMedium,
	}
}

// restUser is a playlist's creator in the public REST API.
type restUser struct {
	ID   json.Number `json:"id"`
	Name string      `json:"name"`
}

type restArtistDTO struct {
	ID            json.Number `json:"id"`
	Name          string      `json:"name"`
	PictureMedium string      `json:"picture_medium"`
	NbFan         int         `json:"nb_fan"`
}

func (a restArtistDTO) toArtistInfo() ArtistInfo {
	return ArtistInfo{
		ID:         a.ID.String(),
		Name:       a.Name,
		ArtworkURL: a.PictureMedium,
		NbFans:     a.NbFan,
	}
}

// Charts fetches the global top tracks/albums/artists/playlists from the public
// REST /chart endpoint (no auth). Pass genreID "0" for the global chart.
func (c *Client) Charts(genreID string) (*Chart, error) {
	if genreID == "" {
		genreID = "0"
	}
	b, err := c.restGet("/chart/" + genreID + "?limit=50")
	if err != nil {
		return nil, err
	}
	var r struct {
		Tracks    struct{ Data []restTrackDTO }  `json:"tracks"`
		Albums    struct{ Data []restAlbumDTO }  `json:"albums"`
		Artists   struct{ Data []restArtistDTO } `json:"artists"`
		Playlists struct {
			Data []struct {
				ID            json.Number `json:"id"`
				Title         string      `json:"title"`
				User          restUser    `json:"user"`
				NbTracks      int         `json:"nb_tracks"`
				PictureMedium string      `json:"picture_medium"`
			}
		} `json:"playlists"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	ch := &Chart{}
	for _, t := range r.Tracks.Data {
		ch.Tracks = append(ch.Tracks, t.toTrack())
	}
	for _, a := range r.Albums.Data {
		ch.Albums = append(ch.Albums, a.toAlbum())
	}
	for _, a := range r.Artists.Data {
		ch.Artists = append(ch.Artists, a.toArtistInfo())
	}
	for _, p := range r.Playlists.Data {
		ch.Playlists = append(ch.Playlists, Playlist{
			ID: p.ID.String(), Name: p.Title, Owner: p.User.Name, OwnerID: p.User.ID.String(),
			TrackCount: p.NbTracks, ArtworkURL: p.PictureMedium,
		})
	}
	return ch, nil
}

// Genre is a chart genre.
type Genre struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Genres lists the chart genres (public REST /genre), "All" (id "0") first.
func (c *Client) Genres() ([]Genre, error) {
	b, err := c.restGet("/genre")
	if err != nil {
		return nil, err
	}
	var r struct {
		Data []struct {
			ID   json.Number `json:"id"`
			Name string      `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	out := []Genre{{ID: "0", Name: "All"}}
	for _, g := range r.Data {
		if g.ID.String() != "0" {
			out = append(out, Genre{ID: g.ID.String(), Name: g.Name})
		}
	}
	return out, nil
}

// artistAlbumsMax bounds an artist page's albums.
const artistAlbumsMax = 500

// ArtistTop lists an artist's most popular tracks (public REST).
func (c *Client) ArtistTop(id string) ([]Track, error) {
	b, err := c.restGet("/artist/" + id + "/top?limit=50")
	if err != nil {
		return nil, err
	}
	var r struct {
		Data []restTrackDTO `json:"data"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	out := make([]Track, 0, len(r.Data))
	for _, t := range r.Data {
		out = append(out, t.toTrack())
	}
	return out, nil
}

// ArtistProfile fetches an artist's profile plus top tracks, albums and related
// artists (all public REST, the four calls in parallel). A failure on any
// sub-list is tolerated so a partial profile still renders.
func (c *Client) ArtistProfile(id string) (*ArtistPage, error) {
	page := &ArtistPage{}
	var infoErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		b, err := c.restGet("/artist/" + id)
		var info restArtistDTO
		if err == nil {
			err = json.Unmarshal(b, &info)
		}
		page.Artist, infoErr = info.toArtistInfo(), err
	})
	wg.Go(func() { page.Top, _ = c.ArtistTop(id) })
	// Every album, page after page while REST names a next one, up to artistAlbumsMax; an
	// album a later page repeats is listed once (clients key their rows by id).
	wg.Go(func() {
		seen := map[string]bool{}
		for index := 0; index < artistAlbumsMax; index += 100 {
			var r struct {
				Data []restAlbumDTO `json:"data"`
				Next string         `json:"next"`
			}
			ab, err := c.restGet(fmt.Sprintf("/artist/%s/albums?limit=100&index=%d", id, index))
			if err != nil || json.Unmarshal(ab, &r) != nil {
				return
			}
			for _, a := range r.Data {
				if id := a.ID.String(); !seen[id] {
					seen[id] = true
					page.Albums = append(page.Albums, a.toAlbum())
				}
			}
			if r.Next == "" || len(r.Data) == 0 {
				return
			}
		}
	})
	wg.Go(func() {
		var r struct {
			Data []restArtistDTO `json:"data"`
		}
		if rb, err := c.restGet("/artist/" + id + "/related?limit=20"); err == nil && json.Unmarshal(rb, &r) == nil {
			for _, a := range r.Data {
				page.Related = append(page.Related, a.toArtistInfo())
			}
		}
	})
	wg.Wait()
	if infoErr != nil {
		return nil, infoErr
	}
	return page, nil
}

// Lyrics fetches a track's lyrics as the web player does, from Pipe (GetLyrics),
// with time-synced lines and their translation when Deezer has them; gw
// song.getLyrics is the fallback when Pipe fails.
func (c *Client) Lyrics(trackID string) (*Lyrics, error) {
	var p struct {
		Track struct {
			Lyrics *struct {
				Text  string `json:"text"`
				Lines []struct {
					Line         string `json:"line"`
					Translated   string `json:"lineTranslated"`
					Milliseconds int64  `json:"milliseconds"`
				} `json:"synchronizedLines"`
			} `json:"lyrics"`
		} `json:"track"`
	}
	const q = `query GetLyrics($trackId: String!) { track(trackId: $trackId) { id lyrics { id text synchronizedLines { lrcTimestamp line lineTranslated milliseconds duration } licence copyright writers } } }`
	if err := c.pipe("GetLyrics", q, map[string]any{"trackId": trackID}, &p); err == nil {
		lyr := &Lyrics{}
		if l := p.Track.Lyrics; l != nil {
			lyr.Plain = l.Text
			for _, s := range l.Lines {
				lyr.Synced = append(lyr.Synced, LyricLine{TimeMS: s.Milliseconds, Text: s.Line, Translated: s.Translated})
			}
		}
		return lyr, nil
	}
	// Pipe failed: gw song.getLyrics.
	b, err := c.gw("song.getLyrics", fmt.Sprintf(`{"sng_id":%s}`, jsonEsc(trackID)))
	if err != nil {
		return nil, err
	}
	var r struct {
		Results struct {
			LyricsText string `json:"LYRICS_TEXT"`
			Sync       []struct {
				Milliseconds json.Number `json:"milliseconds"`
				Line         string      `json:"line"`
			} `json:"LYRICS_SYNC_JSON"`
		} `json:"results"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	lyr := &Lyrics{Plain: r.Results.LyricsText}
	for _, s := range r.Results.Sync {
		ms, _ := s.Milliseconds.Int64()
		// Blank separator entries are kept for their timing.
		lyr.Synced = append(lyr.Synced, LyricLine{TimeMS: ms, Text: s.Line})
	}
	return lyr, nil
}
