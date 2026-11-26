// Package config centralizes Purple Disc's user configuration (env vars +
// ~/.config/purpledisc files) for the bits shared between the TUI and the GUI
// engine (corelib): the control API and Discord Rich Presence settings.
package config

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	pdlog "github.com/Scharkenberg/PurpleDisc/v4/internal/log"
)

var (
	baseDirMu        sync.RWMutex
	baseDirOverride  string
	cacheDirOverride string
)

// SetBaseDir overrides the directory Dir returns ("" restores the default). Android
// and iOS hosts must call it at startup with an app-private directory, since
// os.UserConfigDir is outside their sandbox.
func SetBaseDir(dir string) {
	baseDirMu.Lock()
	baseDirOverride = strings.TrimSpace(dir)
	baseDirMu.Unlock()
}

// Dir is ~/.config/purpledisc (platform UserConfigDir + "purpledisc").
func Dir() (string, error) {
	baseDirMu.RLock()
	override := baseDirOverride
	baseDirMu.RUnlock()
	if override != "" {
		return override, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "purpledisc"), nil
}

// SetCacheBaseDir overrides the directory CacheBaseDir returns, for hosts
// whose os.UserCacheDir is not an app-private path (Android: the app's cache
// directory). "" restores the default.
func SetCacheBaseDir(dir string) {
	baseDirMu.Lock()
	cacheDirOverride = strings.TrimSpace(dir)
	baseDirMu.Unlock()
}

// CacheBaseDir holds what the engine can lose: <user cache dir>/purpledisc
// (%LOCALAPPDATA%\purpledisc on Windows). Stream temp files go in its tmp
// subfolder, and it is the default media cache folder.
func CacheBaseDir() (string, error) {
	baseDirMu.RLock()
	override := cacheDirOverride
	baseDirMu.RUnlock()
	if override != "" {
		return override, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "purpledisc"), nil
}

func readFile(name string) string {
	// Primary: the platform config dir (macOS: ~/Library/Application Support).
	if dir, err := Dir(); err == nil {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	// Fallback: ~/.config/purpledisc (Linux-style), so a file placed there still
	// works on macOS where UserConfigDir differs.
	if home, err := os.UserHomeDir(); err == nil {
		if b, err := os.ReadFile(filepath.Join(home, ".config", "purpledisc", name)); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return ""
}

// Control holds the control-API settings (remote control + MCP).
type Control struct {
	Enabled     bool
	Addr        string // host:port; "" -> 127.0.0.1:7654
	Token       string // bearer token ("" = no auth, localhost only)
	SameAccount bool   // require a matching Deezer account when no token (LAN)
}

// LoadControl reads the control-API config from $PURPLEDISC_CONTROL ("1"/addr) +
// $PURPLEDISC_CONTROL_TOKEN, else ~/.config/purpledisc/{control.txt,control-token.txt}.
func LoadControl() Control {
	c := Control{Addr: "127.0.0.1:7654"}
	v := strings.TrimSpace(os.Getenv("PURPLEDISC_CONTROL"))
	if v == "" {
		v = readFile("control.txt")
	}
	switch {
	case v == "":
		// Control is not enabled: keep Enabled false but still read the token below. The phone
		// remote and the Connect host start a LAN server regardless and must honor a
		// configured token.
	case v == "1" || strings.EqualFold(v, "on") || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes"):
		c.Enabled = true
	case v == "0" || strings.EqualFold(v, "off") || strings.EqualFold(v, "false") || strings.EqualFold(v, "no"):
		c.Enabled = false
	default:
		c.Enabled = true
		c.Addr = v // an explicit host:port
	}
	c.Token = strings.TrimSpace(os.Getenv("PURPLEDISC_CONTROL_TOKEN"))
	if c.Token == "" {
		c.Token = readFile("control-token.txt")
	}
	// LAN bind + no token => default to same-account auth.
	if c.Enabled && c.Token == "" && !IsLoopbackAddr(c.Addr) {
		c.SameAccount = true
	}
	if v := strings.TrimSpace(os.Getenv("PURPLEDISC_CONTROL_SAMEACCOUNT")); v != "" {
		c.SameAccount = v == "1" || strings.EqualFold(v, "on") || strings.EqualFold(v, "true")
	}
	return c
}

// WriteFile writes contents to a file under Dir(), creating the directory if needed.
// It writes only to the primary config dir.
func WriteFile(name, contents string) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	// Write atomically (temp file and rename) so no reader sees a torn file; an empty
	// control-token.txt would drop the LAN control API to same-account auth.
	tmp, err := os.CreateTemp(dir, name+".tmp*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(contents); err != nil {
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
	return os.Rename(tmpName, filepath.Join(dir, name))
}

// SaveControlEnabled saves whether the control API starts automatically. addr is the
// bind address to remember while enabled (usually LoadControl().Addr); "" disables it.
func SaveControlEnabled(enabled bool, addr string) error {
	v := ""
	if enabled {
		v = strings.TrimSpace(addr)
	}
	return WriteFile("control.txt", v)
}

// SaveControlToken persists the control-API bearer token. "" clears it, which
// falls back to same-account auth on a LAN bind.
func SaveControlToken(token string) error {
	return WriteFile("control-token.txt", strings.TrimSpace(token))
}

// IsLoopbackAddr reports whether a host:port binds only the loopback interface.
func IsLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	switch host {
	case "", "0.0.0.0", "::":
		return false // wildcard = all interfaces
	case "localhost":
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// LoadPeers returns manually configured Connect peers (host[:port]) from
// $PURPLEDISC_CONNECT_PEERS (comma-separated) and connect-peers.txt (one per line),
// for networks without multicast (e.g. Tailscale).
func LoadPeers() []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !strings.HasPrefix(s, "#") && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, p := range strings.Split(os.Getenv("PURPLEDISC_CONNECT_PEERS"), ",") {
		add(p)
	}
	for _, line := range strings.Split(readFile("connect-peers.txt"), "\n") {
		add(line)
	}
	return out
}

// PeerHostPorts returns the configured Connect peers (LoadPeers) normalized to
// host:port form, for passing to discovery.Discover as unicast probe targets so
// static peers answer even on multicast-filtered networks.
func PeerHostPorts() []string {
	var out []string
	for _, p := range LoadPeers() {
		if _, hp := NormalizePeer(p); hp != "" {
			out = append(out, hp)
		}
	}
	return out
}

// NormalizePeer turns user input ("host", "host:port", "http://host:port") into
// a base URL + host:port, defaulting the port to 7654. Returns ("","") if empty.
func NormalizePeer(addr string) (base, hostport string) {
	addr = strings.TrimSpace(addr)
	addr = strings.TrimPrefix(addr, "http://")
	addr = strings.TrimPrefix(addr, "https://")
	addr = strings.TrimRight(addr, "/")
	if addr == "" {
		return "", ""
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		// No port: bracket a bare IPv6 literal before appending the default port, so the result
		// is a valid host:port.
		if ip := net.ParseIP(addr); ip != nil && ip.To4() == nil {
			addr = "[" + addr + "]"
		}
		addr += ":7654"
	}
	return "http://" + addr, addr
}

// EQ is the persisted equalizer + mono-downmix state. It is loaded and saved by
// the audio engine itself (not per client), so the TUI, every GUI and the
// control API all share one set of settings through ~/.config/purpledisc.
type EQ struct {
	Enabled  bool      `json:"enabled"`
	Mono     bool      `json:"mono"`
	PreampDB float64   `json:"preampDb"`
	GainsDB  []float64 `json:"gainsDb"`
	Preset   string    `json:"preset"`
}

// LoadEQ reads eq.json from the config dir. ok is false when the file is
// missing or unparseable (caller falls back to defaults).
func LoadEQ() (eq EQ, ok bool) {
	s := readFile("eq.json")
	if s == "" {
		return EQ{}, false
	}
	if err := json.Unmarshal([]byte(s), &eq); err != nil {
		return EQ{}, false
	}
	return eq, true
}

// SaveEQ persists the equalizer state to eq.json in the config dir.
func SaveEQ(eq EQ) error {
	b, err := json.Marshal(eq)
	if err != nil {
		return err
	}
	return WriteFile("eq.json", string(b))
}

// LoadWebRelease returns the web player's build number and the script it was
// read from, as the last login left them (web-release.json); "" when none did.
func LoadWebRelease() (bundle, release string) {
	var r struct{ Bundle, Release string }
	_ = json.Unmarshal([]byte(readFile("web-release.json")), &r)
	return r.Bundle, r.Release
}

// SaveWebRelease keeps the web player's build number read from bundle.
func SaveWebRelease(bundle, release string) error {
	b, err := json.Marshal(struct{ Bundle, Release string }{bundle, release})
	if err != nil {
		return err
	}
	return WriteFile("web-release.json", string(b))
}

// maxListens bounds the listens kept for a later send (listens.json).
const maxListens = 1000

// LoadListens returns the listen log entries kept because they could not be
// sent (listens.json), oldest first.
func LoadListens() []json.RawMessage {
	var l []json.RawMessage
	_ = json.Unmarshal([]byte(readFile("listens.json")), &l)
	return l
}

// SaveListens keeps l, the listen log entries to send later, the newest
// maxListens of them.
func SaveListens(l []json.RawMessage) error {
	if len(l) > maxListens {
		l = l[len(l)-maxListens:]
	}
	if len(l) == 0 {
		l = []json.RawMessage{}
	}
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	return WriteFile("listens.json", string(b))
}

// Playback is the persisted playback settings (playback.json in the config
// dir), loaded and saved by the engine and the terminal UI alike, so every
// client plays with the same ones.
type Playback struct {
	Quality     int  `json:"quality"` // 0 MP3 128, 1 MP3 320, 2 FLAC
	Gapless     bool `json:"gapless"`
	CrossfadeMS int  `json:"crossfadeMs"`
	ReplayGain  bool `json:"replayGain"` // loudness normalization

	// GainTarget is the loudness, in dB, normalization turns tracks down to:
	// the account's GAIN.TARGET from its last login.
	GainTarget float64 `json:"gainTarget,omitempty"`

	// PlayAlongside turns single-device playback off: playback neither pauses
	// when another session of the account starts nor announces its own starts.
	PlayAlongside bool `json:"playAlongside,omitempty"`
}

// LoadPlayback reads playback.json. When it is missing, the TUI's older files
// (quality.txt, gapless.txt, crossfade.txt, replaygain.txt) are migrated into it once.
// ok is false when there is nothing to read or it does not parse.
func LoadPlayback() (pb Playback, ok bool) {
	s := readFile("playback.json")
	if s == "" {
		return migratePlayback()
	}
	if json.Unmarshal([]byte(s), &pb) != nil {
		return Playback{}, false
	}
	return pb, true
}

// migratePlayback writes the terminal UI's older files into playback.json; ok
// is false when there are none.
func migratePlayback() (pb Playback, ok bool) {
	dir, err := Dir()
	if err != nil {
		return Playback{}, false
	}
	read := func(name string) (string, bool) {
		b, err := os.ReadFile(filepath.Join(dir, name))
		ok = ok || err == nil
		return strings.TrimSpace(string(b)), err == nil
	}
	pb = Playback{Gapless: true}
	switch v, _ := read("quality.txt"); v {
	case "high":
		pb.Quality = 1
	case "hifi", "flac", "lossless":
		pb.Quality = 2
	}
	if v, found := read("gapless.txt"); found {
		pb.Gapless = v != "0"
	}
	v, _ := read("crossfade.txt")
	pb.CrossfadeMS, _ = strconv.Atoi(v)
	pb.CrossfadeMS = max(pb.CrossfadeMS, 0)
	v, _ = read("replaygain.txt")
	pb.ReplayGain = v == "1"
	if !ok {
		return Playback{}, false
	}
	if err := SavePlayback(pb); err != nil {
		pdlog.Warn("playback.json: %v", err)
	}
	return pb, true
}

// SavePlayback writes playback.json.
func SavePlayback(pb Playback) error {
	b, err := json.Marshal(pb)
	if err != nil {
		return err
	}
	return WriteFile("playback.json", string(b))
}

// playbackMu serializes UpdatePlayback's read, edit and write.
var playbackMu sync.Mutex

// UpdatePlayback writes playback.json with edit applied to its values, or,
// while it does not exist yet, to current(): the values the caller has in
// force, which its first write migrates into the file.
func UpdatePlayback(current func() Playback, edit func(*Playback)) error {
	playbackMu.Lock()
	defer playbackMu.Unlock()
	pb, ok := LoadPlayback()
	if !ok {
		pb = current()
	}
	edit(&pb)
	return SavePlayback(pb)
}

// Media is the persisted media/streaming configuration (media.json in the
// config dir), shared across every client like EQ. Its zero value is the
// default behavior: no on-disk stream cache.
type Media struct {
	// MediaCacheMB is the on-disk raw-stream cache budget in megabytes (see
	// internal/mediacache). 0, the default, disables the cache; negatives clamp to 0.
	MediaCacheMB int `json:"mediaCacheMB"`
	// MediaCacheDir is the folder the user picked for the cache; "" (the
	// default) is CacheBaseDir. The cache lives in its subfolder
	// purpledisc-cache (CacheStoreDir).
	MediaCacheDir string `json:"mediaCacheDir,omitempty"`
	// MediaCacheMaxAgeDays removes cached tracks not played for longer than
	// this many days; 0 (the default) keeps them. Negative values are
	// clamped to 0.
	MediaCacheMaxAgeDays int `json:"mediaCacheMaxAgeDays,omitempty"`
	// OldCachesRemoved records that the old-layout caches kept outside
	// an purpledisc-cache folder were looked for and removed.
	OldCachesRemoved bool `json:"oldCachesRemoved,omitempty"`
}

// CacheMaxAge is MediaCacheMaxAgeDays as a duration (0 = no limit).
func (m Media) CacheMaxAge() time.Duration {
	return time.Duration(m.MediaCacheMaxAgeDays) * 24 * time.Hour
}

// LoadMedia reads media.json from the config dir. A missing or unparseable
// file yields the zero value (cache disabled). Negative cache sizes and ages
// are clamped to 0.
func LoadMedia() Media {
	var m Media
	if s := readFile("media.json"); s != "" {
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			m = Media{}
		}
	}
	m.MediaCacheMB = max(m.MediaCacheMB, 0)
	m.MediaCacheMaxAgeDays = max(m.MediaCacheMaxAgeDays, 0)
	return m
}

// CacheDir is the folder the media cache goes in: MediaCacheDir, else
// CacheBaseDir.
func (m Media) CacheDir() (string, error) {
	if m.MediaCacheDir != "" {
		return m.MediaCacheDir, nil
	}
	return CacheBaseDir()
}

// CacheStoreDir is the folder the media cache lives in: the subfolder
// purpledisc-cache of CacheDir, which the cache creates. The cache deletes
// nothing outside it.
func (m Media) CacheStoreDir() (string, error) {
	dir, err := m.CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "purpledisc-cache"), nil
}

// SaveMedia persists the media settings to media.json in the config dir,
// clamping a negative cache size to 0 (disabled) first.
func SaveMedia(m Media) error {
	if m.MediaCacheMB < 0 {
		m.MediaCacheMB = 0
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return WriteFile("media.json", string(b))
}

// LoadDiscordAppID returns the Discord application id for Rich Presence, from
// $PURPLEDISC_DISCORD_APP_ID or ~/.config/purpledisc/discord-app-id.txt. Empty
// disables the feature.
func LoadDiscordAppID() string {
	if v := strings.TrimSpace(os.Getenv("PURPLEDISC_DISCORD_APP_ID")); v != "" {
		return v
	}
	return readFile("discord-app-id.txt")
}

// LoadLanguage returns the UI language code (e.g. "fr") from $PURPLEDISC_LANG or
// language.txt, shared by every client. "" means auto: use locale detection.
func LoadLanguage() string {
	if v := strings.TrimSpace(os.Getenv("PURPLEDISC_LANG")); v != "" {
		return v
	}
	return readFile("language.txt")
}

// LanguageSetting returns only the saved language.txt value, ignoring $PURPLEDISC_LANG,
// so the in-app Language menu shows and edits its own selection.
func LanguageSetting() string {
	return readFile("language.txt")
}

// SaveLanguage persists the UI language code. "" clears it (back to auto).
func SaveLanguage(code string) error {
	return WriteFile("language.txt", strings.TrimSpace(code))
}

// DefaultDownloadDir is the download folder when none is chosen: <home>/Music/PurpleDisc,
// else <home>/Downloads/PurpleDisc, else "Purple Disc" in the working directory.
func DefaultDownloadDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		music := filepath.Join(home, "Music")
		if fi, err := os.Stat(music); err == nil && fi.IsDir() {
			return filepath.Join(music, "PurpleDisc")
		}
		return filepath.Join(home, "Downloads", "PurpleDisc")
	}
	return "PurpleDisc"
}

// LoadDownloadDir returns the download folder from $PURPLEDISC_DOWNLOAD_DIR, then
// download-dir.txt, then [DefaultDownloadDir]; shared by every client.
func LoadDownloadDir() string {
	if v := strings.TrimSpace(os.Getenv("PURPLEDISC_DOWNLOAD_DIR")); v != "" {
		return v
	}
	if v := readFile("download-dir.txt"); v != "" {
		return v
	}
	return DefaultDownloadDir()
}

// SaveDownloadDir persists the download folder. "" clears it (back to the
// default).
func SaveDownloadDir(path string) error {
	return WriteFile("download-dir.txt", strings.TrimSpace(path))
}

// LoadAdsDisabled reports whether the user opted out of free-tier play reporting, from
// $PURPLEDISC_DISABLE_ADS or ads-disabled.txt (default false). It is the user's own
// at-risk choice (see deezer.SetAdsDisabled).
func LoadAdsDisabled() bool {
	v := strings.TrimSpace(os.Getenv("PURPLEDISC_DISABLE_ADS"))
	if v == "" {
		v = readFile("ads-disabled.txt")
	}
	return v == "1" || strings.EqualFold(v, "on") || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// SaveAdsDisabled persists the free-tier ads opt-out.
func SaveAdsDisabled(disabled bool) error {
	v := "0"
	if disabled {
		v = "1"
	}
	return WriteFile("ads-disabled.txt", v)
}
