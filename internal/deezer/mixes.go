package deezer

import (
	"encoding/json"
	"fmt"
)

// This file holds per-seed mixes: "start radio from this song / this artist".
// Like Flow (flow.go) these go through gw-light, as the web player calls them.

// TrackMix returns a mix seeded from one track ("song radio"). Uses the gw
// song.getSearchTrackMix method; start_with_input_track keeps the seed track
// as the first entry so playback can begin from the song the user picked.
func (c *Client) TrackMix(trackID string) ([]Track, error) {
	body := fmt.Sprintf(`{"sng_id":%s,"start_with_input_track":"true"}`, jsonEsc(trackID))
	b, err := c.gw("song.getSearchTrackMix", body)
	if err != nil {
		return nil, err
	}
	return parseGwTrackList(b)
}

// ArtistMix returns Deezer's "smart radio" seeded from an artist ("artist
// radio"). Uses the gw smart.getSmartRadio method.
func (c *Client) ArtistMix(artistID string) ([]Track, error) {
	body := fmt.Sprintf(`{"art_id":%s}`, jsonEsc(artistID))
	b, err := c.gw("smart.getSmartRadio", body)
	if err != nil {
		return nil, err
	}
	return parseGwTrackList(b)
}

// parseGwTrackList decodes a gw track list: {results:{data:[...]}} or a bare
// {results:[...]}. An empty list is nil; a body of neither shape is an error.
func parseGwTrackList(b []byte) ([]Track, error) {
	var obj struct {
		Results struct {
			Data *[]gwTrackDTO `json:"data"`
		} `json:"results"`
	}
	var arr struct {
		Results *[]gwTrackDTO `json:"results"`
	}
	var dtos []gwTrackDTO
	switch {
	case json.Unmarshal(b, &obj) == nil && obj.Results.Data != nil:
		dtos = *obj.Results.Data
	case json.Unmarshal(b, &arr) == nil && arr.Results != nil:
		dtos = *arr.Results
	default:
		return nil, fmt.Errorf("unreadable track list")
	}
	if len(dtos) == 0 {
		return nil, nil
	}
	out := make([]Track, len(dtos))
	for i, t := range dtos {
		out[i] = t.toTrack()
	}
	return out, nil
}
