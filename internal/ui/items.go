package ui

import (
	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/i18n"
	"github.com/charmbracelet/bubbles/list"
)

// rowKind identifies what a list row represents.
type rowKind int

const (
	rowMenu rowKind = iota
	rowTrack
	rowPlaylist
	rowAlbum
	rowArtist
	rowPodcast
	rowEpisode
	rowDevice
	rowPeer    // a discovered PurpleDisc Connect device
	rowSection // non-activatable section header (artist page)
	rowHistory // a history entry on the stats screen (playable by track id)
	rowInfo    // non-activatable info line (stats totals, top artists)
	rowSetting // a Settings row (settings()[row.setting])
)

// kindGlyphs mark the rows that open something other than a track.
var kindGlyphs = map[rowKind]string{rowAlbum: "●", rowArtist: "♪", rowPlaylist: "≡", rowPodcast: "○"}

// menuAction is the action a rowMenu triggers.
type menuAction int

const (
	actLiked menuAction = iota
	actPlaylists
	actSearch
	actCharts
	actResume
	actFlow
	actPodcasts
	actRemote
	actRemoteManual // enter a device address by hand
	actWebRemote    // phone web remote (QR + pairing code)
	actSettings     // the Settings screen
	actUpdate       // open the available update's release page
	actNewPlaylist  // "New playlist…" entry in the add-to-playlist picker
	actStats        // local listening stats (history + top tracks/artists)
	actFlowMoods    // list Flow's moods
	actFlowMood     // play the mood in row.id
	actChartGenres  // list the chart genres
	actChartGenre   // open the charts of the genre in row.id
)

// row is a single list entry. It implements bubbles/list.Item.
type row struct {
	kind     rowKind
	title    string
	desc     string
	right    string            // right column: a track's duration, a setting's value
	glyph    string            // a Home row's mark, drawn in the accent
	action   menuAction        // for rowMenu
	track    deezer.Track      // for rowTrack
	playlist deezer.Playlist   // for rowPlaylist
	album    deezer.Album      // for rowAlbum
	artist   deezer.ArtistInfo // for rowArtist
	podcast  deezer.Podcast    // for rowPodcast
	episode  deezer.Episode    // for rowEpisode
	deviceID string            // for rowDevice ("" = system default)
	peerAddr string            // for rowPeer (host:port)
	histID   string            // for rowHistory (Deezer track id; may be "")
	id       string            // for actFlowMood (mood id) and actChartGenre (genre id)
	setting  int               // for rowSetting
}

func (r row) Title() string       { return r.title }
func (r row) Description() string { return r.desc }

// FilterValue matches the list filter (ctrl+f) against title and description, so
// tracks are found by artist or album too. Section headers never match.
func (r row) FilterValue() string {
	if r.kind == rowSection {
		return ""
	}
	if r.desc == "" {
		return r.title
	}
	return r.title + " " + r.desc
}

// sectionRow is a non-activatable header separating artist-page sections; an empty
// title is a blank row.
func sectionRow(title string) row {
	return row{kind: rowSection, title: title}
}

// withSection appends a section header to items, after a blank row unless it opens
// the list.
func withSection(items []list.Item, title string) []list.Item {
	if len(items) > 0 {
		items = append(items, sectionRow(""))
	}
	return append(items, sectionRow(title))
}

func trackRow(t deezer.Track) row {
	r := row{kind: rowTrack, title: t.Name, desc: t.ArtistLine() + " · " + t.AlbumName, track: t}
	if t.DurationMS > 0 {
		r.right = fmtMS(t.DurationMS)
	}
	return r
}

func playlistRow(p deezer.Playlist) row {
	d := p.Owner
	if d == "" {
		d = i18n.T("playlist")
	}
	return row{kind: rowPlaylist, title: p.Name, desc: d, playlist: p}
}

func albumRow(a deezer.Album) row {
	name := ""
	if len(a.Artists) > 0 {
		name = a.Artists[0].Name
	}
	return row{kind: rowAlbum, title: a.Name, desc: name, album: a}
}

func artistRow(a deezer.ArtistInfo) row {
	return row{kind: rowArtist, title: a.Name, desc: i18n.T("artist"), artist: a}
}

func podcastRow(p deezer.Podcast) row {
	return row{kind: rowPodcast, title: p.Name, desc: i18n.T("podcast"), podcast: p}
}

func episodeRow(e deezer.Episode) row {
	d := e.ReleaseDate
	if d == "" {
		d = i18n.T("episode")
	}
	return row{kind: rowEpisode, title: e.Title, desc: d, episode: e}
}

func deviceRow(id, name string, current bool) row {
	title := name
	desc := i18n.T("output device")
	if current {
		title = "● " + name
		desc = i18n.T("current")
	}
	return row{kind: rowDevice, title: title, desc: desc, deviceID: id}
}

// peerRow renders a discovered Connect device with its client, version and address.
func peerRow(p peerDevice) row {
	desc := deviceTypeLabel(p.dev.Client)
	if p.dev.Version != "" {
		desc += " · v" + p.dev.Version
	}
	desc += " · " + p.dev.Addr
	return row{kind: rowPeer, title: p.dev.Name, desc: desc, peerAddr: p.dev.Addr}
}

// deviceTypeLabel maps a client id to a friendly device type.
func deviceTypeLabel(client string) string {
	switch client {
	case "tui":
		return i18n.T("Terminal")
	case "darwin", "macos":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux", "gnome", "kde":
		return "Linux"
	case "":
		return "Purple Disc"
	default:
		return client
	}
}
