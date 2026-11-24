package deezer

// OpenDeezer talks to Deezer as its web player does: a desktop browser User-Agent, the
// web player's origin, and its build number (releaseNumeric) in the listen log and on
// the live socket, read from the script the logged-in page names.

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"regexp"
	"runtime"
	"sync"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
	odlog "github.com/Cycl0o0/OpenDeezer/v3/internal/log"
)

// webOrigin is the web player's origin.
const webOrigin = "https://www.deezer.com"

// UserAgent is a current desktop Edge's on this platform, the browser the web
// player runs in: every request to Deezer and its CDN carries it.
var UserAgent = browserUserAgent(runtime.GOOS)

func browserUserAgent(goos string) string {
	platform := "Windows NT 10.0; Win64; x64"
	switch goos {
	case "darwin", "ios":
		platform = "Macintosh; Intel Mac OS X 10_15_7"
	case "linux", "android", "freebsd", "openbsd", "netbsd":
		platform = "X11; Linux x86_64"
	}
	return "Mozilla/5.0 (" + platform + ") AppleWebKit/537.36 (KHTML, like Gecko) Chrome/160.0.0.0 Safari/537.36 Edg/160.0.0.0"
}

// fallbackRelease is the web player's build as of this OpenDeezer release
// (2026-11-23), used until a login reads the current one.
const fallbackRelease = "10020261123132810"

var (
	releaseMu     sync.Mutex
	releaseBundle string // the script the current build was read from
	releaseNumber string // "" for fallbackRelease
)

// WebRelease returns the web player's build number OpenDeezer reports and the
// script it was read from ("" before one was).
func WebRelease() (release, bundle string) {
	releaseMu.Lock()
	defer releaseMu.Unlock()
	if releaseNumber == "" {
		return fallbackRelease, releaseBundle
	}
	return releaseNumber, releaseBundle
}

// SetWebRelease sets the build number read from bundle before, which a host
// keeps between starts so the script is fetched only for a new release.
func SetWebRelease(bundle, release string) {
	releaseMu.Lock()
	releaseBundle, releaseNumber = bundle, release
	releaseMu.Unlock()
}

// KeepWebRelease reads the web player's current build (LearnWebRelease) and
// keeps it in web-release.json for the next start.
func KeepWebRelease(c *Client) {
	_, before := WebRelease()
	c.LearnWebRelease()
	if release, bundle := WebRelease(); bundle != before {
		if err := config.SaveWebRelease(bundle, release); err != nil {
			odlog.Warn("web-release.json: %v", err)
		}
	}
}

var (
	bundleRe  = regexp.MustCompile(`https://cdn-files\.dzcdn\.net/cache/js/app-web\.[0-9a-f]+\.js`)
	releaseRe = regexp.MustCompile(`releaseNumeric:"(\d+)"`)
)

// LearnWebRelease reads the build number from the script the logged-in page names, when
// it differs from the last one read; hosts run it after a login, off their loop. On
// failure the known build is kept.
func (c *Client) LearnWebRelease() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, webOrigin+"/", nil)
	if err != nil {
		return
	}
	_, cookie := c.session()
	req.Header.Set("Cookie", cookie)
	webHeaders(req, "")
	resp, err := c.http.Do(req)
	if err != nil {
		odlog.Debug("web release: page: %v", err)
		return
	}
	page, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	resp.Body.Close()
	if err != nil {
		return
	}
	bundle := string(bundleRe.Find(page))
	if _, known := WebRelease(); bundle == "" || bundle == known {
		return
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, bundle, nil)
	if err != nil {
		return
	}
	webHeaders(req, "")
	resp, err = c.http.Do(req)
	if err != nil {
		odlog.Debug("web release: script: %v", err)
		return
	}
	defer resp.Body.Close()
	// The number sits about 3 MB into a 4 MB script: scan it as it arrives.
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	sc.Split(func(data []byte, atEOF bool) (int, []byte, error) { // statement by statement
		for i, b := range data {
			if b == ';' {
				return i + 1, data[:i+1], nil
			}
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	for sc.Scan() {
		if m := releaseRe.FindSubmatch(sc.Bytes()); m != nil {
			SetWebRelease(bundle, string(m[1]))
			odlog.Info("web release %s", m[1])
			return
		}
	}
}

// webHeaders sets the headers the web player's requests carry: the browser's
// User-Agent, and for a call from its page the origin, the referrer and, once
// known, the user.
func webHeaders(req *http.Request, userID string) {
	req.Header.Set("User-Agent", UserAgent)
	if req.Method != http.MethodGet {
		req.Header.Set("Origin", webOrigin)
		req.Header.Set("Referer", webOrigin+"/")
	}
	if userID != "" {
		req.Header.Set("X-Deezer-User", userID)
	}
}

// ServerNow is the time on Deezer's clock, as the web player keeps it: the
// local clock moved by the difference the last login's SERVER_TIMESTAMP showed.
func (c *Client) ServerNow() time.Time {
	c.mu.RLock()
	d := c.serverDelta
	c.mu.RUnlock()
	return time.Now().Add(d)
}
