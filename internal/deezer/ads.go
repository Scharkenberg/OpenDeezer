package deezer

import "encoding/json"

// SetAdsDisabled turns off play reporting (gw log.listen), which credits artists and
// drives free-tier ads. Disabling it breaks Deezer's free-tier terms; it exists so the
// user can choose that explicitly, at their own risk.
func (c *Client) SetAdsDisabled(disabled bool) { c.adsDisabled.Store(disabled) }

// AdsDisabled reports whether play reporting is turned off.
func (c *Client) AdsDisabled() bool { return c.adsDisabled.Load() }

// ListenMedia names a track in the listen log: its id and its type ("song",
// "episode").
type ListenMedia struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// LogListen sends gw log.listen as Deezer's players do when a track starts: params is
// the listen that ended, next the track that starts (each may be nil). It carries the
// web player's build; a no-op when reporting is off.
func (c *Client) LogListen(params json.RawMessage, next *ListenMedia) error {
	if c.adsDisabled.Load() {
		return nil
	}
	if params != nil {
		params = withDev(params)
	}
	body := struct {
		Params json.RawMessage `json:"params,omitempty"`
		Next   *struct {
			Media *ListenMedia `json:"media"`
		} `json:"next_media,omitempty"`
	}{Params: params}
	if next != nil {
		body.Next = &struct {
			Media *ListenMedia `json:"media"`
		}{next}
	}
	b, _ := json.Marshal(body)
	_, err := c.gw("log.listen", string(b))
	return err
}

// LogListens sends listens that could not be sent when they ended (offline, or
// the request failed) in one gw log.batch_listen. A no-op when the user has
// turned reporting off.
func (c *Client) LogListens(params []json.RawMessage) error {
	if c.adsDisabled.Load() || len(params) == 0 {
		return nil
	}
	withDevs := make([]json.RawMessage, len(params))
	for i, p := range params {
		withDevs[i] = withDev(p)
	}
	b, _ := json.Marshal(struct {
		Array []json.RawMessage `json:"array"`
	}{withDevs})
	_, err := c.gw("log.batch_listen", string(b))
	return err
}

// withDev returns the listen params with the device Deezer's web player names
// in it: its build, of type 0.
func withDev(params json.RawMessage) json.RawMessage {
	var p map[string]json.RawMessage
	if json.Unmarshal(params, &p) != nil {
		return params
	}
	release, _ := WebRelease()
	p["dev"], _ = json.Marshal(map[string]any{"v": release, "t": 0})
	b, _ := json.Marshal(p)
	return b
}
