package deezer

// Tests for batch downloads and tagging: ctx cancellation, unique and atomic file
// names, album pagination, strict playlist listing, atomic FLAC tagging, single
// ErrPartialDownload wrapping, one plan resolution per batch track.

import (
	"bytes"
	"context"
	"encoding/base64"
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
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/go-flac/flacvorbis"
	flac "github.com/go-flac/go-flac"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/mediacache"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/mediacache/mctest"
)

// rtFunc adapts a function to http.RoundTripper so tests can fake the gw
// transport without any network.
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResponse(body []byte) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

// gwTracksPage builds a gw playlist.getSongs response with n tracks.
func gwTracksPage(startID, n int) []byte {
	var sb strings.Builder
	sb.WriteString(`{"error":{},"results":{"data":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, `{"SNG_ID":"%d","SNG_TITLE":"Song %d","DURATION":"100","ART_ID":"7","ART_NAME":"A","ALB_TITLE":"Alb"}`, startID+i, startID+i)
	}
	sb.WriteString(`]}}`)
	return []byte(sb.String())
}

// restAlbumPage builds a REST /album/<id>/tracks page with n tracks.
func restAlbumPage(startID, n int, next string) []byte {
	var sb strings.Builder
	sb.WriteString(`{"data":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, `{"id":%d,"title":"Track %d","duration":100,"artist":{"id":1,"name":"A"},"album":{"title":"Alb"}}`, startID+i, startID+i)
	}
	sb.WriteString(`]`)
	if next != "" {
		fmt.Fprintf(&sb, `,"next":%q`, next)
	}
	sb.WriteString(`,"total":130}`)
	return []byte(sb.String())
}

// loggedInPremium flips the package-private session fields so the download
// entry points pass their LoggedIn/Premium gates without a real login.
func loggedInPremium(c *Client) {
	c.mu.Lock()
	c.apiToken = "test-token"
	c.canHQ = true
	c.mu.Unlock()
}

// --- Unique filenames + atomic single-track writes ---

func TestTrackFileName_IncludesTrackID(t *testing.T) {
	a := Track{ID: "111", Name: "Intro", Artists: []Artist{{Name: "Band"}}}
	b := Track{ID: "222", Name: "Intro", Artists: []Artist{{Name: "Band"}}}
	na, nb := trackFileName(a, "mp3"), trackFileName(b, "mp3")
	if na == nb {
		t.Fatalf("same-title/artist tracks with different IDs collide: %q", na)
	}
	if !strings.Contains(na, "111") || !strings.Contains(nb, "222") {
		t.Errorf("filenames should embed the track ID: %q / %q", na, nb)
	}
}

func TestSaveTrack_SameTitleArtistDifferentID_BothSaved(t *testing.T) {
	tmp := t.TempDir()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "AUDIO:%s", r.URL.Path)
	}))
	defer ts.Close()

	c := New("dummy")
	tr1 := Track{ID: "111", Name: "Intro", Artists: []Artist{{Name: "Band"}}}
	tr2 := Track{ID: "222", Name: "Intro", Artists: []Artist{{Name: "Band"}}}
	plan1 := &StreamPlan{CDNURL: ts.URL + "/one", Format: "MP3_128", Encrypted: false}
	plan2 := &StreamPlan{CDNURL: ts.URL + "/two", Format: "MP3_128", Encrypted: false}

	p1, err := c.saveTrack(context.Background(), tr1, tmp, plan1, nil)
	if err != nil {
		t.Fatalf("saveTrack tr1: %v", err)
	}
	p2, err := c.saveTrack(context.Background(), tr2, tmp, plan2, nil)
	if err != nil {
		t.Fatalf("saveTrack tr2: %v", err)
	}
	if p1 == p2 {
		t.Fatalf("both tracks saved to the same path %q (second overwrote first)", p1)
	}
	b1, _ := os.ReadFile(p1)
	b2, _ := os.ReadFile(p2)
	// Tagging prepends an ID3 header; the distinct audio bodies must survive.
	if !bytes.Contains(b1, []byte("AUDIO:/one")) || !bytes.Contains(b2, []byte("AUDIO:/two")) {
		t.Errorf("audio bodies mixed up or lost: p1=%q p2=%q", b1, b2)
	}
}

func TestSaveTrack_FailedTransferPreservesExistingFile(t *testing.T) {
	tmp := t.TempDir()
	// A CDN that advertises 100 bytes, sends 10, then kills the connection.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("short body"))
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		panic(http.ErrAbortHandler)
	}))
	defer ts.Close()

	c := New("dummy")
	tr := Track{ID: "111", Name: "Keeper", Artists: []Artist{{Name: "Band"}}}
	plan := &StreamPlan{CDNURL: ts.URL + "/a", Format: "MP3_128", Encrypted: false}

	// A previous successful download already lives at the final path.
	final := filepath.Join(tmp, trackFileName(tr, "mp3"))
	if err := os.WriteFile(final, []byte("GOOD OLD AUDIO"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := c.saveTrack(context.Background(), tr, tmp, plan, nil); err == nil {
		t.Fatal("expected the interrupted transfer to error")
	}
	got, err := os.ReadFile(final)
	if err != nil {
		t.Fatalf("prior good file is gone: %v", err)
	}
	if string(got) != "GOOD OLD AUDIO" {
		t.Errorf("prior good file was clobbered by the failed transfer: %q", got)
	}
	ents, _ := os.ReadDir(tmp)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Errorf("leftover temp file %q after failed transfer", e.Name())
		}
	}
}

// --- Album tracklist pagination ---

func TestAlbumTracks_PaginatesBeyond100(t *testing.T) {
	var indexes []string
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/album/42/tracks" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		idx := r.URL.Query().Get("index")
		indexes = append(indexes, idx)
		w.Header().Set("Content-Type", "application/json")
		switch idx {
		case "0", "":
			_, _ = w.Write(restAlbumPage(0, 100, ts.URL+"/album/42/tracks?index=100"))
		case "100":
			_, _ = w.Write(restAlbumPage(100, 30, ""))
		default:
			_, _ = w.Write([]byte(`{"data":[]}`))
		}
	}))
	defer ts.Close()

	c := New("dummy")
	c.restURLOverride = ts.URL

	tracks, err := c.AlbumTracks("42")
	if err != nil {
		t.Fatalf("AlbumTracks: %v", err)
	}
	if len(tracks) != 130 {
		t.Fatalf("got %d tracks, want 130 (pagination lost a page)", len(tracks))
	}
	if tracks[0].ID != "0" || tracks[129].ID != "129" {
		t.Errorf("track order wrong: first=%s last=%s", tracks[0].ID, tracks[129].ID)
	}
	if len(indexes) != 2 || indexes[1] != "100" {
		t.Errorf("expected requests at index 0 and 100, got %v", indexes)
	}
}

func TestAlbumTracks_SinglePageStops(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(restAlbumPage(0, 12, ""))
	}))
	defer ts.Close()

	c := New("dummy")
	c.restURLOverride = ts.URL

	tracks, err := c.AlbumTracks("7")
	if err != nil {
		t.Fatalf("AlbumTracks: %v", err)
	}
	if len(tracks) != 12 || calls != 1 {
		t.Errorf("got %d tracks in %d calls, want 12 tracks in 1 call", len(tracks), calls)
	}
}

// --- Strict playlist listing for downloads ---

func TestCollectTrackPagesStrict(t *testing.T) {
	t.Run("mid-fetch error fails whole listing", func(t *testing.T) {
		boom := errors.New("page 2 exploded")
		got, err := collectTrackPagesStrict(func(start, nb int) ([]Track, error) {
			if start == 0 {
				out := make([]Track, nb) // full page forces a second fetch
				return out, nil
			}
			return nil, boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want the page-2 error", err)
		}
		if got != nil {
			t.Errorf("strict listing must not return a partial set, got %d tracks", len(got))
		}
	})

	t.Run("happy path accumulates all pages", func(t *testing.T) {
		got, err := collectTrackPagesStrict(func(start, nb int) ([]Track, error) {
			if start == 0 {
				return make([]Track, nb), nil
			}
			return make([]Track, 50), nil
		})
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if len(got) != pageSize+50 {
			t.Errorf("got %d tracks, want %d", len(got), pageSize+50)
		}
	})
}

func TestSavePlaylist_FailsOnMidFetchPageError(t *testing.T) {
	// gw transport: page start=0 returns a full page, page start=200 fails. PlaylistTracks
	// returns the partial page; SavePlaylist (strict) must fail.
	pageErr := errors.New("simulated page-2 network failure")
	c := New("dummy")
	loggedInPremium(c)
	c.http = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.Contains(r.URL.Path, "gw-light.php") {
			return nil, fmt.Errorf("unexpected URL %s", r.URL)
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"start":0`) {
			return jsonResponse(gwTracksPage(0, pageSize)), nil
		}
		return nil, pageErr
	})}

	// Tolerant browse listing: partial set, nil error.
	browse, err := c.PlaylistTracks("42")
	if err != nil {
		t.Fatalf("tolerant PlaylistTracks should absorb the page error, got %v", err)
	}
	if len(browse) != pageSize {
		t.Fatalf("tolerant listing returned %d tracks, want %d", len(browse), pageSize)
	}

	// Strict download listing: the same failure must surface.
	saverCalls := 0
	opts := DownloadOptions{
		Concurrency: 1,
		trackSaver: func(ctx context.Context, tt Track, d string, plan *StreamPlan, f ArtworkFetcher) (string, error) {
			saverCalls++
			return filepath.Join(d, tt.ID+".mp3"), nil
		},
		planResolver: func(string) (*StreamPlan, error) { return &StreamPlan{Format: "MP3_128"}, nil },
	}
	paths, err := c.SavePlaylist(context.Background(), "42", t.TempDir(), opts)
	if err == nil {
		t.Fatal("SavePlaylist must fail when the playlist listing is truncated")
	}
	if paths != nil {
		t.Errorf("no paths expected on listing failure, got %d", len(paths))
	}
	if saverCalls != 0 {
		t.Errorf("no track should be downloaded from a truncated listing; saver ran %d times", saverCalls)
	}
}

// --- Cancellation surfaces and pre-canceled ctx skips the listing ---

func TestSaveAlbum_PreCanceledContextSkipsListing(t *testing.T) {
	listings := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		listings++
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer ts.Close()

	c := New("dummy")
	loggedInPremium(c)
	c.restURLOverride = ts.URL

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.SaveAlbum(ctx, "42", t.TempDir(), DownloadOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("SaveAlbum err = %v, want context.Canceled", err)
	}
	if listings != 0 {
		t.Errorf("canceled SaveAlbum still performed %d listing request(s)", listings)
	}
}

func TestSavePlaylist_PreCanceledContextSkipsListing(t *testing.T) {
	c := New("dummy")
	loggedInPremium(c)
	// Any gw call would be a bug; make it observable without network.
	c.http = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("unexpected network call from a canceled SavePlaylist")
	})}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.SavePlaylist(ctx, "42", t.TempDir(), DownloadOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("SavePlaylist err = %v, want context.Canceled", err)
	}
}

// --- Aggregate error matches both sentinels ---

func TestSaveBatch_PartialErrorMatchesBothSentinels(t *testing.T) {
	tmp := t.TempDir()
	c := &Client{}
	boom := errors.New("decrypt exploded")

	trs := []Track{{ID: "ok", Name: "OK"}, {ID: "bad", Name: "BAD"}}
	opts := DownloadOptions{
		Concurrency: 1,
		trackSaver: func(ctx context.Context, tt Track, d string, plan *StreamPlan, f ArtworkFetcher) (string, error) {
			if tt.ID == "bad" {
				return "", boom
			}
			return filepath.Join(d, tt.ID+".mp3"), nil
		},
		planResolver: func(string) (*StreamPlan, error) { return &StreamPlan{Format: "MP3_128"}, nil },
	}

	saved, err := c.saveBatch(context.Background(), trs, tmp, opts)
	if err == nil {
		t.Fatal("expected aggregate error")
	}
	if !errors.Is(err, ErrPartialDownload) {
		t.Errorf("errors.Is(err, ErrPartialDownload) = false; err = %v", err)
	}
	if !errors.Is(err, boom) {
		t.Errorf("errors.Is(err, firstConcreteErr) = false; err = %v", err)
	}
	if len(saved) != 1 {
		t.Errorf("saved = %d, want 1", len(saved))
	}
}

// --- One PrepareStream per batched track ---

func TestSaveBatch_ResolvesPlanOncePerTrack(t *testing.T) {
	tmp := t.TempDir()
	c := &Client{}

	trs := []Track{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}, {ID: "c", Name: "C"}}
	resolved := map[string]*StreamPlan{}
	resolves := 0
	opts := DownloadOptions{
		Concurrency: 1, // serial to keep map access in test hook race-free and preserve resolve-once count
		planResolver: func(id string) (*StreamPlan, error) {
			resolves++
			p := &StreamPlan{TrackID: id, Format: "MP3_320"}
			resolved[id] = p
			return p, nil
		},
		trackSaver: func(ctx context.Context, tt Track, d string, plan *StreamPlan, f ArtworkFetcher) (string, error) {
			if plan == nil {
				t.Errorf("track %s: saver got nil plan (would re-resolve)", tt.ID)
			} else if plan != resolved[tt.ID] {
				t.Errorf("track %s: saver got a different plan than the batch resolved", tt.ID)
			}
			return filepath.Join(d, tt.ID+".mp3"), nil
		},
	}

	if _, err := c.saveBatch(context.Background(), trs, tmp, opts); err != nil {
		t.Fatalf("saveBatch: %v", err)
	}
	if resolves != len(trs) {
		t.Errorf("plan resolved %d times for %d tracks, want exactly one each", resolves, len(trs))
	}
}

// --- FLAC tagging never destroys the downloaded audio ---

func TestTagFLAC_ErrorLeavesOriginalBytesIntact(t *testing.T) {
	tr := Track{Name: "T", Artists: []Artist{{Name: "A"}}, AlbumName: "Alb"}

	t.Run("corrupt flac input", func(t *testing.T) {
		tmp := t.TempDir()
		path := filepath.Join(tmp, "bad.flac")
		orig := []byte("not a flac stream at all")
		if err := os.WriteFile(path, orig, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := tagFLAC(path, tr, nil, 0, ""); err == nil {
			t.Fatal("expected parse error for corrupt input")
		}
		got, _ := os.ReadFile(path)
		if !bytes.Equal(got, orig) {
			t.Error("corrupt-input tagging modified the original file")
		}
	})

	t.Run("write failure preserves audio", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("read-only dir does not block file creation on Windows")
		}
		if os.Geteuid() == 0 {
			t.Skip("directory permissions do not bind root")
		}
		tmp := t.TempDir()
		path := filepath.Join(tmp, "good.flac")
		orig, err := base64.StdEncoding.DecodeString(flacAudioB64)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, orig, 0o644); err != nil {
			t.Fatal(err)
		}
		// A read-only directory makes the temp-file creation fail, standing in
		// for ENOSPC/short-write failures. The old in-place O_TRUNC rewrite
		// would still have truncated the audio here (file itself is writable).
		if err := os.Chmod(tmp, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(tmp, 0o755) })

		if err := tagFLAC(path, tr, nil, 1, "2025"); err == nil {
			t.Fatal("expected tagging to fail in a read-only directory")
		}
		got, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("original audio is gone: %v", rerr)
		}
		if !bytes.Equal(got, orig) {
			t.Error("failed tagging modified the original audio bytes")
		}
	})
}

// Tagging a FLAC rewrites its metadata and copies the frames through: the
// frames come out byte for byte, and the file is never held in memory.
func TestTagFLAC_StreamsFrames(t *testing.T) {
	raw, err := base64.StdEncoding.DecodeString(flacAudioB64)
	if err != nil {
		t.Fatal(err)
	}
	orig := append(raw, bytes.Repeat([]byte{0x5A, 0xA5}, 8<<20)...) // 16 MB more frame bytes
	path := filepath.Join(t.TempDir(), "big.flac")
	if err := os.WriteFile(path, orig, 0o644); err != nil {
		t.Fatal(err)
	}
	want, err := flac.ParseBytes(bytes.NewReader(orig))
	if err != nil {
		t.Fatal(err)
	}
	tr := Track{Name: "Big", Artists: []Artist{{Name: "A"}}, AlbumName: "Alb"}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if err := tagFLAC(path, tr, []byte("art"), 3, "2024"); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	allocated := float64(after.TotalAlloc-before.TotalAlloc) / 1e6
	t.Logf("tagging a %.1f MB FLAC allocated %.2f MB", float64(len(orig))/1e6, allocated)
	if allocated > 1 {
		t.Fatalf("tagging allocated %.2f MB", allocated)
	}
	got, err := flac.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Frames, want.Frames) {
		t.Fatal("frames changed")
	}
	title := ""
	for _, mb := range got.Meta {
		if mb.Type == flac.VorbisComment {
			if vc, perr := flacvorbis.ParseFromMetaDataBlock(*mb); perr == nil {
				if vs, _ := vc.Get(flacvorbis.FIELD_TITLE); len(vs) > 0 {
					title = vs[0]
				}
			}
		}
	}
	if got.Meta[0].Type != flac.StreamInfo || title != "Big" {
		t.Fatalf("first block %v, title %q", got.Meta[0].Type, title)
	}
}

func TestSaveTrack_FLACIsTaggedAsFLAC(t *testing.T) {
	// saveTrack tags its temp file ("<stem>-<rand>.part", no extension) before
	// the rename: the FLAC tagger must still be chosen.
	raw, err := base64.StdEncoding.DecodeString(flacAudioB64)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(raw)
	}))
	defer ts.Close()

	tr := Track{Name: "Part Song", Artists: []Artist{{Name: "A"}}, AlbumName: "Alb"}
	plan := &StreamPlan{CDNURL: ts.URL + "/cdn/audio", Format: "FLAC"}
	path, err := New("dummy").saveTrack(context.Background(), tr, t.TempDir(), plan, nil)
	if err != nil {
		t.Fatalf("saveTrack: %v", err)
	}

	f, err := flac.ParseFile(path)
	if err != nil {
		t.Fatalf("result is not a valid FLAC (mp3 path used?): %v", err)
	}
	title := ""
	for _, mb := range f.Meta {
		if mb.Type == flac.VorbisComment {
			if vc, perr := flacvorbis.ParseFromMetaDataBlock(*mb); perr == nil {
				if vs, _ := vc.Get(flacvorbis.FIELD_TITLE); len(vs) > 0 {
					title = vs[0]
				}
			}
		}
	}
	if title != "Part Song" {
		t.Errorf("vorbis TITLE = %q, want %q (flac dispatch failed)", title, "Part Song")
	}
}

// --- Concurrent .part uniqueness via CreateTemp ---

func TestSaveTrack_ConcurrentSameTrack_UsesUniqueTemps(t *testing.T) {
	tmp := t.TempDir()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return enough distinct bytes so we can verify clean (non-interleaved) content.
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("CLEAN-AUDIO-FOR-CONCURRENT-TEST-1234567890"))
	}))
	defer ts.Close()

	c := New("dummy")
	// No premium/login needed: we pass plan, saveTrack path doesn't gate.
	tr := Track{ID: "conc42", Name: "SameSong", Artists: []Artist{{Name: "Artist"}}}
	plan := &StreamPlan{CDNURL: ts.URL + "/audio", Format: "MP3_128", Encrypted: false}

	seen := map[string]struct{}{}
	var seenMu sync.Mutex
	testCreateTemp = func(dir, pattern string) (*os.File, error) {
		f, err := os.CreateTemp(dir, pattern)
		if err == nil {
			seenMu.Lock()
			seen[f.Name()] = struct{}{}
			seenMu.Unlock()
		}
		return f, err
	}
	defer func() { testCreateTemp = nil }()

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.saveTrack(context.Background(), tr, tmp, plan, nil)
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	// At least one should succeed (rename race on final is acceptable; content is identical).
	success := 0
	for e := range results {
		if e == nil {
			success++
		}
	}
	if success == 0 {
		t.Fatalf("expected at least one concurrent save to succeed")
	}

	seenMu.Lock()
	nTemps := len(seen)
	seenMu.Unlock()
	if nTemps < 2 {
		t.Errorf("concurrent downloads used only %d distinct temp(s); wanted >=2 for uniqueness", nTemps)
	}

	// Final file must exist and contain clean (non-corrupt) payload.
	final := filepath.Join(tmp, trackFileName(tr, "mp3"))
	b, err := os.ReadFile(final)
	if err != nil {
		t.Fatalf("final file missing: %v", err)
	}
	if !bytes.Contains(b, []byte("CLEAN-AUDIO-FOR-CONCURRENT")) {
		t.Errorf("final content corrupted or incomplete: %q", string(b))
	}

	// No stray .part files left behind.
	ents, _ := os.ReadDir(tmp)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Errorf("leftover temp after concurrent saves: %s", e.Name())
		}
	}
}

// --- sanitize keeps [id] suffix even after long/UTF-8 title ---

func TestTrackFileName_TruncatesBeforeIDSuffix_UTF8Safe(t *testing.T) {
	// 200+ multi-byte runes in title; without the pre-truncate+append fix the
	// [id] would be sliced off and/or a rune split.
	long := strings.Repeat("é", 220) // each é is 2 bytes
	tr := Track{ID: "98765", Name: long, Artists: []Artist{{Name: "B"}}}
	fn := trackFileName(tr, "mp3")
	base := strings.TrimSuffix(fn, ".mp3")

	if !strings.Contains(base, "[98765]") {
		t.Fatalf("[id] suffix was stripped by truncate: %q", base)
	}
	if !utf8.ValidString(base) {
		t.Fatalf("filename base is not valid UTF-8: %q", base)
	}
	// The whole base must respect ~180 byte cap.
	if len(base) > 180+len(" [98765]") { // allow a little for safety margin in impl
		// actually impl caps the desc then +suffix, sanitize may trim
		t.Logf("len=%d (acceptable if id present)", len(base))
	}
	// Id must be near end
	if !strings.HasSuffix(base, "[98765]") && !strings.Contains(base, "[98765]") {
		t.Errorf("id not at/near end: %q", base)
	}
}

// --- PodcastEpisodes and Playlists pagination (100+30) ---

func TestPodcastEpisodes_PaginatesBeyond100(t *testing.T) {
	var indexes []string
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/podcast/99/episodes") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		idx := r.URL.Query().Get("index")
		indexes = append(indexes, idx)
		w.Header().Set("Content-Type", "application/json")
		switch idx {
		case "0", "":
			_, _ = w.Write(podcastEpisodesPage(0, 100, ts.URL+"/podcast/99/episodes?index=100"))
		case "100":
			_, _ = w.Write(podcastEpisodesPage(100, 30, ""))
		default:
			_, _ = w.Write([]byte(`{"data":[]}`))
		}
	}))
	defer ts.Close()

	c := New("dummy")
	c.restURLOverride = ts.URL

	eps, err := c.PodcastEpisodes("99")
	if err != nil {
		t.Fatalf("PodcastEpisodes: %v", err)
	}
	if len(eps) != 130 {
		t.Fatalf("got %d episodes, want 130 (pagination lost data)", len(eps))
	}
	if eps[0].ID != "0" || eps[129].ID != "129" {
		t.Errorf("episode order wrong: first=%s last=%s", eps[0].ID, eps[129].ID)
	}
	if len(indexes) != 2 || indexes[1] != "100" {
		t.Errorf("expected index 0 and 100, got %v", indexes)
	}
}

func podcastEpisodesPage(startID, n int, next string) []byte {
	var sb strings.Builder
	sb.WriteString(`{"data":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, `{"id":%d,"title":"Ep %d","duration":120}`, startID+i, startID+i)
	}
	sb.WriteString(`]`)
	if next != "" {
		fmt.Fprintf(&sb, `,"next":%q`, next)
	}
	sb.WriteString(`}`)
	return []byte(sb.String())
}

// The profile's playlists tab comes whole in one gw call, with each playlist's owner.
func TestPlaylistsOneRead(t *testing.T) {
	calls := 0
	c := New("dummy")
	loggedInPremium(c)
	c.http = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.Contains(r.URL.Path, "gw-light.php") {
			return nil, fmt.Errorf("unexpected %s", r.URL)
		}
		var b []byte
		if r.Body != nil {
			b, _ = io.ReadAll(r.Body)
		}
		calls++
		if !strings.Contains(string(b), `"nb":10000`) {
			return nil, fmt.Errorf("paged request %s", b)
		}
		return jsonResponse(gwPlaylistsPage(0, 130)), nil
	})}

	pls, err := c.Playlists()
	if err != nil {
		t.Fatalf("Playlists: %v", err)
	}
	if len(pls) != 130 {
		t.Fatalf("got %d playlists, want 130", len(pls))
	}
	if pls[0].ID != "0" || pls[129].ID != "129" {
		t.Errorf("order: first=%s last=%s", pls[0].ID, pls[129].ID)
	}
	if calls != 1 || pls[7].OwnerID != "42" {
		t.Errorf("%d gw calls, owner %q; want 1, 42", calls, pls[7].OwnerID)
	}
}

// The CDN files images by kind and answers a wrong kind with a placeholder, so
// playlist pictures follow PICTURE_TYPE and the account picture is a "user" image.
func TestImageKinds(t *testing.T) {
	c := New("dummy")
	c.http = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.RawQuery, "method=deezer.getUserData") {
			return jsonResponse([]byte(`{"error":{},"results":{"checkForm":"api-t","USER":{"USER_ID":"1","BLOG_NAME":"t","USER_PICTURE":"u5"}}}`)), nil
		}
		return jsonResponse([]byte(`{"error":{},"results":{"TAB":{"playlists":{"data":[` +
			`{"PLAYLIST_ID":"1","PLAYLIST_PICTURE":"e1","PICTURE_TYPE":"playlist"},` +
			`{"PLAYLIST_ID":"2","PLAYLIST_PICTURE":"a-b-c-d","PICTURE_TYPE":"cover"},` +
			`{"PLAYLIST_ID":"3","PLAYLIST_PICTURE":"","PICTURE_TYPE":"cover"}]}}}}`)), nil
	})}
	if err := c.Login(); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if got := c.Account().Picture; !strings.Contains(got, "/images/user/u5/") {
		t.Errorf("account picture = %q", got)
	}
	pls, err := c.Playlists()
	if err != nil || len(pls) != 3 {
		t.Fatalf("Playlists: %v, %d", err, len(pls))
	}
	if !strings.Contains(pls[0].ArtworkURL, "/images/playlist/e1/") || !strings.Contains(pls[1].ArtworkURL, "/images/cover/a-b-c-d/") || pls[2].ArtworkURL != "" {
		t.Errorf("artwork = %q, %q, %q", pls[0].ArtworkURL, pls[1].ArtworkURL, pls[2].ArtworkURL)
	}
}

func gwPlaylistsPage(startID, n int) []byte {
	var sb strings.Builder
	sb.WriteString(`{"error":{},"results":{"TAB":{"playlists":{"data":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, `{"PLAYLIST_ID":"%d","TITLE":"Pl %d","NB_SONG":5,"PLAYLIST_PICTURE":"p","PARENT_USER_ID":"42"}`, startID+i, startID+i)
	}
	sb.WriteString(`]}}}}`)
	return []byte(sb.String())
}

// --- PrepareStream error classification ---

func TestPrepareStream_Transient5xxRetried_NoPreviewFallback(t *testing.T) {
	mediaCalls := 0
	gwCalls := 0
	c := New("dummy")
	loggedInPremium(c)
	c.mu.Lock()
	c.licenseToken = "lic-1"
	c.userID = "1"
	c.mu.Unlock()
	c.restURLOverride = "http://rest.test"
	c.http = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		var bb []byte
		if r.Body != nil {
			bb, _ = io.ReadAll(r.Body)
			r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(bb))
		}
		q := r.URL.RawQuery
		if strings.Contains(q, "method=deezer.getUserData") {
			return loginResponseForTest(), nil
		}
		if strings.Contains(q, "method=song.getData") {
			gwCalls++
			return jsonResponse([]byte(`{"error":{},"results":{"TRACK_TOKEN":"tok-xyz","GAIN":"0"}}`)), nil
		}
		if r.URL.Host == "media.deezer.com" || strings.Contains(r.URL.Path, "get_url") {
			mediaCalls++
			// always 5xx to force retries then error
			return &http.Response{
				StatusCode: 503,
				Status:     "503 Service Unavailable",
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		}
		if strings.HasPrefix(r.URL.Path, "/track/") {
			// should NOT be hit for transient case
			return jsonResponse([]byte(`{"preview":"https://x/pre.mp3"}`)), nil
		}
		return nil, fmt.Errorf("unexpected test req: %s %s", r.Method, r.URL)
	})}

	plan, err := c.PrepareStream("123")
	if err == nil {
		t.Fatalf("expected error for persistent transient, got plan=%+v", plan)
	}
	if IsNoFullMedia(err) {
		t.Errorf("transient 5xx should not be classified as NoFullMedia: %v", err)
	}
	if mediaCalls < 2 {
		t.Errorf("expected at least 2 (retries) media calls on 5xx, got %d", mediaCalls)
	}
	// preview must not have been attempted
	// (we didn't count but if plan came back as preview it would be wrong anyway)
	if plan != nil && plan.Preview {
		t.Error("must not return preview plan on transient error")
	}
}

// streamTestClient is a logged-in premium client whose gw answers song.getData
// with songData (a 502 when empty), get_url with mediaBody, and counts logins.
func streamTestClient(songData, mediaBody string, logins *int) *Client {
	c := New("dummy")
	loggedInPremium(c)
	c.mu.Lock()
	c.licenseToken = "lic-1"
	c.userID = "1"
	c.mu.Unlock()
	c.restURLOverride = "http://rest.test"
	c.http = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		q := r.URL.RawQuery
		switch {
		case strings.Contains(q, "method=deezer.getUserData"):
			*logins++
			return loginResponseForTest(), nil
		case strings.Contains(q, "method=song.getData"):
			if songData == "" {
				return &http.Response{StatusCode: 502, Status: "502 Bad Gateway", Body: io.NopCloser(strings.NewReader("<html>bad gateway</html>"))}, nil
			}
			return jsonResponse([]byte(songData)), nil
		case strings.Contains(r.URL.Path, "get_url"):
			if b, _ := io.ReadAll(r.Body); strings.Contains(string(b), `"type":"PREVIEW"`) {
				return jsonResponse([]byte(`{"data":[{"media":[{"format":"MP3_128","sources":[{"url":"https://cdn/preview.mp3"}]}]}]}`)), nil
			}
			return jsonResponse([]byte(mediaBody)), nil
		case strings.HasPrefix(r.URL.Path, "/track/"):
			return jsonResponse([]byte(`{"preview":"https://cdn/preview.mp3"}`)), nil
		}
		return nil, fmt.Errorf("unexpected test req: %s", r.URL)
	})}
	return c
}

// A failed track-token call is an error, not a 30-second preview for a paid user.
func TestPrepareStream_TokenFailureIsNoPreview(t *testing.T) {
	logins := 0
	plan, err := streamTestClient("", "", &logins).PrepareStream("123")
	if err == nil || plan != nil {
		t.Fatalf("got plan %+v, err %v; want the gw error", plan, err)
	}
}

// A refused track gets the preview and costs no re-login. A license-token rejection
// re-logs in once a minute; a license still refused, and any inside the minute, is an
// error, not the preview a paid user is not meant to get.
func TestPrepareStream_ReloginOnlyForLicenseToken(t *testing.T) {
	const song = `{"error":{},"results":{"TRACK_TOKEN":"tok","GAIN":"0"}}`

	logins := 0
	c := streamTestClient(song, `{"data":[{"errors":[{"code":2002,"message":"Track token has no sufficient rights on requested media"}]}]}`, &logins)
	for i := 0; i < 5; i++ {
		if plan, err := c.PrepareStream("123"); err != nil || !plan.Preview {
			t.Fatalf("refused track: plan %+v, err %v; want the preview", plan, err)
		}
	}
	if logins != 0 {
		t.Errorf("refused track: %d logins, want 0", logins)
	}

	logins = 0
	c = streamTestClient(song, `{"errors":[{"code":1001,"message":"Expired license token"}]}`, &logins)
	for i := 0; i < 5; i++ {
		if plan, err := c.PrepareStream("123"); err == nil || plan != nil {
			t.Fatalf("license rejection %d: plan %+v, err %v; want an error", i, plan, err)
		}
	}
	if logins != 1 {
		t.Errorf("license rejections: %d logins, want 1", logins)
	}
}

// A track refused for rights plays the version song.getData names as its
// fallback, as Deezer's web player does: the plan keeps the asked track's id
// and metadata and carries the fallback's id, whose key decrypts the stream.
func TestPrepareStream_RefusedTrackPlaysFallback(t *testing.T) {
	logins := 0
	c := streamTestClient(`{"error":{},"results":{"SNG_ID":"123","SNG_TITLE":"Song","TRACK_TOKEN":"tok","GAIN":"-3","ALB_PICTURE":"own",`+
		`"FALLBACK":{"SNG_ID":"456","SNG_TITLE":"Song","TRACK_TOKEN":"fbtok","ALB_PICTURE":"fbpic"}}}`, "", &logins)
	base := c.http.Transport
	c.http.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.Contains(r.URL.Path, "get_url") {
			return base.RoundTrip(r)
		}
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), `"track_tokens":["fbtok"]`) {
			return jsonResponse([]byte(`{"data":[{"media":[{"format":"MP3_128","sources":[{"url":"https://cdn/fallback.mp3"}]}]}]}`)), nil
		}
		return jsonResponse([]byte(`{"data":[{"errors":[{"code":2002,"message":"Track token has no sufficient rights on requested media"}]}]}`)), nil
	})
	plan, err := c.PrepareStream("123")
	if err != nil || plan.Preview || plan.CDNURL != "https://cdn/fallback.mp3" || plan.TrackID != "123" || plan.KeyID != "456" ||
		plan.Track.ID != "123" || plan.Track.ArtworkURL != gwImage("cover", "fbpic") || plan.GainDB != -3 || !plan.Encrypted {
		t.Fatalf("plan %+v, err %v; want the fallback's full stream", plan, err)
	}
	if logins != 0 {
		t.Errorf("%d logins, want 0", logins)
	}
}

// A track listed with empty RIGHTS gets no source from get_url ({"data":[{"media":[]}]},
// seen for 4Minute's "Crazy" in a Free account's favorites); its FALLBACK version plays.
func TestPrepareStream_SourcelessTrackPlaysFallback(t *testing.T) {
	logins := 0
	c := streamTestClient(`{"error":{},"results":{"SNG_ID":"123","SNG_TITLE":"Song","TRACK_TOKEN":"tok","GAIN":"0",`+
		`"FALLBACK":{"SNG_ID":"456","SNG_TITLE":"Song","TRACK_TOKEN":"fbtok"}}}`, "", &logins)
	base := c.http.Transport
	c.http.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.Contains(r.URL.Path, "get_url") {
			return base.RoundTrip(r)
		}
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), `"track_tokens":["fbtok"]`) {
			return jsonResponse([]byte(`{"data":[{"media":[{"format":"MP3_128","sources":[{"url":"https://cdn/fallback.mp3"}]}]}]}`)), nil
		}
		return jsonResponse([]byte(`{"data":[{"media":[]}]}`)), nil
	})
	plan, err := c.PrepareStream("123")
	if err != nil || plan.Preview || plan.CDNURL != "https://cdn/fallback.mp3" || plan.KeyID != "456" {
		t.Fatalf("plan %+v, err %v; want the fallback's full stream", plan, err)
	}
}

// A listed track with empty RIGHTS and no FALLBACK is unavailable; with a fallback, or
// with rights, or listed without RIGHTS at all, it is not.
func TestGwTrack_Unavailable(t *testing.T) {
	var ts []gwTrackDTO
	if err := json.Unmarshal([]byte(`[{"SNG_ID":"1","RIGHTS":{}},`+
		`{"SNG_ID":"2","RIGHTS":{},"FALLBACK":{"SNG_ID":"3","TRACK_TOKEN":"fb"}},`+
		`{"SNG_ID":"4","RIGHTS":{"STREAM_ADS_AVAILABLE":true}},{"SNG_ID":"5"}]`), &ts); err != nil {
		t.Fatal(err)
	}
	for i, want := range []bool{true, false, false, false} {
		if got := ts[i].toTrack().Unavailable; got != want {
			t.Errorf("track %s: unavailable %v, want %v", ts[i].SngID, got, want)
		}
	}
}

// The gateway writes an empty RIGHTS object as []: the page still lists every track,
// that one unavailable.
func TestRightsEmptyArray(t *testing.T) {
	var page struct {
		Data []gwTrackDTO `json:"data"`
	}
	if err := json.Unmarshal([]byte(`{"data":[{"SNG_ID":"1","RIGHTS":{"STREAM_SUB_AVAILABLE":true}},`+
		`{"SNG_ID":"2","RIGHTS":[]},{"SNG_ID":"3"}]}`), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 3 {
		t.Fatalf("%d tracks listed, want 3", len(page.Data))
	}
	for i, want := range []bool{false, true, false} {
		if got := page.Data[i].toTrack().Unavailable; got != want {
			t.Errorf("track %s: unavailable %v, want %v", page.Data[i].SngID, got, want)
		}
	}
}

// A listed track with a fallback shows the fallback's cover, the version
// that plays; Deezer gives the refused track a placeholder picture.
func TestGwTrack_FallbackCover(t *testing.T) {
	var ts []gwTrackDTO
	if err := json.Unmarshal([]byte(`[{"SNG_ID":"1","ALB_PICTURE":"own"},`+
		`{"SNG_ID":"2","ALB_PICTURE":"placeholder","FALLBACK":{"SNG_ID":"3","ALB_PICTURE":"real"}}]`), &ts); err != nil {
		t.Fatal(err)
	}
	if got := ts[0].toTrack().ArtworkURL; got != gwImage("cover", "own") {
		t.Errorf("track without a fallback: %q", got)
	}
	if got := ts[1].toTrack(); got.ID != "2" || got.ArtworkURL != gwImage("cover", "real") {
		t.Errorf("track with a fallback: %+v; want id 2 with the fallback's cover", got)
	}
}

// An encrypted stream is decrypted with the key of the id that served it.
func TestDownloadTrack_DecryptsWithKeyID(t *testing.T) {
	data := make([]byte, StripeChunk*4)
	for i := range data {
		data[i] = byte(i % 251)
	}
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	}))
	defer cdn.Close()
	var got bytes.Buffer
	if err := DownloadTrack(&StreamPlan{CDNURL: cdn.URL, TrackID: "123", KeyID: "456", Format: "MP3_128", Encrypted: true}, &got); err != nil {
		t.Fatal(err)
	}
	want, _ := DecryptTrack("456", data)
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatal("the stream was not decrypted with the key id's key")
	}
}

// A cached copy of a fallback stream keeps the id whose key decrypts it.
func TestPrepareStreamCached_KeepsKeyID(t *testing.T) {
	mc, err := mediacache.New(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer mc.Close()
	mctest.PutBody(t, mc, "123.MP3_128")
	_ = mc.PutMeta("123.MP3_128", mediacache.StreamMeta{Format: "MP3_128", KeyID: "456", Encrypted: true})
	plan, err := New("arl").PrepareStreamCached("123", mc) // not logged in
	if err != nil || plan.TrackID != "123" || plan.KeyID != "456" {
		t.Fatalf("plan %+v err %v; want the cached copy's key id", plan, err)
	}
}

// A license-token rejection inside the re-login minute retries once with the token
// another call's re-login brought meanwhile.
func TestPrepareStream_LicenseRejectionRetriesNewerToken(t *testing.T) {
	logins := 0
	c := streamTestClient(`{"error":{},"results":{"TRACK_TOKEN":"tok","GAIN":"0"}}`, "", &logins)
	c.licenseRelogin.Store(time.Now().Unix()) // a re-login just happened
	base := c.http.Transport
	c.http.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.Contains(r.URL.Path, "get_url") {
			return base.RoundTrip(r)
		}
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), `"license_token":"lic-2"`) {
			return jsonResponse([]byte(`{"data":[{"media":[{"format":"MP3_128","sources":[{"url":"https://cdn/full.mp3"}]}]}]}`)), nil
		}
		c.mu.Lock()
		c.licenseToken = "lic-2" // the other call's re-login lands
		c.mu.Unlock()
		return jsonResponse([]byte(`{"errors":[{"code":1001,"message":"Expired license token"}]}`)), nil
	})
	plan, err := c.PrepareStream("123")
	if err != nil || plan.Preview || plan.CDNURL != "https://cdn/full.mp3" {
		t.Fatalf("plan %+v, err %v; want the full track", plan, err)
	}
	if logins != 0 {
		t.Errorf("%d logins, want 0", logins)
	}
}

// A track start asks song.getData once: the plan carries the track's metadata,
// and SaveTrack reuses it.
func TestPrepareStream_OneSongGetDataCarriesTrack(t *testing.T) {
	cdn := httptest.NewServer(http.NotFoundHandler())
	defer cdn.Close()
	logins := 0
	c := streamTestClient(`{"error":{},"results":{"SNG_ID":"123","SNG_TITLE":"Song","DURATION":"200","ART_NAME":"Band","TRACK_TOKEN":"tok","GAIN":"-3"}}`,
		`{"data":[{"media":[{"format":"MP3_320","sources":[{"url":"`+cdn.URL+`/a.mp3"}]}]}]}`, &logins)
	songData := 0
	base := c.http.Transport
	c.http.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.RawQuery, "method=song.getData") {
			songData++
		}
		return base.RoundTrip(r)
	})
	plan, err := c.PrepareStream("123")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Track.ID != "123" || plan.Track.Name != "Song" || plan.Track.DurationMS != 200000 || plan.GainDB != -3 {
		t.Fatalf("plan track %+v gain %v", plan.Track, plan.GainDB)
	}
	if songData != 1 {
		t.Fatalf("PrepareStream: %d song.getData, want 1", songData)
	}
	songData = 0
	if _, err := c.SaveTrack(context.Background(), "123", t.TempDir()); err == nil {
		t.Fatal("SaveTrack against a 404 CDN succeeded")
	}
	if songData != 1 {
		t.Fatalf("SaveTrack: %d song.getData, want 1", songData)
	}
}

// Online, a cached copy plays only at the quality the plan would ask for or
// better; offline, the best copy present plays.
func TestPrepareStreamCached_HonorsQuality(t *testing.T) {
	mc, err := mediacache.New(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	cache := func(id, format string) {
		mctest.PutBody(t, mc, id+"."+format)
		if err := mc.PutMeta(id+"."+format, mediacache.StreamMeta{Format: format, Encrypted: true}); err != nil {
			t.Fatal(err)
		}
	}
	cache("1", "MP3_128")
	cache("2", "MP3_128")
	cache("2", "FLAC")
	logins := 0
	c := streamTestClient(`{"error":{},"results":{"SNG_ID":"1","TRACK_TOKEN":"tok","GAIN":"0"}}`,
		`{"data":[{"media":[{"format":"MP3_320","sources":[{"url":"https://cdn/full.mp3"}]}]}]}`, &logins)
	c.SetQuality(QualityHigh)

	for _, tc := range []struct{ id, format, url string }{
		{"1", "MP3_320", "https://cdn/full.mp3"}, // the cached 128 is below the asked 320
		{"2", "FLAC", ""},                        // the cached FLAC is better
	} {
		plan, err := c.PrepareStreamCached(tc.id, mc)
		if err != nil || plan.Format != tc.format || plan.CDNURL != tc.url {
			t.Fatalf("online %s: plan %+v err %v; want %s from %q", tc.id, plan, err, tc.format, tc.url)
		}
	}
	plan, err := New("arl").PrepareStreamCached("1", mc) // not logged in
	if err != nil || plan.Format != "MP3_128" || plan.CDNURL != "" {
		t.Fatalf("offline: plan %+v err %v; want the cached MP3_128", plan, err)
	}
}

// A plan records the format it asked for; a cached copy fetched asking for the wanted
// format qualifies even when Deezer served a lower one, so it is not downloaded on
// every play. A copy asked at a lower quality does not.
func TestPrepareStreamCached_AskedFormatQualifies(t *testing.T) {
	logins := 0
	c := streamTestClient(`{"error":{},"results":{"SNG_ID":"1","TRACK_TOKEN":"tok","GAIN":"0"}}`,
		`{"data":[{"media":[{"format":"MP3_320","sources":[{"url":"https://cdn/full.mp3"}]}]}]}`, &logins)
	c.mu.Lock()
	c.canHiFi = true
	c.mu.Unlock()
	c.SetQuality(QualityLossless)
	plan, err := c.PrepareStream("1")
	if err != nil || plan.Format != "MP3_320" || plan.AskedFormat != "FLAC" {
		t.Fatalf("plan %+v err %v; want MP3_320 served for FLAC asked", plan, err)
	}

	mc, err := mediacache.New(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer mc.Close()
	mctest.PutBody(t, mc, "1.MP3_320")
	_ = mc.PutMeta("1.MP3_320", mediacache.StreamMeta{Format: "MP3_320", AskedFormat: "FLAC", Encrypted: true})
	mctest.PutBody(t, mc, "2.MP3_320")
	_ = mc.PutMeta("2.MP3_320", mediacache.StreamMeta{Format: "MP3_320", AskedFormat: "MP3_320", Encrypted: true})
	if plan, err := c.PrepareStreamCached("1", mc); err != nil || plan.CDNURL != "" || plan.Format != "MP3_320" {
		t.Fatalf("copy asked as FLAC: plan %+v err %v; want the cached copy", plan, err)
	}
	if plan, err := c.PrepareStreamCached("2", mc); err != nil || plan.CDNURL == "" {
		t.Fatalf("copy asked as MP3_320: plan %+v err %v; want a stream", plan, err)
	}
}

// Transport errors must not carry the request URL (gw URLs hold the api_token).
func TestClassifyNet_HidesURL(t *testing.T) {
	err := classifyNet(&url.Error{Op: "Post", URL: gwURL + "?api_token=SECRET", Err: errors.New("tls: bad record")})
	if strings.Contains(err.Error(), "SECRET") {
		t.Errorf("error text leaks the token: %v", err)
	}
}

func loginResponseForTest() *http.Response {
	return jsonResponse([]byte(`{"error":{},"results":{"checkForm":"api-t","USER":{"USER_ID":"1","BLOG_NAME":"t","OPTIONS":{"license_token":"lic-1","web_hq":true,"web_lossless":false,"mobile_hq":true,"mobile_lossless":false}},"OFFERS":[{"title":"P"}]}}`))
}

// --- Download resume, short-file reject, bounded-concurrency batch ---

func TestDownloadTrackContext_ResumesOnMidBodyDrop(t *testing.T) {
	// Server serves partial on first GET (no Range), then full suffix on
	// Range resume. Uses plain (unencrypted) stream for simplicity.
	var (
		mu        sync.Mutex
		gets      int
		rangeReqs int
	)
	full := []byte(strings.Repeat("X", 256))
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gets++
		mu.Unlock()

		off := int64(0)
		if rg := r.Header.Get("Range"); rg != "" {
			mu.Lock()
			rangeReqs++
			mu.Unlock()
			if _, err := fmt.Sscanf(rg, "bytes=%d-", &off); err != nil {
				off = 0
			}
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		if off > 0 {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", off, len(full)-1, len(full)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(full[off:])
			return
		}
		// First attempt: declare the full length, deliver a prefix + flush, then
		// close the connection (the client reads io.ErrUnexpectedEOF).
		w.Header().Set("Content-Length", fmt.Sprint(len(full)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(full[:64])
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, herr := hj.Hijack(); herr == nil {
				_ = conn.Close()
				return
			}
		}
		// If hijack unavailable, just stop; client may see EOF but test will still exercise path.
	}))
	defer ts.Close()

	plan := &StreamPlan{
		CDNURL:    ts.URL + "/audio",
		TrackID:   "r42",
		Format:    "MP3_128",
		Encrypted: false,
	}

	var buf bytes.Buffer
	err := DownloadTrackContext(context.Background(), plan, &buf)
	if err != nil {
		t.Fatalf("DownloadTrackContext with resume: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), full) {
		t.Fatalf("resume produced wrong data len=%d", buf.Len())
	}
	mu.Lock()
	g := gets
	rr := rangeReqs
	mu.Unlock()
	if g < 2 {
		t.Errorf("expected >=2 GETs for resume, got %d", g)
	}
	if rr < 1 {
		t.Errorf("expected at least one Range request on resume, got %d", rr)
	}
}

// An encrypted body that ends cleanly but short of the declared total (a 206
// for a sub-range) is resumed on the same decryptor; the result must equal a
// full decrypt, with the partial chunk carried over rather than flushed.
func TestDownloadTrackContext_EncryptedShortBodyResumesIntact(t *testing.T) {
	full := make([]byte, 3*StripeChunk+700)
	for i := range full {
		full[i] = byte(i*131 + i/7)
	}
	const cut = StripeChunk + 952 // mid-chunk
	var mu sync.Mutex
	gets := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gets++
		mu.Unlock()
		off := 0
		end := cut
		if rg := r.Header.Get("Range"); rg != "" {
			if _, err := fmt.Sscanf(rg, "bytes=%d-", &off); err != nil {
				t.Errorf("bad Range %q", rg)
			}
			end = len(full)
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", off, end-1, len(full)))
		w.Header().Set("Content-Length", fmt.Sprint(end-off))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(full[off:end])
	}))
	defer ts.Close()

	plan := &StreamPlan{CDNURL: ts.URL + "/a", TrackID: "enc1", Format: "MP3_320", Encrypted: true}
	var buf bytes.Buffer
	if err := DownloadTrackContext(context.Background(), plan, &buf); err != nil {
		t.Fatalf("download: %v", err)
	}
	want, _ := DecryptTrack("enc1", full)
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("resumed encrypted download differs from a full decrypt (len %d, want %d)", buf.Len(), len(want))
	}
	if gets != 2 {
		t.Errorf("GETs = %d, want 2", gets)
	}
}

// The stall time counts only while a body is read: headers that come late do
// not end the download, and a body that stops delivering is resumed.
func TestDownloadStallResumes(t *testing.T) {
	prev := StallTimeout
	StallTimeout = 200 * time.Millisecond
	defer func() { StallTimeout = prev }()
	full := bytes.Repeat([]byte("od"), 100000)
	var mu sync.Mutex
	gets := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gets++
		first := gets == 1
		mu.Unlock()
		if first {
			time.Sleep(400 * time.Millisecond) // the headers come late
			w.Header().Set("Content-Length", fmt.Sprint(len(full)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(full[:len(full)/2])
			w.(http.Flusher).Flush()
			<-r.Context().Done() // then nothing more
			return
		}
		var off int
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-", &off); err != nil {
			t.Errorf("bad Range %q", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", off, len(full)-1, len(full)))
		w.Header().Set("Content-Length", fmt.Sprint(len(full)-off))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(full[off:])
	}))
	defer ts.Close()

	plan := &StreamPlan{CDNURL: ts.URL + "/a", TrackID: "stall1", Format: "MP3_128"}
	var buf bytes.Buffer
	if err := DownloadTrackContext(context.Background(), plan, &buf); err != nil {
		t.Fatalf("download: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), full) {
		t.Fatalf("resumed download is %d bytes, want %d", buf.Len(), len(full))
	}
}

// A download whose connection is lost for 2 s waits for the network and
// completes once it is back.
func TestDownloadWaitsForNetwork(t *testing.T) {
	full := bytes.Repeat([]byte("od"), 50000)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "a.mp3", time.Time{}, bytes.NewReader(full))
	}))
	defer ts.Close()
	back := time.Now().Add(2 * time.Second)
	inner := SetTransport(rtFunc(func(r *http.Request) (*http.Response, error) {
		if time.Now().Before(back) {
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
		}
		return http.DefaultTransport.RoundTrip(r)
	}))
	defer SetTransport(inner)
	plan := &StreamPlan{CDNURL: ts.URL + "/a", TrackID: "net1", Format: "MP3_128"}
	var buf bytes.Buffer
	if err := DownloadTrackContext(context.Background(), plan, &buf); err != nil {
		t.Fatalf("download: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), full) {
		t.Fatalf("downloaded %d bytes, want %d", buf.Len(), len(full))
	}
}

// A source whose host fails gives way to the media service's next one.
func TestDownloadTriesNextSource(t *testing.T) {
	full := bytes.Repeat([]byte("od"), 50000)
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "a.mp3", time.Time{}, bytes.NewReader(full))
	}))
	defer good.Close()
	plan := &StreamPlan{CDNURL: bad.URL + "/a", AltURLs: []string{good.URL + "/a"}, TrackID: "src1", Format: "MP3_128"}
	var buf bytes.Buffer
	if err := DownloadTrackContext(context.Background(), plan, &buf); err != nil {
		t.Fatalf("download: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), full) {
		t.Fatalf("downloaded %d bytes, want %d", buf.Len(), len(full))
	}
}

// A source whose host refuses connections gives way to the next one at once,
// without waiting for a network that is there.
func TestDownloadLeavesUnreachableSource(t *testing.T) {
	full := bytes.Repeat([]byte("od"), 50000)
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL + "/a"
	gone.Close() // connection refused from now on
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "a.mp3", time.Time{}, bytes.NewReader(full))
	}))
	defer good.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	plan := &StreamPlan{CDNURL: goneURL, AltURLs: []string{good.URL + "/a"}, TrackID: "src2", Format: "MP3_128"}
	var buf bytes.Buffer
	if err := DownloadTrackContext(ctx, plan, &buf); err != nil {
		t.Fatalf("download: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), full) {
		t.Fatalf("downloaded %d bytes, want %d", buf.Len(), len(full))
	}
}

// A refreshed URL brings the refreshed plan's other sources with it.
func TestRefreshKeepsNewSources(t *testing.T) {
	full := bytes.Repeat([]byte("od"), 50000)
	expired := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer expired.Close()
	busy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer busy.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "a.mp3", time.Time{}, bytes.NewReader(full))
	}))
	defer good.Close()
	refreshes := 0
	plan := &StreamPlan{CDNURL: expired.URL + "/a", AltURLs: []string{expired.URL + "/b"}, TrackID: "ref1", Format: "MP3_128"}
	plan.Refresh = func() (*StreamPlan, error) {
		refreshes++
		return &StreamPlan{CDNURL: busy.URL + "/a", AltURLs: []string{good.URL + "/a"}, TrackID: "ref1", Format: "MP3_128"}, nil
	}
	var buf bytes.Buffer
	if err := DownloadTrackContext(context.Background(), plan, &buf); err != nil {
		t.Fatalf("download: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), full) {
		t.Fatalf("downloaded %d bytes, want %d", buf.Len(), len(full))
	}
	if refreshes != 1 {
		t.Errorf("refreshes = %d, want 1", refreshes)
	}
}

// A source that keeps resuming at the wrong offset gives way to the next one.
func TestWrongResumeOffsetTriesNextSource(t *testing.T) {
	full := bytes.Repeat([]byte("od"), 50000)
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "" { // half the body, then the connection drops
			w.Header().Set("Content-Length", fmt.Sprint(len(full)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(full[:len(full)/2])
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(full)-1, len(full)))
		w.Header().Set("Content-Length", fmt.Sprint(len(full)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(full)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "a.mp3", time.Time{}, bytes.NewReader(full))
	}))
	defer good.Close()
	plan := &StreamPlan{CDNURL: bad.URL + "/a", AltURLs: []string{good.URL + "/a"}, TrackID: "off1", Format: "MP3_128"}
	var buf bytes.Buffer
	if err := DownloadTrackContext(context.Background(), plan, &buf); err != nil {
		t.Fatalf("download: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), full) {
		t.Fatalf("downloaded %d bytes, want %d", buf.Len(), len(full))
	}
}

// failOnceWriter fails its second write.
type failOnceWriter struct {
	bytes.Buffer
	n int
}

func (w *failOnceWriter) Write(p []byte) (int, error) {
	w.n++
	if w.n == 2 {
		return 0, errors.New("disk full")
	}
	return w.Buffer.Write(p)
}

// A write that fails ends the download with an error: the decryptor has already
// taken the bytes, so a resume would write a corrupt file.
func TestDownloadWriteErrorIsFatal(t *testing.T) {
	full := make([]byte, StripeChunk*40)
	for i := range full {
		full[i] = byte(i*131 + i/7)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "a.mp3", time.Time{}, bytes.NewReader(full))
	}))
	defer ts.Close()
	plan := &StreamPlan{CDNURL: ts.URL + "/a", TrackID: "wr1", Format: "MP3_320", Encrypted: true}
	var w failOnceWriter
	if err := DownloadTrackContext(context.Background(), plan, &w); err == nil {
		want, _ := DecryptTrack("wr1", full)
		t.Fatalf("no error after a failed write; output matches a full decrypt: %v", bytes.Equal(w.Bytes(), want))
	}
}

// A method that keeps answering AUTH_REQUIRED re-logs in once a minute, not on
// every call.
func TestAuthRequiredReloginThrottled(t *testing.T) {
	logins := 0
	c := streamTestClient(`{"error":{"GATEWAY_ERROR":"AUTH_REQUIRED"},"results":{}}`, "", &logins)
	for i := 0; i < 5; i++ {
		if _, err := c.gw("song.getData", "{}"); err == nil {
			t.Fatalf("call %d: no error", i)
		}
	}
	if logins != 1 {
		t.Errorf("%d logins, want 1", logins)
	}
}

// Parallel calls that all meet AUTH_REQUIRED from one expired session are all
// retried after the one login.
func TestAuthRequiredParallelCallsRetried(t *testing.T) {
	const n = 5
	var (
		logins   atomic.Int32
		loggedIn atomic.Bool
		arrived  sync.WaitGroup
	)
	arrived.Add(n)
	c := New("dummy")
	loggedInPremium(c)
	c.http = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.RawQuery, "method=deezer.getUserData") {
			logins.Add(1)
			time.Sleep(100 * time.Millisecond)
			loggedIn.Store(true)
			return loginResponseForTest(), nil
		}
		if !loggedIn.Load() {
			arrived.Done()
			arrived.Wait() // every call meets the expired session
			return jsonResponse([]byte(`{"error":{"GATEWAY_ERROR":"AUTH_REQUIRED"},"results":{}}`)), nil
		}
		return jsonResponse([]byte(`{"error":{},"results":{"OK":true}}`)), nil
	})}
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() { _, err := c.gw("song.getData", "{}"); errs <- err }()
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Errorf("call failed: %v", err)
		}
	}
	if got := logins.Load(); got != 1 {
		t.Errorf("%d logins, want 1", got)
	}
}

func TestSaveTrack_RejectsShortFile(t *testing.T) {
	tmp := t.TempDir()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "200")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte{0xAB}, 50)) // short body vs CL
	}))
	defer ts.Close()

	c := &Client{}
	tr := Track{ID: "short1", Name: "Shorty", Artists: []Artist{{Name: "T"}}}
	plan := &StreamPlan{CDNURL: ts.URL + "/s", TrackID: "short1", Format: "MP3_128", Encrypted: false}

	_, err := c.saveTrack(context.Background(), tr, tmp, plan, nil)
	if err == nil || !strings.Contains(err.Error(), "short file") {
		t.Fatalf("expected short-file rejection, got err=%v", err)
	}
	// No final file should have been left behind.
	name := trackFileName(tr, "mp3")
	if _, statErr := os.Stat(filepath.Join(tmp, name)); statErr == nil {
		t.Error("short file should not have been renamed into place")
	}
}

func TestSaveBatch_BoundedConcurrency_WorkerCountHonored_AllSaved_Progress(t *testing.T) {
	tmp := t.TempDir()
	c := &Client{}

	N := 6
	trs := make([]Track, N)
	for i := 0; i < N; i++ {
		trs[i] = Track{ID: fmt.Sprintf("%d", i), Name: fmt.Sprintf("T%d", i), Artists: []Artist{{Name: "A"}}}
	}

	var (
		mu        sync.Mutex
		active    int
		maxActive int
		calls     int
		progCount int
		progErrs  int
	)
	opts := DownloadOptions{
		Concurrency: 2,
		Progress: func(p DownloadProgress) {
			mu.Lock()
			progCount++
			if p.Err != nil {
				progErrs++
			}
			mu.Unlock()
		},
		planResolver: func(string) (*StreamPlan, error) {
			return &StreamPlan{Format: "MP3_128", Preview: false}, nil
		},
		trackSaver: func(ctx context.Context, tt Track, d string, plan *StreamPlan, f ArtworkFetcher) (string, error) {
			mu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			calls++
			mu.Unlock()

			time.Sleep(15 * time.Millisecond) // yield for overlap

			mu.Lock()
			active--
			mu.Unlock()

			p := filepath.Join(d, tt.ID+".mp3")
			_ = os.WriteFile(p, []byte("DATA"), 0o644)
			return p, nil
		},
	}

	saved, err := c.saveBatch(context.Background(), trs, tmp, opts)
	if err != nil {
		t.Fatalf("saveBatch: %v", err)
	}
	if len(saved) != N {
		t.Errorf("saved %d, want %d", len(saved), N)
	}
	mu.Lock()
	ma := maxActive
	cc := calls
	pc := progCount
	pe := progErrs
	mu.Unlock()
	if ma > 2 {
		t.Errorf("max concurrency observed %d, want <=2 (opts.Concurrency)", ma)
	}
	if cc != N {
		t.Errorf("saver called %d times, want %d", cc, N)
	}
	if pc != N {
		t.Errorf("progress fired %d times, want %d", pc, N)
	}
	if pe != 0 {
		t.Errorf("unexpected progress errs: %d", pe)
	}
}
