// Package history keeps a local listening history in <config-dir>/history.jsonl (one
// JSON object per line), never sent anywhere. The app and the TUI both write to it;
// Record only appends, and a store reads the file again when the other wrote.
package history

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/config"
)

// Kind values for Entry.Kind. A song carries "" (also what lines without a kind decode
// to) or KindTrack; KindEpisode marks a podcast episode, replayed by the episode
// player.
const (
	KindTrack   = "track"
	KindEpisode = "episode"
)

// Entry is one played song or podcast episode from the local history. Kind "" or
// KindTrack is a song, KindEpisode an episode.
type Entry struct {
	TrackID           string `json:"trackId"`
	Title             string `json:"title"`
	Artist            string `json:"artist"`
	Album             string `json:"album,omitempty"`
	Kind              string `json:"kind,omitempty"`    // "" / "track" = song, "episode" = podcast
	StartedAt         int64  `json:"startedAt"`         // unix seconds
	DurationPlayedSec int64  `json:"durationPlayedSec"` // seconds actually listened
	ArtworkURL        string `json:"artworkUrl,omitempty"`
	DurationMS        int64  `json:"durationMs,omitempty"` // the track's length
}

// Deezer's CDN serves artwork at
// https://<host>.dzcdn.net/images/<kind>/<hash>/250x250-000000-80-0-0.jpg, kind
// being "cover" for albums, "talk" for podcasts and so on.
const (
	cdnHost  = "e-cdns-images"
	cdnPath  = ".dzcdn.net/images/"
	cdnImage = "/250x250-000000-80-0-0.jpg"
)

// line is an Entry as stored: a CDN artwork URL is stored as Cover ("<kind>/<hash>")
// and CDN (the host, omitted for cdnHost) with ArtworkURL empty; other URLs stay in
// ArtworkURL.
type line struct {
	Entry
	Cover string `json:"cover,omitempty"`
	CDN   string `json:"cdn,omitempty"`
}

// toLine moves a CDN artwork URL into Cover and CDN, copied so the URL itself
// is not kept alive.
func toLine(e Entry) line {
	rest, ok := strings.CutPrefix(e.ArtworkURL, "https://")
	host, rest, ok2 := strings.Cut(rest, cdnPath)
	cover, ok3 := strings.CutSuffix(rest, cdnImage)
	if !ok || !ok2 || !ok3 {
		return line{Entry: e}
	}
	if host == cdnHost {
		host = ""
	}
	e.ArtworkURL = ""
	return line{Entry: e, Cover: strings.Clone(cover), CDN: strings.Clone(host)}
}

// entry returns the Entry with its artwork URL built again.
func (l line) entry() Entry {
	if l.Cover != "" {
		host := l.CDN
		if host == "" {
			host = cdnHost
		}
		l.ArtworkURL = "https://" + host + cdnPath + l.Cover + cdnImage
	}
	return l.Entry
}

// isEpisode reports whether this entry is a podcast episode rather than a song.
// Music aggregates (TopTracks/TopArtists) exclude episodes so a binge-listened
// podcast can't masquerade as a top "track" or "artist".
func (e Entry) isEpisode() bool { return e.Kind == KindEpisode }

// TrackStat aggregates plays of one track.
type TrackStat struct {
	TrackID  string
	Title    string
	Artist   string
	Plays    int
	TotalSec int64
}

// ArtistStat aggregates plays of one artist.
type ArtistStat struct {
	Artist   string
	Plays    int
	TotalSec int64
	TrackID  string // the artist's latest played track: the way to their id and picture
}

// DefaultCap is how many entries the file keeps: past DefaultCap plus 10 % it is
// rotated back to the newest DefaultCap.
const DefaultCap = 50000

// recentMax is how many of the newest entries a store keeps in memory for
// Recent; longer queries and the stats read the file.
const recentMax = 500

// Store is a JSONL-backed listening history.
type Store struct {
	path string
	cap  int

	mu     sync.RWMutex
	lines  int    // entries in the file; -1 until the first Record counts them
	loaded bool   // recent holds the file's newest entries
	recent []line // the newest recentMax entries, oldest -> newest
	// seen is the file's size as recent knows it: another program's append makes the
	// file larger, and the next Recent reads it again.
	seen int64
}

// New returns a store backed by the given JSONL file. The file (and its
// directory) is created on the first Record.
func New(path string) *Store {
	return &Store{path: path, cap: DefaultCap, lines: -1}
}

// Default returns the store at <config-dir>/history.jsonl (the same config
// dir resolution the rest of the app uses).
func Default() (*Store, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	return New(filepath.Join(dir, "history.jsonl")), nil
}

// Path returns the backing file path.
func (s *Store) Path() string { return s.path }

// Record appends one entry (single write and fsync) without reading the history. Past
// the size cap the file is rewritten atomically with the newest cap entries.
func (s *Store) Record(e Entry) error {
	if e.StartedAt == 0 {
		e.StartedAt = time.Now().Unix()
	}
	l := toLine(e)
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lines < 0 {
		n, err := s.countLinesLocked()
		if err != nil {
			return err
		}
		s.lines = n
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	// A line torn by a crash gets its newline first, so this entry is not lost with it.
	data := append(b, '\n')
	if fi, err := f.Stat(); err == nil && fi.Size() > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, fi.Size()-1); err == nil && last[0] != '\n' {
			data = append([]byte{'\n'}, data...)
		}
	}
	// One Write call for the whole line so concurrent external readers never
	// see a torn record; Sync so a crash right after Record can't lose it.
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	s.lines++
	if s.loaded {
		s.seen += int64(len(data))
		if len(s.recent) == recentMax {
			s.recent = append(s.recent[:0], s.recent[1:]...)
		}
		s.recent = append(s.recent, l)
	}
	// Rotate with slack, so a full history is not rewritten on every play. The
	// TUI and the GUI share the file: rotation reads it again, so what the
	// other appended meanwhile survives.
	if s.lines > s.cap+s.cap/10 {
		s.loaded = false // the file is rewritten: the next Recent reads it again
		return s.rotateLocked()
	}
	return nil
}

// countLinesLocked counts the entries in the file (0 when it does not exist).
func (s *Store) countLinesLocked() (int, error) {
	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	n := 0
	buf := make([]byte, 64*1024)
	for {
		k, err := f.Read(buf)
		n += bytes.Count(buf[:k], []byte{'\n'})
		if err == io.EOF {
			return n, nil
		}
		if err != nil {
			return 0, err
		}
	}
}

// rotateLocked atomically rewrites the file with its newest cap entries.
// Caller holds the write lock.
func (s *Store) rotateLocked() error {
	var keep []line
	if err := s.eachLocked(func(l line) { keep = append(keep, l) }); err != nil {
		return err
	}
	if len(keep) > s.cap {
		keep = keep[len(keep)-s.cap:]
	}
	s.loaded = false
	s.recent = nil
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	w := bufio.NewWriter(tmp)
	for _, l := range keep {
		b, err := json.Marshal(l)
		if err != nil {
			_ = tmp.Close()
			return err
		}
		if _, err := w.Write(append(b, '\n')); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return err
	}
	s.lines = len(keep)
	return nil
}

// eachLocked calls fn for every entry, oldest first, with a full artwork URL split into
// Cover and CDN. Unparseable lines are skipped; a missing file has no entries. Caller
// holds the lock.
func (s *Store) eachLocked(fn func(line)) error {
	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		b := sc.Bytes()
		if len(b) == 0 {
			continue
		}
		var l line
		if json.Unmarshal(b, &l) != nil {
			continue // skip corrupt line
		}
		if l.Cover == "" {
			l = toLine(l.Entry)
		}
		fn(l)
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read %s: %w", s.path, err)
	}
	return nil
}

// Recent returns the newest n entries, newest first. n <= 0 returns all.
// Up to recentMax entries come from memory, read from the file again when its size
// differs from what this store wrote or read (the other program appended).
func (s *Store) Recent(n int) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var from []line
	if n > 0 && n <= recentMax {
		fi, statErr := os.Stat(s.path)
		if s.loaded && (statErr != nil || fi.Size() != s.seen) {
			s.loaded = false // another program wrote to the file
		}
		if !s.loaded {
			if statErr == nil {
				s.seen = fi.Size()
			}
			var win []line // the newest entries read so far, at most 2*recentMax
			err := s.eachLocked(func(l line) {
				if len(win) == 2*recentMax {
					win = append(win[:0], win[recentMax:]...)
				}
				win = append(win, l)
			})
			if err != nil {
				return nil, err
			}
			s.recent = slices.Clone(win[max(0, len(win)-recentMax):])
			s.loaded = true
		}
		from = s.recent
	} else if err := s.eachLocked(func(l line) { from = append(from, l) }); err != nil {
		return nil, err
	}
	total := len(from)
	if n <= 0 || n > total {
		n = total
	}
	out := make([]Entry, 0, n)
	for i := total - 1; i >= total-n; i-- {
		out = append(out, from[i].entry())
	}
	return out, nil
}

// TopTracks returns the n most-played tracks since the given time (zero = all), by
// play count, then listened time, then title. Podcast episodes are excluded.
func (s *Store) TopTracks(since time.Time, n int) ([]TrackStat, error) {
	cut := sinceUnix(since)
	byKey := map[string]*TrackStat{}
	s.mu.RLock()
	err := s.eachLocked(func(e line) {
		if e.StartedAt < cut || e.isEpisode() {
			return
		}
		key := e.TrackID
		if key == "" {
			key = e.Artist + "\x00" + e.Title // tolerate entries without an id
		}
		st, ok := byKey[key]
		if !ok {
			st = &TrackStat{TrackID: e.TrackID, Title: e.Title, Artist: e.Artist}
			byKey[key] = st
		}
		st.Plays++
		st.TotalSec += e.DurationPlayedSec
	})
	s.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	out := make([]TrackStat, 0, len(byKey))
	for _, st := range byKey {
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Plays != out[j].Plays {
			return out[i].Plays > out[j].Plays
		}
		if out[i].TotalSec != out[j].TotalSec {
			return out[i].TotalSec > out[j].TotalSec
		}
		return out[i].Title < out[j].Title
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out, nil
}

// TopArtists returns the n most-played artists since the given time (zero = all), by
// play count, then listened time, then name. Podcast episodes are excluded.
func (s *Store) TopArtists(since time.Time, n int) ([]ArtistStat, error) {
	cut := sinceUnix(since)
	byArtist := map[string]*ArtistStat{}
	s.mu.RLock()
	err := s.eachLocked(func(e line) {
		if e.StartedAt < cut || e.Artist == "" || e.isEpisode() {
			return
		}
		st, ok := byArtist[e.Artist]
		if !ok {
			st = &ArtistStat{Artist: e.Artist}
			byArtist[e.Artist] = st
		}
		st.Plays++
		st.TotalSec += e.DurationPlayedSec
		if e.TrackID != "" {
			st.TrackID = e.TrackID
		}
	})
	s.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	out := make([]ArtistStat, 0, len(byArtist))
	for _, st := range byArtist {
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Plays != out[j].Plays {
			return out[i].Plays > out[j].Plays
		}
		if out[i].TotalSec != out[j].TotalSec {
			return out[i].TotalSec > out[j].TotalSec
		}
		return out[i].Artist < out[j].Artist
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out, nil
}

// TotalListenedSec sums the seconds listened since the given time (zero = all), songs
// and podcast episodes alike.
func (s *Store) TotalListenedSec(since time.Time) (int64, error) {
	cut := sinceUnix(since)
	var total int64
	s.mu.RLock()
	defer s.mu.RUnlock()
	err := s.eachLocked(func(e line) {
		if e.StartedAt >= cut {
			total += e.DurationPlayedSec
		}
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}

// sinceUnix maps a zero time to "everything" and anything else to its unix
// seconds cutoff.
func sinceUnix(since time.Time) int64 {
	if since.IsZero() {
		return 0
	}
	return since.Unix()
}
