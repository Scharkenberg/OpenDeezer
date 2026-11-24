package deezer

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Library and playlist write operations, all over gw-light with a logged-in session.
// The method and param shapes follow the deemix conventions and need a Premium account
// to verify.

// AddFavoriteTrack likes a track (adds it to Liked Songs).
func (c *Client) AddFavoriteTrack(trackID string) error {
	_, err := c.gw("favorite_song.add", fmt.Sprintf(`{"SNG_ID":%s}`, jsonEsc(trackID)))
	return err
}

// RemoveFavoriteTrack unlikes a track.
func (c *Client) RemoveFavoriteTrack(trackID string) error {
	_, err := c.gw("favorite_song.remove", fmt.Sprintf(`{"SNG_ID":%s}`, jsonEsc(trackID)))
	return err
}

// AddToPlaylist appends a track to a playlist the user can edit.
func (c *Client) AddToPlaylist(playlistID, trackID string) error {
	_, err := c.gw("playlist.addSongs",
		fmt.Sprintf(`{"playlist_id":%s,"songs":[[%s,0]]}`, jsonEsc(playlistID), jsonEsc(trackID)))
	return err
}

// RemoveFromPlaylist removes a track from a playlist.
func (c *Client) RemoveFromPlaylist(playlistID, trackID string) error {
	_, err := c.gw("playlist.deleteSongs",
		fmt.Sprintf(`{"playlist_id":%s,"songs":[[%s,0]]}`, jsonEsc(playlistID), jsonEsc(trackID)))
	return err
}

// songsArray builds a gw "songs" payload: [["id",0],["id",0],...].
func songsArray(trackIDs []string) string {
	if len(trackIDs) == 0 {
		return "[]"
	}
	parts := make([]string, len(trackIDs))
	for i, id := range trackIDs {
		parts[i] = fmt.Sprintf(`[%s,0]`, jsonEsc(id))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// CreatePlaylist creates a new playlist (optionally seeded with tracks) and
// returns its id.
func (c *Client) CreatePlaylist(title string, trackIDs []string) (string, error) {
	t, _ := json.Marshal(title) // JSON-escape the title
	body := fmt.Sprintf(`{"title":%s,"description":"","songs":%s,"status":0}`,
		t, songsArray(trackIDs))
	b, err := c.gw("playlist.create", body)
	if err != nil {
		return "", err
	}
	// gw returns the new playlist id as results (a bare number) or an object.
	var r struct {
		Results json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return "", err
	}
	s := strings.Trim(strings.TrimSpace(string(r.Results)), `"`)
	if s == "" || s == "null" {
		return "", fmt.Errorf("playlist.create: no id returned")
	}
	return s, nil
}

// RenamePlaylist changes a playlist's title.
func (c *Client) RenamePlaylist(playlistID, title string) error {
	t, _ := json.Marshal(title)
	_, err := c.gw("playlist.update",
		fmt.Sprintf(`{"playlist_id":%s,"title":%s}`, jsonEsc(playlistID), t))
	return err
}

// DeletePlaylist deletes a playlist the user owns.
func (c *Client) DeletePlaylist(playlistID string) error {
	_, err := c.gw("playlist.delete", fmt.Sprintf(`{"playlist_id":%s}`, jsonEsc(playlistID)))
	return err
}
