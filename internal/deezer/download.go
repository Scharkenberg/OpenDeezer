package deezer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	pdlog "github.com/Scharkenberg/PurpleDisc/v4/internal/log"
)

// ErrPremiumRequired is returned by SaveTrack when the account is not entitled to full
// tracks (Deezer Free). Callers check it with errors.Is to hide the download action.
var ErrPremiumRequired = errors.New("downloads require a paid Deezer plan")

// ErrPreviewOnly is returned by SaveTrack when Deezer offers only the 30-second preview
// for a track; a clip is never saved under a full-track filename.
var ErrPreviewOnly = errors.New("full track not available for download")

// IsPremiumRequired reports whether err is the premium-required download gate.
func IsPremiumRequired(err error) bool { return errors.Is(err, ErrPremiumRequired) }

// DownloadProgress reports status for one track in a batch (SaveAlbum / SavePlaylist).
// It is delivered to the optional Progress callback even for skipped or errored tracks.
type DownloadProgress struct {
	Index   int // 1-based index in the batch
	Total   int
	TrackID string
	Title   string
	Err     error // per-track error (nil on success or skip)
}

// DownloadOptions tunes batch downloads. All fields are optional.
type DownloadOptions struct {
	SkipExisting bool
	// Progress is invoked after each track (success, skip, or error). Safe to be nil.
	Progress func(DownloadProgress)
	// ArtworkFetcher overrides cover art retrieval (for tests; nil = default Deezer CDN).
	ArtworkFetcher ArtworkFetcher

	// Concurrency is the maximum number of tracks to download in parallel
	// inside SaveAlbum/SavePlaylist (bounded worker pool). <=0 selects the
	// default (3).
	Concurrency int

	// trackSaver is internal hook for tests (same package) to inject a fake
	// downloader. Zero means use the real client-backed implementation.
	trackSaver trackDownloadFunc

	// planResolver is internal hook for tests (same package) to supply
	// *StreamPlan for filename/ext/preview decisions without calling
	// c.PrepareStream (no network in tests).
	planResolver planResolverFunc
}

// trackDownloadFunc is the injectable per-track saver used by batch code (tests only).
// plan is the stream plan when the caller already resolved it; nil resolves it.
type trackDownloadFunc func(ctx context.Context, t Track, dir string, plan *StreamPlan, fetch ArtworkFetcher) (string, error)

// planResolverFunc is the signature for the injectable plan getter used by
// batch pre-checks for filename and preview decisions. Same package only.
type planResolverFunc func(string) (*StreamPlan, error)

// testCreateTemp is a test-only hook (same package) so concurrency tests
// can observe the distinct temp paths created by concurrent saveTrack calls.
// Production always uses os.CreateTemp when this is nil.
var testCreateTemp func(dir, pattern string) (*os.File, error)

// Resume tuning for a dropped or torn CDN body, for the player's streams and downloads:
// failed reopens without progress, URL refreshes after a 403/410, the base backoff
// (times the attempt) and the progress that resets the attempt count.
const (
	MaxResumeAttempts  = 3
	MaxRefreshAttempts = 2
	ResumeBackoff      = 300 * time.Millisecond
	MinResumeProgress  = 64 * 1024
)

// DownloadTrack fetches and decrypts a Deezer stream to w. plan must come from
// [Client.PrepareStream] (tracks) or [Client.PodcastEpisodeStream] (episodes,
// which are unencrypted). The bytes written form a valid MP3 or FLAC file.
func DownloadTrack(plan *StreamPlan, w io.Writer) error {
	return DownloadTrackContext(context.Background(), plan, w)
}

// DownloadTrackContext is [DownloadTrack] with cancellation. It resumes dropped
// transfers with Range and If-Range, retries with backoff, refreshes the URL on 403/410
// (same stream only) and rejects a body shorter than Content-Length.
func DownloadTrackContext(ctx context.Context, plan *StreamPlan, w io.Writer) error {
	_, _, err := downloadToWriter(ctx, plan, w)
	return err
}

// downloadToWriter is the resilient fetcher used by Download* and by the
// Save* paths (via saveTrack). It returns bytes written to w, the last-seen
// content length (for the short-file check), and error.
func downloadToWriter(ctx context.Context, plan *StreamPlan, w io.Writer) (written, contentLen int64, err error) {
	if plan == nil || plan.CDNURL == "" {
		return 0, 0, fmt.Errorf("download: empty stream plan")
	}

	var dec *StripeDecryptor
	if plan.Encrypted {
		var derr error
		dec, derr = NewStripeDecryptor(plan.CipherID())
		if derr != nil {
			return 0, 0, derr
		}
	}
	var plain []byte
	var waited time.Duration // spent waiting for the network
	written, contentLen, err = Resume{
		Plan: plan,
		URL:  plan.CDNURL,
		Pump: func(body io.Reader, buf []byte, consumed *int64, skip int64) (bool, error) {
			return pumpDownloadBody(body, buf, &plain, dec, w, consumed, skip)
		},
		// The decryptor has taken the bytes of a failed write, so a resume would corrupt
		// the file.
		Fatal: func(err error) bool { return errors.As(err, new(writeError)) },
		WaitNetwork: func(n int) bool {
			d := NetRetryDelays[min(n, len(NetRetryDelays)-1)]
			if waited+d > NetDryWait {
				return false
			}
			waited += d
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return false
			case <-t.C:
				return true
			}
		},
	}.Run(ctx)
	// The trailing partial chunk is plaintext; flushed only once the whole body is in,
	// so a resume keeps the decryptor's chunk state.
	if err == nil && dec != nil {
		if tail := dec.Finish(plain[:0]); len(tail) > 0 {
			if _, werr := w.Write(tail); werr != nil {
				return written, contentLen, werr
			}
		}
	}
	return written, contentLen, err
}

// A reopen that fails for want of a network is tried again without counting against
// MaxResumeAttempts, after NetRetryDelays (the last one repeating). The player keeps
// trying while audio is buffered and for at most NetDryWait once it ran dry; a download
// waits at most NetDryWait in all.
var (
	NetRetryDelays = []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second}
	NetDryWait     = 30 * time.Second
)

// Resume is one CDN body fetched with resumes, for the player's streams and downloads:
// a torn body is reopened where it stopped (Range, with the first response's validator
// as If-Range), an expired URL (403/410) is renewed through Plan.Refresh for the same
// stream only, and a lost network is left to WaitNetwork instead of the attempt budget.
type Resume struct {
	Plan      *StreamPlan
	URL       string
	Refreshes int // URL renewals already made
	// Pump feeds one body into the sink through buf, dropping skip leading bytes and
	// advancing *consumed; completed is true only at a clean end.
	Pump func(body io.Reader, buf []byte, consumed *int64, skip int64) (completed bool, err error)
	// WaitNetwork waits before the n-th retry after a lost network and reports whether to
	// try again.
	WaitNetwork func(n int) bool
	// Opened, when set, runs after each response.
	Opened func()
	// Total, when set, gets the entity's full length from the first response.
	Total func(int64)
	// Fatal, when set, reports a pump error that ends the fetch at once.
	Fatal func(error) bool
}

// Run fetches until the body is complete (nil error) and returns the bytes consumed and
// the declared length; once ctx ends it returns ctx's cause.
func (r Resume) Run(ctx context.Context) (consumed, total int64, err error) {
	url := r.URL
	var alt []string // the plan's other sources, tried when a host fails
	if r.Plan != nil {
		alt = r.Plan.AltURLs
	}
	buf := make([]byte, 64*1024)
	attempts, netTries, netFails, refreshes := 0, 0, 0, r.Refreshes
	validator, validatorSet, lenSet := "", false, false
	// nextSource moves to the next source from the same offset, with a fresh budget.
	nextSource := func() bool {
		if len(alt) == 0 {
			return false
		}
		url, alt = alt[0], alt[1:]
		attempts, validator, validatorSet = 0, "", false
		return true
	}
	// rotate keeps the sources as a ring for unreachable hosts: the current one goes
	// to the end and the next one is tried.
	rotate := func() {
		if len(alt) > 0 {
			next := alt[0]
			alt = append(alt[1:len(alt):len(alt)], url) // a copy: the plan's slice stays as it is
			url = next
			validator, validatorSet = "", false
		}
	}
	for {
		if ctx.Err() != nil {
			return consumed, total, context.Cause(ctx)
		}
		startOff := consumed
		resp, oerr := OpenStream(ctx, url, consumed, validator)
		if oerr != nil {
			if ctx.Err() != nil {
				return consumed, total, context.Cause(ctx)
			}
			if IsNoNetwork(oerr) {
				// One wait per round: only once every source has failed in a row.
				netFails++
				if netFails%(len(alt)+1) == 0 {
					if !r.WaitNetwork(netTries) {
						return consumed, total, classifyNet(oerr)
					}
					netTries++
				}
				rotate()
				continue
			}
			attempts++
			if attempts > MaxResumeAttempts {
				if nextSource() {
					continue
				}
				return consumed, total, classifyNet(oerr)
			}
			Backoff(ctx, attempts)
			continue
		}
		netTries, netFails = 0, 0
		if r.Opened != nil {
			r.Opened()
		}
		switch {
		case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusGone:
			resp.Body.Close()
			if u2, a2 := RefreshPlanURL(r.Plan, &refreshes); u2 != "" {
				url, alt = u2, a2
				continue
			}
			return consumed, total, fmt.Errorf("CDN returned %s", resp.Status)
		case resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent:
			resp.Body.Close()
			if nextSource() {
				continue
			}
			return consumed, total, fmt.Errorf("CDN returned %s", resp.Status)
		}
		// A resume 206 must start exactly at the consumed offset; a mismatched or
		// unparsable Content-Range start counts as a failed attempt.
		if resp.StatusCode == http.StatusPartialContent && consumed > 0 {
			if start := ContentRangeStart(resp); start != consumed {
				resp.Body.Close()
				attempts++
				if attempts > MaxResumeAttempts {
					if nextSource() {
						continue
					}
					return consumed, total, fmt.Errorf("CDN resume at %d answered Content-Range start %d", consumed, start)
				}
				Backoff(ctx, attempts)
				continue
			}
		}
		if !lenSet {
			if total = TotalLength(resp); total > 0 && r.Total != nil {
				r.Total(total)
			}
			lenSet = true
		}
		if !validatorSet {
			validator = ResponseValidator(resp)
			validatorSet = true
		}
		// The server ignored the Range (200 full): skip the bytes already consumed, but
		// only while the validator still matches; a changed entity fails the fetch.
		var skip int64
		if consumed > 0 && resp.StatusCode == http.StatusOK {
			if validator != "" && ResponseValidator(resp) != validator {
				resp.Body.Close()
				return consumed, total, fmt.Errorf("stream entity changed during resume (validator mismatch)")
			}
			skip = consumed
		}
		// A body that delivers nothing for StallTimeout is closed, which ends the pump
		// with an error: a dropped connection, resumed below (the transport only bounds
		// setup and headers).
		stall := time.AfterFunc(StallTimeout, func() { resp.Body.Close() })
		completed, rerr := r.Pump(StallReader{resp.Body, stall, StallTimeout}, buf, &consumed, skip)
		stall.Stop()
		resp.Body.Close()
		// A body that ends under the declared length is a dropped connection.
		short := total > 0 && consumed < total
		if completed && !short {
			return consumed, total, nil
		}
		if ctx.Err() != nil {
			return consumed, total, context.Cause(ctx)
		}
		if rerr != nil && r.Fatal != nil && r.Fatal(rerr) {
			return consumed, total, rerr
		}
		// Torn mid-body: resume. Progress past MinResumeProgress refreshes the budget, so
		// long streams with sporadic drops keep going while a byte-dripping host runs out.
		if consumed-startOff >= MinResumeProgress {
			attempts = 0
		}
		attempts++
		if attempts > MaxResumeAttempts {
			switch {
			case short:
				return consumed, total, fmt.Errorf("download: short file (got %d, declared %d)", consumed, total)
			case rerr != nil:
				return consumed, total, rerr
			}
			return consumed, total, io.ErrUnexpectedEOF
		}
		Backoff(ctx, attempts)
	}
}

// StallTimeout is how long a stream or download body may deliver nothing
// before it counts as a dropped connection and resumes (a variable so tests
// can shorten it).
var StallTimeout = 15 * time.Second

// StallReader reads R and re-arms Timer for After on every read that
// delivers bytes, so Timer fires only once R has delivered nothing for After.
type StallReader struct {
	R     io.Reader
	Timer *time.Timer
	After time.Duration
}

func (s StallReader) Read(p []byte) (int, error) {
	n, err := s.R.Read(p)
	if n > 0 {
		s.Timer.Reset(s.After)
	}
	return n, err
}

// OpenStream issues the CDN GET, from offset (Range, with validator as If-Range when
// set); ctx cancels it. It sends the web player's User-Agent, which podcast hosts need.
func OpenStream(ctx context.Context, url string, offset int64, validator string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		if validator != "" {
			req.Header.Set("If-Range", validator)
		}
	}
	resp, err := HTTPClient(0).Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// writeError is a failed write to the download's sink.
type writeError struct{ error }

func (e writeError) Unwrap() error { return e.error }

// pumpDownloadBody feeds one response body (after skip) into w, advancing the
// decryptor (if any) and consumed count. Returns completed=true only for io.EOF.
func pumpDownloadBody(body io.Reader, buf []byte, plain *[]byte, dec *StripeDecryptor, w io.Writer, consumed *int64, skip int64) (completed bool, err error) {
	for {
		n, rerr := body.Read(buf)
		if n > 0 {
			b := buf[:n]
			if skip > 0 {
				d := skip
				if d > int64(len(b)) {
					d = int64(len(b))
				}
				b = b[d:]
				skip -= d
			}
			if len(b) > 0 {
				if dec != nil {
					*plain = dec.Feed(b, (*plain)[:0])
					if _, werr := w.Write(*plain); werr != nil {
						return false, writeError{werr}
					}
					*consumed = dec.Consumed()
				} else {
					if _, werr := w.Write(b); werr != nil {
						return false, writeError{werr}
					}
					*consumed += int64(len(b))
				}
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				return true, nil
			}
			return false, rerr
		}
	}
}

// RefreshPlanURL re-resolves via plan.Refresh (if present) on 403/410, but
// only accepts a plan describing the identical stream (same Format/Encrypted/Preview).
// A changed plan would corrupt the decryptor state or the output file. It returns the
// new plan's URL and its other sources.
func RefreshPlanURL(plan *StreamPlan, count *int) (url string, alt []string) {
	if plan.Refresh == nil || *count >= MaxRefreshAttempts {
		return "", nil
	}
	*count++
	np, err := plan.Refresh()
	if err != nil || np == nil || np.CDNURL == "" ||
		np.Format != plan.Format || np.CipherID() != plan.CipherID() || np.Encrypted != plan.Encrypted || np.Preview != plan.Preview {
		return "", nil
	}
	return np.CDNURL, np.AltURLs
}

// Backoff sleeps ResumeBackoff times attempt, or until ctx is done.
func Backoff(ctx context.Context, attempt int) {
	t := time.NewTimer(ResumeBackoff * time.Duration(attempt))
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// TotalLength returns the full entity size (from Content-Range on 206, else
// Content-Length).
func TotalLength(resp *http.Response) int64 {
	if resp.StatusCode == http.StatusPartialContent {
		cr := resp.Header.Get("Content-Range")
		if i := strings.LastIndex(cr, "/"); i >= 0 {
			if v, err := strconv.ParseInt(strings.TrimSpace(cr[i+1:]), 10, 64); err == nil && v > 0 {
				return v
			}
		}
		return 0
	}
	if resp.ContentLength > 0 {
		return resp.ContentLength
	}
	return 0
}

// ContentRangeStart parses start of "bytes N-M/T" or returns -1.
func ContentRangeStart(resp *http.Response) int64 {
	cr := strings.TrimSpace(resp.Header.Get("Content-Range"))
	cr = strings.TrimSpace(strings.TrimPrefix(cr, "bytes"))
	dash := strings.IndexByte(cr, '-')
	if dash < 0 {
		return -1
	}
	v, err := strconv.ParseInt(strings.TrimSpace(cr[:dash]), 10, 64)
	if err != nil || v < 0 {
		return -1
	}
	return v
}

// ResponseValidator returns ETag or Last-Modified (for If-Range).
func ResponseValidator(resp *http.Response) string {
	if et := resp.Header.Get("ETag"); et != "" {
		return et
	}
	return resp.Header.Get("Last-Modified")
}

// SaveTrack downloads trackID into dir ("" = working directory), named from title and
// artist, and returns the path. Premium only; preview-only tracks are refused. A tag
// failure is logged and leaves the audio as written.
func (c *Client) SaveTrack(ctx context.Context, trackID, dir string) (string, error) {
	if !c.LoggedIn() {
		return "", fmt.Errorf("not logged in")
	}
	if !c.Account().Premium {
		return "", ErrPremiumRequired
	}
	plan, err := c.PrepareStream(trackID)
	if err != nil {
		return "", err
	}
	if plan.Track.ID == "" {
		return "", fmt.Errorf("track %s: not found", trackID)
	}
	return c.saveTrack(ctx, plan.Track, dir, plan, nil)
}

// saveTrack downloads and tags a Track already in hand, for SaveTrack and the batch
// methods. plan may be the resolved stream plan; nil resolves it here.
func (c *Client) saveTrack(ctx context.Context, t Track, dir string, plan *StreamPlan, fetch ArtworkFetcher) (string, error) {
	if plan == nil {
		var err error
		plan, err = c.PrepareStream(t.ID)
		if err != nil {
			return "", err
		}
	}
	if plan.Preview {
		return "", ErrPreviewOnly
	}

	ext := "mp3"
	if strings.Contains(strings.ToUpper(plan.Format), "FLAC") {
		ext = "flac"
	}
	name := trackFileName(t, ext)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}
	path := filepath.Join(dir, name)

	// Write to a unique temp file (os.CreateTemp, O_EXCL) in the target dir, so concurrent
	// downloads of one track never share a .part and the final rename is atomic. On error
	// only this temp file is removed.
	targetDir := dir
	if targetDir == "" {
		targetDir = "."
	}
	stem := strings.TrimSuffix(name, "."+ext)
	pattern := stem + "-*.part"
	var f *os.File
	var err error
	if testCreateTemp != nil {
		f, err = testCreateTemp(targetDir, pattern)
	} else {
		f, err = os.CreateTemp(targetDir, pattern)
	}
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	_, _, derr := downloadToWriter(ctx, plan, f)
	if derr != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", derr
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}

	// Tag the temp file before it gets its final name; a tag failure never fails the
	// download.
	au := upgradeArtworkURL(t.ArtworkURL)
	if tagErr := tagFile(ctx, tmp, ext, t, au, t.TrackNumber, t.Year, fetch); tagErr != nil {
		pdlog.Warn("download: tagging %s failed (non-fatal, file left as raw audio): %v", path, tagErr)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return path, nil
}

// trackFileName builds a track's file name, with the track id so same-titled tracks
// never collide. The [id] suffix is added after truncating the title part on a rune
// boundary. Batch skip checks and saveTrack must use the same formula.
func trackFileName(t Track, ext string) string {
	desc := t.Name + " - " + t.ArtistLine()
	suffix := " [" + t.ID + "]"
	room := 180 - len(suffix)
	if room < 0 {
		room = 0
	}
	desc = truncateSafe(desc, room)
	cand := desc + suffix
	return sanitizeFilename(cand) + "." + ext
}

// truncateSafe returns at most max bytes of s, stopping at a UTF-8 rune
// boundary so the result is always valid UTF-8.
func truncateSafe(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// Walk runes so we never cut inside a multi-byte sequence.
	var b strings.Builder
	n := 0
	for _, r := range s {
		rl := utf8.RuneLen(r)
		if n+rl > max {
			break
		}
		b.WriteRune(r)
		n += rl
	}
	return b.String()
}

// sanitizeFilename maps characters illegal on common filesystems (Windows is the
// strict one) to underscores and trims to a sane length, so a track title can be
// used verbatim as a filename on every platform.
func sanitizeFilename(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, s)
	s = strings.Trim(s, " .")
	if s == "" {
		s = "track"
	}
	// Cap the base name well under filesystem path limits, truncating on a rune boundary.
	if len(s) > 180 {
		s = strings.TrimSpace(truncateSafe(s, 180))
	}
	return s
}

// SaveAlbum downloads every album track into dir, DownloadOptions.Concurrency at a
// time, premium only. Failed tracks yield an error wrapping [ErrPartialDownload] with
// the saved paths; canceling ctx stops between tracks.
func (c *Client) SaveAlbum(ctx context.Context, albumID, dir string, opts DownloadOptions) ([]string, error) {
	if !c.LoggedIn() {
		return nil, fmt.Errorf("not logged in")
	}
	if !c.Account().Premium {
		return nil, ErrPremiumRequired
	}
	// Skip the network listing for an already canceled batch.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tracks, err := c.AlbumTracks(albumID)
	if err != nil {
		return nil, err
	}
	return c.saveBatch(ctx, tracks, dir, opts)
}

// SavePlaylist downloads every track in the playlist (via PlaylistTracks) to
// files inside dir. Same semantics as SaveAlbum.
func (c *Client) SavePlaylist(ctx context.Context, playlistID, dir string, opts DownloadOptions) ([]string, error) {
	if !c.LoggedIn() {
		return nil, fmt.Errorf("not logged in")
	}
	if !c.Account().Premium {
		return nil, ErrPremiumRequired
	}
	// Skip the network listing for an already canceled batch.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Strict listing: unlike the browse-oriented PlaylistTracks, a mid-fetch
	// page error must fail the download rather than silently truncate it.
	tracks, _, err := c.PlaylistTracksStrict(playlistID)
	if err != nil {
		return nil, err
	}
	return c.saveBatch(ctx, tracks, dir, opts)
}

func (c *Client) saveBatch(ctx context.Context, tracks []Track, dir string, opts DownloadOptions) ([]string, error) {
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}

	saver := opts.trackSaver
	if saver == nil {
		saver = c.saveTrack
	}

	// Consecutive tracks of one album share their cover: it is fetched once
	// while the tracks ask for the same URL. A failed fetch is not kept.
	base := opts.ArtworkFetcher
	if base == nil {
		base = defaultArtworkFetcher
	}
	type cover struct {
		url  string
		once sync.Once
		b    []byte
		err  error
	}
	var coverMu sync.Mutex
	var last *cover
	fetch := func(ctx context.Context, u string) ([]byte, error) {
		coverMu.Lock()
		if last == nil || last.url != u {
			last = &cover{url: u}
		}
		cv := last
		coverMu.Unlock()
		cv.once.Do(func() { cv.b, cv.err = base(ctx, u) })
		if cv.err != nil {
			coverMu.Lock()
			if last == cv {
				last = nil
			}
			coverMu.Unlock()
		}
		return cv.b, cv.err
	}

	// The extension the current quality asks for. A file already there under
	// it is skipped before its stream is resolved.
	wantExt := "mp3"
	if c.wantedFormat() == "FLAC" {
		wantExt = "flac"
	}

	getPlan := opts.planResolver
	if getPlan == nil {
		getPlan = c.PrepareStream
	}

	conc := opts.Concurrency
	if conc <= 0 {
		conc = 3
	}
	n := len(tracks)
	if conc > n {
		conc = n
	}
	if conc < 1 {
		conc = 1
	}

	// Work queue (sends in order; workers may complete out of order).
	type job struct {
		idx int
		tr  Track
	}
	jobs := make(chan job, n)
	for i, tr := range tracks {
		jobs <- job{idx: i, tr: tr}
	}
	close(jobs)

	var (
		mu       sync.Mutex
		saved    []string
		firstErr error
	)
	// report records one track's outcome and calls Progress under mu, so the
	// callback (callers count failures in it) never runs concurrently.
	report := func(prog DownloadProgress, path string) {
		mu.Lock()
		defer mu.Unlock()
		if prog.Err != nil {
			if firstErr == nil {
				firstErr = prog.Err
			}
		} else {
			saved = append(saved, path)
		}
		if opts.Progress != nil {
			opts.Progress(prog)
		}
	}

	var wg sync.WaitGroup
	for w := 0; w < conc; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				prog := DownloadProgress{Index: j.idx + 1, Total: n, TrackID: j.tr.ID, Title: j.tr.Name}
				// Cancellation is checked between tracks.
				if prog.Err = ctx.Err(); prog.Err != nil {
					report(prog, "")
					continue
				}
				if opts.SkipExisting {
					path := filepath.Join(dir, trackFileName(j.tr, wantExt))
					if _, err := os.Stat(path); err == nil {
						report(prog, path)
						continue
					}
				}
				plan, err := getPlan(j.tr.ID)
				if err == nil && plan.Preview {
					err = ErrPreviewOnly
				}
				if err != nil {
					prog.Err = err
					report(prog, "")
					continue
				}

				ext := "mp3"
				if strings.Contains(strings.ToUpper(plan.Format), "FLAC") {
					ext = "flac"
				}
				path := filepath.Join(dir, trackFileName(j.tr, ext))
				if opts.SkipExisting && ext != wantExt { // the track fell back to another format
					if _, err := os.Stat(path); err == nil {
						report(prog, path)
						continue
					}
				}
				path, prog.Err = saver(ctx, j.tr, dir, plan, fetch)
				report(prog, path)
			}
		}()
	}
	wg.Wait()

	// Cancellation takes precedence, as in the sequential pre-check.
	if cerr := ctx.Err(); cerr != nil {
		return saved, cerr
	}
	if firstErr != nil {
		// Double-wrap so callers can match BOTH the ErrPartialDownload
		// sentinel and the concrete first failure via errors.Is/As.
		return saved, fmt.Errorf("%w: %w (%d failed, %d saved)", ErrPartialDownload, firstErr, n-len(saved), len(saved))
	}
	return saved, nil
}

// ErrPartialDownload is returned (wrapped) by SaveAlbum/SavePlaylist when one
// or more tracks could not be downloaded. The first error and counts are
// included in the message; successfully downloaded paths are still returned.
var ErrPartialDownload = errors.New("batch download had failures")
