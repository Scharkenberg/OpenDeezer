package deezer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

// trackInfoCacheSize bounds the per-client TrackInfo cache.
const trackInfoCacheSize = 128

// partialInfoTTL is how long a result some source failed to fill is kept.
const partialInfoTTL = 5 * time.Minute

// trackInfoTimeout bounds one TrackInfo fetch, all sources together.
var trackInfoTimeout = 10 * time.Second

// infoEntry is a cached TrackInfo; until is zero for a complete result.
type infoEntry struct {
	ti    TrackInfo
	until time.Time
}

// infoCall is a TrackInfo fetch in flight that later callers wait for.
type infoCall struct {
	done chan struct{}
	ti   TrackInfo
	err  error
}

// TrackArtist is one artist credited on a track. Role is "main" or
// "featured" (REST contributor roles, lower-cased).
type TrackArtist struct {
	ID         string
	Name       string
	Role       string
	PictureURL string
}

// TrackAlbum is the album a track belongs to, as shown on a track overview.
type TrackAlbum struct {
	ID          string
	Title       string
	CoverURL    string // 1000x1000
	ReleaseDate string
	Label       string
	TrackCount  int
	Genres      []string
}

// Contributor is one credit role with its people. Role is Deezer's
// SNG_CONTRIBUTORS key ("composer", "author", "producer", ...).
type Contributor struct {
	Role  string
	Names []string
}

// TrackInfo is everything Deezer exposes about one track, merged from gw
// song.getData and REST /track and /album. Zero values mean unknown.
type TrackInfo struct {
	ID           string
	Title        string
	Version      string // "Live", "Remastered", ...
	Artists      []TrackArtist
	Album        TrackAlbum
	TrackNumber  int
	DiskNumber   int
	DurationMS   int64
	ReleaseDate  string
	BPM          float64
	Gain         float64 // ReplayGain in dB
	ISRC         string
	Explicit     bool
	Copyright    string
	Contributors []Contributor
}

// gwContributors decodes SNG_CONTRIBUTORS, an object of role -> names, in the
// order gw sends the roles. gw sends an empty set as [] instead of {}; that
// and null decode to no credits.
type gwContributors []Contributor

func (g *gwContributors) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok != json.Delim('{') {
		return nil
	}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return err
		}
		var names []string
		if err := dec.Decode(&names); err != nil {
			return fmt.Errorf("SNG_CONTRIBUTORS %v: %w", key, err)
		}
		*g = append(*g, Contributor{Role: key.(string), Names: names})
	}
	return nil
}

type gwSongInfoDTO struct {
	SngID    json.Number `json:"SNG_ID"`
	SngTitle string      `json:"SNG_TITLE"`
	Version  string      `json:"VERSION"`
	Artists  []struct {
		ArtID      json.Number `json:"ART_ID"`
		ArtName    string      `json:"ART_NAME"`
		ArtPicture string      `json:"ART_PICTURE"`
		RoleID     json.Number `json:"ROLE_ID"` // 0 = main artist
	} `json:"ARTISTS"`
	AlbID               json.Number    `json:"ALB_ID"`
	AlbTitle            string         `json:"ALB_TITLE"`
	AlbPicture          string         `json:"ALB_PICTURE"`
	Duration            json.Number    `json:"DURATION"`
	TrackNumber         json.Number    `json:"TRACK_NUMBER"`
	DiskNumber          json.Number    `json:"DISK_NUMBER"`
	Gain                json.Number    `json:"GAIN"`
	ISRC                string         `json:"ISRC"`
	Explicit            json.Number    `json:"EXPLICIT_LYRICS"`
	PhysicalReleaseDate string         `json:"PHYSICAL_RELEASE_DATE"`
	Copyright           string         `json:"COPYRIGHT"`
	Contributors        gwContributors `json:"SNG_CONTRIBUTORS"`
}

type restTrackInfoDTO struct {
	ID             json.Number `json:"id"`
	Title          string      `json:"title"`
	TitleShort     string      `json:"title_short"`
	TitleVersion   string      `json:"title_version"`
	ISRC           string      `json:"isrc"`
	Duration       json.Number `json:"duration"`
	TrackPosition  json.Number `json:"track_position"`
	DiskNumber     json.Number `json:"disk_number"`
	ReleaseDate    string      `json:"release_date"`
	ExplicitLyrics bool        `json:"explicit_lyrics"`
	BPM            float64     `json:"bpm"`
	Gain           float64     `json:"gain"`
	Contributors   []struct {
		ID            json.Number `json:"id"`
		Name          string      `json:"name"`
		PictureMedium string      `json:"picture_medium"`
		Role          string      `json:"role"` // "Main", "Featured"
	} `json:"contributors"`
	Album struct {
		ID          json.Number `json:"id"`
		Title       string      `json:"title"`
		CoverXL     string      `json:"cover_xl"`
		ReleaseDate string      `json:"release_date"`
	} `json:"album"`
}

type restAlbumInfoDTO struct {
	ID          json.Number `json:"id"`
	Title       string      `json:"title"`
	CoverXL     string      `json:"cover_xl"`
	Label       string      `json:"label"`
	NbTracks    int         `json:"nb_tracks"`
	ReleaseDate string      `json:"release_date"`
	Genres      struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	} `json:"genres"`
}

// restDecode GETs a REST path and decodes the body into v.
func (c *Client) restDecode(ctx context.Context, path string, v any) error {
	b, err := c.restGetCtx(ctx, path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func firstString(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstInt(vals ...json.Number) int64 {
	for _, v := range vals {
		if n, err := v.Int64(); err == nil && n != 0 {
			return n
		}
	}
	return 0
}

// TrackInfo returns everything known about one track. gw song.getData and
// REST /track/{id} run concurrently, all within trackInfoTimeout; REST
// /album/{id} starts as soon as either names the album. Any one source may
// fail and the rest still fill the result; only when gw and REST /track both
// fail is it an error. Results are cached per id: complete ones until evicted,
// partial ones for partialInfoTTL. Concurrent calls for one id share a fetch.
func (c *Client) TrackInfo(id string) (ti TrackInfo, err error) {
	c.infoMu.Lock()
	if e, ok := c.infoCache[id]; ok && (e.until.IsZero() || time.Now().Before(e.until)) {
		c.infoMu.Unlock()
		return e.ti, nil
	}
	if call := c.infoFlight[id]; call != nil {
		c.infoMu.Unlock()
		<-call.done
		return call.ti, call.err
	}
	call := &infoCall{done: make(chan struct{})}
	if c.infoFlight == nil {
		c.infoFlight = map[string]*infoCall{}
	}
	c.infoFlight[id] = call
	c.infoMu.Unlock()

	complete := false
	defer func() {
		c.infoMu.Lock()
		delete(c.infoFlight, id)
		if err == nil {
			if c.infoCache == nil {
				c.infoCache = make(map[string]infoEntry)
			}
			if _, dup := c.infoCache[id]; !dup {
				if len(c.infoOrder) == trackInfoCacheSize {
					delete(c.infoCache, c.infoOrder[0])
					c.infoOrder = c.infoOrder[1:]
				}
				c.infoOrder = append(c.infoOrder, id)
			}
			e := infoEntry{ti: ti}
			if !complete {
				e.until = time.Now().Add(partialInfoTTL)
			}
			c.infoCache[id] = e
		}
		c.infoMu.Unlock()
		call.ti, call.err = ti, err
		close(call.done)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), trackInfoTimeout)
	defer cancel()
	var (
		wg                      sync.WaitGroup
		albumOnce               sync.Once
		g                       gwSongInfoDTO
		r                       restTrackInfoDTO
		a                       restAlbumInfoDTO
		gwErr, restErr, albmErr error
	)
	fetchAlbum := func(albumID string) {
		if albumID == "" || albumID == "0" { // 0: no album
			return
		}
		albumOnce.Do(func() {
			wg.Go(func() {
				var out restAlbumInfoDTO
				if albmErr = c.restDecode(ctx, "/album/"+url.PathEscape(albumID), &out); albmErr == nil {
					a = out
				}
			})
		})
	}
	wg.Go(func() {
		var out struct {
			Results gwSongInfoDTO `json:"results"`
		}
		b, err := c.gwCtx(ctx, "song.getData", fmt.Sprintf(`{"sng_id":%s}`, jsonEsc(id)))
		if err == nil {
			err = json.Unmarshal(b, &out)
		}
		if err == nil && out.Results.SngID == "" {
			err = fmt.Errorf("track %s: %w", id, ErrNotFound)
		}
		if gwErr = err; err == nil {
			g = out.Results
			fetchAlbum(g.AlbID.String())
		}
	})
	wg.Go(func() {
		var out restTrackInfoDTO
		if restErr = c.restDecode(ctx, "/track/"+url.PathEscape(id), &out); restErr == nil {
			r = out
			fetchAlbum(r.Album.ID.String())
		}
	})
	wg.Wait()
	if gwErr != nil && restErr != nil {
		return TrackInfo{}, errors.Join(gwErr, restErr)
	}

	version := strings.TrimSpace(firstString(g.Version, r.TitleVersion))
	if strings.HasPrefix(version, "(") && strings.HasSuffix(version, ")") {
		version = strings.TrimSpace(version[1 : len(version)-1])
	}
	gain, _ := g.Gain.Float64()
	if gain == 0 {
		gain = r.Gain
	}
	explicit, _ := g.Explicit.Int64()
	ti = TrackInfo{
		ID:          firstString(g.SngID.String(), r.ID.String()),
		Title:       firstString(g.SngTitle, r.TitleShort, r.Title),
		Version:     version,
		TrackNumber: int(firstInt(g.TrackNumber, r.TrackPosition)),
		DiskNumber:  int(firstInt(g.DiskNumber, r.DiskNumber)),
		DurationMS:  firstInt(g.Duration, r.Duration) * 1000,
		ReleaseDate: firstString(g.PhysicalReleaseDate, r.ReleaseDate),
		BPM:         r.BPM,
		Gain:        gain,
		ISRC:        firstString(g.ISRC, r.ISRC),
		Explicit:    explicit != 0 || r.ExplicitLyrics,
		Copyright:   g.Copyright,
		Album: TrackAlbum{
			ID:          firstString(a.ID.String(), r.Album.ID.String(), g.AlbID.String()),
			Title:       firstString(a.Title, r.Album.Title, g.AlbTitle),
			CoverURL:    firstString(a.CoverXL, r.Album.CoverXL, gwImageSize("cover", g.AlbPicture, 1000)),
			ReleaseDate: firstString(a.ReleaseDate, r.Album.ReleaseDate),
			Label:       a.Label,
			TrackCount:  a.NbTracks,
		},
		Contributors: g.Contributors,
	}
	for _, gen := range a.Genres.Data {
		ti.Album.Genres = append(ti.Album.Genres, gen.Name)
	}
	for _, ct := range r.Contributors {
		ti.Artists = append(ti.Artists, TrackArtist{
			ID: ct.ID.String(), Name: ct.Name, Role: strings.ToLower(ct.Role), PictureURL: ct.PictureMedium,
		})
	}
	if len(ti.Artists) == 0 {
		for _, ga := range g.Artists {
			role := "main"
			if n, _ := ga.RoleID.Int64(); n != 0 {
				role = "featured"
			}
			ti.Artists = append(ti.Artists, TrackArtist{
				ID: ga.ArtID.String(), Name: ga.ArtName, Role: role, PictureURL: gwImage("artist", ga.ArtPicture),
			})
		}
	}
	complete = gwErr == nil && restErr == nil && albmErr == nil
	return ti, nil
}
