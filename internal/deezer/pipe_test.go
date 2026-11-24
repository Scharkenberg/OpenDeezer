package deezer

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// pipeFake is Deezer's auth service and Pipe: login/arl and login/renew hand out JWTs
// expiring after ttl; Pipe answers reply(op, variables), 401 for a bad JWT. gwReply,
// when set, answers gateway calls.
type pipeFake struct {
	mu            sync.Mutex
	ttl           time.Duration
	issued        int
	logins, renew int
	valid         map[string]time.Time
	reply         func(op string, vars map[string]any) string
	gwReply       func(method, body string) string
	ops           []string
	vars          []map[string]any
}

func jwtFor(n int, exp time.Time) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc([]byte(fmt.Sprintf(`{"iat":%d,"exp":%d,"n":%d}`, time.Now().Unix(), exp.Unix(), n))) + ".sig"
}

func (f *pipeFake) client() *Client {
	c := New("dummy")
	loggedInPremium(c)
	c.mu.Lock()
	c.userID = "7"
	c.mu.Unlock()
	f.valid = map[string]time.Time{}
	c.http = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		resp := func(code int, body string) (*http.Response, error) {
			return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
		}
		issue := func() string {
			f.issued++
			exp := time.Now().Add(f.ttl)
			j := jwtFor(f.issued, exp)
			f.valid[j] = exp
			return fmt.Sprintf(`{"jwt":%q,"refresh_token":"rt-%d"}`, j, f.issued)
		}
		switch {
		case r.URL.Host == "auth.deezer.com" && r.URL.Path == "/login/arl":
			f.logins++
			return resp(200, issue())
		case r.URL.Host == "auth.deezer.com" && r.URL.Path == "/login/renew":
			f.renew++
			b, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(b), `"refresh_token":"rt-`) {
				return resp(401, `{}`)
			}
			return resp(200, issue())
		case r.URL.Host == "pipe.deezer.com":
			j := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if exp, ok := f.valid[j]; !ok || time.Now().After(exp) {
				return resp(401, `{"errors":[{"message":"unauthorized"}]}`)
			}
			var q struct {
				Op   string         `json:"operationName"`
				Vars map[string]any `json:"variables"`
			}
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &q)
			f.ops = append(f.ops, q.Op)
			f.vars = append(f.vars, q.Vars)
			return resp(200, f.reply(q.Op, q.Vars))
		case r.URL.Host == "www.deezer.com" && f.gwReply != nil:
			b, _ := io.ReadAll(r.Body)
			return resp(200, f.gwReply(r.URL.Query().Get("method"), string(b)))
		}
		return resp(200, `{"error":{},"results":{}}`)
	})}
	return c
}

// Pipe calls carry a JWT from login/arl, reused until it nears its expiry and
// then renewed with the refresh token; a 401 renews once and retries.
func TestPipeJWTReuseAndRenewal(t *testing.T) {
	f := &pipeFake{ttl: time.Hour, reply: func(string, map[string]any) string { return `{"data":{"me":{"id":"7"}}}` }}
	c := f.client()
	var out struct {
		Me struct{ ID string } `json:"me"`
	}
	for i := 0; i < 3; i++ {
		if err := c.pipe("Me", `query Me { me { id } }`, nil, &out); err != nil || out.Me.ID != "7" {
			t.Fatalf("call %d: %v, %+v", i, err, out)
		}
	}
	if f.logins != 1 || f.renew != 0 {
		t.Fatalf("%d logins, %d renewals for three calls; want 1, 0", f.logins, f.renew)
	}
	// The JWT expires: the next call renews it with the refresh token.
	c.pipeMu.Lock()
	c.jwtExp = time.Now().Add(-time.Second)
	c.pipeMu.Unlock()
	if err := c.pipe("Me", `query Me { me { id } }`, nil, &out); err != nil {
		t.Fatal(err)
	}
	if f.logins != 1 || f.renew != 1 {
		t.Fatalf("after expiry: %d logins, %d renewals; want 1, 1", f.logins, f.renew)
	}
	// Pipe refuses the JWT (401): renewed once, retried.
	f.mu.Lock()
	f.valid = map[string]time.Time{}
	f.mu.Unlock()
	if err := c.pipe("Me", `query Me { me { id } }`, nil, &out); err != nil {
		t.Fatal(err)
	}
	if f.renew != 2 {
		t.Fatalf("after a 401: %d renewals, want 2", f.renew)
	}
}

// GraphQL errors are Go errors.
func TestPipeGraphQLError(t *testing.T) {
	f := &pipeFake{ttl: time.Hour, reply: func(string, map[string]any) string {
		return `{"data":null,"errors":[{"message":"track not found"}]}`
	}}
	err := f.client().pipe("X", `query X { x }`, nil, &struct{}{})
	if err == nil || !strings.Contains(err.Error(), "track not found") {
		t.Fatalf("err %v", err)
	}
}
