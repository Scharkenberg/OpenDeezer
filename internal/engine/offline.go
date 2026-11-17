package engine

// Offline music: tracks, albums and playlists pinned for play with no
// network. The media cache keeps their bodies as pinned entries (never
// evicted, outside its size); offline.json in the config dir keeps the pins,
// each collection and each pinned track's wire metadata and stream meta, so a
// lost cache index loses nothing. Pins belong to the account that made them.
// Offline mode requests nothing: plays come from pins and the cache.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/audio"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/bridge"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
	odlog "github.com/Cycl0o0/OpenDeezer/v3/internal/log"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/mediacache"
)

const offlineFileName = "offline.json"

// offlineTrack is one pinned track: its wire metadata and the stream meta of
// its body, set when the body is fetched (or found in the cache).
type offlineTrack struct {
	Track bridge.Track           `json:"track"`
	Meta  *mediacache.StreamMeta `json:"meta,omitempty"`
}

// offlineColl is one pin: an album, a playlist, or a single track (Kind
// "track", ID the track's id), with its tracks in order.
type offlineColl struct {
	User     string   `json:"user"`
	Kind     string   `json:"kind"`
	ID       string   `json:"id"`
	Title    string   `json:"title,omitempty"`
	Cover    string   `json:"cover,omitempty"` // picture URL
	Tracks   []string `json:"tracks"`
	PinnedAt int64    `json:"pinnedAt"` // Unix ms
	Quality  int      `json:"quality"`  // the stream quality when pinned
}

// pin is the string the cache entries of c carry.
func (c offlineColl) pin() string { return c.Kind + ":" + c.ID + "@" + c.User }

type offlineData struct {
	// LastUser is the account that signed in last on this machine: offline
	// at start, its pins are the ones shown and played.
	LastUser    string                  `json:"lastUser,omitempty"`
	Collections []offlineColl           `json:"collections"`
	Tracks      map[string]offlineTrack `json:"tracks"`
}

var (
	offMu     sync.Mutex
	offLoaded bool
	off       offlineData
)

// offlineLocked returns the store, read from offline.json on first use.
func offlineLocked() *offlineData {
	if !offLoaded {
		offLoaded = true
		off = offlineData{}
		if dir, err := config.Dir(); err == nil {
			if b, err := os.ReadFile(filepath.Join(dir, offlineFileName)); err == nil {
				if err := json.Unmarshal(b, &off); err != nil {
					odlog.Warn("%s: %v", offlineFileName, err)
					off = offlineData{}
				}
			}
		}
		if off.Tracks == nil {
			off.Tracks = map[string]offlineTrack{}
		}
	}
	return &off
}

var (
	// saveMu keeps the writes of offline.json in the order of their snapshots.
	saveMu sync.Mutex
	// writeOffline writes offline.json (atomically, synced); tests replace it.
	writeOffline = func(b []byte) error { return config.WriteFile(offlineFileName, string(b)) }
)

// saveOffline writes the store to offline.json: marshalled under offMu,
// written outside it, so readers of the store do not wait for the disk.
func saveOffline() error {
	saveMu.Lock()
	defer saveMu.Unlock()
	offMu.Lock()
	b, err := json.Marshal(offlineLocked())
	offMu.Unlock()
	if err != nil {
		return err
	}
	return writeOffline(b)
}

// pictureKey is the cache key of a stored picture: a hash of its URL.
func pictureKey(url string) string {
	if url == "" {
		return ""
	}
	h := sha256.Sum256([]byte(url))
	return "img-" + hex.EncodeToString(h[:16]) + ".IMG"
}

// wantedPinsLocked returns the pins of every cache entry the store keeps (the
// fetched tracks and the pictures) and the stream meta of those tracks.
func wantedPinsLocked() (map[string][]string, map[string]mediacache.StreamMeta) {
	d := offlineLocked()
	pins := map[string][]string{}
	metas := map[string]mediacache.StreamMeta{}
	add := func(key, pin string) {
		if key != "" {
			pins[key] = append(pins[key], pin)
		}
	}
	for _, c := range d.Collections {
		p := c.pin()
		add(pictureKey(c.Cover), p)
		for _, id := range c.Tracks {
			t := d.Tracks[id]
			add(pictureKey(t.Track.ArtworkURL), p)
			if t.Meta != nil {
				k := id + "." + t.Meta.Format
				add(k, p)
				metas[k] = *t.Meta
			}
		}
	}
	return pins, metas
}

var (
	// applyMu keeps each applyPins' snapshot and SetPins together, so a later
	// snapshot is never overwritten by an earlier one.
	applyMu sync.Mutex
	// pinsSnapshotted runs between the snapshot and SetPins; tests hold it.
	pinsSnapshotted = func() {}
)

// applyPins gives mc's entries the store's pins (and stream meta an index
// rebuilt from the folder lacks).
func applyPins(mc *mediacache.Cache) {
	if mc == nil {
		return // the cache folder could not be opened
	}
	applyMu.Lock()
	defer applyMu.Unlock()
	offMu.Lock()
	pins, metas := wantedPinsLocked()
	offMu.Unlock()
	pinsSnapshotted()
	mc.SetPins(pins, metas)
}

// attachCache hands the cache in use (nil: none) to the player and gives it
// the store's pins. A pinned track offline.json holds no stream meta for (it
// is saved at a job's end, not per track) takes it from its pinned entry in
// the cache's index, which names the body only once it is complete.
func attachCache(c *mediacache.Cache) {
	withPlayer(func(p *audio.Player) {
		if c != nil {
			p.SetStreamCache(c)
		} else {
			p.SetStreamCache(nil)
		}
	})
	if c != nil {
		offMu.Lock()
		d := offlineLocked()
		for id, t := range d.Tracks {
			if m, ok := c.GetMetaForTrack(id); ok && t.Meta == nil && c.Pinned(id+"."+m.Format) {
				t.Meta = &m
				d.Tracks[id] = t
			}
		}
		offMu.Unlock()
	}
	applyPins(c)
}

// anyPinned reports whether any account pinned anything: the cache then stays
// open at a size of 0.
func anyPinned() bool {
	offMu.Lock()
	defer offMu.Unlock()
	return len(offlineLocked().Collections) > 0
}

// pinsChanged saves the store, opens or closes the cache to match (it stays
// open while anything is pinned) and sets its pins.
func pinsChanged() error {
	err := saveOffline()
	applyPins(caches.Current()) // first: a cache that closes evicts what is no longer pinned
	if oerr := caches.Open(); err == nil {
		err = oerr // a cache that opens takes the pins in Attach
	}
	return err
}

// offlineUser is the account whose pins are shown and played: the signed-in
// one, else the last one that signed in on this machine.
func offlineUser() string {
	if c := Client(); c != nil {
		return c.UserID()
	}
	offMu.Lock()
	defer offMu.Unlock()
	return offlineLocked().LastUser
}

// rememberUser records uid as the last account that signed in.
func rememberUser(uid string) {
	offMu.Lock()
	d := offlineLocked()
	changed := d.LastUser != uid
	d.LastUser = uid
	offMu.Unlock()
	if !changed {
		return
	}
	if err := saveOffline(); err != nil {
		odlog.Warn("%s: %v", offlineFileName, err)
	}
}

// ---- offline mode ----

var (
	offlineOn atomic.Bool
	// offlineAuto is set when Init turned offline mode on (no network at
	// sign-in); a later successful Init turns only such a mode off.
	offlineAuto atomic.Bool
)

// ErrOfflineMode is the refusal of anything that would reach Deezer while
// offline mode is on.
var ErrOfflineMode = errors.New("offline mode: nothing is sent to Deezer")

// SetOfflineMode turns offline mode on or off. In it nothing is requested:
// only pinned and cached tracks play, and browsing, likes, playlist edits and
// listen reports are refused. Turning it on stops the fetches that request
// (they leave the list); turning it off starts them again.
func SetOfflineMode(on bool) {
	offlineOn.Store(on)
	offlineAuto.Store(false)
	if on {
		stopNetJobs()
	} else {
		resumeJobs()
	}
}

// stopNetJobs stops the fetches made with a client, the ones that request.
func stopNetJobs() {
	dropJobs(func(j *offlineJob) bool { return j.client != nil && j.running })
}

// OfflineMode reports whether offline mode is on.
func OfflineMode() bool { return offlineOn.Load() }

// netClient is the client for requests: nil before a login and in offline
// mode.
func netClient() *deezer.Client {
	if offlineOn.Load() {
		return nil
	}
	return Client()
}

// Online returns the client for a request to Deezer, or why there is none
// (offline mode, no login).
func Online() (*deezer.Client, error) {
	if offlineOn.Load() {
		return nil, ErrOfflineMode
	}
	if c := Client(); c != nil {
		return c, nil
	}
	return nil, errNotReady
}

// ---- pins ----

// findCollLocked returns the index of the user's pin of kind and id, -1 when
// there is none; user "" matches any account.
func findCollLocked(user, kind, id string) int {
	return slices.IndexFunc(offlineLocked().Collections, func(c offlineColl) bool {
		return (user == "" || c.User == user) && c.Kind == kind && c.ID == id
	})
}

// dropUnusedTracksLocked forgets the tracks no pin holds any more.
func dropUnusedTracksLocked() {
	d := offlineLocked()
	used := map[string]bool{}
	for _, c := range d.Collections {
		for _, id := range c.Tracks {
			used[id] = true
		}
	}
	for id := range d.Tracks {
		if !used[id] {
			delete(d.Tracks, id)
		}
	}
}

// storeTracksLocked records ts' wire metadata, keeping what was fetched.
func storeTracksLocked(ts []deezer.Track) {
	d := offlineLocked()
	for _, t := range ts {
		ot := d.Tracks[t.ID]
		ot.Track = bridge.FromTrack(t)
		d.Tracks[t.ID] = ot
	}
}

func trackIDs(ts []deezer.Track) []string {
	ids := make([]string, len(ts))
	for i, t := range ts {
		ids[i] = t.ID
	}
	return ids
}

// fetchCollection lists an album's or a playlist's tracks (the whole list or
// an error) with its title and cover.
func fetchCollection(c *deezer.Client, kind, id string) (ts []deezer.Track, title, cover string, err error) {
	if kind == "album" {
		ts, err = c.AlbumTracks(id)
		if len(ts) > 0 {
			title, cover = ts[0].AlbumName, ts[0].ArtworkURL
		}
		return ts, title, cover, err
	}
	if ts, _, err = c.PlaylistTracksStrict(id); err != nil {
		return nil, "", "", err
	}
	if info, ierr := c.PlaylistInfo(id); ierr == nil {
		title, cover = info.Name, info.ArtworkURL
	}
	if cover == "" && len(ts) > 0 {
		cover = ts[0].ArtworkURL
	}
	return ts, title, cover, nil
}

// PinOffline pins a track, an album or a playlist for the signed-in account
// and fetches its tracks in the background (OfflineProgress). An album's or a
// playlist's track list is fetched first; in offline mode it must be pinned
// already (by any account). Episodes are refused.
func PinOffline(kind, id string) error {
	if kind != "track" && kind != "album" && kind != "playlist" {
		return fmt.Errorf("a %q cannot be kept offline", kind)
	}
	if id == "" {
		return errors.New("no id to keep offline")
	}
	user := offlineUser()
	if user == "" {
		return errNotReady
	}
	c := netClient()
	coll := offlineColl{User: user, Kind: kind, ID: id, PinnedAt: time.Now().UnixMilli(), Quality: Quality()}
	var listed []deezer.Track
	switch {
	case kind == "track":
		coll.Tracks = []string{id}
	case c == nil:
		offMu.Lock()
		i := findCollLocked("", kind, id)
		if i >= 0 {
			prev := offlineLocked().Collections[i]
			coll.Title, coll.Cover, coll.Tracks = prev.Title, prev.Cover, prev.Tracks
		}
		offMu.Unlock()
		if i < 0 {
			return fmt.Errorf("%w: the %s's track list is not stored", ErrOfflineMode, kind)
		}
	default:
		ts, title, cover, err := fetchCollection(c, kind, id)
		if err != nil {
			return err
		}
		listed, coll.Title, coll.Cover, coll.Tracks = ts, title, cover, trackIDs(ts)
	}
	offMu.Lock()
	d := offlineLocked()
	if i := findCollLocked(user, kind, id); i >= 0 {
		coll.PinnedAt = d.Collections[i].PinnedAt
		d.Collections[i] = coll
	} else {
		d.Collections = append(d.Collections, coll)
	}
	storeTracksLocked(listed)
	for _, tid := range coll.Tracks {
		if _, ok := d.Tracks[tid]; !ok {
			d.Tracks[tid] = offlineTrack{Track: bridge.Track{ID: tid}}
		}
	}
	dropUnusedTracksLocked()
	offMu.Unlock()
	clearStoppedJobs()
	err := pinsChanged()
	startJob(c, coll)
	return err
}

// UnpinOffline removes the signed-in account's pin of kind and id and stops
// its fetch; kind "track" with id "" removes every single-track pin of the
// account. A track also pinned through a collection stays pinned through it.
// Unpinning what is not pinned changes nothing.
func UnpinOffline(kind, id string) error {
	user := offlineUser()
	match := func(u, k, i string) bool { return u == user && k == kind && (i == id || (kind == "track" && id == "")) }
	offMu.Lock()
	d := offlineLocked()
	n := len(d.Collections)
	d.Collections = slices.DeleteFunc(d.Collections, func(c offlineColl) bool { return match(c.User, c.Kind, c.ID) })
	removed := len(d.Collections) < n
	if removed {
		dropUnusedTracksLocked()
	}
	offMu.Unlock()
	dropJobs(func(j *offlineJob) bool { return match(j.user, j.Kind, j.ID) })
	clearStoppedJobs()
	if !removed {
		return nil
	}
	return pinsChanged()
}

// syncPinned keeps the signed-in account's pin of kind and id in step with ts,
// its track list just fetched online: new tracks are pinned and fetched,
// removed ones unpinned. A list shorter than total, the length the reply
// declared, is not complete and changes nothing; nor does an empty album.
func syncPinned(c *deezer.Client, kind, id string, ts []deezer.Track, total int) {
	if len(ts) < total || (kind == "album" && len(ts) == 0) {
		return
	}
	user := offlineUser()
	ids := trackIDs(ts)
	offMu.Lock()
	d := offlineLocked()
	i := findCollLocked(user, kind, id)
	if i < 0 || slices.Equal(d.Collections[i].Tracks, ids) {
		offMu.Unlock()
		return
	}
	d.Collections[i].Tracks = ids
	coll := d.Collections[i]
	storeTracksLocked(ts)
	dropUnusedTracksLocked()
	offMu.Unlock()
	if err := pinsChanged(); err != nil {
		odlog.Warn("offline %s %s: %v", kind, id, err)
	}
	startJob(c, coll)
}

// PlaylistTracks lists a playlist's tracks; a playlist the signed-in account
// pinned is listed whole (or not at all) and kept in step.
func PlaylistTracks(id string) ([]deezer.Track, error) {
	c, err := Online()
	if err != nil {
		return nil, err
	}
	user := offlineUser()
	offMu.Lock()
	pinned := findCollLocked(user, "playlist", id) >= 0
	offMu.Unlock()
	if !pinned {
		return c.PlaylistTracks(id)
	}
	ts, total, err := c.PlaylistTracksStrict(id)
	if err == nil {
		syncPinned(c, "playlist", id, ts, total)
	}
	return ts, err
}

// AlbumTracks lists an album's tracks; an album the signed-in account pinned
// is kept in step.
func AlbumTracks(id string) ([]deezer.Track, error) {
	c, err := Online()
	if err != nil {
		return nil, err
	}
	ts, total, err := c.AlbumTracksTotal(id)
	if err == nil {
		syncPinned(c, "album", id, ts, total)
	}
	return ts, err
}

// ClearCache removes every cached track not pinned; with includePinned also
// every pin of every account, with their fetches.
func ClearCache(includePinned bool) error {
	if includePinned {
		dropJobs(func(*offlineJob) bool { return true })
		offMu.Lock()
		d := offlineLocked()
		d.Collections, d.Tracks = nil, map[string]offlineTrack{}
		offMu.Unlock()
		if err := saveOffline(); err != nil {
			return err
		}
	}
	if err := caches.Clear(includePinned); err != nil {
		return err
	}
	if includePinned {
		return caches.Open() // nothing pinned: closed at a size of 0
	}
	return nil
}

// SetMediaCacheDir moves the media cache to dir as Host.SetDir does. The
// running fetches stop for the move, so none writes into the folder being
// left; after it the signed-in account's unfinished pins are fetched as at a
// login (none in offline mode or signed out). A stopped fetch stays listed
// with its reason.
func SetMediaCacheDir(dir string) error {
	stop := func() {
		for _, j := range dropJobs(func(j *offlineJob) bool { return j.running && !j.dropped }) {
			<-j.done
		}
	}
	stop()
	err := caches.SetDir(dir)
	stop() // pins made during the move fetched into the old folder
	resumeJobs()
	return err
}

// ---- fetching ----

// offlineJob is the fetch of one pin.
type offlineJob struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Done   int    `json:"done"`
	Total  int    `json:"total"`
	Failed int    `json:"failed"`
	Err    string `json:"error"`

	user    string
	client  *deezer.Client // nil when started in offline mode
	running bool
	dropped bool // unpinned or cleared: leaves the list without an error
	cancel  context.CancelFunc
	done    chan struct{} // closed when runJob returns
}

var (
	jobMu sync.Mutex
	jobs  []*offlineJob
	// fetchSlots bounds the tracks fetched at once, over every job.
	fetchSlots = make(chan struct{}, 3)
	// fetchStream fetches a stream into the cache; tests replace it.
	fetchStream = audio.Fetch
)

// startJob fetches coll's tracks and pictures in the background. It is the
// collection's one job: one already listed for it (running, cancelled or
// stopped) is stopped and leaves the list, and the new one starts fetching
// once that one ended.
func startJob(c *deezer.Client, coll offlineColl) {
	jobMu.Lock()
	startJobLocked(c, coll)
	jobMu.Unlock()
}

// startJobLocked is startJob with jobMu held.
func startJobLocked(c *deezer.Client, coll offlineColl) {
	ctx, cancel := context.WithCancel(context.Background())
	j := &offlineJob{Kind: coll.Kind, ID: coll.ID, Total: len(coll.Tracks), user: coll.User, client: c, running: true, cancel: cancel, done: make(chan struct{})}
	var prev []*offlineJob
	jobs = slices.DeleteFunc(jobs, func(x *offlineJob) bool {
		same := x.user == j.user && x.Kind == j.Kind && x.ID == j.ID
		if same {
			x.dropped = true
			x.cancel()
			prev = append(prev, x)
		}
		return same
	})
	jobs = append(jobs, j)
	go runJob(ctx, c, j, coll, prev)
}

// resumeJobs starts the fetch of every pin of the signed-in account that has
// a track not stored and no fetch listed: one running, or one stopped (a full
// disk, a cancel), which waits listed with its reason for the next pin, unpin
// or cancel. Nothing in offline mode. The client is read under jobMu: offline
// mode, a logout and another account's login change it before they stop the
// listed fetches, so none started here runs on with a client no longer in use.
func resumeJobs() {
	mc := caches.Current()
	jobMu.Lock()
	defer jobMu.Unlock()
	c := netClient()
	if c == nil {
		return
	}
	user := c.UserID()
	offMu.Lock()
	var todo []offlineColl
	for _, coll := range offlineLocked().Collections {
		if coll.User == user && slices.ContainsFunc(coll.Tracks, func(id string) bool { return !storedLocked(mc, id) }) {
			todo = append(todo, coll)
		}
	}
	offMu.Unlock()
	for _, coll := range todo {
		listed := slices.ContainsFunc(jobs, func(j *offlineJob) bool {
			return !j.dropped && j.user == coll.User && j.Kind == coll.Kind && j.ID == coll.ID
		})
		if !listed {
			startJobLocked(c, coll)
		}
	}
}

// runJob waits for the jobs it replaces to end, then fetches coll's tracks,
// at most len(fetchSlots) at a time over every job, then their pictures. A
// write failure (a full disk) stops it with the reason; what was fetched
// stays pinned.
func runJob(ctx context.Context, c *deezer.Client, j *offlineJob, coll offlineColl, prev []*offlineJob) {
	defer close(j.done)
	for _, p := range prev {
		<-p.done
	}
	mc := caches.Current()
	var wg sync.WaitGroup
loop:
	for _, id := range coll.Tracks {
		select {
		case fetchSlots <- struct{}{}:
		case <-ctx.Done():
			break loop
		}
		if ctx.Err() != nil {
			<-fetchSlots
			break
		}
		wg.Go(func() {
			defer func() { <-fetchSlots }()
			err := fetchTrack(ctx, c, mc, coll, id)
			jobMu.Lock()
			switch {
			case err == nil:
				j.Done++
			case ctx.Err() != nil:
			case errors.Is(err, audio.ErrWrite), errors.Is(err, errSave):
				if j.Err == "" {
					j.Err = err.Error()
				}
				j.cancel()
			default:
				j.Failed++
				odlog.Warn("offline %s %s: track %s: %v", coll.Kind, coll.ID, id, err)
			}
			jobMu.Unlock()
		})
	}
	wg.Wait()
	if c != nil && mc != nil && ctx.Err() == nil {
		storePictures(ctx, mc, coll) // after the tracks: a single track's metadata comes with its fetch
	}
	applyPins(caches.Current())
	// The fetched tracks' stream meta, kept in the index until now.
	serr := saveOffline()
	jobMu.Lock()
	if serr != nil && j.Err == "" {
		j.Err = fmt.Errorf("%w: %w", errSave, serr).Error()
	}
	j.running = false
	if ctx.Err() != nil && j.Err == "" && !j.dropped {
		j.Err = "cancelled"
	}
	if j.Err == "" || j.dropped {
		jobs = slices.DeleteFunc(jobs, func(x *offlineJob) bool { return x == j })
	}
	jobMu.Unlock()
	j.cancel()
}

// pinningCache adopts the fetched body pinned.
type pinningCache struct {
	*mediacache.Cache
	pins []string
}

// Adopt names the body in the cache's index with its pins and stream meta;
// the next start takes the track as fetched from that entry.
func (c pinningCache) Adopt(key, path string, size int64, meta mediacache.StreamMeta) error {
	return c.AdoptPinned(key, path, size, meta, c.pins)
}

// errSave stops a job: offline.json could not be written.
var errSave = errors.New(offlineFileName + " could not be written")

// fetchTrack stores track id for coll: nothing to do when its body is
// stored; a copy cached at the quality coll was pinned at (or better) is
// taken without a request; otherwise it is resolved and its stream fetched
// into the cache, pinned.
func fetchTrack(ctx context.Context, c *deezer.Client, mc *mediacache.Cache, coll offlineColl, id string) error {
	if mc == nil {
		return errors.New("no media cache: its folder could not be opened")
	}
	offMu.Lock()
	t := offlineLocked().Tracks[id]
	offMu.Unlock()
	if t.Meta != nil {
		if _, ok := mc.Path(id + "." + t.Meta.Format); ok {
			return nil
		}
	}
	want := 0
	if c != nil {
		want = mediacache.FormatRank(c.WantedFormat(coll.Quality))
	}
	if m, ok := mc.GetMetaForTrack(id); ok && (mediacache.FormatRank(m.Format) >= want || mediacache.FormatRank(m.AskedFormat) >= want) {
		var dto deezer.Track
		if t.Track.Name == "" && c != nil {
			dto, _ = c.Track(id)
		}
		if _, named := setTrackFetched(id, dto, m); named {
			if err := saveOffline(); err != nil {
				return fmt.Errorf("%w: %w", errSave, err)
			}
		}
		// Pinned at once: eviction and the age limit never take it from here on.
		applyPins(mc)
		return nil
	}
	if c == nil {
		return fmt.Errorf("%w: track %s is not stored", ErrOfflineMode, id)
	}
	plan, err := c.PrepareStreamAt(id, coll.Quality)
	if err != nil {
		return err
	}
	pins, named := setTrackFetched(id, plan.Track, mediacache.StreamMeta{
		Format: plan.Format, AskedFormat: plan.AskedFormat, KeyID: plan.KeyID, Encrypted: plan.Encrypted, GainDB: plan.GainDB, Preview: plan.Preview,
	})
	// Metadata only song.getData brought (a single-track pin's) is saved
	// before the body: the index entry does not hold it.
	if named {
		if err := saveOffline(); err != nil {
			return fmt.Errorf("%w: %w", errSave, err)
		}
	}
	return fetchStream(ctx, plan, pinningCache{mc, pins})
}

// setTrackFetched records the stream meta of track id's body (before the body
// lands, so the cache pins it from the start) and its metadata when the
// store has none, and returns the pins of its entry and whether it recorded
// metadata.
func setTrackFetched(id string, dto deezer.Track, m mediacache.StreamMeta) (pins []string, named bool) {
	offMu.Lock()
	defer offMu.Unlock()
	d := offlineLocked()
	t := d.Tracks[id]
	if t.Track.Name == "" && dto.ID != "" {
		t.Track = bridge.FromTrack(dto)
		named = true
	}
	t.Meta = &m
	d.Tracks[id] = t
	all, _ := wantedPinsLocked()
	return all[id+"."+m.Format], named
}

// storePictures stores coll's cover and its tracks' pictures, pinned.
// Best-effort: a host shows the CDN URL of a picture not stored.
func storePictures(ctx context.Context, mc *mediacache.Cache, coll offlineColl) {
	offMu.Lock()
	urls := []string{coll.Cover}
	for _, id := range coll.Tracks {
		urls = append(urls, offlineLocked().Tracks[id].Track.ArtworkURL)
	}
	pins, _ := wantedPinsLocked()
	offMu.Unlock()
	seen := map[string]bool{}
	for _, u := range urls {
		k := pictureKey(u)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		if _, ok := mc.Path(k); ok {
			continue
		}
		if err := storePicture(ctx, mc, u, k, pins[k]); err != nil {
			odlog.Debug("offline picture %s: %v", u, err)
		}
	}
}

// maxPicture bounds a stored picture.
const maxPicture = 8 << 20

// storePicture fetches the picture at url into mc as key.
func storePicture(ctx context.Context, mc *mediacache.Cache, url, key string, pins []string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := deezer.HTTPClient(30 * time.Second).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	f, err := mc.NewTemp()
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxPicture))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && resp.ContentLength > 0 && n != resp.ContentLength {
		err = fmt.Errorf("%d of %d bytes", n, resp.ContentLength)
	}
	if err == nil {
		err = mc.AdoptPinned(key, f.Name(), n, mediacache.StreamMeta{Format: "IMG"}, pins)
	}
	if err != nil {
		_ = os.Remove(f.Name())
	}
	return err
}

// clearStoppedJobs forgets the stopped jobs (a full disk, a cancel): a pin,
// an unpin or a cancel clears them.
func clearStoppedJobs() {
	jobMu.Lock()
	jobs = slices.DeleteFunc(jobs, func(j *offlineJob) bool { return !j.running })
	jobMu.Unlock()
}

// dropJobs stops the jobs match picks and returns them; they leave the list,
// without an error, as they end.
func dropJobs(match func(*offlineJob) bool) []*offlineJob {
	var stopped []*offlineJob
	jobMu.Lock()
	for _, j := range jobs {
		if match(j) {
			j.dropped = true
			j.cancel()
			stopped = append(stopped, j)
		}
	}
	jobMu.Unlock()
	return stopped
}

// CancelOffline stops every fetch of the signed-in account; each stays
// listed as cancelled until the next pin, unpin or cancel.
func CancelOffline() {
	user := offlineUser()
	clearStoppedJobs()
	jobMu.Lock()
	for _, j := range jobs {
		if j.user == user && !j.dropped {
			j.cancel()
		}
	}
	jobMu.Unlock()
}

// OfflineJob is one pin's fetch as OfflineProgress reports it.
type OfflineJob struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Done   int    `json:"done"`
	Total  int    `json:"total"`
	Failed int    `json:"failed"`
	Error  string `json:"error"`
}

// OfflineProgress returns the signed-in account's fetches: those running and
// those stopped with their reason; active is true while one runs.
func OfflineProgress() (active bool, list []OfflineJob) {
	user := offlineUser()
	list = []OfflineJob{}
	jobMu.Lock()
	defer jobMu.Unlock()
	for _, j := range jobs {
		if j.user != user || j.dropped {
			continue
		}
		active = active || j.running
		list = append(list, OfflineJob{Kind: j.Kind, ID: j.ID, Done: j.Done, Total: j.Total, Failed: j.Failed, Error: j.Err})
	}
	return active, list
}

// ---- what is offline ----

// OfflineCollection is one pin as the hosts list it. All single-track pins
// are one entry of Kind "track" and ID "".
type OfflineCollection struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	Title      string `json:"title"`
	Cover      string `json:"cover"` // a local file once stored, else the URL
	TrackCount int    `json:"trackCount"`
	Fetched    int    `json:"fetched"` // tracks whose body is stored
	PinnedAt   int64  `json:"pinnedAt"`
}

// storedLocked reports whether track id's body is in mc.
func storedLocked(mc *mediacache.Cache, id string) bool {
	t := offlineLocked().Tracks[id]
	if mc == nil || t.Meta == nil {
		return false
	}
	_, ok := mc.Path(id + "." + t.Meta.Format)
	return ok
}

// localPicture is the stored file of the picture at url, else url.
func localPicture(mc *mediacache.Cache, url string) string {
	if k := pictureKey(url); k != "" && mc != nil {
		if p, ok := mc.Path(k); ok {
			return p
		}
	}
	return url
}

// singlesLocked returns user's single-track pins, newest first.
func singlesLocked(user string) []offlineColl {
	var out []offlineColl
	for _, c := range offlineLocked().Collections {
		if c.User == user && c.Kind == "track" {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].PinnedAt > out[j].PinnedAt })
	return out
}

// OfflineCollections returns the signed-in (else the last signed-in)
// account's pins, newest first.
func OfflineCollections() []OfflineCollection {
	user, mc := offlineUser(), caches.Current()
	offMu.Lock()
	defer offMu.Unlock()
	d := offlineLocked()
	out := []OfflineCollection{}
	fetched := func(ids []string) (n int) {
		for _, id := range ids {
			if storedLocked(mc, id) {
				n++
			}
		}
		return n
	}
	for _, c := range d.Collections {
		if c.User == user && c.Kind != "track" {
			out = append(out, OfflineCollection{Kind: c.Kind, ID: c.ID, Title: c.Title, Cover: localPicture(mc, c.Cover),
				TrackCount: len(c.Tracks), Fetched: fetched(c.Tracks), PinnedAt: c.PinnedAt})
		}
	}
	if singles := singlesLocked(user); len(singles) > 0 {
		ids := make([]string, len(singles))
		for i, s := range singles {
			ids[i] = s.ID
		}
		out = append(out, OfflineCollection{Kind: "track", Cover: localPicture(mc, d.Tracks[ids[0]].Track.ArtworkURL),
			TrackCount: len(ids), Fetched: fetched(ids), PinnedAt: singles[0].PinnedAt})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].PinnedAt > out[j].PinnedAt })
	return out
}

// OfflineTracks returns the tracks of the account's pin of kind and id in
// order (kind "track", id "": the single-track pins, newest first), each
// picture a local file once stored. It needs no network.
func OfflineTracks(kind, id string) ([]bridge.Track, error) {
	user, mc := offlineUser(), caches.Current()
	offMu.Lock()
	defer offMu.Unlock()
	d := offlineLocked()
	var ids []string
	if kind == "track" && id == "" {
		for _, s := range singlesLocked(user) {
			ids = append(ids, s.ID)
		}
	} else if i := findCollLocked(user, kind, id); i >= 0 {
		ids = d.Collections[i].Tracks
	} else {
		return nil, fmt.Errorf("%s %s is not kept offline", kind, id)
	}
	out := make([]bridge.Track, 0, len(ids))
	for _, tid := range ids {
		t := d.Tracks[tid].Track
		t.ArtworkURL = localPicture(mc, t.ArtworkURL)
		out = append(out, t)
	}
	return out, nil
}

// OfflineIDs returns every track the account can play offline: pinned,
// directly or through a collection, and fetched.
func OfflineIDs() []string {
	user, mc := offlineUser(), caches.Current()
	offMu.Lock()
	defer offMu.Unlock()
	seen := map[string]bool{}
	ids := []string{}
	for _, c := range offlineLocked().Collections {
		if c.User != user {
			continue
		}
		for _, id := range c.Tracks {
			if !seen[id] && storedLocked(mc, id) {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	sort.Strings(ids)
	return ids
}

// ---- playing with no network ----

// userPinsLocked reports whether user pinned track id, directly or through a
// collection.
func userPinsLocked(user, id string) bool {
	for _, c := range offlineLocked().Collections {
		if c.User == user && slices.Contains(c.Tracks, id) {
			return true
		}
	}
	return false
}

// localPlan plans track id from the cache alone: a track the account pinned
// and fetched, or one cached and pinned by nobody (another account's pins do
// not play). The plan has no Refresh: nothing is requested.
func localPlan(id string) (*deezer.StreamPlan, error) {
	mc := caches.Current()
	if mc == nil {
		return nil, fmt.Errorf("track %s is not available offline", id)
	}
	user := offlineUser()
	offMu.Lock()
	t := offlineLocked().Tracks[id]
	pinned := userPinsLocked(user, id)
	stored := pinned && storedLocked(mc, id)
	offMu.Unlock()
	var m mediacache.StreamMeta
	if stored {
		m = *t.Meta
	} else {
		cm, ok := mc.GetMetaForTrack(id)
		if !ok || (mc.Pinned(id+"."+cm.Format) && !pinned) {
			return nil, fmt.Errorf("track %s is not available offline", id)
		}
		m = cm
	}
	plan := &deezer.StreamPlan{TrackID: id, KeyID: m.KeyID, Format: m.Format, AskedFormat: m.AskedFormat, GainDB: m.GainDB, Encrypted: m.Encrypted, Preview: m.Preview}
	if pinned && t.Track.ID != "" {
		plan.Track = trackFromWire(t.Track)
	}
	return plan, nil
}
