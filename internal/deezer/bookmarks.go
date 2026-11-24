package deezer

// Podcast progress, as Deezer's web player keeps it (measured 2026-11-23): bookmarks
// come from gw deezer.userMenu (EPISODE_BOOKMARKS) and are written with
// episode.bookmarkSet; episode.markAsPlayed / markAsNotPlayed set the played state.

import (
	"encoding/json"
	"fmt"
)

// EpisodeBookmark is where the account stopped in an episode, in seconds,
// and whether it heard it.
type EpisodeBookmark struct {
	OffsetS int  `json:"offset"`
	Heard   bool `json:"heard"`
}

// EpisodeBookmarks returns the account's episode bookmarks by episode id.
func (c *Client) EpisodeBookmarks() (map[string]EpisodeBookmark, error) {
	b, err := c.gw("deezer.userMenu", `{"checksum":true,"checksums":{}}`)
	if err != nil {
		return nil, err
	}
	var r struct {
		Results struct {
			Bookmarks struct {
				Data []struct {
					EpisodeID json.Number `json:"EPISODE_ID"`
					Offset    json.Number `json:"OFFSET"`
					IsHeard   string      `json:"IS_HEARD"`
				} `json:"data"`
			} `json:"EPISODE_BOOKMARKS"`
		} `json:"results"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("parse deezer.userMenu: %w", err)
	}
	out := make(map[string]EpisodeBookmark, len(r.Results.Bookmarks.Data))
	for _, d := range r.Results.Bookmarks.Data {
		off, _ := d.Offset.Int64()
		out[d.EpisodeID.String()] = EpisodeBookmark{OffsetS: int(off), Heard: d.IsHeard == "1"}
	}
	return out, nil
}

// SetEpisodeBookmark records that the account stopped at offsetS of an
// episode durationS long, and whether that counts as heard (EpisodeHeard).
func (c *Client) SetEpisodeBookmark(episodeID string, offsetS, durationS int, heard bool) error {
	b, _ := json.Marshal(map[string]any{"EPISODE_ID": episodeID, "OFFSET": offsetS, "DURATION": durationS, "ISHEARD": heard})
	_, err := c.gw("episode.bookmarkSet", string(b))
	return err
}

// MarkEpisodePlayed marks an episode as played, or as not played.
func (c *Client) MarkEpisodePlayed(episodeID string, played bool) error {
	method := "episode.markAsNotPlayed"
	if played {
		method = "episode.markAsPlayed"
	}
	_, err := c.gw(method, fmt.Sprintf(`{"EPISODE_ID":%s}`, jsonEsc(episodeID)))
	return err
}

// EpisodeHeard reports whether stopping at posS of an episode durS long
// counts as having heard it: from 90 % of its length with at most two
// minutes left, as both Deezer players count it.
func EpisodeHeard(posS, durS int) bool {
	return durS > 0 && posS*10 >= durS*9 && durS-posS <= 120
}
