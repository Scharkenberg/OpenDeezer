// Package deezer is a Deezer client: ARL login, gw-light + public REST browse,
// and track -> CDN-url resolution. The ARL never leaves the machine beyond the
// requests it makes to Deezer.
package deezer

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	mrand "math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/mediacache"
	"golang.org/x/net/websocket"
)

// ErrARLExpired is returned by Login when the ARL cookie is missing/expired or
// otherwise rejected, so callers can distinguish "re-auth needed" from a
// transient network failure and prompt the user accordingly.
var ErrARLExpired = errors.New("ARL expired or invalid: re-login required")

// ErrNoNetwork wraps a transport failure reaching Deezer (DNS, refused, unreachable,
// timeouts), so callers can offer a retry instead of a new login. A reachable Deezer
// that rejects the ARL yields ErrARLExpired instead.
var ErrNoNetwork = errors.New("no internet connection")

// ErrNoFullMedia means the account is not entitled to a full-length source (Free tier,
// geo, licensing). PrepareStream falls back to the 30 s preview only on this error;
// any other resolve error is returned as is.
var ErrNoFullMedia = errors.New("no full-length media available for this account")

// ErrNotFound wraps a REST lookup of an id or code Deezer does not know.
var ErrNotFound = errors.New("not found")

// restNoData is the REST error code for an unknown id ("no data").
const restNoData = 800

// restQuota is the REST error code for a client over Deezer's request quota.
const restQuota = 4

// classifyNet wraps err with ErrNoNetwork when it is a transport failure and returns
// it unchanged otherwise. Every c.http.Do call site uses it; HTTP-level rejections are
// not network errors.
func classifyNet(err error) error {
	if err == nil {
		return nil
	}
	network := isNetworkErr(err)
	// A *url.Error prints its URL, and gw URLs carry the api_token.
	var ue *url.Error
	if errors.As(err, &ue) {
		err = fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	if network {
		return fmt.Errorf("%w: %v", ErrNoNetwork, err)
	}
	return err
}

// isNetworkErr reports whether err is a transport-level connectivity failure; *url.Error
// unwraps to the underlying net, DNS, errno or context error.
func isNetworkErr(err error) bool {
	if err == nil {
		return false
	}
	// Dial/read/write timeouts and the request context deadline (30s client
	// timeout fires as context.DeadlineExceeded).
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// DNS resolution failure (host not found / no route to resolver).
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	// Refused / unreachable / network-down at the syscall layer.
	for _, se := range []syscall.Errno{
		syscall.ECONNREFUSED, syscall.ECONNRESET, syscall.EHOSTUNREACH,
		syscall.ENETUNREACH, syscall.ENETDOWN, syscall.ETIMEDOUT,
	} {
		if errors.Is(err, se) {
			return true
		}
	}
	// Any remaining net.Error that reports itself as a timeout, plus the generic
	// dial/OpError case (covers platforms/TLS handshakes not caught above).
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

// IsNoNetwork reports whether err (or anything it wraps) is a transport-level
// connectivity failure. Exported for callers outside this package (the FFI
// bindings, the TUI) that classify a login/browse failure.
func IsNoNetwork(err error) bool { return errors.Is(err, ErrNoNetwork) || isNetworkErr(err) }

// IsARLExpired reports whether err is an expired/invalid-ARL auth failure.
func IsARLExpired(err error) bool { return errors.Is(err, ErrARLExpired) }

// IsNoFullMedia reports whether err indicates an entitlement rejection
// (no full track source) rather than a transient problem.
func IsNoFullMedia(err error) bool { return errors.Is(err, ErrNoFullMedia) }

// errNoSource marks a get_url answer with neither media nor errors ({"data":[{"media":[]}]},
// as Deezer answers a track listed with empty RIGHTS): its FALLBACK version may play,
// never the preview.
var errNoSource = errors.New("neither media nor errors")

// errMediaBusy marks a get_url answer of 5xx or 429: retried, never a preview.
var errMediaBusy = errors.New("media service busy")

// The media service's error codes, as Deezer's players name them. Codes 1xxx concern
// the whole request (its license token), 2xxx one track token.
const (
	mediaInvalidLicense      = 1000
	mediaUnauthorizedLicense = 1002 // 1001 between them: expired
	mediaInvalidTrackToken   = 2000
	mediaExpiredTrackToken   = 2001
	mediaUnauthorizedTrack   = 2002 // the account has no rights to the track
	mediaCountryMismatch     = 2004
)

// mediaRejection is a get_url refusal: the media service's code and message,
// and whether it came for the whole request rather than the track. A refusal
// of the track itself (2002, 2004) is an ErrNoFullMedia.
type mediaRejection struct {
	code    int
	message string
	request bool
}

func (r *mediaRejection) Error() string {
	return fmt.Sprintf("get_url: %s (code %d)", r.message, r.code)
}

func (r *mediaRejection) Unwrap() error {
	if r.refused() {
		return ErrNoFullMedia
	}
	return nil
}

// license reports a refused license token (1000-1002): a login renews it.
func (r *mediaRejection) license() bool {
	return r.request && r.code >= mediaInvalidLicense && r.code <= mediaUnauthorizedLicense
}

// trackToken reports a track token that is invalid or expired: song.getData
// hands out a new one.
func (r *mediaRejection) trackToken() bool {
	return !r.request && (r.code == mediaInvalidTrackToken || r.code == mediaExpiredTrackToken)
}

// refused reports a track the account may not play in full.
func (r *mediaRejection) refused() bool {
	return !r.request && (r.code == mediaUnauthorizedTrack || r.code == mediaCountryMismatch)
}

// asRejection returns err's media rejection, nil when it is none.
func asRejection(err error) *mediaRejection {
	var r *mediaRejection
	if errors.As(err, &r) {
		return r
	}
	return nil
}

const (
	gwURL     = "https://www.deezer.com/ajax/gw-light.php"
	mediaHost = "https://media.deezer.com"
	restURL   = "https://api.deezer.com"
)

// Client holds an authenticated Deezer session.
type Client struct {
	arl             string // immutable after New
	quality         int32  // 0=MP3_128, 1=MP3_320, 2=FLAC (lossless), set atomically
	http            *http.Client
	restURLOverride string // override for testing REST API base URL
	liveURLOverride string // override for testing the live socket URL

	// mu guards the session and identity fields below. Login writes them in one locked
	// block while the control API and UI goroutines read them through locked accessors.
	mu           sync.RWMutex
	apiToken     string
	licenseToken string
	userToken    string // authenticates the live socket (live.go)
	liveSession  string // immutable after New: the UNIQID of this client's announcements (live.go)
	sid          string
	userID       string
	userName     string
	offerName    string        // e.g. "Deezer Premium", "Deezer Free"
	picture      string        // profile picture URL, "" when the user has none
	canHiFi      bool          // account entitled to lossless
	canHQ        bool          // account entitled to MP3_320
	mediaHost    string        // the media service the login named (URL_MEDIA), "" for mediaHost
	gainTarget   float64       // GAIN.TARGET in dB, 0 when the login had none
	serverDelta  time.Duration // Deezer's clock minus the local one, from the login's SERVER_TIMESTAMP

	loginMu        sync.Mutex
	licenseRelogin atomic.Int64 // unix seconds of the last re-login for a license-token rejection
	authRelogin    atomic.Int64 // unix seconds of the last re-login for AUTH_REQUIRED
	refused        atomic.Bool  // the last login was refused (Refused)

	adsDisabled atomic.Bool // the user turned play reporting off (ads.go; free, at own risk)

	// TrackInfo cache (see trackinfo.go): results per track id, at most
	// trackInfoCacheSize, oldest evicted first, and the fetches in flight.
	// infoMu guards all three.
	infoMu     sync.Mutex
	infoCache  map[string]infoEntry
	infoOrder  []string
	infoFlight map[string]*infoCall

	// favMu guards the liked-song ids last read and their checksum (favorites.go).
	favMu       sync.Mutex
	favChecksum string
	favIDs      []string
	explicit    ExplicitContent // the login's explicit-content setting (mu)
	tuner       string          // the account's Flow tuner, read once from Pipe (mu)
	tunerRead   bool            // the tuner was asked for, whatever the answer (mu)

	// pipeMu guards the Pipe JWT (pipe.go), its expiry and refresh token.
	pipeMu     sync.Mutex
	jwt        string
	jwtExp     time.Time
	jwtRefresh string

	// liveMu guards liveWS, the live socket WatchStreams has open (nil while
	// none), and serializes the frames sent on it.
	liveMu sync.Mutex
	liveWS *websocket.Conn
}

// Account summarizes the logged-in user's plan and entitlements.
type Account struct {
	UserID   string `json:"userId"`
	Name     string `json:"name"`
	Offer    string `json:"offer"`
	Picture  string `json:"picture"` // profile picture URL, "" when none
	CanHQ    bool   `json:"canHq"`   // entitled to MP3 320
	CanHiFi  bool   `json:"canHifi"` // entitled to lossless FLAC
	Premium  bool   `json:"premium"` // a paid plan that can stream on-demand
	LoggedIn bool   `json:"loggedIn"`
}

// Quality levels.
const (
	QualityNormal   = 0 // MP3 128
	QualityHigh     = 1 // MP3 320
	QualityLossless = 2 // FLAC (HiFi), MP3 when unavailable
)

// SetQuality selects the preferred stream quality (0..2). Deezer returns the
// first format the account+track is entitled to, so an unentitled FLAC/320
// automatically falls back to a lower MP3 format.
func (c *Client) SetQuality(q int) {
	if q < 0 {
		q = 0
	} else if q > 2 {
		q = 2
	}
	atomic.StoreInt32(&c.quality, int32(q))
}

// Quality returns the current preference (0..2).
func (c *Client) Quality() int { return int(atomic.LoadInt32(&c.quality)) }

// NewUUID is a random (version 4) UUID.
func NewUUID() string {
	var u [16]byte
	_, _ = rand.Read(u[:])
	u[6] = u[6]&0x0f | 0x40
	u[8] = u[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:])
}

// New builds a client for the given ARL (not yet logged in).
func New(arl string) *Client {
	return &Client{
		arl:         strings.TrimSpace(arl),
		liveSession: NewUUID(),
		http: &http.Client{
			Transport: Transport(),
			Timeout:   30 * time.Second,
			// No cookie jar is installed (the Cookie header is set by hand); CheckRedirect only
			// keeps a 10-hop cap, which a bare `return nil` would remove.
			CheckRedirect: func(_ *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return http.ErrUseLastResponse
				}
				return nil
			},
		},
	}
}

// LoggedIn reports whether Login succeeded.
func (c *Client) LoggedIn() bool { return c.apiTok() != "" }

// UserID returns the numeric Deezer user id (after Login).
func (c *Client) UserID() string { return c.uid() }

// Locked accessors for the session fields (safe under concurrent Login).
func (c *Client) apiTok() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.apiToken
}
func (c *Client) uid() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.userID
}
func (c *Client) licTok() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.licenseToken
}

// session returns the API token and the cookie of one login, read together so a
// concurrent re-login cannot pair a new token with an old sid.
func (c *Client) session() (apiToken, cookie string) {
	c.mu.RLock()
	apiToken, sid := c.apiToken, c.sid
	c.mu.RUnlock()
	cookie = "arl=" + c.arl // arl is immutable after New
	if sid != "" {
		cookie += "; sid=" + sid
	}
	return apiToken, cookie
}

// Login authenticates and fetches api_token + license_token + sid + user id.
// Concurrent calls run one at a time.
func (c *Client) Login() error {
	c.loginMu.Lock()
	defer c.loginMu.Unlock()
	return c.login()
}

// reloginIfStale logs in again for a call that met a stale token tok, unless an
// earlier call already did (the token in use differs from tok). For a lost
// session (authRequired) it logs in once a minute; inside the minute the caller
// retries without a login.
func (c *Client) reloginIfStale(tok string, authRequired bool) error {
	c.loginMu.Lock()
	defer c.loginMu.Unlock()
	if c.apiTok() != tok {
		return nil
	}
	if authRequired && !claimRelogin(&c.authRelogin) {
		return nil
	}
	return c.login()
}

// login is Login under loginMu.
func (c *Client) login() error {
	u := gwURL + "?method=deezer.getUserData&input=3&api_version=1.0&api_token="
	req, err := http.NewRequest(http.MethodPost, u, strings.NewReader("{}"))
	if err != nil {
		return err
	}
	req.Header.Set("Cookie", "arl="+c.arl)
	req.Header.Set("Content-Type", "application/json")
	webHeaders(req, "")

	resp, err := c.http.Do(req)
	if err != nil {
		return classifyNet(err)
	}
	defer resp.Body.Close()
	// A 5xx with an empty result is an outage, not an expired ARL.
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("deezer gw deezer.getUserData: %s", resp.Status)
	}

	// Pull sid from Set-Cookie.
	var sid string
	for _, ck := range resp.Cookies() {
		if strings.EqualFold(ck.Name, "sid") {
			sid = ck.Value
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var parsed struct {
		Results struct {
			CheckForm string `json:"checkForm"`
			UserToken string `json:"USER_TOKEN"`
			User      struct {
				UserID    json.Number `json:"USER_ID"`
				BlogName  string      `json:"BLOG_NAME"`
				Firstname string      `json:"FIRSTNAME"`
				Picture   string      `json:"USER_PICTURE"`
				Options   struct {
					LicenseToken   string `json:"license_token"`
					WebHQ          bool   `json:"web_hq"`
					WebLossless    bool   `json:"web_lossless"`
					MobileHQ       bool   `json:"mobile_hq"`
					MobileLossless bool   `json:"mobile_lossless"`
				} `json:"OPTIONS"`
				ExplicitLevel     string   `json:"EXPLICIT_CONTENT_LEVEL"`
				ExplicitAvailable []string `json:"EXPLICIT_CONTENT_LEVELS_AVAILABLE"`
				ExplicitCanEdit   bool     `json:"CAN_EDIT_EXPLICIT_CONTENT_LEVEL"`
			} `json:"USER"`
			Offers []struct {
				Title string `json:"title"`
			} `json:"OFFERS"`
			URLMedia        string `json:"URL_MEDIA"`
			ServerTimestamp int64  `json:"SERVER_TIMESTAMP"`
			Gain            struct {
				Target json.Number `json:"TARGET"`
			} `json:"GAIN"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("parse getUserData: %w", err)
	}
	// An error envelope with empty results is a gateway or quota failure, not an expired
	// ARL, unless it asks for auth.
	if gwErr := gwError(body); gwErr != "" && !strings.Contains(gwErr, "AUTH") {
		return fmt.Errorf("deezer gw deezer.getUserData: %s", gwErr)
	}
	apiToken := parsed.Results.CheckForm
	userID := parsed.Results.User.UserID.String()
	licenseToken := parsed.Results.User.Options.LicenseToken
	if apiToken == "" || userID == "" || userID == "0" {
		// A blank checkForm / user 0 is exactly what Deezer returns for an
		// anonymous (= expired/invalid ARL) session.
		c.refused.Store(true)
		return ErrARLExpired
	}
	userName := parsed.Results.User.BlogName
	if userName == "" {
		userName = parsed.Results.User.Firstname
	}
	opt := parsed.Results.User.Options
	canHQ := opt.WebHQ || opt.MobileHQ
	canHiFi := opt.WebLossless || opt.MobileLossless
	offerName := ""
	if len(parsed.Results.Offers) > 0 {
		offerName = parsed.Results.Offers[0].Title
	}
	if offerName == "" {
		switch {
		case canHiFi:
			offerName = "Deezer (HiFi)"
		case canHQ:
			offerName = "Deezer Premium"
		default:
			offerName = "Deezer Free"
		}
	}
	// Deezer's Android app reads GAIN.TARGET the same way: 0 when unreadable.
	gainTarget, _ := strconv.ParseFloat(parsed.Results.Gain.Target.String(), 64)

	// Commit all session fields atomically so concurrent readers never observe a
	// half-updated session (and concurrent re-logins don't tear each other's writes).
	c.mu.Lock()
	c.sid = sid
	c.apiToken = apiToken
	c.userID = userID
	c.licenseToken = licenseToken
	c.userToken = parsed.Results.UserToken
	c.userName = userName
	c.canHQ = canHQ
	c.canHiFi = canHiFi
	c.offerName = offerName
	c.picture = gwImage("user", parsed.Results.User.Picture)
	c.mediaHost = strings.TrimSuffix(parsed.Results.URLMedia, "/")
	c.gainTarget = gainTarget
	pu := parsed.Results.User
	c.explicit = ExplicitContent{Level: pu.ExplicitLevel, Available: pu.ExplicitAvailable, CanEdit: pu.ExplicitCanEdit}
	if ts := parsed.Results.ServerTimestamp; ts > 0 {
		c.serverDelta = time.Until(time.Unix(ts, 0))
	}
	c.mu.Unlock()
	c.refused.Store(false)
	return nil
}

// Refused reports whether Deezer refused the ARL at the last login: the
// session ended, and only a new sign-in brings one back.
func (c *Client) Refused() bool { return c.refused.Load() }

// GainTarget is the loudness, in dB, the account's tracks are normalized to
// (getUserData GAIN.TARGET; -15 measured on 2026-11-23); 0 before a login.
func (c *Client) GainTarget() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.gainTarget
}

// mediaEndpoint is the get_url URL on the media host the login named.
func (c *Client) mediaEndpoint() string {
	c.mu.RLock()
	host := c.mediaHost
	c.mu.RUnlock()
	if host == "" {
		host = mediaHost
	}
	return host + "/v1/get_url"
}

// Account returns the logged-in user's plan + entitlement summary.
func (c *Client) Account() Account {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Account{
		UserID:  c.userID,
		Name:    c.userName,
		Offer:   c.offerName,
		Picture: c.picture,
		CanHQ:   c.canHQ,
		CanHiFi: c.canHiFi,
		// Premium = entitled to on-demand HQ/lossless streaming. Deezer Free has
		// neither, so it can't actually stream tracks in this client.
		Premium:  c.canHQ || c.canHiFi,
		LoggedIn: c.apiToken != "",
	}
}

// gwRaw performs one gw-light call and returns the raw response body.
func (c *Client) gwRaw(ctx context.Context, method, jsonBody string) (body []byte, apiToken string, err error) {
	apiToken, cookie := c.session()
	if apiToken == "" {
		return nil, apiToken, fmt.Errorf("not logged in")
	}
	u := gwURL + "?method=" + method + "&input=3&api_version=1.0&api_token=" + url.QueryEscape(apiToken) +
		"&cid=" + strconv.Itoa(mrand.IntN(1e9)) // as the web player numbers its calls
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(jsonBody))
	if err != nil {
		return nil, apiToken, err
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Content-Type", "application/json")
	webHeaders(req, c.uid())
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, apiToken, classifyNet(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, apiToken, fmt.Errorf("deezer gw %s: %s", method, resp.Status)
	}
	body, err = io.ReadAll(resp.Body)
	return body, apiToken, err
}

// gwError extracts a gw error message from the response envelope. gw returns
// "error":{} / "error":[] when OK, and a populated object/array otherwise
// (e.g. {"VALID_TOKEN_REQUIRED":"..."}).
func gwError(body []byte) string {
	var env struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &env) != nil {
		return ""
	}
	s := strings.TrimSpace(string(env.Error))
	if s == "" || s == "[]" || s == "{}" || s == "null" {
		return ""
	}
	return s
}

// jsonEsc renders s as a JSON string literal so caller-supplied ids can never
// break out of a gw body built with Sprintf (quotes/backslashes get escaped).
func jsonEsc(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// gw calls a gw method, transparently re-logging in once if the API token has
// expired (Deezer rotates it), and returns an error on a non-empty envelope.
func (c *Client) gw(method, jsonBody string) ([]byte, error) {
	return c.gwCtx(context.Background(), method, jsonBody)
}

// gwCtx is gw bounded by ctx.
func (c *Client) gwCtx(ctx context.Context, method, jsonBody string) ([]byte, error) {
	body, tok, err := c.gwRaw(ctx, method, jsonBody)
	if err != nil {
		return nil, err
	}
	gwErr := gwError(body)
	// Stale token, or a lost session (once a minute: a method that always answers
	// AUTH_REQUIRED must not log in on every call): re-login once and retry.
	stale, lost := strings.Contains(gwErr, "TOKEN"), strings.Contains(gwErr, "AUTH_REQUIRED")
	if stale || lost {
		if err := c.reloginIfStale(tok, !stale); err != nil {
			return nil, fmt.Errorf("re-login: %w", err)
		}
		body, _, err = c.gwRaw(ctx, method, jsonBody)
		if err != nil {
			return nil, err
		}
		gwErr = gwError(body)
	}
	if gwErr != "" {
		return nil, fmt.Errorf("deezer gw %s: %s", method, gwErr)
	}
	return body, nil
}

func (c *Client) getRestURL() string {
	if c.restURLOverride != "" {
		return c.restURLOverride
	}
	return restURL
}

// restGet calls the public REST API (no auth needed). REST reports failures as
// an HTTP-200 body with an "error" envelope ({"error":{"type":...,"code":...}}),
// so that is surfaced as a Go error rather than decoding to empty results.
func (c *Client) restGet(path string) ([]byte, error) {
	return c.restGetCtx(context.Background(), path)
}

// restQuotaWait is how long a REST call over Deezer's request quota waits before its
// one retry.
var restQuotaWait = 5 * time.Second

// errRestQuota marks a REST answer over the request quota (error code 4).
var errRestQuota = errors.New("request quota exceeded")

// restGetCtx is restGet bounded by ctx; over the request quota it waits restQuotaWait and
// tries once more.
func (c *Client) restGetCtx(ctx context.Context, path string) ([]byte, error) {
	b, err := c.restGetOnce(ctx, path)
	if !errors.Is(err, errRestQuota) {
		return b, err
	}
	t := time.NewTimer(restQuotaWait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.C:
	}
	return c.restGetOnce(ctx, path)
}

func (c *Client) restGetOnce(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.getRestURL()+path, nil)
	if err != nil {
		return nil, err
	}
	webHeaders(req, "")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, classifyNet(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("deezer rest %s: HTTP %d", path, resp.StatusCode)
	}
	var env struct {
		Error *struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &env) == nil && env.Error != nil &&
		(env.Error.Type != "" || env.Error.Message != "" || env.Error.Code != 0) {
		msg := env.Error.Message
		if msg == "" {
			msg = env.Error.Type
		}
		if env.Error.Code == restNoData {
			return nil, fmt.Errorf("deezer rest %s: %s: %w", path, msg, ErrNotFound)
		}
		if env.Error.Code == restQuota {
			return nil, fmt.Errorf("deezer rest %s: %s: %w", path, msg, errRestQuota)
		}
		return nil, fmt.Errorf("deezer rest %s: %s (code %d)", path, msg, env.Error.Code)
	}
	return b, nil
}

// gwImage builds a 250x250 CDN image URL from a gw md5 hash. The CDN files images
// by kind ("cover", "playlist", "user", "artist") and answers a wrong kind with a
// blank placeholder (HTTP 200), so the kind must match the hash's.
func gwImage(kind, md5 string) string {
	return gwImageSize(kind, md5, 250)
}

// gwImageSize is gwImage for a square edge of px pixels; 1000 is the largest
// size the CDN serves (the REST *_xl URLs).
func gwImageSize(kind, md5 string, px int) string {
	if md5 == "" {
		return ""
	}
	return fmt.Sprintf("https://e-cdns-images.dzcdn.net/images/%s/%s/%dx%d-000000-80-0-0.jpg", kind, md5, px, px)
}

// ---- REST DTOs ----

type restArtist struct {
	ID   json.Number `json:"id"`
	Name string      `json:"name"`
}
type restTrackDTO struct {
	ID            json.Number `json:"id"`
	Title         string      `json:"title"`
	Duration      json.Number `json:"duration"`
	ExplicitLyric bool        `json:"explicit_lyrics"`
	// TrackPosition is the album track number, present on album-tracks listings.
	TrackPosition json.Number `json:"track_position"`
	// ReleaseDate is an ISO date ("2021-06-15"); present on track/album objects.
	ReleaseDate string     `json:"release_date"`
	Artist      restArtist `json:"artist"`
	Album       struct {
		ID          json.Number `json:"id"`
		Title       string      `json:"title"`
		CoverMedium string      `json:"cover_medium"`
	} `json:"album"`
}

func (r restTrackDTO) toTrack() Track {
	durSec, _ := r.Duration.Int64()
	pos, _ := r.TrackPosition.Int64()
	return Track{
		ID:          r.ID.String(),
		Name:        r.Title,
		DurationMS:  durSec * 1000,
		Artists:     []Artist{{ID: r.Artist.ID.String(), Name: r.Artist.Name}},
		AlbumName:   r.Album.Title,
		AlbumID:     r.Album.ID.String(),
		ArtworkURL:  r.Album.CoverMedium,
		Explicit:    r.ExplicitLyric,
		TrackNumber: int(pos),
		Year:        yearFromDate(r.ReleaseDate),
	}
}

// yearFromDate extracts a 4-digit release year from a Deezer ISO date
// ("2021-06-15" -> "2021"). It returns "" when the date is absent or does not
// begin with four digits, so the tagger leaves the year field unset.
func yearFromDate(date string) string {
	if len(date) < 4 {
		return ""
	}
	y := date[:4]
	for i := 0; i < 4; i++ {
		if y[i] < '0' || y[i] > '9' {
			return ""
		}
	}
	return y
}

// ---- gw DTOs (mixed string/number ids; ALL-CAPS keys) ----

type gwTrackDTO struct {
	SngID      json.Number `json:"SNG_ID"`
	SngTitle   string      `json:"SNG_TITLE"`
	Duration   json.Number `json:"DURATION"`
	ArtID      json.Number `json:"ART_ID"`
	ArtName    string      `json:"ART_NAME"`
	AlbID      json.Number `json:"ALB_ID"`
	AlbTitle   string      `json:"ALB_TITLE"`
	AlbPicture string      `json:"ALB_PICTURE"`
	Explicit   json.Number `json:"EXPLICIT_LYRICS"` // "0"/"1" (or absent)
	// Fallback is the version Deezer serves instead when the track has no
	// rights in the account's country; absent otherwise.
	Fallback gwFallback `json:"FALLBACK"`
	// Rights says where the track streams; {} when nowhere (Deezer's players then grey
	// it out), absent from replies that do not list it.
	Rights *gwRights `json:"RIGHTS"`
}

// gwRights is where a track streams.
type gwRights struct {
	Ads bool `json:"STREAM_ADS_AVAILABLE"`
	Sub bool `json:"STREAM_SUB_AVAILABLE"`
}

// UnmarshalJSON reads [], the gateway's way of writing an empty object, as no rights.
func (r *gwRights) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '[' {
		*r = gwRights{}
		return nil
	}
	type plain gwRights
	return json.Unmarshal(b, (*plain)(r))
}

// gwFallback is the part of a track's FALLBACK version Purple Disc uses.
type gwFallback struct {
	SngID      json.Number `json:"SNG_ID"`
	TrackToken string      `json:"TRACK_TOKEN"`
	AlbPicture string      `json:"ALB_PICTURE"`
}

// toTrack converts a gw track. A track with a fallback shows the fallback's
// cover, as Deezer's web player does: it is the version that plays, and the
// refused track's own picture is often Deezer's placeholder.
func (g gwTrackDTO) toTrack() Track {
	durSec, _ := g.Duration.Int64()
	exp, _ := g.Explicit.Int64()
	pic := g.AlbPicture
	if g.Fallback.AlbPicture != "" {
		pic = g.Fallback.AlbPicture
	}
	return Track{
		ID:         g.SngID.String(),
		Name:       g.SngTitle,
		DurationMS: durSec * 1000,
		Artists:    []Artist{{ID: g.ArtID.String(), Name: g.ArtName}},
		AlbumName:  g.AlbTitle,
		AlbumID:    g.AlbID.String(),
		ArtworkURL: gwImage("cover", pic),
		Explicit:   exp != 0,
		// Without rights a track plays only through its fallback version.
		Unavailable: g.Rights != nil && !g.Rights.Ads && !g.Rights.Sub && g.Fallback.TrackToken == "",
	}
}

// Search queries tracks, albums, artists and playlists concurrently. It succeeds when
// at least one category does; when all four fail the errors are joined and returned.
func (c *Client) Search(query string) (*SearchResults, error) {
	enc := url.QueryEscape(query)

	var (
		wg sync.WaitGroup

		errTracks    error
		tracksResult []Track

		errAlbums    error
		albumsResult []Album

		errArtists    error
		artistsResult []ArtistInfo

		errPlaylists    error
		playlistsResult []Playlist
	)

	wg.Add(4)

	// 1. Tracks sub-search
	go func() {
		defer wg.Done()
		b, err := c.restGet("/search?q=" + enc + "&limit=40")
		if err != nil {
			errTracks = err
			return
		}
		var r struct {
			Data []restTrackDTO `json:"data"`
		}
		if err = json.Unmarshal(b, &r); err != nil {
			errTracks = fmt.Errorf("json unmarshal: %w", err)
			return
		}
		tracksResult = make([]Track, 0, len(r.Data))
		for _, t := range r.Data {
			tracksResult = append(tracksResult, t.toTrack())
		}
	}()

	// 2. Albums sub-search
	go func() {
		defer wg.Done()
		b, err := c.restGet("/search/album?q=" + enc + "&limit=20")
		if err != nil {
			errAlbums = err
			return
		}
		var r struct {
			Data []restAlbumDTO `json:"data"`
		}
		if err = json.Unmarshal(b, &r); err != nil {
			errAlbums = fmt.Errorf("json unmarshal: %w", err)
			return
		}
		albumsResult = make([]Album, 0, len(r.Data))
		for _, a := range r.Data {
			albumsResult = append(albumsResult, a.toAlbum())
		}
	}()

	// 3. Artists sub-search
	go func() {
		defer wg.Done()
		b, err := c.restGet("/search/artist?q=" + enc + "&limit=20")
		if err != nil {
			errArtists = err
			return
		}
		var r struct {
			Data []restArtistDTO `json:"data"`
		}
		if err = json.Unmarshal(b, &r); err != nil {
			errArtists = fmt.Errorf("json unmarshal: %w", err)
			return
		}
		artistsResult = make([]ArtistInfo, 0, len(r.Data))
		for _, a := range r.Data {
			artistsResult = append(artistsResult, a.toArtistInfo())
		}
	}()

	// 4. Playlists sub-search
	go func() {
		defer wg.Done()
		b, err := c.restGet("/search/playlist?q=" + enc + "&limit=20")
		if err != nil {
			errPlaylists = err
			return
		}
		var r struct {
			Data []struct {
				ID            json.Number `json:"id"`
				Title         string      `json:"title"`
				User          restUser    `json:"user"`
				NbTracks      int         `json:"nb_tracks"`
				PictureMedium string      `json:"picture_medium"`
			} `json:"data"`
		}
		if err = json.Unmarshal(b, &r); err != nil {
			errPlaylists = fmt.Errorf("json unmarshal: %w", err)
			return
		}
		playlistsResult = make([]Playlist, 0, len(r.Data))
		for _, p := range r.Data {
			playlistsResult = append(playlistsResult, Playlist{
				ID:         p.ID.String(),
				Name:       p.Title,
				Owner:      p.User.Name,
				OwnerID:    p.User.ID.String(),
				TrackCount: p.NbTracks,
				ArtworkURL: p.PictureMedium,
			})
		}
	}()

	wg.Wait()

	var errs []error
	if errTracks != nil {
		errs = append(errs, fmt.Errorf("tracks: %w", errTracks))
	}
	if errAlbums != nil {
		errs = append(errs, fmt.Errorf("albums: %w", errAlbums))
	}
	if errArtists != nil {
		errs = append(errs, fmt.Errorf("artists: %w", errArtists))
	}
	if errPlaylists != nil {
		errs = append(errs, fmt.Errorf("playlists: %w", errPlaylists))
	}

	if len(errs) == 4 {
		return nil, errors.Join(errs...)
	}

	sr := &SearchResults{
		Tracks:    tracksResult,
		Albums:    albumsResult,
		Artists:   artistsResult,
		Playlists: playlistsResult,
	}
	return sr, nil
}

// albumPageSize is the per-request page for the REST album tracklist.
const albumPageSize = 100

// AlbumTracks lists an album's tracks via the REST API, paging (100 per page) up to
// maxTracks. Any page error fails the whole listing, since SaveAlbum needs a complete
// tracklist.
func (c *Client) AlbumTracks(id string) ([]Track, error) {
	ts, _, err := c.AlbumTracksTotal(id)
	return ts, err
}

// AlbumTracksTotal is AlbumTracks with the album's declared track count (the
// first page's "total"; 0 when the reply has none).
func (c *Client) AlbumTracksTotal(id string) (out []Track, total int, err error) {
	for index := 0; index < maxTracks; index += albumPageSize {
		b, err := c.restGet(fmt.Sprintf("/album/%s/tracks?limit=%d&index=%d", id, albumPageSize, index))
		if err != nil {
			return nil, 0, err
		}
		var r struct {
			Data  []restTrackDTO `json:"data"`
			Next  string         `json:"next"`
			Total int            `json:"total"`
		}
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, 0, err
		}
		if index == 0 {
			total = r.Total
		}
		for _, t := range r.Data {
			out = append(out, t.toTrack())
		}
		// A short/empty page or a response without a "next" link means we've
		// reached the end.
		if len(r.Data) < albumPageSize || r.Next == "" {
			break
		}
	}
	return out, total, nil
}

// pageSize is the per-request page for paginated gw lists.
const pageSize = 200

// maxTracks caps a paginated fetch so a huge library can't run away.
const maxTracks = 5000

// gwTrackPage fetches one page of gw tracks for a method whose body is
// "<extra>,\"nb\":<n>,\"start\":<s>", with the list's declared length
// (results.total; 0 when the reply has none).
func (c *Client) gwTrackPage(method, extra string, start, nb int) ([]Track, int, error) {
	body := fmt.Sprintf(`{%s,"nb":%d,"start":%d}`, extra, nb, start)
	b, err := c.gw(method, body)
	if err != nil {
		return nil, 0, err
	}
	var r struct {
		Results struct {
			Data  []gwTrackDTO `json:"data"`
			Total int          `json:"total"`
		} `json:"results"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, 0, err
	}
	out := make([]Track, 0, len(r.Results.Data))
	for _, t := range r.Results.Data {
		out = append(out, t.toTrack())
	}
	return out, r.Results.Total, nil
}

// gwTrackAll pages through a gw track list until it's exhausted.
func (c *Client) gwTrackAll(method, extra string) ([]Track, error) {
	var all []Track
	for start := 0; start < maxTracks; start += pageSize {
		page, _, err := c.gwTrackPage(method, extra, start, pageSize)
		if err != nil {
			if len(all) > 0 {
				return all, nil // keep what we have on a mid-fetch error
			}
			return nil, err
		}
		all = append(all, page...)
		if len(page) < pageSize {
			break
		}
	}
	return all, nil
}

// PlaylistTracks lists a playlist's tracks (gw, private playlists too). A page error
// mid-fetch gives a partial list and nil error; batch downloads use
// PlaylistTracksStrict.
func (c *Client) PlaylistTracks(id string) ([]Track, error) {
	return c.gwTrackAll("playlist.getSongs", fmt.Sprintf(`"playlist_id":%s`, jsonEsc(id)))
}

// PlaylistTracksStrict is PlaylistTracks where any page error fails the listing, for
// SavePlaylist and offline pins. total is the playlist's declared length (0 when
// absent).
func (c *Client) PlaylistTracksStrict(id string) (ts []Track, total int, err error) {
	extra := fmt.Sprintf(`"playlist_id":%s`, jsonEsc(id))
	ts, err = collectTrackPagesStrict(func(start, nb int) ([]Track, error) {
		page, n, err := c.gwTrackPage("playlist.getSongs", extra, start, nb)
		if start == 0 {
			total = n
		}
		return page, err
	})
	return ts, total, err
}

// collectTrackPagesStrict pages through a track list via fetch(start, nb),
// returning nil and the error if ANY page fails (no partial tolerance).
// Bounded by maxTracks like gwTrackAll.
func collectTrackPagesStrict(fetch func(start, nb int) ([]Track, error)) ([]Track, error) {
	var all []Track
	for start := 0; start < maxTracks; start += pageSize {
		page, err := fetch(start, pageSize)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < pageSize {
			break
		}
	}
	return all, nil
}

// Track fetches a single track's metadata by id (gw song.getData). Used by the
// control API / MCP to "play track <id>".
func (c *Client) Track(id string) (Track, error) {
	b, err := c.gw("song.getData", fmt.Sprintf(`{"sng_id":%s}`, jsonEsc(id)))
	if err != nil {
		return Track{}, err
	}
	var r struct {
		Results gwTrackDTO `json:"results"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return Track{}, err
	}
	t := r.Results.toTrack()
	if t.ID == "" {
		return Track{}, fmt.Errorf("track %s: not found", id)
	}
	return t, nil
}

// Playlists lists the user's playlists and the ones they follow (gw pageProfile,
// the whole tab in one read), each with its owner's user id.
func (c *Client) Playlists() ([]Playlist, error) {
	var r struct {
		Results struct {
			Tab struct {
				Playlists struct {
					Data []struct {
						PlaylistID      json.Number `json:"PLAYLIST_ID"`
						Title           string      `json:"TITLE"`
						NbSong          json.Number `json:"NB_SONG"`
						PlaylistPicture string      `json:"PLAYLIST_PICTURE"`
						PictureType     string      `json:"PICTURE_TYPE"`
						ParentUserID    json.Number `json:"PARENT_USER_ID"`
					} `json:"data"`
				} `json:"playlists"`
			} `json:"TAB"`
		} `json:"results"`
	}
	if err := c.profileTab("playlists", &r); err != nil {
		return nil, err
	}
	out := make([]Playlist, 0, len(r.Results.Tab.Playlists.Data))
	for _, p := range r.Results.Tab.Playlists.Data {
		n, _ := p.NbSong.Int64()
		out = append(out, Playlist{
			ID:         p.PlaylistID.String(),
			Name:       p.Title,
			TrackCount: int(n),
			OwnerID:    p.ParentUserID.String(),
			ArtworkURL: playlistImage(p.PictureType, p.PlaylistPicture),
		})
	}
	return out, nil
}

// PlaylistInfo returns one playlist's title, picture and track count (gw
// playlist.getData, private playlists too).
func (c *Client) PlaylistInfo(id string) (Playlist, error) {
	b, err := c.gw("playlist.getData", fmt.Sprintf(`{"playlist_id":%s}`, jsonEsc(id)))
	if err != nil {
		return Playlist{}, err
	}
	var r struct {
		Results struct {
			Title           string      `json:"TITLE"`
			NbSong          json.Number `json:"NB_SONG"`
			PlaylistPicture string      `json:"PLAYLIST_PICTURE"`
			PictureType     string      `json:"PICTURE_TYPE"`
		} `json:"results"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return Playlist{}, err
	}
	n, _ := r.Results.NbSong.Int64()
	return Playlist{ID: id, Name: r.Results.Title, TrackCount: int(n), ArtworkURL: playlistImage(r.Results.PictureType, r.Results.PlaylistPicture)}, nil
}

// playlistImage is a playlist's picture URL: PICTURE_TYPE "playlist" for
// editorial and dynamic pictures, else "cover" (also the four-hash mosaics of
// user playlists).
func playlistImage(pictureType, md5 string) string {
	if pictureType == "playlist" {
		return gwImage("playlist", md5)
	}
	return gwImage("cover", md5)
}

// songData fetches song.getData once: metadata, the track token for media URLs, the
// fallback version Deezer serves when the track has no rights in the country (zero
// when none), and the loudness (GAIN, dB).
func (c *Client) songData(trackID string) (t Track, token string, fallback gwFallback, gainDB float64, err error) {
	b, err := c.gw("song.getData", fmt.Sprintf(`{"sng_id":%s}`, jsonEsc(trackID)))
	if err != nil {
		return Track{}, "", gwFallback{}, 0, err
	}
	var r struct {
		Results struct {
			gwTrackDTO
			TrackToken string      `json:"TRACK_TOKEN"`
			Gain       json.Number `json:"GAIN"`
		} `json:"results"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return Track{}, "", gwFallback{}, 0, err
	}
	g, _ := strconv.ParseFloat(r.Results.Gain.String(), 64)
	return r.Results.toTrack(), r.Results.TrackToken, r.Results.Fallback, g, nil
}

// StreamPlan is the resolved CDN URL + track id for decryption.
type StreamPlan struct {
	CDNURL string
	// AltURLs are the media service's other sources for the same stream, tried in order
	// when CDNURL's host fails.
	AltURLs []string `json:"-"`
	TrackID string
	// KeyID is the id of the track whose stream CDNURL serves when Deezer
	// refused TrackID and served the version song.getData names as its
	// fallback; "" when the stream is TrackID's own.
	KeyID  string
	Format string
	// AskedFormat is the format the resolve asked for first (the best quality and account
	// allow); Format is what Deezer served. "" for a plan not from PrepareStream.
	AskedFormat string  `json:"-"`
	GainDB      float64 // the track's loudness in dB (Deezer's GAIN; 0 if unknown)
	Encrypted   bool    // false for plain CDN streams (e.g. podcast episodes)
	Preview     bool    // true when this is Deezer's public 30-second preview

	// Track is the track's metadata from the song.getData call that resolved
	// the plan; zero for a cache-sourced plan and for podcast episodes.
	Track Track `json:"-"`

	// Refresh resolves a fresh plan for the same track when the signed CDN URL expires
	// mid-stream (403/410). Set by Client.PrepareStream; nil when unavailable (podcasts,
	// hand-built plans). Not serialized.
	Refresh func() (*StreamPlan, error) `json:"-"`
}

// CipherID is the id whose key decrypts the plan's stream.
func (p *StreamPlan) CipherID() string {
	if p.KeyID != "" {
		return p.KeyID
	}
	return p.TrackID
}

// wantedFormat is the first format resolveMediaURL asks for: the best the
// quality setting and the account allow.
func (c *Client) wantedFormat() string { return c.WantedFormat(c.Quality()) }

// WantedFormat is the first format a stream at quality q is asked for: the
// best that quality and the account allow.
func (c *Client) WantedFormat(q int) string {
	c.mu.RLock()
	canHQ, canHiFi := c.canHQ, c.canHiFi
	c.mu.RUnlock()
	switch {
	case q == QualityLossless && canHiFi:
		return "FLAC"
	case q >= QualityHigh && canHQ:
		return "MP3_320"
	}
	return "MP3_128"
}

// fullMedia is get_url's request for the full stream at quality q: formats in
// preference order, Deezer serving the first the account and track have. Tiers the
// account lacks are never asked for.
func (c *Client) fullMedia(q int) string {
	const (
		f128  = `{"cipher":"BF_CBC_STRIPE","format":"MP3_128"}`
		f64   = `{"cipher":"BF_CBC_STRIPE","format":"MP3_64"}`
		fmisc = `{"cipher":"BF_CBC_STRIPE","format":"MP3_MISC"}`
		f320  = `{"cipher":"BF_CBC_STRIPE","format":"MP3_320"}`
		fflac = `{"cipher":"BF_CBC_STRIPE","format":"FLAC"}`
	)
	c.mu.RLock()
	canHQ, canHiFi := c.canHQ, c.canHiFi
	c.mu.RUnlock()
	var order []string
	if q == QualityLossless && canHiFi {
		order = append(order, fflac)
	}
	if q >= QualityHigh && canHQ {
		order = append(order, f320)
	}
	order = append(order, f128, f64, fmisc)
	return `{"type":"FULL","formats":[` + strings.Join(order, ",") + `]}`
}

// previewMedia is get_url's request for the plain 30-second preview, as
// Deezer's players ask for it.
const previewMedia = `{"type":"PREVIEW","formats":[{"cipher":"NONE","format":"MP3_128"}]}`

// resolveMediaURL asks the media service for media of trackToken, retrying a busy
// service or dropped connection. A refused license token is renewed by a login at most
// once a minute for all calls; other refusals return their *mediaRejection.
func (c *Client) resolveMediaURL(trackToken, media string) (urls []string, format string, err error) {
	license := c.licTok()
	if license == "" || trackToken == "" {
		return nil, "", fmt.Errorf("missing license or track token")
	}
	const maxAttempts = 3
	renewed := false
	for attempt := 1; ; attempt++ {
		urls, format, err = c.getMediaURL(license, trackToken, media)
		if err == nil {
			return urls, format, nil
		}
		if r := asRejection(err); r != nil && r.license() && !renewed {
			renewed = true
			if c.claimLicenseRelogin() {
				if lerr := c.Login(); lerr != nil {
					return nil, "", fmt.Errorf("%w (re-login: %v)", err, lerr)
				}
			} else if c.licTok() == license {
				return nil, "", err
			}
			license = c.licTok()
			continue
		}
		busy := errors.Is(err, errMediaBusy) || IsNoNetwork(err)
		if !busy || attempt == maxAttempts {
			return nil, "", err
		}
		time.Sleep(time.Duration(50*attempt) * time.Millisecond)
	}
}

// claimLicenseRelogin reports whether this call may re-log in for a license token
// rejection: the first call in a minute claims it.
func (c *Client) claimLicenseRelogin() bool { return claimRelogin(&c.licenseRelogin) }

// claimRelogin reports whether the first call in a minute claims the re-login whose
// last time (unix seconds) last holds.
func claimRelogin(last *atomic.Int64) bool {
	now, prev := time.Now().Unix(), last.Load()
	return now-prev >= 60 && last.CompareAndSwap(prev, now)
}

// getMediaURL performs one get_url call. A refusal is a *mediaRejection (top-level
// license errors or the track's own); a 5xx or 429 is errMediaBusy; a reply without
// media or errors is an error. The sources come in the service's order.
func (c *Client) getMediaURL(licenseToken, trackToken, media string) (urls []string, format string, err error) {
	body := `{"license_token":` + jsonEsc(licenseToken) + `,"media":[` + media + `],"track_tokens":[` + jsonEsc(trackToken) + `]}`
	req, err := http.NewRequest(http.MethodPost, c.mediaEndpoint(), strings.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	webHeaders(req, "")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", classifyNet(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return nil, "", fmt.Errorf("get_url: HTTP %d: %w", resp.StatusCode, errMediaBusy)
	}
	type mediaError struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	var r struct {
		Errors []mediaError `json:"errors"`
		Data   []struct {
			Errors []mediaError `json:"errors"`
			Media  []struct {
				Format  string `json:"format"`
				Sources []struct {
					URL string `json:"url"`
				} `json:"sources"`
			} `json:"media"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, "", fmt.Errorf("get_url: HTTP %d: unreadable reply", resp.StatusCode)
	}
	if len(r.Errors) > 0 {
		return nil, "", &mediaRejection{code: r.Errors[0].Code, message: r.Errors[0].Message, request: true}
	}
	if len(r.Data) > 0 {
		d := r.Data[0]
		if len(d.Errors) > 0 {
			return nil, "", &mediaRejection{code: d.Errors[0].Code, message: d.Errors[0].Message}
		}
		if len(d.Media) > 0 {
			for _, s := range d.Media[0].Sources {
				if s.URL != "" {
					urls = append(urls, s.URL)
				}
			}
			if len(urls) > 0 {
				return urls, d.Media[0].Format, nil
			}
		}
	}
	return nil, "", fmt.Errorf("get_url: HTTP %d: %w", resp.StatusCode, errNoSource)
}

// PrepareStream resolves a track id to a playable stream at the client's
// quality (PrepareStreamAt).
func (c *Client) PrepareStream(trackID string) (*StreamPlan, error) {
	return c.PrepareStreamAt(trackID, c.Quality())
}

// PrepareStreamAt is PrepareStream at quality q (0..2). A refused license token is
// renewed, an expired track token fetched again once, and an unplayable track (2002,
// 2004) plays its fallback version or the 30-second preview.
func (c *Client) PrepareStreamAt(trackID string, q int) (*StreamPlan, error) {
	t, tok, fb, gain, err := c.songData(trackID)
	if err != nil {
		return nil, err
	}
	asked, media := c.WantedFormat(q), c.fullMedia(q)
	u, format, err := c.resolveMediaURL(tok, media)
	if r := asRejection(err); r != nil && r.trackToken() {
		if t, tok, fb, gain, err = c.songData(trackID); err != nil {
			return nil, err
		}
		u, format, err = c.resolveMediaURL(tok, media)
	}
	keyID := ""
	// A track refused for rights, or answered without a source, plays the version
	// Deezer names as its fallback, as Deezer's web player does.
	if (errors.Is(err, ErrNoFullMedia) || errors.Is(err, errNoSource)) && fb.TrackToken != "" {
		if fu, fformat, fErr := c.resolveMediaURL(fb.TrackToken, media); fErr == nil {
			u, format, keyID, err = fu, fformat, fb.SngID.String(), nil
		}
	}
	if err == nil {
		// Refresh re-resolves a fresh signed URL for the same track when the
		// CDN's expires mid-download.
		return &StreamPlan{
			CDNURL: u[0], AltURLs: u[1:], TrackID: trackID, KeyID: keyID, Format: format, AskedFormat: asked, GainDB: gain, Encrypted: true, Track: t,
			Refresh: func() (*StreamPlan, error) { return c.PrepareStreamAt(trackID, q) },
		}, nil
	}
	if !errors.Is(err, ErrNoFullMedia) {
		return nil, err
	}
	pu, pformat, pErr := c.resolveMediaURL(tok, previewMedia)
	if pErr != nil {
		return nil, fmt.Errorf("%w; preview: %v", err, pErr)
	}
	return &StreamPlan{CDNURL: pu[0], AltURLs: pu[1:], TrackID: trackID, Format: pformat, GainDB: gain, Encrypted: false, Preview: true, Track: t}, nil
}

// PrepareStreamCached plans a stream from the media cache when it holds the
// track in the wanted format or better (signed out: in any format): CDNURL is
// "" and the caller reads the cache. Otherwise it is PrepareStream.
func (c *Client) PrepareStreamCached(trackID string, cache *mediacache.Cache) (*StreamPlan, error) {
	if cache != nil {
		// Online, a cached copy plays only at the format a stream would ask for or better, or
		// when it was fetched asking for that format or better; offline the best copy plays.
		want := mediacache.FormatRank(c.wantedFormat())
		if m, ok := cache.GetMetaForTrack(trackID); ok && (!c.LoggedIn() ||
			mediacache.FormatRank(m.Format) >= want || mediacache.FormatRank(m.AskedFormat) >= want) {
			return &StreamPlan{
				CDNURL:      "", // signals cache-sourced; audio path uses mc.Get to obtain body
				TrackID:     trackID,
				KeyID:       m.KeyID,
				Format:      m.Format,
				AskedFormat: m.AskedFormat,
				GainDB:      m.GainDB,
				Encrypted:   m.Encrypted,
				Preview:     m.Preview,
				// The entry can go before the track opens (evicted, expired,
				// cleared): the player then streams it like a resolved plan.
				Refresh: func() (*StreamPlan, error) { return c.PrepareStream(trackID) },
			}, nil
		}
	}
	// Miss or no cache: normal path (requires login).
	return c.PrepareStream(trackID)
}

// TrackIDOf extracts a numeric id from "deezer:track:123", a URL, or "123".
// Returns "" when no valid id is present.
func TrackIDOf(uri string) string {
	uri = strings.TrimSpace(uri)
	// Drop query/fragment first: share URLs append params whose digits would
	// otherwise pollute the id (e.g. ?utm_content=track-<id>, ?host=0).
	if i := strings.IndexAny(uri, "?#"); i >= 0 {
		uri = uri[:i]
	}
	uri = strings.TrimSuffix(uri, "/")
	if i := strings.LastIndexAny(uri, ":/"); i >= 0 {
		uri = uri[i+1:]
	}
	// Valid ids are all digits, optionally with a leading '-' (user uploads).
	digits := strings.TrimPrefix(uri, "-")
	if digits == "" {
		return ""
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return uri
}
