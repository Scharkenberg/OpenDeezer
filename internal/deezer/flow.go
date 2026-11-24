package deezer

// Flow, as Deezer's web player plays it (measured 2026-11-23): gw radio.getUserRadio
// tuned by the account's Flow tuner (Pipe flowTuner, lowercase), plus a flowConfig id
// for a mood. Each call returns a new batch; RadioFill tops a list up.

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// flowMoodIDs are the Flow moods of the web player, by flowConfig id.
var flowMoodIDs = []string{"chill", "motivation", "focus", "party", "melancholy", "you_and_me"}

// Flow returns a batch of the account's Flow, or of one of its moods (a
// FlowMoods id; "" for Flow itself).
func (c *Client) Flow(mood string) ([]Track, error) {
	if !c.LoggedIn() {
		return nil, fmt.Errorf("not logged in")
	}
	c.mu.RLock()
	tuner, read := c.tuner, c.tunerRead
	c.mu.RUnlock()
	// Read once per session, also when the read fails: Deezer then plays the
	// default tuner.
	if !read {
		var r struct {
			Me struct {
				FlowTuner struct {
					DiscoveryTuner string `json:"discoveryTuner"`
				} `json:"flowTuner"`
			} `json:"me"`
		}
		if c.pipe("Tuner", `query Tuner { me { id flowTuner { discoveryTuner } } }`, nil, &r) == nil {
			tuner = strings.ToLower(r.Me.FlowTuner.DiscoveryTuner)
		}
		c.mu.Lock()
		c.tuner, c.tunerRead = tuner, true
		c.mu.Unlock()
	}
	body := map[string]string{"user_id": c.uid()}
	if tuner != "" {
		body["tuner"] = tuner
	}
	if mood != "" {
		body["config_id"] = mood
	}
	b, _ := json.Marshal(body)
	resp, err := c.gw("radio.getUserRadio", string(b))
	if err != nil {
		return nil, err
	}
	return parseGwTrackList(resp)
}

// FlowMood is one of Flow's moods: its id (Flow's mood argument) and its title.
type FlowMood struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// FlowMoods lists Flow's moods with their titles, in one Pipe query
// (one flowConfig per mood).
func (c *Client) FlowMoods() ([]FlowMood, error) {
	var q strings.Builder
	q.WriteString("query FlowMoods {")
	for i, id := range flowMoodIDs {
		fmt.Fprintf(&q, ` m%d: flowConfig(flowConfigId: %q) { id title }`, i, id)
	}
	q.WriteString(" }")
	var r map[string]struct {
		Title string `json:"title"`
	}
	if err := c.pipe("FlowMoods", q.String(), nil, &r); err != nil {
		return nil, err
	}
	out := make([]FlowMood, len(flowMoodIDs))
	for i, id := range flowMoodIDs {
		m := r[fmt.Sprintf("m%d", i)]
		out[i] = FlowMood{ID: id, Title: m.Title}
	}
	return out, nil
}

// IsRadio reports whether a list from pageType is endless (RadioFill).
func IsRadio(pageType string) bool { return radioFetch(nil, pageType, "") != nil }

// radioFetch returns the call that fetches the next batch of a list from
// pageType/id, or nil when the list is not endless.
func radioFetch(c *Client, pageType, id string) func() ([]Track, error) {
	switch {
	case strings.HasSuffix(pageType, "_user_radio"):
		return func() ([]Track, error) { return c.Flow("") }
	case strings.HasSuffix(pageType, "_flow_config"):
		return func() ([]Track, error) { return c.Flow(id) }
	case pageType == "track_mix_contextual":
		return func() ([]Track, error) { return c.TrackMix(id) }
	case pageType == "artist_smartradio":
		return func() ([]Track, error) { return c.ArtistMix(id) }
	}
	return nil
}

// radioLow is how few tracks may be left in a radio list before it is topped
// up, as the web player's refill does.
const radioLow = 4

// RadioFill tops up a list played from an endless source: Flow ("*_user_radio"), a mood
// ("*_flow_config"), a track mix ("track_mix_contextual") or an artist's
// ("artist_smartradio"). The zero value is ready and safe for concurrent use.
type RadioFill struct {
	mu    sync.Mutex
	key   string          // the source the list is from
	seen  map[string]bool // the tracks it holds or held
	ended bool            // a batch brought nothing new
	busy  bool            // a fetch is under way
}

// More returns tracks to append to a list from source pageType/id with left tracks
// after the playing one, or nil (not a radio, enough left, a fetch running, source dry).
// queued returns the list's tracks when the source changes.
func (r *RadioFill) More(c *Client, pageType, id string, left int, queued func() []Track) []Track {
	fetch := radioFetch(c, pageType, id)
	if fetch == nil || left > radioLow {
		return nil
	}
	r.mu.Lock()
	if key := pageType + "\x00" + id; key != r.key {
		r.key, r.ended, r.seen = key, false, map[string]bool{}
		for _, t := range queued() {
			r.seen[t.ID] = true
		}
	}
	if r.ended || r.busy {
		r.mu.Unlock()
		return nil
	}
	r.busy = true
	key := r.key
	r.mu.Unlock()

	batch, err := fetch()

	r.mu.Lock()
	defer r.mu.Unlock()
	r.busy = false
	if err != nil || key != r.key {
		return nil // tried again at the next start
	}
	var fresh []Track
	for _, t := range batch {
		if !r.seen[t.ID] {
			r.seen[t.ID] = true
			fresh = append(fresh, t)
		}
	}
	r.ended = len(fresh) == 0
	return fresh
}
