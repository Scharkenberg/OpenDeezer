package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/audio"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/config"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/mediacache"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/mediacache/mctest"
)

// fakeDeezer answers the gateway, the REST API, get_url, the CDN and the
// picture host on loopback. Track ids are numbers; a track's body is
// bodyOf(id).
type fakeDeezer struct {
	srv *httptest.Server

	mu        sync.Mutex
	playlists map[string][]string
	albums    map[string][]string
	gone      map[string]bool   // tracks Deezer does not serve
	fallback  map[string]string // refused track -> the version Deezer serves instead
	history   []map[string]any  // user.getSongsHistory, newest first
	logs      []string          // "method body" of each listen log call

	calls    atomic.Int32 // requests through deezer.Transport
	menus    atomic.Int32 // deezer.userMenu calls (episode bookmarks)
	conns    atomic.Int32 // connections accepted
	streams  atomic.Int32 // CDN body requests
	inFlight atomic.Int32
	maxIn    atomic.Int32
	down     atomic.Bool  // every request fails as with no network
	listErr  atomic.Bool  // playlist.getSongs answers a gateway error
	unlisted atomic.Int32 // tracks a listing declares in its total but leaves out
	delay    time.Duration
	onTrack  func() // runs as song.getData is answered, under no lock; nil for none
}

func bodyOf(id string) []byte { return []byte(strings.Repeat("body of "+id+";", 300)) }

func gwTrack(id string) map[string]any {
	return map[string]any{"SNG_ID": id, "SNG_TITLE": "Song " + id, "DURATION": "200", "ART_ID": "5",
		"ART_NAME": "Artist", "ALB_TITLE": "Album", "ALB_PICTURE": "md5" + id}
}

func newFakeDeezer(t *testing.T) *fakeDeezer {
	t.Helper()
	fd := &fakeDeezer{playlists: map[string][]string{}, albums: map[string][]string{}, gone: map[string]bool{}, fallback: map[string]string{}}
	write := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	fd.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasPrefix(p, "/stream/"):
			fd.streams.Add(1)
			n := fd.inFlight.Add(1)
			for m := fd.maxIn.Load(); n > m && !fd.maxIn.CompareAndSwap(m, n); m = fd.maxIn.Load() {
			}
			time.Sleep(fd.delay)
			fd.inFlight.Add(-1)
			b := bodyOf(strings.TrimPrefix(p, "/stream/"))
			w.Header().Set("Content-Length", strconv.Itoa(len(b)))
			_, _ = w.Write(b)
		case p == "/": // the logged-in web player's page names its script
			_, _ = w.Write([]byte(`<script src="https://cdn-files.dzcdn.net/cache/js/app-web.cafe.js"></script>`))
		case p == "/cache/js/app-web.cafe.js":
			_, _ = w.Write([]byte(`env:"production",releaseNumeric:"10020261124120000",bot:1`))
		case strings.HasPrefix(p, "/images/"):
			_, _ = w.Write([]byte("picture " + p))
		case p == "/v1/get_url":
			var body struct {
				Tokens []string `json:"track_tokens"`
				Media  []struct {
					Type string `json:"type"`
				} `json:"media"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			id := strings.TrimPrefix(body.Tokens[0], "tok")
			fd.mu.Lock()
			_, refused := fd.fallback[id]
			fd.mu.Unlock()
			if body.Media[0].Type == "PREVIEW" {
				write(w, map[string]any{"data": []any{map[string]any{"media": []any{map[string]any{
					"format": "MP3_128", "sources": []any{map[string]any{"url": fd.srv.URL + "/stream/preview" + id}}}}}}})
				return
			}
			if refused {
				write(w, map[string]any{"data": []any{map[string]any{"errors": []any{map[string]any{
					"code": 2002, "message": "Track token has no sufficient rights on requested media"}}}}})
				return
			}
			write(w, map[string]any{"data": []any{map[string]any{"media": []any{map[string]any{
				"format": "MP3_128", "sources": []any{map[string]any{"url": fd.srv.URL + "/stream/" + id}}}}}}})
		case strings.HasPrefix(p, "/album/"):
			id := strings.TrimSuffix(strings.TrimPrefix(p, "/album/"), "/tracks")
			fd.mu.Lock()
			ids := fd.albums[id]
			fd.mu.Unlock()
			var data []any
			for _, tid := range ids {
				data = append(data, map[string]any{"id": tid, "title": "Song " + tid, "duration": 200,
					"artist": map[string]any{"id": 5, "name": "Artist"},
					"album":  map[string]any{"title": "Album " + id, "cover_medium": "https://e-cdns-images.dzcdn.net/images/cover/album" + id + "/250x250.jpg"}})
			}
			write(w, map[string]any{"data": data, "total": len(ids) + int(fd.unlisted.Load())})
		default:
			raw, _ := io.ReadAll(r.Body)
			var body map[string]json.RawMessage
			_ = json.Unmarshal(raw, &body)
			str := func(k string) string { var s string; _ = json.Unmarshal(body[k], &s); return s }
			switch m := r.URL.Query().Get("method"); m {
			case "log.listen", "log.batch_listen":
				fd.mu.Lock()
				fd.logs = append(fd.logs, m+" "+string(raw))
				fd.mu.Unlock()
				write(w, map[string]any{"error": map[string]any{}, "results": false})
			case "deezer.getUserData":
				user := strings.TrimPrefix(r.Header.Get("Cookie"), "arl=")
				write(w, map[string]any{"error": map[string]any{}, "results": map[string]any{"checkForm": "tok",
					"USER":   map[string]any{"USER_ID": user, "BLOG_NAME": "u", "OPTIONS": map[string]any{"license_token": "lic", "web_hq": user == "1003"}},
					"OFFERS": []any{map[string]any{"title": "Premium"}}, "GAIN": map[string]any{"TARGET": "-15", "ADS": "-13"}}})
			case "song.getData":
				var id json.Number
				_ = json.Unmarshal(body["sng_id"], &id)
				fd.mu.Lock()
				gone := fd.gone[id.String()]
				fb := fd.fallback[id.String()]
				onTrack := fd.onTrack
				fd.mu.Unlock()
				if onTrack != nil {
					onTrack()
				}
				if gone {
					write(w, map[string]any{"error": map[string]any{"DATA_ERROR": "song not found"}, "results": map[string]any{}})
					return
				}
				tr := gwTrack(id.String())
				tr["TRACK_TOKEN"], tr["GAIN"] = "tok"+id.String(), "-2"
				if fb != "" {
					tr["FALLBACK"] = map[string]any{"SNG_ID": fb, "TRACK_TOKEN": "tok" + fb}
				}
				write(w, map[string]any{"error": map[string]any{}, "results": tr})
			case "playlist.getSongs":
				if fd.listErr.Load() {
					write(w, map[string]any{"error": map[string]any{"DATA_ERROR": "playlist not found"}, "results": map[string]any{}})
					return
				}
				var start int
				_ = json.Unmarshal(body["start"], &start)
				fd.mu.Lock()
				ids := fd.playlists[str("playlist_id")]
				fd.mu.Unlock()
				var data []any
				for i, tid := range ids {
					if i >= start {
						data = append(data, gwTrack(tid))
					}
				}
				write(w, map[string]any{"error": map[string]any{}, "results": map[string]any{"data": data, "total": len(ids) + int(fd.unlisted.Load())}})
			case "deezer.userMenu":
				fd.menus.Add(1)
				write(w, map[string]any{"error": map[string]any{}, "results": map[string]any{"EPISODE_BOOKMARKS": map[string]any{
					"data": []any{map[string]any{"EPISODE_ID": "77", "OFFSET": "120", "IS_HEARD": "0"}}}}})
			case "user.getSongsHistory":
				fd.mu.Lock()
				data := fd.history
				fd.mu.Unlock()
				write(w, map[string]any{"error": map[string]any{}, "results": map[string]any{"data": data}})
			case "playlist.getData":
				write(w, map[string]any{"error": map[string]any{}, "results": map[string]any{"TITLE": "Playlist " + str("playlist_id"),
					"PLAYLIST_PICTURE": "pl" + str("playlist_id"), "PICTURE_TYPE": "playlist"}})
			default:
				http.NotFound(w, r)
			}
		}
	}))
	fd.srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			fd.conns.Add(1)
		}
	}
	fd.srv.Start()
	t.Cleanup(fd.srv.Close)
	return fd
}

func (fd *fakeDeezer) setPlaylist(id string, tracks ...string) {
	fd.mu.Lock()
	fd.playlists[id] = tracks
	fd.mu.Unlock()
}

// offlineEngine runs the engine against a fake Deezer, signed in as user,
// with a player that has no output, a fresh profile and no media.json (a
// cache size of 0).
func offlineEngine(t *testing.T, user string) *fakeDeezer {
	t.Helper()
	cacheProfile(t)
	fd := newFakeDeezer(t)
	target, _ := url.Parse(fd.srv.URL)
	orig := deezer.Transport()
	deezer.SetTransport(rtFunc(func(r *http.Request) (*http.Response, error) {
		fd.calls.Add(1)
		if fd.down.Load() {
			return nil, &net.DNSError{Err: "no such host", Name: r.URL.Host}
		}
		r = r.Clone(r.Context())
		r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
		return orig.RoundTrip(r)
	}))
	t.Cleanup(func() {
		waitJobsDone(t)
		deezer.SetTransport(orig)
	})
	prevLearn := learnRelease
	learnRelease = func(*deezer.Client) {} // no deezer.com here
	t.Cleanup(func() { learnRelease = prevLearn })
	mu.Lock()
	prevClient, prevPlayer := client, player
	player = &audio.Player{}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		client, player = prevClient, prevPlayer
		mu.Unlock()
	})
	signIn(t, user)
	return fd
}

// signIn makes user the signed-in account, as a successful Init does.
func signIn(t *testing.T, user string) {
	t.Helper()
	c := deezer.New(user)
	if err := c.Login(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	client = c
	mu.Unlock()
	rememberUser(user)
}

// goOffline drops the client and turns offline mode on, as an Init without a
// network does.
func goOffline(fd *fakeDeezer) {
	mu.Lock()
	client = nil
	mu.Unlock()
	offlineOn.Store(true)
	fd.down.Store(true)
}

func pinAndWait(t *testing.T, kind, id string) {
	t.Helper()
	if err := PinOffline(kind, id); err != nil {
		t.Fatalf("PinOffline(%s, %s): %v", kind, id, err)
	}
	waitJobsDone(t)
}

func collection(kind, id string) (OfflineCollection, bool) {
	for _, c := range OfflineCollections() {
		if c.Kind == kind && c.ID == id {
			return c, true
		}
	}
	return OfflineCollection{}, false
}

// Pinning a playlist records it at once and fetches every track, 3 at a time,
// into the media cache with its cover; the collection lists its tracks in
// order with local pictures.
func TestPinPlaylistFetchesTracks(t *testing.T) {
	fd := offlineEngine(t, "1001")
	fd.delay = 30 * time.Millisecond
	fd.setPlaylist("9", "101", "102", "103", "104", "105")
	pinAndWait(t, "playlist", "9")

	c, ok := collection("playlist", "9")
	if !ok || c.Title != "Playlist 9" || c.TrackCount != 5 || c.Fetched != 5 {
		t.Fatalf("collection %+v %v", c, ok)
	}
	if _, err := os.Stat(c.Cover); err != nil {
		t.Fatalf("cover %q is not a stored file: %v", c.Cover, err)
	}
	if n := fd.streams.Load(); n != 5 {
		t.Fatalf("%d stream requests, want 5", n)
	}
	if m := fd.maxIn.Load(); m > 3 {
		t.Fatalf("%d streams at once, want at most 3", m)
	}
	ts, err := OfflineTracks("playlist", "9")
	if err != nil || len(ts) != 5 || ts[0].ID != "101" || ts[4].ID != "105" || ts[0].Name != "Song 101" {
		t.Fatalf("tracks %+v %v", ts, err)
	}
	if _, err := os.Stat(ts[0].ArtworkURL); err != nil {
		t.Fatalf("track picture %q is not a stored file", ts[0].ArtworkURL)
	}
	if ids := OfflineIDs(); !slices.Equal(ids, []string{"101", "102", "103", "104", "105"}) {
		t.Fatalf("ids %v", ids)
	}
	// A pin keeps the format it was asked in, with its gain, as a played track does.
	if m, ok := caches.Current().GetMeta("101.MP3_128"); !ok || m.AskedFormat != "MP3_128" || m.GainDB != -2 || !m.Encrypted {
		t.Fatalf("meta %+v %v", m, ok)
	}
	if active, jobs := OfflineProgress(); active || len(jobs) != 0 {
		t.Fatalf("progress %v %+v after the fetch", active, jobs)
	}
}

// A track refused in this country plays the fallback version song.getData names: the
// pin stores that stream under the refused track's id with the fallback's id (whose key
// decrypts it), and it plays offline.
func TestRefusedTrackFetchesFallback(t *testing.T) {
	fd := offlineEngine(t, "1001")
	fd.fallback["101"] = "901"
	pinAndWait(t, "track", "101")
	if m, ok := caches.Current().GetMeta("101.MP3_128"); !ok || m.KeyID != "901" {
		t.Fatalf("meta %+v %v; want the fallback's key id", m, ok)
	}
	if f, _, ok := caches.Current().Get("101.MP3_128"); !ok {
		t.Fatal("the fallback's body is not stored")
	} else {
		got, _ := io.ReadAll(f)
		_ = f.Close()
		if !bytes.Equal(got, bodyOf("901")) {
			t.Fatalf("stored %q, want the fallback's body", got)
		}
	}
	goOffline(fd)
	plan, err := preparePlan(netClient(), "101")
	if err != nil || plan.TrackID != "101" || plan.KeyID != "901" || plan.Preview || plan.Track.Name != "Song 101" {
		t.Fatalf("plan %+v %v", plan, err)
	}
}

// With a cache size of 0 pinning still works: the cache opens for the pins,
// which are reported apart.
func TestPinWithCacheSizeZero(t *testing.T) {
	offlineEngine(t, "1001")
	if caches.Current() != nil {
		t.Fatal("a cache is open at a size of 0 before any pin")
	}
	pinAndWait(t, "track", "101")
	st := caches.Stats()
	if caches.Current() == nil || st.MaxBytes != 0 || st.PinnedBytes < int64(len(bodyOf("101"))) || st.Bytes != st.PinnedBytes {
		t.Fatalf("stats %+v", st)
	}
	if !slices.Equal(OfflineIDs(), []string{"101"}) {
		t.Fatalf("ids %v", OfflineIDs())
	}
}

// A pinned track plays with no client and a transport that fails every
// request: the plan comes from the pin, with its metadata, and nothing is
// requested (the preload path, which runs on a player without an output).
func TestPinnedTrackPlaysWithNoClient(t *testing.T) {
	fd := offlineEngine(t, "1001")
	pinAndWait(t, "track", "101")
	goOffline(fd)
	calls, streams := fd.calls.Load(), fd.streams.Load()

	plan, err := preparePlan(netClient(), "101")
	if err != nil || plan.CDNURL != "" || plan.Format != "MP3_128" || !plan.Encrypted || plan.GainDB != -2 || plan.Track.Name != "Song 101" {
		t.Fatalf("plan %+v %v", plan, err)
	}
	if f, _, ok := caches.Current().Get("101.MP3_128"); !ok {
		t.Fatal("the pinned body is not readable")
	} else {
		_ = f.Close()
	}
	preloads.Clear()
	Player().SetGapless(true)
	Preload("101", 200000)
	preloads.Resolve()
	if armed, ok := preloads.Take(); !ok || armed.ID != "101" || armed.Name != "Song 101" {
		t.Fatalf("armed %+v %v", armed, ok)
	}
	if fd.calls.Load() != calls || fd.streams.Load() != streams {
		t.Fatalf("%d requests and %d streams with no network, want none", fd.calls.Load()-calls, fd.streams.Load()-streams)
	}
}

// In offline mode a track neither pinned nor cached fails at once, with the
// reason in LastError, and nothing is requested.
func TestOfflineModeUnpinnedPlayFails(t *testing.T) {
	fd := offlineEngine(t, "1001")
	goOffline(fd)
	calls := fd.calls.Load()
	if Play("555", 0) != PlayFailed {
		t.Fatal("an unpinned track played offline")
	}
	if e := Player().LastError(); !strings.Contains(e, "not available offline") {
		t.Fatalf("LastError %q", e)
	}
	if fd.calls.Load() != calls {
		t.Fatal("an offline play made a request")
	}
}

// Offline mode set by the host while signed in requests nothing either: a
// pinned track plans from its pin, an unpinned one fails at once.
func TestOfflineModeWithClientRequestsNothing(t *testing.T) {
	fd := offlineEngine(t, "1001")
	pinAndWait(t, "track", "101")
	SetOfflineMode(true)
	calls, streams := fd.calls.Load(), fd.streams.Load()
	if Play("555", 0) != PlayFailed {
		t.Fatal("an unpinned track played in offline mode")
	}
	if plan, err := preparePlan(netClient(), "101"); err != nil || plan.Refresh != nil {
		t.Fatalf("pinned plan %+v %v", plan, err)
	}
	if fd.calls.Load() != calls || fd.streams.Load() != streams {
		t.Fatal("offline mode made a request")
	}
}

// Offline mode refuses what would reach Deezer, with the reason.
func TestOfflineModeRefusesRequests(t *testing.T) {
	fd := offlineEngine(t, "1001")
	SetOfflineMode(true)
	calls := fd.calls.Load()
	if _, err := Online(); !errors.Is(err, ErrOfflineMode) {
		t.Fatalf("Online = %v", err)
	}
	if _, err := PlaylistTracks("9"); !errors.Is(err, ErrOfflineMode) {
		t.Fatalf("PlaylistTracks = %v", err)
	}
	if _, err := DownloadTrack("101", ""); !errors.Is(err, ErrOfflineMode) {
		t.Fatalf("DownloadTrack = %v", err)
	}
	if err := engineQueueAdd("101", false); !errors.Is(err, ErrOfflineMode) {
		t.Fatalf("queue add = %v", err)
	}
	if fd.calls.Load() != calls {
		t.Fatal("a request went out in offline mode")
	}
}

// Clear(includePinned=0) keeps the pinned tracks; with pins it
// removes them all, for every account.
func TestClearCacheIncludePinned(t *testing.T) {
	offlineEngine(t, "1001")
	if err := caches.SetMB(1); err != nil {
		t.Fatal(err)
	}
	pinAndWait(t, "track", "101")
	mctest.PutBody(t, caches.Current(), "777.MP3_128")
	pinned := caches.Stats().PinnedBytes
	if err := ClearCache(false); err != nil {
		t.Fatal(err)
	}
	if st := caches.Stats(); st.Entries != 2 || st.PinnedBytes != pinned || !slices.Equal(OfflineIDs(), []string{"101"}) {
		t.Fatalf("after a clear of the cache: %+v, ids %v", st, OfflineIDs())
	}
	if err := ClearCache(true); err != nil {
		t.Fatal(err)
	}
	if st := caches.Stats(); st.Entries != 0 || st.PinnedBytes != 0 || len(OfflineCollections()) != 0 {
		t.Fatalf("after a clear with the pins: %+v, collections %v", st, OfflineCollections())
	}
}

// Pins survive a deleted index.json, also with a maximum age the files
// are older than: offline.json gives the rebuilt entries their pins and
// stream meta back before the age applies, and the track plays offline.
func TestPinsSurviveDeletedIndex(t *testing.T) {
	fd := offlineEngine(t, "1001")
	pinAndWait(t, "track", "101")
	dir, _ := config.LoadMedia().CacheStoreDir()
	caches.Current().Close()
	if err := os.Remove(filepath.Join(dir, "index.json")); err != nil {
		t.Fatal(err)
	}
	m := config.LoadMedia()
	m.MediaCacheMaxAgeDays = 7
	if err := config.SaveMedia(m); err != nil {
		t.Fatal(err)
	}
	then := time.Now().Add(-30 * 24 * time.Hour)
	for _, f := range dataFiles(t, dir) {
		if err := os.Chtimes(filepath.Join(dir, f), then, then); err != nil {
			t.Fatal(err)
		}
	}
	caches = &mediacache.Host{Attach: attachCache, Pinned: anyPinned}
	if err := caches.Open(); err != nil {
		t.Fatal(err)
	}
	if st := caches.Stats(); st.PinnedBytes < int64(len(bodyOf("101"))) || !caches.Current().Pinned("101.MP3_128") {
		t.Fatalf("stats after the index was lost: %+v", st)
	}
	goOffline(fd)
	if _, err := preparePlan(nil, "101"); err != nil {
		t.Fatalf("offline plan after the index was lost: %v", err)
	}
	// The limit of 0 evicts nothing pinned.
	if err := caches.SetMB(0); err != nil || caches.Stats().PinnedBytes == 0 {
		t.Fatalf("SetMB(0): %v, stats %+v", err, caches.Stats())
	}
}

// restartOffline drops the offline store and the cache in memory and opens
// them again from disk, as a new process does.
func restartOffline(t *testing.T) {
	t.Helper()
	waitJobsDone(t)
	jobMu.Lock()
	jobs = nil
	jobMu.Unlock()
	if c := caches.Current(); c != nil {
		c.Close()
	}
	offMu.Lock()
	offLoaded, off = false, offlineData{}
	offMu.Unlock()
	caches = &mediacache.Host{Attach: attachCache, Pinned: anyPinned}
	if err := caches.Open(); err != nil {
		t.Fatal(err)
	}
}

// A process killed right after a fetched body is adopted keeps the track
// pinned: offline.json already holds its stream meta, so the next start pins
// the entry again and a size of 0 keeps it.
func TestPinSurvivesKillBetweenAdoptAndSave(t *testing.T) {
	offlineEngine(t, "1001")
	dir, _ := config.Dir()
	var onDisk []byte
	fetchStream = func(ctx context.Context, plan *deezer.StreamPlan, c audio.StreamCache) error {
		err := audio.Fetch(ctx, plan, c)
		onDisk, _ = os.ReadFile(filepath.Join(dir, offlineFileName)) // what a kill leaves at this point
		return err
	}
	pinAndWait(t, "track", "101")
	fetchStream = audio.Fetch
	if err := os.WriteFile(filepath.Join(dir, offlineFileName), onDisk, 0o600); err != nil {
		t.Fatal(err)
	}
	restartOffline(t)
	if mc := caches.Current(); mc == nil || !mc.Pinned("101.MP3_128") || !slices.Equal(OfflineIDs(), []string{"101"}) {
		t.Fatalf("after the restart: ids %v", OfflineIDs())
	}
}

// countSaves counts the writes of offline.json after the call; fail makes the
// writes after the first n fail.
func countSaves(t *testing.T, failAfter int32) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	orig := writeOffline
	writeOffline = func(b []byte) error {
		if n.Add(1) > failAfter {
			return errors.New("there is not enough space on the disk")
		}
		return orig(b)
	}
	t.Cleanup(func() { writeOffline = orig })
	return &n
}

// A full clear waits for the fetches it stops: one whose track request was under way
// when the clear began records no track after the wipe.
func TestClearCacheWaitsForStoppedFetches(t *testing.T) {
	fd := offlineEngine(t, "1001")
	fd.setPlaylist("9", "101")
	var once sync.Once
	entered, release := make(chan struct{}), make(chan struct{})
	fd.mu.Lock()
	fd.onTrack = func() {
		once.Do(func() { close(entered) })
		<-release
	}
	fd.mu.Unlock()
	pinned := make(chan error, 1)
	go func() { pinned <- PinOffline("playlist", "9") }()
	<-entered
	cleared := make(chan error, 1)
	go func() { cleared <- ClearCache(true) }()
	time.Sleep(50 * time.Millisecond)
	close(release)
	if err := <-pinned; err != nil {
		t.Fatal(err)
	}
	if err := <-cleared; err != nil {
		t.Fatal(err)
	}
	waitJobsDone(t)
	offMu.Lock()
	n := len(offlineLocked().Tracks)
	offMu.Unlock()
	if n != 0 {
		t.Fatalf("%d tracks recorded after a full clear, want 0", n)
	}
}

// A 50-track playlist pin writes offline.json twice: at the pin and at the
// job's end, not once per track.
func TestPlaylistPinSavesTwice(t *testing.T) {
	fd := offlineEngine(t, "1001")
	ids := make([]string, 50)
	for i := range ids {
		ids[i] = strconv.Itoa(101 + i)
	}
	fd.setPlaylist("9", ids...)
	saves := countSaves(t, 1000)
	pinAndWait(t, "playlist", "9")
	if c, _ := collection("playlist", "9"); c.Fetched != 50 {
		t.Fatalf("collection %+v", c)
	}
	if n := saves.Load(); n != 2 {
		t.Fatalf("%d saves of offline.json for a 50-track playlist pin, want 2", n)
	}
}

// A single-track pin killed right after its body was adopted keeps the
// track's title and picture: they are saved with the fetch's song.getData,
// before the body.
func TestSingleTrackKilledAfterAdoptKeepsTitle(t *testing.T) {
	offlineEngine(t, "1001")
	dir, _ := config.Dir()
	var onDisk []byte
	fetchStream = func(ctx context.Context, plan *deezer.StreamPlan, c audio.StreamCache) error {
		err := audio.Fetch(ctx, plan, c)
		onDisk, _ = os.ReadFile(filepath.Join(dir, offlineFileName)) // what a kill leaves at this point
		return err
	}
	pinAndWait(t, "track", "101")
	fetchStream = audio.Fetch
	if err := os.WriteFile(filepath.Join(dir, offlineFileName), onDisk, 0o600); err != nil {
		t.Fatal(err)
	}
	restartOffline(t)
	ts, err := OfflineTracks("track", "")
	if err != nil || len(ts) != 1 || ts[0].Name != "Song 101" || ts[0].ArtworkURL == "" {
		t.Fatalf("after the restart: %+v, %v", ts, err)
	}
	if !slices.Equal(OfflineIDs(), []string{"101"}) {
		t.Fatalf("ids %v after the restart", OfflineIDs())
	}
}

// A playlist pin killed after a track's body was adopted keeps that track at
// the next start: offline.json was not saved since the pin, and the track's
// fetched state and stream meta come from its pinned index entry.
func TestPlaylistKilledAfterAdoptKeepsTrack(t *testing.T) {
	fd := offlineEngine(t, "1001")
	fd.setPlaylist("9", "101")
	dir, _ := config.Dir()
	var onDisk []byte
	fetchStream = func(ctx context.Context, plan *deezer.StreamPlan, c audio.StreamCache) error {
		err := audio.Fetch(ctx, plan, c)
		onDisk, _ = os.ReadFile(filepath.Join(dir, offlineFileName))
		return err
	}
	pinAndWait(t, "playlist", "9")
	fetchStream = audio.Fetch
	if err := os.WriteFile(filepath.Join(dir, offlineFileName), onDisk, 0o600); err != nil {
		t.Fatal(err)
	}
	restartOffline(t)
	if c, _ := collection("playlist", "9"); c.Fetched != 1 || !slices.Equal(OfflineIDs(), []string{"101"}) {
		t.Fatalf("after the restart: %+v, ids %v", c, OfflineIDs())
	}
}

// A save of offline.json that fails stops the job with the reason, before the
// body is fetched.
func TestFailedSaveStopsFetch(t *testing.T) {
	fd := offlineEngine(t, "1001")
	countSaves(t, 1) // the pin's own save succeeds
	pinAndWait(t, "track", "101")
	active, js := OfflineProgress()
	if active || len(js) != 1 || !strings.Contains(js[0].Error, "not enough space") {
		t.Fatalf("progress %v %+v after a failed save", active, js)
	}
	if n := fd.streams.Load(); n != 0 {
		t.Fatalf("%d bodies fetched although the save failed", n)
	}
}

// A collection whose fetch stopped half way is fetched to the end at the next
// login.
func TestLoginResumesHalfFetchedCollection(t *testing.T) {
	fd := offlineEngine(t, "1001")
	fd.setPlaylist("9", "101", "102", "103", "104")
	fetchStream = func(ctx context.Context, plan *deezer.StreamPlan, c audio.StreamCache) error {
		if plan.TrackID == "103" || plan.TrackID == "104" {
			return errors.New("connection reset")
		}
		return audio.Fetch(ctx, plan, c)
	}
	pinAndWait(t, "playlist", "9")
	if c, _ := collection("playlist", "9"); c.Fetched != 2 {
		t.Fatalf("collection %+v before the restart", c)
	}
	fetchStream = audio.Fetch
	restartOffline(t)
	mu.Lock()
	client = nil
	mu.Unlock()
	t.Cleanup(Logout)
	if !Init("1001") {
		t.Fatal("login failed")
	}
	waitJobsDone(t)
	if c, _ := collection("playlist", "9"); c.Fetched != 4 {
		t.Fatalf("collection %+v after the login", c)
	}
}

// A track a pin finds in the cache is pinned at once, not when its job ends:
// a size of 0 set while the job still runs keeps it.
func TestCachedTrackPinnedWhenFound(t *testing.T) {
	fd := offlineEngine(t, "1001")
	if err := caches.SetMB(1); err != nil {
		t.Fatal(err)
	}
	mc := caches.Current()
	mctest.PutBody(t, mc, "101.MP3_128")
	if err := mc.PutMeta("101.MP3_128", mediacache.StreamMeta{Format: "MP3_128", AskedFormat: "MP3_128", Encrypted: true}); err != nil {
		t.Fatal(err)
	}
	fd.setPlaylist("9", "101", "102")
	release := make(chan struct{})
	fetchStream = func(ctx context.Context, plan *deezer.StreamPlan, c audio.StreamCache) error {
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		return audio.Fetch(ctx, plan, c)
	}
	if err := PinOffline("playlist", "9"); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if _, js := OfflineProgress(); len(js) == 1 && js[0].Done == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the cached track was not taken")
		}
	}
	if err := caches.SetMB(0); err != nil {
		t.Fatal(err)
	}
	pinned := mc.Pinned("101.MP3_128")
	close(release)
	waitJobsDone(t)
	if !pinned {
		t.Fatal("the cached track was unpinned while its job ran")
	}
	if ids := OfflineIDs(); !slices.Equal(ids, []string{"101", "102"}) {
		t.Fatalf("ids %v", ids)
	}
}

// Unpinning the last pin makes the entry evictable: at a size of 0 it
// goes, and the cache closes.
func TestUnpinMakesEntryEvictable(t *testing.T) {
	offlineEngine(t, "1001")
	pinAndWait(t, "track", "101")
	dir, _ := config.LoadMedia().CacheStoreDir()
	if err := UnpinOffline("track", "101"); err != nil {
		t.Fatal(err)
	}
	if caches.Current() != nil {
		t.Fatal("the cache stayed open at 0 with nothing pinned")
	}
	if f := dataFiles(t, dir); len(f) != 0 {
		t.Fatalf("files left after the last unpin: %v", f)
	}
}

// Unpinning a track also pinned through a collection leaves it pinned through
// the collection.
func TestUnpinTrackKeepsCollectionPin(t *testing.T) {
	fd := offlineEngine(t, "1001")
	fd.setPlaylist("9", "101", "102")
	pinAndWait(t, "playlist", "9")
	pinAndWait(t, "track", "101")
	if err := UnpinOffline("track", "101"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(OfflineIDs(), "101") || !caches.Current().Pinned("101.MP3_128") {
		t.Fatal("the track left with its single pin though its playlist holds it")
	}
	if _, ok := collection("track", ""); ok {
		t.Fatal("the single-track entry stayed")
	}
}

// Unpinning kind "track" with id "" (the Songs entry) unpins every
// single-track pin of the account, and nothing else: a track also pinned
// through a playlist stays, and another account's single pins stay.
func TestUnpinAllSingleTracks(t *testing.T) {
	fd := offlineEngine(t, "1001")
	fd.setPlaylist("9", "101")
	pinAndWait(t, "track", "101")
	pinAndWait(t, "track", "102")
	pinAndWait(t, "playlist", "9")
	signIn(t, "1002")
	pinAndWait(t, "track", "103")
	signIn(t, "1001")
	if err := UnpinOffline("track", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := collection("track", ""); ok {
		t.Fatal("the single-track entry stayed")
	}
	if ids := OfflineIDs(); !slices.Equal(ids, []string{"101"}) {
		t.Fatalf("ids %v, want the playlist's track only", ids)
	}
	signIn(t, "1002")
	if ids := OfflineIDs(); !slices.Equal(ids, []string{"103"}) {
		t.Fatalf("account 1002's ids %v", ids)
	}
}

// Single-track pins are one entry, newest first, with the first track's
// cover; all pins come newest first.
func TestSingleTrackPinsAreOneEntry(t *testing.T) {
	fd := offlineEngine(t, "1001")
	fd.setPlaylist("9", "103")
	pinAndWait(t, "track", "101")
	time.Sleep(2 * time.Millisecond)
	pinAndWait(t, "playlist", "9")
	time.Sleep(2 * time.Millisecond)
	pinAndWait(t, "track", "102")
	cs := OfflineCollections()
	if len(cs) != 2 || cs[0].Kind != "track" || cs[0].ID != "" || cs[0].Title != "" || cs[0].TrackCount != 2 || cs[1].ID != "9" {
		t.Fatalf("collections %+v", cs)
	}
	ts, _ := OfflineTracks("track", "")
	if len(ts) != 2 || ts[0].ID != "102" || ts[1].ID != "101" || cs[0].Cover != ts[0].ArtworkURL {
		t.Fatalf("single tracks %+v, cover %q", ts, cs[0].Cover)
	}
}

// Episodes are not pinnable.
func TestEpisodeNotPinnable(t *testing.T) {
	offlineEngine(t, "1001")
	if err := PinOffline("episode", "1"); err == nil {
		t.Fatal("an episode was pinned")
	}
}

// Pins belong to the account that made them: another account neither sees
// nor plays them offline, and its size of 0 keeps them; back on the first
// account they are there.
func TestPinsBelongToTheirAccount(t *testing.T) {
	fd := offlineEngine(t, "1001")
	pinAndWait(t, "track", "101")
	signIn(t, "1002")
	if len(OfflineCollections()) != 0 || len(OfflineIDs()) != 0 {
		t.Fatalf("account 1002 sees %v %v", OfflineCollections(), OfflineIDs())
	}
	if err := caches.SetMB(0); err != nil {
		t.Fatal(err)
	}
	goOffline(fd)
	if _, err := preparePlan(nil, "101"); err == nil {
		t.Fatal("another account's pin played offline")
	}
	fd.down.Store(false)
	offlineOn.Store(false)
	signIn(t, "1001")
	if !slices.Equal(OfflineIDs(), []string{"101"}) {
		t.Fatalf("account 1001's pin is gone: %v", OfflineIDs())
	}
}

// A pinned playlist is kept in step when its track list is fetched online:
// new tracks are pinned and fetched, removed ones unpinned.
func TestPinnedPlaylistKeptInStep(t *testing.T) {
	fd := offlineEngine(t, "1001")
	fd.setPlaylist("9", "101", "102")
	pinAndWait(t, "playlist", "9")
	fd.setPlaylist("9", "101", "103")
	if _, err := PlaylistTracks("9"); err != nil {
		t.Fatal(err)
	}
	waitJobsDone(t)
	if ids := OfflineIDs(); !slices.Equal(ids, []string{"101", "103"}) {
		t.Fatalf("ids %v after the list changed", ids)
	}
	if _, ok := caches.Current().Path("102.MP3_128"); ok {
		t.Fatal("the removed track is still stored at a size of 0")
	}
}

// The strict listing tells a playlist emptied online from one whose listing
// failed: a gateway error or no network is an error and keeps every pin; an
// empty list without an error unpins every track.
func TestKeepInStepEmptiedVersusFailed(t *testing.T) {
	fd := offlineEngine(t, "1001")
	fd.setPlaylist("9", "101", "102")
	pinAndWait(t, "playlist", "9")
	fd.listErr.Store(true)
	if ts, err := PlaylistTracks("9"); err == nil {
		t.Fatalf("a gateway error listed %d tracks without an error", len(ts))
	}
	fd.listErr.Store(false)
	fd.down.Store(true)
	if ts, err := PlaylistTracks("9"); err == nil {
		t.Fatalf("no network listed %d tracks without an error", len(ts))
	}
	fd.down.Store(false)
	if ids := OfflineIDs(); !slices.Equal(ids, []string{"101", "102"}) {
		t.Fatalf("ids %v after failed listings", ids)
	}
	fd.setPlaylist("9")
	if ts, err := PlaylistTracks("9"); err != nil || len(ts) != 0 {
		t.Fatalf("emptied playlist: %d tracks, %v", len(ts), err)
	}
	waitJobsDone(t)
	if c, ok := collection("playlist", "9"); !ok || c.TrackCount != 0 || len(OfflineIDs()) != 0 {
		t.Fatalf("after the playlist was emptied: %+v %v, ids %v", c, ok, OfflineIDs())
	}
}

// A listing shorter than its declared total does not keep a pin in step (a
// playlist or an album keeps every track), nor does an empty album listing.
func TestKeepInStepShortList(t *testing.T) {
	fd := offlineEngine(t, "1001")
	fd.setPlaylist("9", "101", "102")
	fd.mu.Lock()
	fd.albums["7"] = []string{"103", "104"}
	fd.mu.Unlock()
	pinAndWait(t, "playlist", "9")
	pinAndWait(t, "album", "7")
	want := []string{"101", "102", "103", "104"}

	fd.setPlaylist("9", "101")
	fd.mu.Lock()
	fd.albums["7"] = []string{"103"}
	fd.mu.Unlock()
	fd.unlisted.Store(1)
	if _, err := PlaylistTracks("9"); err != nil {
		t.Fatal(err)
	}
	if _, err := AlbumTracks("7"); err != nil {
		t.Fatal(err)
	}
	waitJobsDone(t)
	if ids := OfflineIDs(); !slices.Equal(ids, want) {
		t.Fatalf("ids %v after listings shorter than their totals", ids)
	}

	fd.unlisted.Store(0)
	fd.mu.Lock()
	fd.albums["7"] = nil
	fd.mu.Unlock()
	if _, err := AlbumTracks("7"); err != nil {
		t.Fatal(err)
	}
	waitJobsDone(t)
	if ids := OfflineIDs(); !slices.Equal(ids, want) {
		t.Fatalf("ids %v after an empty album listing", ids)
	}
}

// A full disk stops the fetch with the reason in the progress; the tracks
// fetched stay pinned; the next pin clears the stopped job.
func TestFullDiskStopsFetch(t *testing.T) {
	fd := offlineEngine(t, "1001")
	fd.setPlaylist("9", "101", "102", "103", "104", "105", "106")
	var n, fetched atomic.Int32
	two := make(chan struct{})
	fetchStream = func(ctx context.Context, plan *deezer.StreamPlan, c audio.StreamCache) error {
		if n.Add(1) <= 2 {
			err := audio.Fetch(ctx, plan, c)
			if fetched.Add(1) == 2 {
				close(two)
			}
			return err
		}
		<-two // the disk fills after two tracks
		return fmt.Errorf("%w: write: there is not enough space on the disk", audio.ErrWrite)
	}
	pinAndWait(t, "playlist", "9")
	active, jobs := OfflineProgress()
	if active || len(jobs) != 1 || !strings.Contains(jobs[0].Error, "not enough space") || jobs[0].Done < 2 {
		t.Fatalf("progress %v %+v", active, jobs)
	}
	if c, _ := collection("playlist", "9"); c.Fetched < 2 || len(OfflineIDs()) != c.Fetched {
		t.Fatalf("collection %+v, ids %v", c, OfflineIDs())
	}
	fetchStream = audio.Fetch
	pinAndWait(t, "track", "101")
	if _, jobs := OfflineProgress(); len(jobs) != 0 {
		t.Fatalf("stopped job still listed after a pin: %+v", jobs)
	}
}

// Cancel stops the fetches; each stays listed as canceled until the next
// unpin.
func TestCancelOffline(t *testing.T) {
	fd := offlineEngine(t, "1001")
	fd.setPlaylist("9", "101", "102")
	started := make(chan struct{}, 2)
	fetchStream = func(ctx context.Context, plan *deezer.StreamPlan, c audio.StreamCache) error {
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	if err := PinOffline("playlist", "9"); err != nil {
		t.Fatal(err)
	}
	<-started
	if active, _ := OfflineProgress(); !active {
		t.Fatal("no active fetch")
	}
	CancelOffline()
	waitJobsDone(t)
	active, jobs := OfflineProgress()
	if active || len(jobs) != 1 || jobs[0].Error != "canceled" {
		t.Fatalf("progress %v %+v after a cancel", active, jobs)
	}
	if err := UnpinOffline("playlist", "9"); err != nil {
		t.Fatal(err)
	}
	if _, jobs := OfflineProgress(); len(jobs) != 0 {
		t.Fatalf("jobs %+v after the unpin", jobs)
	}
}

// Changing the cache folder keeps the pins: they are copied, then the old
// folder's go.
func TestSetDirKeepsPins(t *testing.T) {
	fd := offlineEngine(t, "1001")
	pinAndWait(t, "track", "101")
	old, _ := config.LoadMedia().CacheStoreDir()
	next := filepath.Join(t.TempDir(), "next")
	if err := caches.SetDir(next); err != nil {
		t.Fatal(err)
	}
	if f := dataFiles(t, old); len(f) != 0 {
		t.Fatalf("old folder holds %v", f)
	}
	goOffline(fd)
	if _, err := preparePlan(nil, "101"); err != nil || !slices.Equal(OfflineIDs(), []string{"101"}) {
		t.Fatalf("after the move: %v, ids %v", err, OfflineIDs())
	}
}

// A folder change during a fetch stops the fetch for the move and finishes it
// in the new folder: every track ends there, none counted failed.
func TestSetDirDuringFetch(t *testing.T) {
	fd := offlineEngine(t, "1001")
	fd.setPlaylist("9", "101", "102", "103", "104")
	gate := make(chan struct{})
	fetchStream = func(ctx context.Context, plan *deezer.StreamPlan, c audio.StreamCache) error {
		if plan.TrackID != "101" {
			select {
			case <-gate:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return audio.Fetch(ctx, plan, c)
	}
	if err := PinOffline("playlist", "9"); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if _, js := OfflineProgress(); len(js) == 1 && js[0].Done == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the first track was not fetched")
		}
	}
	old, _ := config.LoadMedia().CacheStoreDir()
	if err := SetMediaCacheDir(filepath.Join(t.TempDir(), "next")); err != nil {
		t.Fatal(err)
	}
	close(gate)
	waitJobsDone(t)
	if c, _ := collection("playlist", "9"); c.Fetched != 4 {
		t.Fatalf("collection %+v after the move", c)
	}
	if _, js := OfflineProgress(); len(js) != 0 {
		t.Fatalf("jobs %+v after the move", js)
	}
	if f := dataFiles(t, old); len(f) != 0 {
		t.Fatalf("old folder holds %v", f)
	}
}

// pinHalfFetched pins playlist 9 of tracks 101 and 102 for account 1001 and
// returns once 101 is fetched; 102's fetch waits until release is called.
func pinHalfFetched(t *testing.T, fd *fakeDeezer) (release func()) {
	t.Helper()
	fd.setPlaylist("9", "101", "102")
	gate := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	fetchStream = func(ctx context.Context, plan *deezer.StreamPlan, c audio.StreamCache) error {
		if plan.TrackID != "101" {
			select {
			case <-gate:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return audio.Fetch(ctx, plan, c)
	}
	if err := PinOffline("playlist", "9"); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if _, js := OfflineProgress(); len(js) == 1 && js[0].Done == 1 {
			return release
		}
		if time.Now().After(deadline) {
			t.Fatal("the first track was not fetched")
		}
	}
}

// waitNoFetch fails t when a fetch still runs after 2 s.
func waitNoFetch(t *testing.T, why string) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if active, _ := OfflineProgress(); !active {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("a fetch still runs " + why)
		}
	}
}

// Offline mode stops the running fetches, which leave the list; when it ends
// they go on.
func TestOfflineModeStopsFetches(t *testing.T) {
	fd := offlineEngine(t, "1001")
	release := pinHalfFetched(t, fd)
	SetOfflineMode(true)
	waitNoFetch(t, "in offline mode")
	if _, js := OfflineProgress(); len(js) != 0 {
		t.Fatalf("jobs %+v in offline mode", js)
	}
	release()
	SetOfflineMode(false)
	waitJobsDone(t)
	if c, _ := collection("playlist", "9"); c.Fetched != 2 {
		t.Fatalf("collection %+v once offline mode ended", c)
	}
}

// A folder change in offline mode starts no fetch; the one offline mode
// stopped finishes in the new folder once offline mode ends.
func TestSetDirInOfflineModeResumesFetch(t *testing.T) {
	fd := offlineEngine(t, "1001")
	release := pinHalfFetched(t, fd)
	SetOfflineMode(true)
	release()
	if err := SetMediaCacheDir(filepath.Join(t.TempDir(), "next")); err != nil {
		t.Fatal(err)
	}
	waitNoFetch(t, "after the move in offline mode")
	if c, _ := collection("playlist", "9"); c.Fetched != 1 {
		t.Fatalf("collection %+v after the move in offline mode", c)
	}
	SetOfflineMode(false)
	waitJobsDone(t)
	if c, _ := collection("playlist", "9"); c.Fetched != 2 {
		t.Fatalf("collection %+v once offline mode ended", c)
	}
}

// moveWith changes the cache folder during a fetch held half way and runs
// during when the new folder is attached, before the move returns; it returns
// the requests made from then on, once every fetch ended.
func moveWith(t *testing.T, during func()) int32 {
	t.Helper()
	fd := offlineEngine(t, "1001")
	release := pinHalfFetched(t, fd)
	var after atomic.Int32
	after.Store(-1)
	caches.Attach = func(c *mediacache.Cache) {
		attachCache(c)
		if c != nil && after.Load() < 0 {
			during()
			after.Store(fd.calls.Load())
		}
	}
	t.Cleanup(func() { caches.Attach = attachCache })
	if err := SetMediaCacheDir(filepath.Join(t.TempDir(), "next")); err != nil {
		t.Fatal(err)
	}
	release()
	waitJobsDone(t)
	return fd.calls.Load() - after.Load()
}

// Offline mode turned on during a folder move: the fetch the move stopped
// does not go on, nothing is requested.
func TestOfflineModeDuringMoveRequestsNothing(t *testing.T) {
	if n := moveWith(t, func() { SetOfflineMode(true) }); n != 0 {
		t.Fatalf("%d requests after offline mode went on during the move", n)
	}
}

// A logout during a folder move: the old account's fetch does not go on,
// nothing is requested.
func TestLogoutDuringMoveRequestsNothing(t *testing.T) {
	if n := moveWith(t, Logout); n != 0 {
		t.Fatalf("%d requests after a logout during the move", n)
	}
}

// Two pin updates land in the order of their snapshots: one that read the
// store before a pin does not remove that pin after it was set (at a size of
// 0 its file too).
func TestApplyPinsKeepsOrder(t *testing.T) {
	offlineEngine(t, "1001")
	pinAndWait(t, "track", "101")
	held, release := make(chan struct{}), make(chan struct{})
	var first atomic.Bool
	pinsSnapshotted = func() {
		if first.CompareAndSwap(false, true) {
			close(held)
			<-release
		}
	}
	t.Cleanup(func() { pinsSnapshotted = func() {} })
	stale := make(chan struct{})
	go func() { applyPins(caches.Current()); close(stale) }()
	<-held
	pinned := make(chan error, 1)
	go func() { pinned <- PinOffline("track", "102") }()
	select {
	case err := <-pinned: // the pin did not wait for the older update
		if err != nil {
			t.Fatal(err)
		}
		waitJobsDone(t)
		pinned <- nil
	case <-time.After(2 * time.Second):
	}
	close(release)
	<-stale
	if err := <-pinned; err != nil {
		t.Fatal(err)
	}
	waitJobsDone(t)
	mc := caches.Current()
	if _, ok := mc.Path("102.MP3_128"); !ok || !mc.Pinned("102.MP3_128") {
		t.Fatalf("track 102 stored %v, pinned %v after the older update landed", ok, mc.Pinned("102.MP3_128"))
	}
}

// A folder change keeps a fetch stopped by a full disk listed with its reason,
// and does not start it again.
func TestSetDirKeepsStoppedReason(t *testing.T) {
	fd := offlineEngine(t, "1001")
	fd.setPlaylist("9", "101", "102")
	var fetches atomic.Int32
	fetchStream = func(ctx context.Context, plan *deezer.StreamPlan, c audio.StreamCache) error {
		fetches.Add(1)
		if plan.TrackID == "101" {
			return audio.Fetch(ctx, plan, c)
		}
		return fmt.Errorf("%w: write: there is not enough space on the disk", audio.ErrWrite)
	}
	pinAndWait(t, "playlist", "9")
	before := fetches.Load()
	if err := SetMediaCacheDir(filepath.Join(t.TempDir(), "next")); err != nil {
		t.Fatal(err)
	}
	waitJobsDone(t)
	if active, js := OfflineProgress(); active || len(js) != 1 || !strings.Contains(js[0].Error, "not enough space") || fetches.Load() != before {
		t.Fatalf("progress %v %+v after the move, %d fetches after it", active, js, fetches.Load()-before)
	}
}

// A fetch that ends on a lost network stops its job, listed with the reason, and
// the next resume fetches it again.
func TestNetworkLossStopsJobAndResumes(t *testing.T) {
	fd := offlineEngine(t, "1001")
	fd.setPlaylist("9", "101", "102")
	var down atomic.Bool
	down.Store(true)
	fetchStream = func(ctx context.Context, plan *deezer.StreamPlan, c audio.StreamCache) error {
		if down.Load() {
			return fmt.Errorf("%w: read: connection reset", deezer.ErrNoNetwork)
		}
		return audio.Fetch(ctx, plan, c)
	}
	pinAndWait(t, "playlist", "9")
	if _, js := OfflineProgress(); len(js) != 1 || !strings.Contains(js[0].Error, "no internet") {
		t.Fatalf("jobs %+v after a lost network, want one listed with the reason", js)
	}
	down.Store(false)
	resumeJobs()
	waitJobsDone(t)
	mc := caches.Current()
	_, ok1 := mc.Path("101.MP3_128")
	_, ok2 := mc.Path("102.MP3_128")
	if _, js := OfflineProgress(); len(js) != 0 || !ok1 || !ok2 {
		t.Fatalf("jobs %+v, stored %v %v after the network came back", js, ok1, ok2)
	}
}

// pinWithHeldFetch pins playlist 9 of two tracks for account 1001 with a
// fetch that holds every track until it is canceled, and returns once one
// is held.
func pinWithHeldFetch(t *testing.T) {
	t.Helper()
	fd := offlineEngine(t, "1001")
	fd.setPlaylist("9", "101", "102")
	held := make(chan struct{}, 2)
	fetchStream = func(ctx context.Context, plan *deezer.StreamPlan, c audio.StreamCache) error {
		held <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	t.Cleanup(func() { dropJobs(func(*offlineJob) bool { return true }) })
	if err := PinOffline("playlist", "9"); err != nil {
		t.Fatal(err)
	}
	<-held
}

// fetchesEnd reports whether every fetch ends within 2 s.
func fetchesEnd() bool {
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		jobMu.Lock()
		running := slices.ContainsFunc(jobs, func(j *offlineJob) bool { return j.running })
		jobMu.Unlock()
		if !running {
			return true
		}
	}
	return false
}

// Logout stops the account's fetches.
func TestLogoutDropsFetches(t *testing.T) {
	pinWithHeldFetch(t)
	Logout()
	if !fetchesEnd() {
		t.Fatal("a fetch runs on after the logout")
	}
}

// A login with another account stops the old account's fetches.
func TestAccountSwitchDropsFetches(t *testing.T) {
	pinWithHeldFetch(t)
	t.Cleanup(Logout)
	if !Init("1002") {
		t.Fatal("login failed")
	}
	if !fetchesEnd() {
		t.Fatal("account 1001's fetch runs on after account 1002 signed in")
	}
}

// Pin, cancel and pin again runs one job for the collection: the new pin
// replaces the canceled job, which fetches nothing beside it and leaves no
// "canceled" entry.
func TestPinCancelPinRunsOneJob(t *testing.T) {
	fd := offlineEngine(t, "1001")
	fd.setPlaylist("9", "101")
	var calls, active, maxActive atomic.Int32
	started, windDown := make(chan struct{}), make(chan struct{})
	fetchStream = func(ctx context.Context, plan *deezer.StreamPlan, c audio.StreamCache) error {
		n := active.Add(1)
		defer active.Add(-1)
		for m := maxActive.Load(); n > m && !maxActive.CompareAndSwap(m, n); m = maxActive.Load() {
		}
		if calls.Add(1) == 1 {
			close(started)
			<-ctx.Done()
			<-windDown // a canceled download that takes a moment to stop
			return ctx.Err()
		}
		return audio.Fetch(ctx, plan, c)
	}
	if err := PinOffline("playlist", "9"); err != nil {
		t.Fatal(err)
	}
	<-started
	CancelOffline()
	if err := PinOffline("playlist", "9"); err != nil {
		t.Fatal(err)
	}
	jobMu.Lock()
	n := 0
	for _, j := range jobs {
		if j.Kind == "playlist" && j.ID == "9" {
			n++
		}
	}
	jobMu.Unlock()
	close(windDown)
	waitJobsDone(t)
	if n != 1 {
		t.Fatalf("%d jobs for one playlist", n)
	}
	if _, js := OfflineProgress(); len(js) != 0 {
		t.Fatalf("jobs %+v after the fetch", js)
	}
	if m := maxActive.Load(); m != 1 {
		t.Fatalf("%d fetches of one track at once", m)
	}
	if ids := OfflineIDs(); !slices.Equal(ids, []string{"101"}) {
		t.Fatalf("ids %v", ids)
	}
}

// A login that fails for want of a network turns offline mode on, for the
// last account that signed in; a later login turns it off. A mode the host
// set stays.
func TestInitWithoutNetworkGoesOffline(t *testing.T) {
	fd := offlineEngine(t, "1001")
	pinAndWait(t, "track", "101")
	mu.Lock()
	client = nil
	mu.Unlock()
	fd.down.Store(true)
	if Init("1001") || LoginErrorKind() != 2 || !OfflineMode() {
		t.Fatalf("login kind %d, offline %v", LoginErrorKind(), OfflineMode())
	}
	if !slices.Equal(OfflineIDs(), []string{"101"}) {
		t.Fatalf("offline at start shows %v", OfflineIDs())
	}
	if _, err := preparePlan(netClient(), "101"); err != nil {
		t.Fatal(err)
	}
	fd.down.Store(false)
	t.Cleanup(Logout)
	if !Init("1001") || OfflineMode() {
		t.Fatalf("after a login: offline %v", OfflineMode())
	}
	SetOfflineMode(true)
	if !Init("1001") || !OfflineMode() {
		t.Fatal("a login turned off the mode the host set")
	}
}

// One HTTP transport: a login, a resolve (gateway and get_url), the stream it
// plans, a download of it and a picture reach one host over one connection.
func TestOneTransportForGatewayStreamsAndDownloads(t *testing.T) {
	fd := offlineEngine(t, "1001") // the login opens the first connection
	mc, err := mediacache.New(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer mc.Close()
	c := Client()
	plan, err := c.PrepareStream("101")
	if err != nil {
		t.Fatal(err)
	}
	if err := audio.Fetch(context.Background(), plan, mc); err != nil {
		t.Fatal(err)
	}
	if err := deezer.DownloadTrackContext(context.Background(), plan, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := storePicture(context.Background(), mc, "https://e-cdns-images.dzcdn.net/images/cover/x/250x250.jpg", "img-1.IMG", []string{"p"}); err != nil {
		t.Fatal(err)
	}
	t.Logf("connections for a login, a resolve, a stream, a download and a picture: %d", fd.conns.Load())
	if n := fd.conns.Load(); n != 1 {
		t.Fatalf("%d connections to one host, want 1", n)
	}
}

// A play that a newer one (a remote's) supersedes while it resolves reports
// PlaySuperseded, not a failure, and sets no error.
func TestPlaySupersededWhileResolving(t *testing.T) {
	offlineEngine(t, "1001")
	inner := deezer.Transport()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	deezer.SetTransport(rtFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("method") == "song.getData" {
			once.Do(func() {
				close(entered)
				<-release
			})
		}
		return inner.RoundTrip(r)
	}))
	t.Cleanup(func() { deezer.SetTransport(inner) })
	signIn(t, "1001") // a client on the transport above
	result := make(chan PlayResult, 1)
	go func() { result <- Play("101", 0) }()
	<-entered
	plays.claim() // a remote's play
	close(release)
	if r := <-result; r != PlaySuperseded {
		t.Fatalf("Play = %d, want %d (superseded)", r, PlaySuperseded)
	}
	if e := Player().LastError(); e != "" {
		t.Fatalf("LastError %q after a superseded play", e)
	}
}

// A play superseded while it resolves and then failing reports superseded and sets
// no error over the newer play.
func TestPlaySupersededThenFailing(t *testing.T) {
	offlineEngine(t, "1001")
	inner := deezer.Transport()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	deezer.SetTransport(rtFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("method") == "song.getData" {
			held := false
			once.Do(func() {
				held = true
				close(entered)
				<-release
			})
			if held {
				return nil, errors.New("connection reset")
			}
		}
		return inner.RoundTrip(r)
	}))
	t.Cleanup(func() { deezer.SetTransport(inner) })
	signIn(t, "1001")
	result := make(chan PlayResult, 1)
	go func() { result <- Play("101", 0) }()
	<-entered
	plays.claim()
	close(release)
	if r := <-result; r != PlaySuperseded {
		t.Fatalf("Play = %d, want %d (superseded)", r, PlaySuperseded)
	}
	if e := Player().LastError(); e != "" {
		t.Fatalf("LastError %q after a superseded play", e)
	}
}

// A pinned track Deezer does not serve stays playable from its pin, also online
// when a stream would be asked at a better quality than the pin's.
func TestPinnedTrackPlaysWhenNoLongerServed(t *testing.T) {
	fd := offlineEngine(t, "1003") // an account with MP3 320
	pinAndWait(t, "track", "101")  // fetched at quality 0: MP3 128
	SetQuality(1)
	fd.mu.Lock()
	fd.gone["101"], fd.gone["102"] = true, true
	fd.mu.Unlock()
	plan, err := preparePlan(netClient(), "101")
	if err != nil || plan.CDNURL != "" || plan.Format != "MP3_128" {
		t.Fatalf("plan %+v %v", plan, err)
	}
	if _, err := preparePlan(netClient(), "102"); err == nil {
		t.Fatal("an unpinned track planned")
	}
}

// A login that fails for want of a network turns offline mode on and stops the live watch.
func TestInitOfflineStopsElsewhere(t *testing.T) {
	fd := offlineEngine(t, "1001")
	watchElsewhere(Client())
	t.Cleanup(stopElsewhere)
	t.Cleanup(Logout)
	fd.down.Store(true)
	if Init("1001") || !OfflineMode() {
		t.Fatalf("login kind %d, offline %v", LoginErrorKind(), OfflineMode())
	}
	if elsewhereWatching() {
		t.Fatal("offline at a failed login left the live watch running")
	}
}

// A login while the host set offline mode starts no live watch and reads no web release.
func TestInitInOfflineModeRequestsNothing(t *testing.T) {
	offlineEngine(t, "1001")
	t.Cleanup(stopElsewhere)
	t.Cleanup(Logout)
	t.Cleanup(func() { SetOfflineMode(false) })
	var learned atomic.Int32
	prev := learnRelease
	learnRelease = func(*deezer.Client) { learned.Add(1) }
	t.Cleanup(func() { learnRelease = prev })
	SetOfflineMode(true)
	if !Init("1001") || !OfflineMode() {
		t.Fatal("login failed or turned off the host's offline mode")
	}
	if elsewhereWatching() || learned.Load() != 0 {
		t.Fatalf("watching %v, web release read %d times", elsewhereWatching(), learned.Load())
	}
}

// elsewhereWatching reports whether the live watch runs.
func elsewhereWatching() bool {
	elseMu.Lock()
	defer elseMu.Unlock()
	return elseStop != nil && elseAnnounce != nil
}
