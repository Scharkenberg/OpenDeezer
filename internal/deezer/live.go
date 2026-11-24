package deezer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"golang.org/x/net/websocket"

	odlog "github.com/Cycl0o0/OpenDeezer/v3/internal/log"
)

// Deezer's players announce each track they start on the account's live socket and
// pause on another session's announcement. WatchStreams follows the announcements and
// AnnouncePlay makes one.

const (
	liveURL = "wss://live.deezer.com/ws/"

	// A dropped connection is retried after liveRetryMin, doubling up to
	// liveRetryMax while connections keep dropping sooner than liveRetryMax.
	liveRetryMin = 2 * time.Second
	liveRetryMax = time.Minute

	// liveWriteTimeout bounds an announcement's write.
	liveWriteTimeout = 5 * time.Second
)

// StreamStart is a track another session of the account started.
type StreamStart struct {
	TrackID string
	Session string // the announcing player's session id
}

// livePlay is the payload of a play announcement, in the field order Deezer's
// web player sends.
type livePlay struct {
	Action string `json:"ACTION"`
	App    string `json:"APP"`
	Value  struct {
		UserID string `json:"USER_ID"`
		UniqID string `json:"UNIQID"`
		SngID  string `json:"SNG_ID"`
	} `json:"VALUE"`
}

// streamChannel is the live channel uid's players announce their starts on.
func streamChannel(uid string) string { return uid + "_" + uid + "_STREAM" }

// WatchStreams reports the tracks other sessions of the account start until
// ctx ends, reconnecting after a drop; this client's own announcements are not
// reported. Watching alone publishes nothing. Requires login.
func (c *Client) WatchStreams(ctx context.Context, onStart func(StreamStart)) {
	delay := liveRetryMin
	for {
		began := time.Now()
		c.watchStreamsOnce(ctx, onStart)
		if time.Since(began) >= liveRetryMax {
			delay = liveRetryMin
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, liveRetryMax)
	}
}

// watchStreamsOnce runs one connection until it drops or ctx ends; meanwhile
// AnnouncePlay sends on it.
func (c *Client) watchStreamsOnce(ctx context.Context, onStart func(StreamStart)) {
	c.mu.RLock()
	uid, token := c.userID, c.userToken
	c.mu.RUnlock()
	if token == "" {
		return
	}
	base := liveURL
	if c.liveURLOverride != "" {
		base = c.liveURLOverride
	}
	// The web player names its build on the socket.
	release, _ := WebRelease()
	cfg, err := websocket.NewConfig(base+token+"?version="+release, webOrigin)
	if err != nil {
		return
	}
	cfg.Header.Set("User-Agent", UserAgent)
	ws, err := cfg.DialContext(ctx)
	if err != nil {
		odlog.Warn("live: connect: %v", err)
		return
	}
	stop := context.AfterFunc(ctx, func() { ws.Close() })
	defer stop()
	defer ws.Close()

	channel := streamChannel(uid)
	sub, _ := json.Marshal([]string{"sub", channel})
	if websocket.Message.Send(ws, string(sub)) != nil {
		return
	}
	c.liveMu.Lock()
	c.liveWS = ws
	c.liveMu.Unlock()
	odlog.Info("live: watching the account's plays")
	defer func() {
		c.liveMu.Lock()
		c.liveWS = nil
		c.liveMu.Unlock()
	}()
	for {
		var msg string
		if err := websocket.Message.Receive(ws, &msg); err != nil {
			odlog.Info("live: connection ended: %v", err)
			return
		}
		// A message is ["msg", channel, payload].
		var parts []json.RawMessage
		var kind, ch string
		var p livePlay
		if json.Unmarshal([]byte(msg), &parts) != nil || len(parts) != 3 ||
			json.Unmarshal(parts[0], &kind) != nil || kind != "msg" ||
			json.Unmarshal(parts[1], &ch) != nil || ch != channel ||
			json.Unmarshal(parts[2], &p) != nil {
			continue
		}
		if p.App == "LIMITATION" && p.Action == "PLAY" && p.Value.SngID != "" && p.Value.UniqID != c.liveSession {
			onStart(StreamStart{TrackID: p.Value.SngID, Session: p.Value.UniqID})
		}
	}
}

// Play is an entry of the account's listening history.
type Play struct {
	TrackID string
	StartMS int64 // when the track started (unix ms)
}

// RecentPlays returns the account's n latest plays, newest first (gw
// user.getSongsHistory). Deezer's players add a track when it ends or is
// skipped, so the history runs a track behind what plays.
func (c *Client) RecentPlays(n int) ([]Play, error) {
	b, err := c.gw("user.getSongsHistory", fmt.Sprintf(`{"start":0,"nb":%d}`, n))
	if err != nil {
		return nil, err
	}
	var r struct {
		Results struct {
			Data []struct {
				SngID json.Number `json:"SNG_ID"`
				TS    int64       `json:"TS"`
			} `json:"data"`
		} `json:"results"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	plays := make([]Play, 0, len(r.Results.Data))
	for _, d := range r.Results.Data {
		plays = append(plays, Play{TrackID: d.SngID.String(), StartMS: d.TS * 1000})
	}
	return plays, nil
}

// AnnouncePlay tells the account's other sessions that this one started trackID, so
// they pause, and reports whether it was sent (not without a WatchStreams connection).
// A write stalled for liveWriteTimeout fails.
func (c *Client) AnnouncePlay(trackID string) bool {
	uid := c.uid()
	p := livePlay{Action: "PLAY", App: "LIMITATION"}
	p.Value.UserID, p.Value.UniqID, p.Value.SngID = uid, c.liveSession, trackID
	frame, _ := json.Marshal([]any{"send", streamChannel(uid), p})
	c.liveMu.Lock()
	defer c.liveMu.Unlock()
	if c.liveWS == nil {
		return false
	}
	_ = c.liveWS.SetWriteDeadline(time.Now().Add(liveWriteTimeout))
	return websocket.Message.Send(c.liveWS, string(frame)) == nil
}
