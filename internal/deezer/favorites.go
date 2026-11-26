package deezer

// The account's library beyond liked songs, as Deezer's web player handles it
// (2026-11-23): liked songs paged with a checksum, favorites from the profile page and
// Pipe, search history and bans via Pipe, the explicit level via the gateway.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// favPage is the web player's page of liked songs (song.getFavoriteIds,
// favorite_song.getList).
const favPage = 10000

// FavoriteIDs returns the ids of the account's liked songs, newest first. As
// the web player does, it sends the checksum of the last list it read, and
// Deezer answers "Not modified" while the list is unchanged.
func (c *Client) FavoriteIDs() ([]string, error) {
	c.favMu.Lock()
	defer c.favMu.Unlock()
	var ids []string
	checksum := c.favChecksum
	for start := 0; ; start += favPage {
		cs := any(nil)
		if start == 0 && checksum != "" {
			cs = checksum
		}
		body, _ := json.Marshal(map[string]any{"start": start, "nb": favPage, "checksum": cs})
		b, err := c.gw("song.getFavoriteIds", string(body))
		if err != nil {
			return nil, err
		}
		var r struct {
			Results json.RawMessage `json:"results"`
		}
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, err
		}
		if string(r.Results) == `"Not modified"` {
			return append([]string(nil), c.favIDs...), nil
		}
		var page struct {
			Data []struct {
				SngID json.Number `json:"SNG_ID"`
			} `json:"data"`
			Total    int    `json:"total"`
			Checksum string `json:"checksum"`
		}
		if err := json.Unmarshal(r.Results, &page); err != nil {
			return nil, fmt.Errorf("parse song.getFavoriteIds: %w", err)
		}
		for _, d := range page.Data {
			ids = append(ids, d.SngID.String())
		}
		if start == 0 {
			checksum = page.Checksum
		}
		if len(page.Data) < favPage || len(ids) >= page.Total {
			break
		}
	}
	c.favChecksum, c.favIDs = checksum, ids
	return append([]string(nil), ids...), nil
}

// Favorites lists the account's liked songs, all of them, in pages of 10 000
// (gw favorite_song.getList, as the web player's profile page reads them).
func (c *Client) Favorites() ([]Track, error) {
	extra := fmt.Sprintf(`"user_id":%s`, jsonEsc(c.uid()))
	var all []Track
	for start := 0; ; start += favPage {
		page, total, err := c.gwTrackPage("favorite_song.getList", extra, start, favPage)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < favPage || (total > 0 && len(all) >= total) {
			return all, nil
		}
	}
}

// FavoriteAlbums lists the account's favorite albums, newest first (gw
// deezer.pageProfile, tab albums).
func (c *Client) FavoriteAlbums() ([]Album, error) {
	var r struct {
		Results struct {
			Tab struct {
				Albums struct {
					Data []struct {
						AlbID   json.Number `json:"ALB_ID"`
						Title   string      `json:"ALB_TITLE"`
						Picture string      `json:"ALB_PICTURE"`
						ArtID   json.Number `json:"ART_ID"`
						ArtName string      `json:"ART_NAME"`
					} `json:"data"`
				} `json:"albums"`
			} `json:"TAB"`
		} `json:"results"`
	}
	if err := c.profileTab("albums", &r); err != nil {
		return nil, err
	}
	out := make([]Album, 0, len(r.Results.Tab.Albums.Data))
	for _, a := range r.Results.Tab.Albums.Data {
		out = append(out, Album{ID: a.AlbID.String(), Name: a.Title, ArtworkURL: gwImage("cover", a.Picture),
			Artists: []Artist{{ID: a.ArtID.String(), Name: a.ArtName}}})
	}
	return out, nil
}

// FavoriteArtists lists the account's favorite artists, newest first (gw
// deezer.pageProfile, tab artists).
func (c *Client) FavoriteArtists() ([]ArtistInfo, error) {
	var r struct {
		Results struct {
			Tab struct {
				Artists struct {
					Data []struct {
						ArtID   json.Number `json:"ART_ID"`
						Name    string      `json:"ART_NAME"`
						Picture string      `json:"ART_PICTURE"`
						NbFan   json.Number `json:"NB_FAN"`
					} `json:"data"`
				} `json:"artists"`
			} `json:"TAB"`
		} `json:"results"`
	}
	if err := c.profileTab("artists", &r); err != nil {
		return nil, err
	}
	out := make([]ArtistInfo, 0, len(r.Results.Tab.Artists.Data))
	for _, a := range r.Results.Tab.Artists.Data {
		fans, _ := a.NbFan.Int64()
		out = append(out, ArtistInfo{ID: a.ArtID.String(), Name: a.Name, ArtworkURL: gwImage("artist", a.Picture), NbFans: int(fans)})
	}
	return out, nil
}

// FavoritePodcasts lists the account's favorite podcasts with their title,
// description and cover, from Pipe, 2 000 a page (a page costs about 20 000 of
// the 25 000 a query may).
func (c *Client) FavoritePodcasts() ([]Podcast, error) {
	const q = `query FavoritePodcasts($first: Int!, $after: String) { me { userFavorites { podcasts(first: $first, after: $after) { pageInfo { hasNextPage endCursor } edges { node { id displayTitle description cover { urls(pictureRequest: {width: 250, height: 250}) } } } } } } }`
	var out []Podcast
	var after any
	for {
		var r struct {
			Me struct {
				UserFavorites struct {
					Podcasts struct {
						PageInfo struct {
							HasNextPage bool   `json:"hasNextPage"`
							EndCursor   string `json:"endCursor"`
						} `json:"pageInfo"`
						Edges []struct {
							Node struct {
								ID           string `json:"id"`
								DisplayTitle string `json:"displayTitle"`
								Description  string `json:"description"`
								Cover        *struct {
									URLs []string `json:"urls"`
								} `json:"cover"`
							} `json:"node"`
						} `json:"edges"`
					} `json:"podcasts"`
				} `json:"userFavorites"`
			} `json:"me"`
		}
		if err := c.pipe("FavoritePodcasts", q, map[string]any{"first": 2000, "after": after}, &r); err != nil {
			return nil, err
		}
		page := r.Me.UserFavorites.Podcasts
		for _, e := range page.Edges {
			p := Podcast{ID: e.Node.ID, Name: e.Node.DisplayTitle, Description: e.Node.Description}
			if e.Node.Cover != nil && len(e.Node.Cover.URLs) > 0 {
				p.ArtworkURL = e.Node.Cover.URLs[0]
			}
			out = append(out, p)
		}
		if !page.PageInfo.HasNextPage || page.PageInfo.EndCursor == "" {
			return out, nil
		}
		after = page.PageInfo.EndCursor
	}
}

// profileTab reads one tab of the account's profile page, all of it.
func (c *Client) profileTab(tab string, out any) error {
	body := fmt.Sprintf(`{"user_id":%s,"tab":%s,"nb":%d}`, jsonEsc(c.uid()), jsonEsc(tab), favPage)
	b, err := c.gw("deezer.pageProfile", body)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// Favorite kinds, as Pipe names them.
const (
	FavAlbum    = "album"
	FavArtist   = "artist"
	FavPlaylist = "playlist"
	FavPodcast  = "podcast"
)

// favMutations are the web player's Pipe operations per kind: add, remove,
// and the id variable.
var favMutations = map[string][3]string{
	FavAlbum: {`mutation AddAlbumToFavorite($albumId: String!) { addAlbumToFavorite(albumId: $albumId) { album { id isFavorite } } }`,
		`mutation RemoveAlbumFromFavorite($albumId: String!) { removeAlbumFromFavorite(albumId: $albumId) { album { id isFavorite } } }`, "albumId"},
	FavArtist: {`mutation AddArtistFavorite($artistId: String!) { addArtistToFavorite(artistId: $artistId) { favoritedAt artist { id isFavorite } } }`,
		`mutation RemoveArtistFromFavorite($artistId: String!) { removeArtistFromFavorite(artistId: $artistId) { artist { id isFavorite } } }`, "artistId"},
	FavPlaylist: {`mutation AddPlaylistToFavorite($playlistId: String!) { addPlaylistToFavorite(playlistId: $playlistId) { playlist { id isFavorite } } }`,
		`mutation RemovePlaylistFromFavorite($playlistId: String!) { removePlaylistFromFavorite(playlistId: $playlistId) { playlist { id isFavorite } } }`, "playlistId"},
	FavPodcast: {`mutation AddPodcastToFavorite($podcastId: String!) { addPodcastToFavorite(podcastId: $podcastId) { podcast { id isPodcastFavorite: isFavorite } } }`,
		`mutation RemovePodcastFromFavorite($podcastId: String!) { removePodcastFromFavorite(podcastId: $podcastId) { podcast { id isPodcastFavorite: isFavorite } } }`, "podcastId"},
}

// SetFavorite adds (on) or removes an album, artist, playlist or podcast from
// the account's favorites (kind: FavAlbum, ...).
func (c *Client) SetFavorite(kind, id string, on bool) error {
	m, ok := favMutations[kind]
	if !ok {
		return fmt.Errorf("favorite: unknown kind %q", kind)
	}
	q := m[1]
	if on {
		q = m[0]
	}
	return c.pipe(opName(q), q, map[string]any{m[2]: id}, nil)
}

// SetRecommendationBan stops (on) or allows again a track ("track") or an
// artist ("artist") in the account's recommendations, as "Don't recommend"
// does.
func (c *Client) SetRecommendationBan(kind, id string, on bool) error {
	var q, v string
	switch kind {
	case "track":
		q, v = `mutation %sTrackFromRecommendation($trackId: String!) { %sTrackFromRecommendation(trackId: $trackId) { track { id isBannedFromRecommendation } } }`, "trackId"
	case "artist":
		q, v = `mutation %sArtistFromRecommendation($artistId: String!) { %sArtistFromRecommendation(artistId: $artistId) { artist { id isBannedFromRecommendation } } }`, "artistId"
	default:
		return fmt.Errorf("recommendation ban: unknown kind %q", kind)
	}
	op, field := "Unban", "unban"
	if on {
		op, field = "Ban", "ban"
	}
	q = fmt.Sprintf(q, op, field)
	return c.pipe(opName(q), q, map[string]any{v: id}, nil)
}

// opName is the operation name a GraphQL text declares.
func opName(q string) string {
	var kind, name string
	fmt.Sscanf(q, "%s %s", &kind, &name)
	for i, r := range name {
		if r == '(' || r == '{' {
			return name[:i]
		}
	}
	return name
}

// SearchHistoryItem is a search history entry or a search's best result: its type
// ("Track", "Album", ...), id, title, subtitle line and a small picture.
type SearchHistoryItem struct {
	Type     string `json:"type"`
	ID       string `json:"id"`
	Title    string `json:"title,omitempty"`
	Subtitle string `json:"subtitle,omitempty"`
	Picture  string `json:"picture,omitempty"`
}

// searchNode is a Pipe search item of any type, as the web player's search
// history fragments read it (measured 2026-11-23).
type searchNode struct {
	Type         string `json:"__typename"`
	ID           string `json:"id"`
	Title        string `json:"title"`
	DisplayTitle string `json:"displayTitle"`
	Name         string `json:"name"`
	Contributors struct {
		Edges []struct {
			Node struct {
				Name string `json:"name"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"contributors"`
	Owner   struct{ Name string } `json:"owner"`
	Podcast struct {
		DisplayTitle string `json:"displayTitle"`
	} `json:"podcast"`
	Album struct {
		Cover struct{ Small []string } `json:"cover"`
	} `json:"album"`
	Cover   struct{ Small []string } `json:"cover"`
	Picture struct{ Small []string } `json:"picture"`
}

// The fields each kind of search item is read with (searchNode). A field of one known type
// selects only its own set; a field that can be any kind (searchFields) selects them all
// as fragments. A fragment of another type on a field of a known type is invalid GraphQL,
// and Pipe then refuses the whole query.
const (
	smallPicture     = `small: urls(pictureRequest: {height: 100, width: 100})`
	trackFields      = `__typename id title contributors { edges { node { ... on Artist { name } } } } album { cover { ` + smallPicture + ` } }`
	albumFields      = `__typename id displayTitle contributors { edges { node { ... on Artist { name } } } } cover { ` + smallPicture + ` }`
	artistFields     = `__typename id name picture { ` + smallPicture + ` }`
	playlistFields   = `__typename id title owner { name } picture { ` + smallPicture + ` }`
	podcastFields    = `__typename id displayTitle cover { ` + smallPicture + ` }`
	episodeFields    = `__typename id title podcast { displayTitle }`
	livestreamFields = `__typename id name cover { ` + smallPicture + ` }`
)

// searchFields reads a searchNode of any kind: the web player's search-history fields.
const searchFields = `__typename ... on Track { ` + trackFields + ` } ... on Album { ` + albumFields + ` } ... on Artist { ` + artistFields +
	` } ... on Playlist { ` + playlistFields + ` } ... on Podcast { ` + podcastFields + ` } ... on PodcastEpisode { ` + episodeFields +
	` } ... on Livestream { ` + livestreamFields + ` }`

func (n searchNode) item() SearchHistoryItem {
	it := SearchHistoryItem{Type: n.Type, ID: n.ID, Title: n.Title}
	for _, s := range []string{n.DisplayTitle, n.Name} {
		if it.Title == "" {
			it.Title = s
		}
	}
	var names []string
	for _, e := range n.Contributors.Edges {
		names = append(names, e.Node.Name)
	}
	it.Subtitle = strings.Join(names, ", ")
	if it.Subtitle == "" {
		it.Subtitle = n.Owner.Name + n.Podcast.DisplayTitle
	}
	for _, p := range [][]string{n.Album.Cover.Small, n.Cover.Small, n.Picture.Small} {
		if it.Picture == "" && len(p) > 0 {
			it.Picture = p[0]
		}
	}
	return it
}

// SearchHistory returns the account's search history, newest first, at most n.
func (c *Client) SearchHistory(n int) ([]SearchHistoryItem, error) {
	var r struct {
		Me struct {
			SearchHistory struct {
				SuccessResults struct {
					Edges []struct {
						Node searchNode `json:"node"`
					} `json:"edges"`
				} `json:"successResults"`
			} `json:"searchHistory"`
		} `json:"me"`
	}
	q := `query History($first: Int, $cursor: String) { me { searchHistory { successResults(first: $first, after: $cursor) { edges { node { ` + searchFields + ` } } } } } }`
	if err := c.pipe("History", q, map[string]any{"first": n}, &r); err != nil {
		return nil, err
	}
	out := make([]SearchHistoryItem, 0, len(r.Me.SearchHistory.SuccessResults.Edges))
	for _, e := range r.Me.SearchHistory.SuccessResults.Edges {
		out = append(out, e.Node.item())
	}
	return out, nil
}

// BestResult returns a search's best result as Deezer ranks it (Pipe
// instantSearch bestResult), or nil when it has none or one of a kind
// Purple Disc cannot open (a channel, a Flow config, a smart tracklist).
func (c *Client) BestResult(query string) (*SearchHistoryItem, error) {
	var r struct {
		InstantSearch struct {
			BestResult map[string]json.RawMessage `json:"bestResult"`
		} `json:"instantSearch"`
	}
	q := `query BestResult($query: String!) { instantSearch(query: $query) { bestResult { __typename` +
		` ... on InstantSearchArtistBestResult { artist { ` + artistFields + ` } }` +
		` ... on InstantSearchAlbumBestResult { album { ` + albumFields + ` } }` +
		` ... on InstantSearchTrackBestResult { track { ` + trackFields + ` } }` +
		` ... on InstantSearchPlaylistBestResult { playlist { ` + playlistFields + ` } }` +
		` ... on InstantSearchPodcastBestResult { podcast { ` + podcastFields + ` } } } } }`
	if err := c.pipe("BestResult", q, map[string]any{"query": query}, &r); err != nil {
		return nil, err
	}
	for _, k := range []string{"artist", "album", "track", "playlist", "podcast"} {
		if raw, ok := r.InstantSearch.BestResult[k]; ok {
			var n searchNode
			if json.Unmarshal(raw, &n) == nil && n.ID != "" {
				it := n.item()
				return &it, nil
			}
		}
	}
	return nil, nil
}

// ErrPinLimit is Deezer refusing a pin: Home holds no more.
var ErrPinLimit = errors.New("home is full")

// pinTypes are the kinds Purple Disc pins to Home and shows there, as Pipe names them.
var pinTypes = map[string]string{"Album": "ALBUM", "Artist": "ARTIST", "Playlist": "PLAYLIST", "Podcast": "PODCAST", "Track": "TRACK"}

// Pinned returns what the account pinned to Home (Pipe quickAccess, context HOMEPAGE), in its
// order, with pictures for tiles.
func (c *Client) Pinned() ([]SearchHistoryItem, error) {
	var r struct {
		Me struct {
			QuickAccess struct {
				PinnedItems []struct {
					Item searchNode `json:"item"`
				} `json:"pinnedItems"`
			} `json:"quickAccess"`
		} `json:"me"`
	}
	fields := strings.ReplaceAll(searchFields, "height: 100, width: 100", "height: 500, width: 500")
	q := `query PinnedItems { me { quickAccess(context: HOMEPAGE) { pinnedItems(support: [ALBUM, ARTIST, PLAYLIST, PODCAST, TRACK]) { item { ` + fields + ` } } } } }`
	if err := c.pipe("PinnedItems", q, nil, &r); err != nil {
		return nil, err
	}
	out := make([]SearchHistoryItem, 0, len(r.Me.QuickAccess.PinnedItems))
	for _, p := range r.Me.QuickAccess.PinnedItems {
		out = append(out, p.Item.item())
	}
	return out, nil
}

// SetPinned pins an item to Home or unpins it (typ as SearchHistoryItem.Type: "Album", "Artist",
// "Playlist", "Podcast", "Track"). A pin Home has no room for is ErrPinLimit.
func (c *Client) SetPinned(typ, id string, on bool) error {
	t, ok := pinTypes[typ]
	if !ok {
		return fmt.Errorf("pin: unknown type %q", typ)
	}
	vars := map[string]any{"id": id, "type": t, "context": "HOMEPAGE"}
	if !on {
		const q = `mutation UnpinItem($id: String!, $type: QuickAccessTypeInput!, $context: QuickAccessContextInput!) { quickAccessUnpinItem(input: {id: $id, type: $type, context: $context}) { __typename } }`
		return c.pipe("UnpinItem", q, vars, nil)
	}
	var r struct {
		Append struct {
			IsLimitExceed         bool `json:"isLimitExceed"`
			IsUnsupportedItemType bool `json:"isUnsupportedItemType"`
		} `json:"quickAccessAppendItem"`
	}
	const q = `mutation PinItem($id: String!, $type: QuickAccessTypeInput!, $context: QuickAccessContextInput!) { quickAccessAppendItem(input: {id: $id, type: $type, context: $context}) { __typename ... on QuickAccessAppendItemMutationError { isLimitExceed isUnsupportedItemType } } }`
	if err := c.pipe("PinItem", q, vars, &r); err != nil {
		return err
	}
	switch {
	case r.Append.IsLimitExceed:
		return ErrPinLimit
	case r.Append.IsUnsupportedItemType:
		return fmt.Errorf("pin: Deezer refused type %s", t)
	}
	return nil
}

// PlaylistDetails is what a playlist's page edits: its title, description,
// and whether it is private or collaborative.
type PlaylistDetails struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	IsPrivate       bool   `json:"isPrivate"`
	IsCollaborative bool   `json:"isCollaborative"`
	OwnerID         string `json:"ownerId"`
}

// PlaylistDetails reads a playlist's details (Pipe playlist).
func (c *Client) PlaylistDetails(id string) (PlaylistDetails, error) {
	var r struct {
		Playlist struct {
			PlaylistDetails
			Owner struct{ ID string } `json:"owner"`
		} `json:"playlist"`
	}
	const q = `query PlaylistDetails($playlistId: String!) { playlist(playlistId: $playlistId) { id title description isPrivate isCollaborative owner { id } } }`
	if err := c.pipe("PlaylistDetails", q, map[string]any{"playlistId": id}, &r); err != nil {
		return PlaylistDetails{}, err
	}
	d := r.Playlist.PlaylistDetails
	d.OwnerID = r.Playlist.Owner.ID
	return d, nil
}

// PlaylistUpdate is a change to a playlist: the fields set change, the others
// stay. A playlist is not private and collaborative at once (Deezer refuses).
type PlaylistUpdate struct {
	Title           *string `json:"title,omitempty"`
	Description     *string `json:"description,omitempty"`
	IsPrivate       *bool   `json:"isPrivate,omitempty"`
	IsCollaborative *bool   `json:"isCollaborative,omitempty"`
}

// UpdatePlaylist changes a playlist the account owns (Pipe UpdatePlaylist, the
// web player's).
func (c *Client) UpdatePlaylist(id string, u PlaylistUpdate) error {
	if u.IsPrivate != nil && u.IsCollaborative != nil && *u.IsPrivate && *u.IsCollaborative {
		return fmt.Errorf("a playlist is not private and collaborative at once")
	}
	input := struct {
		PlaylistID string `json:"playlistId"`
		PlaylistUpdate
	}{id, u}
	const q = `mutation UpdatePlaylist($input: PlaylistUpdateMutationInput!) { updatePlaylist(input: $input) { playlist { id } } }`
	return c.pipe("UpdatePlaylist", q, map[string]any{"input": input}, nil)
}

// ReorderPlaylist puts a playlist's tracks in the order given (gw
// playlist.updateOrder, the web player's).
func (c *Client) ReorderPlaylist(id string, trackIDs []string) error {
	b, _ := json.Marshal(map[string]any{"playlist_id": id, "position": "0", "order": trackIDs})
	_, err := c.gw("playlist.updateOrder", string(b))
	return err
}

// IsFavorite reports whether an album, artist, playlist or podcast (kind:
// FavAlbum, ...) is among the account's favorites.
func (c *Client) IsFavorite(kind, id string) (bool, error) {
	if _, ok := favMutations[kind]; !ok {
		return false, fmt.Errorf("favorite: unknown kind %q", kind)
	}
	var r struct {
		Item struct {
			IsFavorite bool `json:"isFavorite"`
		} `json:"item"`
	}
	q := fmt.Sprintf(`query IsFavorite($id: String!) { item: %[1]s(%[1]sId: $id) { id isFavorite } }`, kind)
	err := c.pipe("IsFavorite", q, map[string]any{"id": id}, &r)
	return r.Item.IsFavorite, err
}

// AddSearchHistory records that item was picked from a search.
func (c *Client) AddSearchHistory(item SearchHistoryItem) error {
	v := map[string]string{"Track": "trackId", "Album": "albumId", "Artist": "artistId", "Playlist": "playlistId",
		"Podcast": "podcastId", "PodcastEpisode": "podcastEpisodeId", "Livestream": "livestreamId"}[item.Type]
	if v == "" {
		return fmt.Errorf("search history: unknown type %q", item.Type)
	}
	q := fmt.Sprintf(`mutation Add%[1]sInSearchHistory($%[2]s: String!) { add%[1]sInSearchSuccessResult(%[2]s: $%[2]s) { status } }`, item.Type, v)
	return c.pipe("Add"+item.Type+"InSearchHistory", q, map[string]any{v: item.ID}, nil)
}

// ClearSearchHistory empties the account's search history.
func (c *Client) ClearSearchHistory() error {
	return c.pipe("ClearSearchHistory", `mutation ClearSearchHistory { clearSearchSuccessResult { status } }`, nil, nil)
}

// Explicit-content levels, as Deezer names them.
const (
	ExplicitDisplay          = "explicit_display"
	ExplicitNoRecommendation = "explicit_no_recommendation"
	ExplicitHide             = "explicit_hide"
)

// ExplicitContent is the account's explicit-content setting from its login.
type ExplicitContent struct {
	Level     string   `json:"level"`
	Available []string `json:"available"`
	CanEdit   bool     `json:"canEdit"`
}

// Explicit returns the account's explicit-content setting.
func (c *Client) Explicit() ExplicitContent {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.explicit
}

// SetExplicitLevel changes the account's explicit-content level (gw
// user.updateExplicitContentLevel).
func (c *Client) SetExplicitLevel(level string) error {
	if _, err := c.gw("user.updateExplicitContentLevel", fmt.Sprintf(`{"EXPLICIT_CONTENT_LEVEL":%s}`, jsonEsc(level))); err != nil {
		return err
	}
	c.mu.Lock()
	c.explicit.Level = level
	c.mu.Unlock()
	return nil
}
