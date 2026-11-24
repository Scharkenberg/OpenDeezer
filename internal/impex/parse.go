package impex

import (
	"encoding/csv"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
)

// Row is one imported playlist line, before resolution against Deezer.
type Row struct {
	Title  string
	Artist string
	Album  string
	ISRC   string
	ID     string // Deezer track id, when the source carries one (an M3U of deezer.com URLs)
}

// ParseCSV reads a playlist from r: a CSV with an Exportify-style header (any column
// order, common aliases), or plain "Artist - Title" lines ('#' and blank lines
// skipped). Rows without a title are dropped; no rows is an error.
func ParseCSV(r io.Reader) ([]Row, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if rows, ok := parseHeaderedCSV(data); ok {
		return rows, nil
	}
	rows := parsePlainLines(data)
	if len(rows) == 0 {
		return nil, fmt.Errorf("no tracks found in input")
	}
	return rows, nil
}

// header aliases, all matched lowercase/trimmed.
var (
	titleCols  = map[string]bool{"track name": true, "title": true, "name": true, "track": true, "song": true}
	artistCols = map[string]bool{"artist name(s)": true, "artist name": true, "artist": true, "artists": true, "artist(s)": true}
	albumCols  = map[string]bool{"album name": true, "album": true}
	isrcCols   = map[string]bool{"isrc": true}
)

// parseHeaderedCSV returns (rows, true) when data is a CSV whose first record
// contains a recognizable title column; (nil, false) otherwise so the caller
// can fall back to plain-text parsing.
func parseHeaderedCSV(data []byte) ([]Row, bool) {
	cr := csv.NewReader(strings.NewReader(string(data)))
	cr.FieldsPerRecord = -1 // tolerate ragged rows
	cr.LazyQuotes = true

	header, err := cr.Read()
	if err != nil {
		return nil, false
	}
	titleIdx, artistIdx, albumIdx, isrcIdx := -1, -1, -1, -1
	for i, h := range header {
		key := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\uFEFF")))
		switch {
		case titleCols[key] && titleIdx < 0:
			titleIdx = i
		case artistCols[key] && artistIdx < 0:
			artistIdx = i
		case albumCols[key] && albumIdx < 0:
			albumIdx = i
		case isrcCols[key] && isrcIdx < 0:
			isrcIdx = i
		}
	}
	if titleIdx < 0 {
		return nil, false
	}
	at := func(rec []string, i int) string {
		if i < 0 || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}
	var rows []Row
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue // skip a malformed record rather than losing the rest
		}
		row := Row{
			Title:  at(rec, titleIdx),
			Artist: at(rec, artistIdx),
			Album:  at(rec, albumIdx),
			ISRC:   at(rec, isrcIdx),
		}
		if row.Title == "" {
			continue
		}
		rows = append(rows, row)
	}
	return rows, true
}

// parsePlainLines parses "Artist - Title" lines and M3U: "#EXTINF:<secs>,<artist> -
// <title>" names the entry and the location line after it is skipped unless it is a
// Deezer track URL, whose id is used. Other '#' lines are skipped.
func parsePlainLines(data []byte) []Row {
	var rows []Row
	var info *Row // the last #EXTINF's entry, waiting for its location line
	add := func(r Row) {
		if r.Title != "" || r.ID != "" {
			rows = append(rows, r)
		}
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if rest, ok := strings.CutPrefix(line, "#EXTINF:"); ok {
			_, name, _ := strings.Cut(rest, ",")
			r := rowFromText(name)
			info = &r
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		id := deezerTrackID(line)
		switch {
		case info != nil:
			info.ID = id
			add(*info)
			info = nil
		case id != "":
			add(Row{ID: id})
		case !isLocation(line):
			add(rowFromText(line))
		}
	}
	return rows
}

// rowFromText reads "Artist - Title"; a line without " - " is a title.
func rowFromText(s string) Row {
	artist, title, found := strings.Cut(strings.TrimSpace(s), " - ")
	if !found {
		return Row{Title: artist}
	}
	artist, title = strings.TrimSpace(artist), strings.TrimSpace(title)
	if title == "" {
		return Row{Title: artist}
	}
	return Row{Title: title, Artist: artist}
}

// deezerTrackID is the id of a deezer.com/…/track/<id> URL, else "".
func deezerTrackID(line string) string {
	l := strings.ToLower(line)
	if !strings.Contains(l, "deezer.com/") || !strings.Contains(l, "/track/") {
		return ""
	}
	return deezer.TrackIDOf(line)
}

// isLocation reports an M3U location: a URL or an audio file path.
func isLocation(line string) bool {
	if strings.Contains(line, "://") {
		return true
	}
	switch strings.ToLower(filepath.Ext(line)) {
	case ".mp3", ".flac", ".m4a", ".aac", ".ogg", ".opus", ".wav", ".wma":
		return true
	}
	return false
}
