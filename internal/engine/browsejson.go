package engine

// The JSON replies both native shells give for Home, the charts, a search and a
// search's best result.

import (
	"encoding/json"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/audio"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/bridge"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
)

// ReplyJSON is v as JSON, or {"error":"..."} when err is set or v does not marshal.
func ReplyJSON(v any, err error) []byte {
	if err == nil {
		b, e := json.Marshal(v)
		if e == nil {
			return b
		}
		err = e
	}
	b, _ := json.Marshal(map[string]string{"error": err.Error()})
	return b
}

// OnlineJSON is fn's answer with the signed-in client, or why there is none
// (offline mode, no login).
func OnlineJSON(fn func(c *deezer.Client) (any, error)) []byte {
	c, err := Online()
	if err != nil {
		return ReplyJSON(nil, err)
	}
	return ReplyJSON(fn(c))
}

// LastErrorJSON is {"error":"...","network":bool}: why the player last stopped or a play
// failed ("" for none), and whether a lost network caused it.
func LastErrorJSON() []byte {
	msg, network := "", false
	withPlayer(func(p *audio.Player) { msg, network = p.LastError(), p.LastErrorNetwork() })
	return ReplyJSON(map[string]any{"error": msg, "network": network}, nil)
}

// HomeJSON is {topTracks, topAlbums} from the charts and the user's {playlists};
// a section that fails to load comes back empty.
func HomeJSON() []byte {
	return OnlineJSON(func(c *deezer.Client) (any, error) {
		var tracks []deezer.Track
		var albums []deezer.Album
		if ch, err := c.Charts("0"); err == nil && ch != nil {
			tracks, albums = ch.Tracks, ch.Albums
		}
		ps, _ := c.Playlists()
		return map[string]any{
			"topTracks": bridge.FromTracks(tracks),
			"topAlbums": bridge.FromAlbums(albums),
			"playlists": bridge.FromPlaylists(ps),
		}, nil
	})
}

// ChartsJSON is the global top {tracks, albums, artists, playlists}.
func ChartsJSON() []byte {
	return OnlineJSON(func(c *deezer.Client) (any, error) {
		ch, err := c.Charts("0")
		if err != nil {
			return nil, err
		}
		return catalog(ch.Tracks, ch.Albums, ch.Artists, ch.Playlists), nil
	})
}

// SearchJSON is a search's {tracks, albums, artists, playlists}.
func SearchJSON(q string) []byte {
	return OnlineJSON(func(c *deezer.Client) (any, error) {
		r, err := c.Search(q)
		if err != nil {
			return nil, err
		}
		return catalog(r.Tracks, r.Albums, r.Artists, r.Playlists), nil
	})
}

// BestResultJSON is a search's best result, {"type","id","title","subtitle",
// "picture"}, or {} when it has none.
func BestResultJSON(q string) []byte {
	return OnlineJSON(func(c *deezer.Client) (any, error) {
		best, err := c.BestResult(q)
		if best == nil {
			return map[string]any{}, err
		}
		return best, err
	})
}

func catalog(tracks []deezer.Track, albums []deezer.Album, artists []deezer.ArtistInfo, playlists []deezer.Playlist) map[string]any {
	return map[string]any{
		"tracks": bridge.FromTracks(tracks), "albums": bridge.FromAlbums(albums),
		"artists": bridge.FromArtistInfos(artists), "playlists": bridge.FromPlaylists(playlists),
	}
}
