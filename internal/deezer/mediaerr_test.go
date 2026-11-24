package deezer

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// mediaFake answers song.getData, deezer.getUserData and get_url like Deezer (measured
// 2026-11-23): FULL requests get full(license, track token), PREVIEW a plain preview.
// Each login hands out the next license token, each song.getData the next track token.
type mediaFake struct {
	full                 func(license, token string) (int, string)
	logins, songs, calls int
	previews             int
	host                 string // the URL_MEDIA a login names; "" for none
}

func (f *mediaFake) client() *Client {
	c := New("dummy")
	loggedInPremium(c)
	c.mu.Lock()
	c.licenseToken = "lic-1"
	c.userID = "1"
	c.mu.Unlock()
	c.restURLOverride = "http://rest.test"
	c.http = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		q := r.URL.RawQuery
		switch {
		case strings.Contains(q, "method=deezer.getUserData"):
			f.logins++
			media := ""
			if f.host != "" {
				media = `,"URL_MEDIA":"` + f.host + `"`
			}
			return jsonResponse([]byte(`{"error":{},"results":{"checkForm":"api-t","USER":{"USER_ID":"1","BLOG_NAME":"t","OPTIONS":` +
				`{"license_token":"lic-` + string(rune('1'+f.logins)) + `","web_hq":true}},"OFFERS":[{"title":"P"}]` + media + `}}`)), nil
		case strings.Contains(q, "method=song.getData"):
			f.songs++
			return jsonResponse([]byte(`{"error":{},"results":{"SNG_ID":"123","TRACK_TOKEN":"tok-` + string(rune('0'+f.songs)) + `","GAIN":"-7"}}`)), nil
		case strings.HasSuffix(r.URL.Path, "/v1/get_url"):
			f.calls++
			var req struct {
				License string   `json:"license_token"`
				Tokens  []string `json:"track_tokens"`
				Media   []struct {
					Type    string `json:"type"`
					Formats []struct {
						Cipher string `json:"cipher"`
						Format string `json:"format"`
					} `json:"formats"`
				} `json:"media"`
			}
			b, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(b, &req); err != nil || len(req.Tokens) != 1 || len(req.Media) != 1 {
				return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"errors":[{"code":0,"message":"bad request"}]}`))}, nil
			}
			if req.Media[0].Type == "PREVIEW" {
				f.previews++
				if len(req.Media[0].Formats) != 1 || req.Media[0].Formats[0].Cipher != "NONE" || req.Media[0].Formats[0].Format != "MP3_128" {
					return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"errors":[{"code":0,"message":"bad preview request"}]}`))}, nil
				}
				return jsonResponse([]byte(`{"data":[{"media":[{"media_type":"PREVIEW","cipher":{"type":"NONE"},"format":"MP3_128","sources":[{"url":"https://cdn/preview.mp3"}]}]}]}`)), nil
			}
			status, body := f.full(req.License, req.Tokens[0])
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
		}
		return nil, io.ErrUnexpectedEOF
	})}
	return c
}

const fullOK = `{"data":[{"media":[{"media_type":"FULL","cipher":{"type":"BF_CBC_STRIPE"},"format":"MP3_128","sources":[{"url":"https://cdn/full.mp3"}]}]}]}`

func perTrackError(code int, msg string) string {
	b, _ := json.Marshal(map[string]any{"data": []any{map[string]any{"errors": []any{map[string]any{"code": code, "message": msg}}}}})
	return string(b)
}

func topLevelError(code int, msg string) string {
	b, _ := json.Marshal(map[string]any{"errors": []any{map[string]any{"code": code, "message": msg}}})
	return string(b)
}

// A license token Deezer refuses (top-level 1000-1002, HTTP 400 as measured)
// is renewed with a login and the track plays in full; it never falls back to
// the preview.
func TestPrepareStream_LicenseRejectionRenewsLicense(t *testing.T) {
	for _, code := range []int{1000, 1001, 1002} {
		f := &mediaFake{full: func(license, _ string) (int, string) {
			if license == "lic-1" {
				return 400, topLevelError(code, "An error occurred while decoding license token.")
			}
			return 200, fullOK
		}}
		plan, err := f.client().PrepareStream("123")
		if err != nil || plan.Preview || plan.CDNURL != "https://cdn/full.mp3" {
			t.Fatalf("code %d: plan %+v, err %v; want the full track", code, plan, err)
		}
		if f.logins != 1 || f.previews != 0 {
			t.Errorf("code %d: %d logins, %d previews; want 1 login, no preview", code, f.logins, f.previews)
		}
	}
}

// A license still refused after the renewal is an error, not the preview.
func TestPrepareStream_LicenseStillRefusedIsError(t *testing.T) {
	f := &mediaFake{full: func(string, string) (int, string) { return 400, topLevelError(1001, "Expired license token") }}
	plan, err := f.client().PrepareStream("123")
	if err == nil || plan != nil || IsNoFullMedia(err) {
		t.Fatalf("plan %+v, err %v; want an error that is not an entitlement refusal", plan, err)
	}
	if f.previews != 0 {
		t.Errorf("%d previews, want 0", f.previews)
	}
}

// An invalid or expired track token (2000, 2001) is fetched again and the
// track plays in full, with no login.
func TestPrepareStream_TrackTokenRejectionRefetchesToken(t *testing.T) {
	for _, code := range []int{2000, 2001} {
		f := &mediaFake{full: func(_, token string) (int, string) {
			if token == "tok-1" {
				return 200, perTrackError(code, "An error occurred while decoding track token.")
			}
			return 200, fullOK
		}}
		plan, err := f.client().PrepareStream("123")
		if err != nil || plan.Preview || plan.CDNURL != "https://cdn/full.mp3" {
			t.Fatalf("code %d: plan %+v, err %v; want the full track", code, plan, err)
		}
		if f.songs != 2 || f.logins != 0 || f.previews != 0 {
			t.Errorf("code %d: %d song.getData, %d logins, %d previews; want 2, 0, 0", code, f.songs, f.logins, f.previews)
		}
	}
}

// A refused track (2002 unauthorized, 2004 country mismatch) plays the preview
// the media service serves for its token.
func TestPrepareStream_RightsRefusalPlaysServicePreview(t *testing.T) {
	for _, code := range []int{2002, 2004} {
		f := &mediaFake{full: func(string, string) (int, string) { return 200, perTrackError(code, "refused") }}
		plan, err := f.client().PrepareStream("123")
		if err != nil || !plan.Preview || plan.CDNURL != "https://cdn/preview.mp3" || plan.Encrypted || plan.Format != "MP3_128" {
			t.Fatalf("code %d: plan %+v, err %v; want the service's preview", code, plan, err)
		}
		if f.logins != 0 || f.previews != 1 {
			t.Errorf("code %d: %d logins, %d previews; want 0, 1", code, f.logins, f.previews)
		}
	}
}

// Replies Deezer's players treat as failures are errors, never the preview: a
// deprecated API (1999), an unknown code at either level, and a reply with
// neither media nor errors.
func TestPrepareStream_UnknownRepliesAreErrors(t *testing.T) {
	for name, reply := range map[string]string{
		"deprecated":        topLevelError(1999, "Deprecated API"),
		"unknown top-level": topLevelError(1500, "?"),
		"unknown per-track": perTrackError(2003, "?"),
		"empty":             `{"data":[{}]}`,
	} {
		f := &mediaFake{full: func(string, string) (int, string) { return 200, reply }}
		plan, err := f.client().PrepareStream("123")
		if err == nil || plan != nil || IsNoFullMedia(err) || f.previews != 0 {
			t.Errorf("%s: plan %+v, err %v, %d previews; want an error and no preview", name, plan, err, f.previews)
		}
	}
}

// get_url goes to the media host the login names (URL_MEDIA).
func TestPrepareStream_UsesLoginMediaHost(t *testing.T) {
	f := &mediaFake{host: "https://media.example", full: func(string, string) (int, string) { return 200, fullOK }}
	c := f.client()
	if err := c.Login(); err != nil {
		t.Fatal(err)
	}
	base := c.http.Transport
	var hosts []string
	c.http.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/v1/get_url") {
			hosts = append(hosts, r.URL.Scheme+"://"+r.URL.Host)
		}
		return base.RoundTrip(r)
	})
	if _, err := c.PrepareStream("123"); err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0] != "https://media.example" {
		t.Errorf("get_url hosts %v, want the login's URL_MEDIA", hosts)
	}
}
