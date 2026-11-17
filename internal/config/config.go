// Package config centralizes OpenDeezer's user configuration (env vars +
// ~/.config/opendeezer files) for the bits shared between the TUI and the GUI
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

	odlog "github.com/Cycl0o0/OpenDeezer/v3/internal/log"
)

var (
	baseDirMu        sync.RWMutex
	baseDirOverride  string
	cacheDirOverride string
)

// SetBaseDir overrides the directory Dir returns. Hosts whose platform
// os.UserConfigDir is not a writable, persistent path — Android and iOS, where
// it resolves outside the app sandbox — must call this once at startup with an
// app-private directory, otherwise settings (media.json, tokens) and on-disk
// caches can't be saved or don't survive a relaunch. "" restores the default.
func SetBaseDir(dir string) {
	baseDirMu.Lock()
	baseDirOverride = strings.TrimSpace(dir)
	baseDirMu.Unlock()
}

// Dir is ~/.config/opendeezer (platform UserConfigDir + "opendeezer").
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
	return filepath.Join(base, "opendeezer"), nil
}

// SetCacheBaseDir overrides the directory CacheBaseDir returns, for hosts
// whose os.UserCacheDir is not an app-private path (Android: the app's cache
// directory). "" restores the default.
func SetCacheBaseDir(dir string) {
	baseDirMu.Lock()
	cacheDirOverride = strings.TrimSpace(dir)
	baseDirMu.Unlock()
}

// CacheBaseDir holds what the engine can lose: <user cache dir>/opendeezer
// (%LOCALAPPDATA%\opendeezer on Windows). Stream temp files go in its tmp
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
	return filepath.Join(base, "opendeezer"), nil
}

func readFile(name string) string {
	// Primary: the platform config dir (macOS: ~/Library/Application Support).
	if dir, err := Dir(); err == nil {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	// Fallback: ~/.config/opendeezer (Linux-style), so a file placed there still
	// works on macOS where UserConfigDir differs.
	if home, err := os.UserHomeDir(); err == nil {
		if b, err := os.ReadFile(filepath.Join(home, ".config", "opendeezer", name)); err == nil {
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

// LoadControl reads the control-API config from $OPENDEEZER_CONTROL ("1"/addr) +
// $OPENDEEZER_CONTROL_TOKEN, else ~/.config/opendeezer/{control.txt,control-token.txt}.
func LoadControl() Control {
	c := Control{Addr: "127.0.0.1:7654"}
	v := strings.TrimSpace(os.Getenv("OPENDEEZER_CONTROL"))
	if v == "" {
		v = readFile("control.txt")
	}
	switch {
	case v == "":
		// Control isn't explicitly enabled — leave Enabled=false, but still fall
		// through to read a configured token below. Callers that start a LAN
		// server regardless of this flag (the phone remote and the Connect host)
		// must honor a configured token; returning here would hand them an empty
		// token and silently downgrade them to same-account auth. Enable-gated
		// callers (startServices) still key off c.Enabled, so this only
		// populates a field they'd otherwise skip.
	case v == "1" || strings.EqualFold(v, "on") || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes"):
		c.Enabled = true
	case v == "0" || strings.EqualFold(v, "off") || strings.EqualFold(v, "false") || strings.EqualFold(v, "no"):
		c.Enabled = false
	default:
		c.Enabled = true
		c.Addr = v // an explicit host:port
	}
	c.Token = strings.TrimSpace(os.Getenv("OPENDEEZER_CONTROL_TOKEN"))
	if c.Token == "" {
		c.Token = readFile("control-token.txt")
	}
	// LAN bind + no token => default to same-account auth.
	if c.Enabled && c.Token == "" && !isLoopbackAddr(c.Addr) {
		c.SameAccount = true
	}
	if v := strings.TrimSpace(os.Getenv("OPENDEEZER_CONTROL_SAMEACCOUNT")); v != "" {
		c.SameAccount = v == "1" || strings.EqualFold(v, "on") || strings.EqualFold(v, "true")
	}
	return c
}

// WriteFile writes contents to a file under Dir(), creating the directory if
// needed. Unlike readFile it only ever targets the primary (platform) config
// dir — there's exactly one place a setting should be written to.
func WriteFile(name, contents string) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	// Write atomically (temp file + rename) so a crash or a concurrent reader in
	// another client never sees a truncated/torn file — an emptied
	// control-token.txt would silently downgrade the LAN control API from bearer
	// auth to same-account auth.
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

// SaveControlEnabled persists whether the control API starts automatically, so
// a Settings UI can flip it without editing env vars or config files by hand.
// addr is the bind address to remember while enabled (typically the current
// LoadControl().Addr); pass "" to disable.
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
func IsLoopbackAddr(addr string) bool { return isLoopbackAddr(addr) }

// isLoopbackAddr reports whether a host:port binds only the loopback interface.
func isLoopbackAddr(addr string) bool {
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

// LoadPeers returns manually-configured Connect peer addresses (host[:port]),
// from $OPENDEEZER_CONNECT_PEERS (comma-separated) and
// ~/.config/opendeezer/connect-peers.txt (one per line). These are merged into
// the device picker alongside LAN discovery, so Connect works over networks that
// carry no multicast/broadcast (e.g. Tailscale/VPN — unicast-only meshes).
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
	for _, p := range strings.Split(os.Getenv("OPENDEEZER_CONNECT_PEERS"), ",") {
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
		// No port present. Bracket bare IPv6 literals (which themselves contain
		// colons) before appending the default port so the result stays a valid
		// host:port — Tailscale/VPN peers are commonly IPv6.
		if ip := net.ParseIP(addr); ip != nil && ip.To4() == nil {
			addr = "[" + addr + "]"
		}
		addr += ":7654"
	}
	return "http://" + addr, addr
}

// EQ is the persisted equalizer + mono-downmix state. It is loaded and saved by
// the audio engine itself (not per client), so the TUI, every GUI and the
// control API all share one set of settings through ~/.config/opendeezer.
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

// Playback is the persisted playback settings (playback.json in the config
// dir), loaded and saved by the engine and the terminal UI alike, so every
// client plays with the same ones.
type Playback struct {
	Quality     int  `json:"quality"` // 0 MP3 128, 1 MP3 320, 2 FLAC
	Gapless     bool `json:"gapless"`
	CrossfadeMS int  `json:"crossfadeMs"`
	ReplayGain  bool `json:"replayGain"` // loudness normalization
}

// LoadPlayback reads playback.json. While it does not exist, the terminal
// UI's older files (quality.txt, gapless.txt, crossfade.txt, replaygain.txt
// in Dir) are written into it once, whichever client loads first. ok is false
// when it is missing with none of those, or unparseable: the caller keeps its
// own values (and a host that stored them itself writes them here with its
// first setter).
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
		odlog.Warn("playback.json: %v", err)
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
	// MediaCacheMB is the on-disk raw-stream cache budget in megabytes
	// (see internal/mediacache). 0 — the default — disables the cache
	// entirely; negative values are clamped to 0.
	MediaCacheMB int `json:"mediaCacheMB"`
	// MediaCacheDir is the folder the user picked for the cache; "" (the
	// default) is CacheBaseDir. The cache lives in its subfolder
	// opendeezer-cache (CacheStoreDir).
	MediaCacheDir string `json:"mediaCacheDir,omitempty"`
	// MediaCacheMaxAgeDays removes cached tracks not played for longer than
	// this many days; 0 (the default) keeps them. Negative values are
	// clamped to 0.
	MediaCacheMaxAgeDays int `json:"mediaCacheMaxAgeDays,omitempty"`
	// OldCachesRemoved records that the caches older versions kept outside
	// an opendeezer-cache folder were looked for and removed.
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
// opendeezer-cache of CacheDir, which the cache creates. The cache deletes
// nothing outside it.
func (m Media) CacheStoreDir() (string, error) {
	dir, err := m.CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "opendeezer-cache"), nil
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
// $OPENDEEZER_DISCORD_APP_ID or ~/.config/opendeezer/discord-app-id.txt. Empty
// disables the feature.
func LoadDiscordAppID() string {
	if v := strings.TrimSpace(os.Getenv("OPENDEEZER_DISCORD_APP_ID")); v != "" {
		return v
	}
	return readFile("discord-app-id.txt")
}

// LoadLanguage returns the persisted UI language code (e.g. "fr", "zh"), from
// $OPENDEEZER_LANG or ~/.config/opendeezer/language.txt. An empty result means
// "auto" — the caller should fall back to locale detection. Shared across every
// client so the language chosen in one place (TUI, GUI) applies everywhere.
func LoadLanguage() string {
	if v := strings.TrimSpace(os.Getenv("OPENDEEZER_LANG")); v != "" {
		return v
	}
	return readFile("language.txt")
}

// LanguageSetting returns ONLY the persisted language file (~/.config/opendeezer/
// language.txt), ignoring $OPENDEEZER_LANG. LoadLanguage lets the env var win so a
// forced locale applies everywhere at startup; the in-app Language menu, however,
// edits and displays its own persisted selection, so it must read the file alone —
// otherwise a set OPENDEEZER_LANG would freeze the menu on one entry and desync its
// label from the locale actually applied.
func LanguageSetting() string {
	return readFile("language.txt")
}

// SaveLanguage persists the UI language code. "" clears it (back to auto).
func SaveLanguage(code string) error {
	return WriteFile("language.txt", strings.TrimSpace(code))
}

// DefaultDownloadDir is where downloaded tracks land when the user hasn't chosen
// a folder: <home>/Music/OpenDeezer, falling back to <home>/Downloads/OpenDeezer
// and finally "OpenDeezer" in the working directory. Shared across every client
// so the default is identical everywhere.
func DefaultDownloadDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		music := filepath.Join(home, "Music")
		if fi, err := os.Stat(music); err == nil && fi.IsDir() {
			return filepath.Join(music, "OpenDeezer")
		}
		return filepath.Join(home, "Downloads", "OpenDeezer")
	}
	return "OpenDeezer"
}

// LoadDownloadDir returns the folder downloaded tracks are saved to, from
// $OPENDEEZER_DOWNLOAD_DIR, then ~/.config/opendeezer/download-dir.txt, then
// [DefaultDownloadDir]. Shared by the TUI and every GUI so a folder chosen in
// one client applies everywhere.
func LoadDownloadDir() string {
	if v := strings.TrimSpace(os.Getenv("OPENDEEZER_DOWNLOAD_DIR")); v != "" {
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

// LoadAdsDisabled reports whether the user has opted out of Deezer Free's
// play-reporting/ads, from $OPENDEEZER_DISABLE_ADS or
// ~/.config/opendeezer/ads-disabled.txt. Default false (ads/reporting on). This
// only affects free accounts; it is the user's explicit, at-own-risk choice
// (see deezer.SetAdsDisabled for the tradeoff). Shared across every client.
func LoadAdsDisabled() bool {
	v := strings.TrimSpace(os.Getenv("OPENDEEZER_DISABLE_ADS"))
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
