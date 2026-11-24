package deezer

// Pipe is Deezer's GraphQL API (pipe.deezer.com), used for lyrics, favorites, search
// history and bans. It takes a short-lived JWT from auth.deezer.com for the session's
// arl, renewed with its refresh token (measured 2026-11-23).

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	authHost = "https://auth.deezer.com"
	pipeURL  = "https://pipe.deezer.com/api"
	// jwtMargin renews a JWT this long before it expires, as the web player's
	// requestDelay does.
	jwtMargin = 15 * time.Second
)

// errPipeAuth is a 401 from Pipe or the auth service.
var errPipeAuth = errors.New("pipe: unauthorized")

// pipe runs a GraphQL operation and decodes its data into out. A 401 renews
// the JWT once and retries.
func (c *Client) pipe(op, query string, vars map[string]any, out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if vars == nil {
		vars = map[string]any{}
	}
	body, _ := json.Marshal(map[string]any{"operationName": op, "query": query, "variables": vars})
	for attempt := 0; ; attempt++ {
		jwt, err := c.pipeJWT(ctx, attempt > 0)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, pipeURL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+jwt)
		webHeaders(req, "")
		resp, err := c.http.Do(req)
		if err != nil {
			return classifyNet(err)
		}
		b, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusUnauthorized {
			if attempt == 0 {
				continue
			}
			return errPipeAuth
		}
		var r struct {
			Data   json.RawMessage `json:"data"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(b, &r); err != nil {
			return fmt.Errorf("pipe %s: HTTP %d: unreadable reply", op, resp.StatusCode)
		}
		if len(r.Errors) > 0 {
			msgs := make([]string, len(r.Errors))
			for i, e := range r.Errors {
				msgs[i] = e.Message
			}
			return fmt.Errorf("pipe %s: %s", op, strings.Join(msgs, "; "))
		}
		if out == nil || len(r.Data) == 0 || string(r.Data) == "null" {
			return nil
		}
		return json.Unmarshal(r.Data, out)
	}
}

// pipeJWT returns a JWT valid for at least jwtMargin: the cached one, else one
// renewed with the refresh token, else a new one for the arl. force drops the
// cached one (Pipe refused it).
func (c *Client) pipeJWT(ctx context.Context, force bool) (string, error) {
	c.pipeMu.Lock()
	defer c.pipeMu.Unlock()
	if !force && c.jwt != "" && time.Until(c.jwtExp) > jwtMargin {
		return c.jwt, nil
	}
	var jwt, refresh string
	var err error
	if c.jwtRefresh != "" {
		b, _ := json.Marshal(map[string]string{"refresh_token": c.jwtRefresh})
		jwt, refresh, err = c.authCall(ctx, authHost+"/login/renew?jo=p&rto=p&i=p", b, false)
	}
	if c.jwtRefresh == "" || err != nil {
		jwt, refresh, err = c.authCall(ctx, authHost+"/login/arl?jo=p&rto=p&i=c", nil, true)
	}
	if err != nil {
		return "", err
	}
	// The exp claim; a JWT without one is renewed at the next call.
	c.jwt, c.jwtRefresh, c.jwtExp = jwt, refresh, time.Now()
	if parts := strings.Split(jwt, "."); len(parts) > 1 {
		var claims struct {
			Exp int64 `json:"exp"`
		}
		if b, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil && json.Unmarshal(b, &claims) == nil && claims.Exp > 0 {
			c.jwtExp = time.Unix(claims.Exp, 0)
		}
	}
	return jwt, nil
}

// authCall asks the auth service for a JWT and a refresh token.
func (c *Client) authCall(ctx context.Context, u string, body []byte, withSession bool) (jwt, refresh string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if withSession {
		_, cookie := c.session()
		req.Header.Set("Cookie", cookie)
	}
	webHeaders(req, "")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", classifyNet(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", "", errPipeAuth
	}
	var r struct {
		JWT     string `json:"jwt"`
		Refresh string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil || r.JWT == "" {
		return "", "", fmt.Errorf("auth: HTTP %d: no token", resp.StatusCode)
	}
	return r.JWT, r.Refresh, nil
}
