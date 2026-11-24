package bridge

import (
	"encoding/json"
	"errors"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
	odlog "github.com/Cycl0o0/OpenDeezer/v3/internal/log"
)

// The requests both native shells answer the same way. Each takes the
// signed-in client and returns the wire value, or the shell's status code.

// LyricLine is one timed lyrics line on the native JSON wire.
type LyricLine struct {
	TimeMS     int64  `json:"timeMs"`
	Text       string `json:"text"`
	Translated string `json:"translated,omitempty"`
}

// Lyrics reads a track's lyrics: {plain, synced:[{timeMs,text}], isSynced}.
func Lyrics(c *deezer.Client, trackID string) (any, error) {
	l, err := c.Lyrics(trackID)
	if err != nil {
		return nil, err
	}
	synced := make([]LyricLine, len(l.Synced))
	for i, s := range l.Synced {
		synced[i] = LyricLine{TimeMS: s.TimeMS, Text: s.Text, Translated: s.Translated}
	}
	return map[string]any{"plain": l.Plain, "synced": synced, "isSynced": l.IsSynced()}, nil
}

// ArtistProfile reads an artist with top tracks, albums and related artists:
// {artist,top,albums,related}.
func ArtistProfile(c *deezer.Client, id string) (any, error) {
	pg, err := c.ArtistProfile(id)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"artist":  FromArtistInfo(pg.Artist),
		"top":     FromTracks(pg.Top),
		"albums":  FromAlbums(pg.Albums),
		"related": FromArtistInfos(pg.Related),
	}, nil
}

// FavoriteIDs reads the liked track ids as a JSON array, [] when there are none.
func FavoriteIDs(c *deezer.Client) (any, error) {
	ids, err := c.FavoriteIDs()
	if ids == nil {
		ids = []string{}
	}
	return ids, err
}

// Pinned reads what the account pinned to Home, in its order:
// {"items":[{"type","id","title","subtitle","picture"},...]}.
func Pinned(c *deezer.Client) (any, error) {
	items, err := c.Pinned()
	if items == nil {
		items = []deezer.SearchHistoryItem{}
	}
	return map[string]any{"items": items}, err
}

// SetPinned pins an item to Home or unpins it (type "Album", "Artist",
// "Playlist", "Podcast", "Track"): 1 on success, 2 when Home holds no more,
// 0 on another failure.
func SetPinned(c *deezer.Client, typ, id string, on bool) int {
	switch err := c.SetPinned(typ, id, on); {
	case err == nil:
		return 1
	case errors.Is(err, deezer.ErrPinLimit):
		return 2
	default:
		odlog.Warn("pin: %v", err)
		return 0
	}
}

// IsFavorite reports whether an album, artist, playlist or podcast is among the
// favorites: 1 yes, 0 no, -1 unknown (the request failed).
func IsFavorite(c *deezer.Client, kind, id string) int {
	fav, err := c.IsFavorite(kind, id)
	switch {
	case err != nil:
		return -1
	case fav:
		return 1
	}
	return 0
}

// CreatePlaylist creates a playlist with the tracks of idsJSON (a JSON array
// of ids; "" or "[]" for none): {"id":"..."}.
func CreatePlaylist(c *deezer.Client, title, idsJSON string) (any, error) {
	var ids []string
	_ = json.Unmarshal([]byte(idsJSON), &ids)
	id, err := c.CreatePlaylist(title, ids)
	if err != nil {
		return nil, err
	}
	return map[string]string{"id": id}, nil
}
