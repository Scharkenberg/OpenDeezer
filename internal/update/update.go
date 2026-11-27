// Package update checks GitHub for a newer Purple Disc release so clients can show an
// "update available" prompt, and downloads the Windows installer of a release.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// Repo is the GitHub owner/name that publishes releases.
const Repo = "Scharkenberg/PurpleDisc"

// latestURL is the release API the check reads (a test points it at its own server).
var latestURL = "https://api.github.com/repos/" + Repo + "/releases/latest"

// installerSuffix ends the name of the Windows installer asset (PurpleDisc-<ver>-windows-x64.exe).
const installerSuffix = "-windows-x64.exe"

// Info is the result of a check.
type Info struct {
	Current   string `json:"current"`   // running version, e.g. "1.5.0"
	Latest    string `json:"latest"`    // latest release, e.g. "1.5.1"
	HasUpdate bool   `json:"hasUpdate"` // Latest is strictly newer than Current
	URL       string `json:"url"`       // release page to open
	Notes     string `json:"notes"`     // release notes (trimmed)
	// The Windows installer asset and its SHA-256 (lowercase hex), both empty when the
	// release has none or GitHub lists no sha256 digest for it.
	Installer       string `json:"installer,omitempty"`
	InstallerSHA256 string `json:"installerSha256,omitempty"`
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	HTMLURL string    `json:"html_url"`
	Body    string    `json:"body"`
	Draft   bool      `json:"draft"`
	Prerel  bool      `json:"prerelease"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"` // "sha256:<hex>"
}

// Check asks GitHub for the latest (non-draft, non-prerelease) release and
// compares it to current. A network/parse failure returns HasUpdate=false with
// the error, so callers can silently ignore it.
func Check(current string) (Info, error) {
	info := Info{Current: strings.TrimPrefix(current, "v")}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return info, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "PurpleDisc-update-check")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return info, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return info, fmt.Errorf("update check: HTTP %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return info, err
	}
	if rel.Draft || rel.Prerel {
		return info, nil
	}

	info.Latest = strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
	// The URL comes from the network and is opened by the apps: only this repository's
	// release pages, else its releases page.
	info.URL = "https://github.com/" + Repo + "/releases"
	if strings.HasPrefix(rel.HTMLURL, "https://github.com/"+Repo+"/") {
		info.URL = rel.HTMLURL
	}
	info.Notes = strings.TrimSpace(rel.Body)
	if len(info.Notes) > 2000 {
		n := 2000
		for !utf8.RuneStart(info.Notes[n]) { // never split a rune
			n--
		}
		info.Notes = info.Notes[:n]
	}
	info.HasUpdate = newer(info.Latest, info.Current)
	for _, a := range rel.Assets {
		if sum, ok := strings.CutPrefix(a.Digest, "sha256:"); ok && strings.HasSuffix(a.Name, installerSuffix) {
			info.Installer, info.InstallerSHA256 = a.URL, strings.ToLower(sum)
		}
	}
	return info, nil
}

// InstalledApp reports whether this program runs from the Windows installer's folder,
// %LOCALAPPDATA%\Programs\PurpleDisc\app: the only copy that updates itself.
func InstalledApp() bool {
	exe, err := os.Executable()
	local := os.Getenv("LOCALAPPDATA")
	return err == nil && runtime.GOOS == "windows" && local != "" &&
		strings.EqualFold(filepath.Dir(exe), filepath.Join(local, "Programs", "PurpleDisc", "app"))
}

var received, size atomic.Int64

// Progress is the running download's bytes so far and its length (0 while unknown).
func Progress() (done, total int64) { return received.Load(), size.Load() }

// Download writes url to path, checked against sum (SHA-256, hex) on the way. On any
// failure, a wrong sum included, no file is left at path.
func Download(url, sum, path string) error {
	received.Store(0)
	size.Store(0)
	client := http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	size.Store(max(resp.ContentLength, 0))
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h, counter{}), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), sum) {
		err = fmt.Errorf("download: SHA-256 does not match the release")
	}
	if err != nil {
		os.Remove(path)
	}
	return err
}

type counter struct{}

func (counter) Write(p []byte) (int, error) {
	received.Add(int64(len(p)))
	return len(p), nil
}

// newer reports whether version a is strictly greater than b (dotted numeric,
// e.g. "1.5.1" > "1.5.0"). Non-numeric or missing parts compare as 0. At equal
// numbers a release is newer than its pre-release ("3.1.0" > "3.1.0-rc1").
func newer(a, b string) bool {
	pa, pb := parts(a), parts(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return !strings.Contains(a, "-") && strings.Contains(b, "-")
}

func parts(v string) [3]int {
	var out [3]int
	// Drop any build/pre-release suffix (e.g. "1.5.1-rc1" -> "1.5.1").
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	for i, s := range strings.SplitN(v, ".", 3) {
		if i > 2 {
			break
		}
		n, _ := strconv.Atoi(strings.TrimSpace(s))
		out[i] = n
	}
	return out
}
