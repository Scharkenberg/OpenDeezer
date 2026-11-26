package discovery

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Configured peers are listed once each, with their /whoami identity, and a
// peer that does not answer is listed by its address once the timeout passed
// instead of holding the list for the control client's 15 s.
func TestProbePeersListsConfiguredPeers(t *testing.T) {
	answering := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-PurpleDisc-Account") != "" {
			t.Errorf("%s sent the account id to an unauthenticated peer", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"name":"Den","auth":"account","client":"tui","version":"9"}`))
	}))
	defer answering.Close()
	release := make(chan struct{})
	silent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer silent.Close()
	defer close(release)
	a := strings.TrimPrefix(answering.URL, "http://")
	s := strings.TrimPrefix(silent.URL, "http://")
	t.Setenv("PURPLEDISC_CONNECT_PEERS", a+","+s+","+answering.URL)

	start := time.Now()
	devs, _ := ProbePeers(500*time.Millisecond, 0)
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("ProbePeers took %v with a silent peer, want about the timeout", took)
	}
	byAddr := map[string][]Device{}
	for _, d := range devs {
		byAddr[d.Addr] = append(byAddr[d.Addr], d)
	}
	if got := byAddr[a]; len(got) != 1 || got[0].Name != "Den" || got[0].Client != "tui" || got[0].Version != "9" {
		t.Fatalf("answering peer = %+v, want it once as Den (tui 9)", got)
	}
	if got := byAddr[s]; len(got) != 1 || got[0].Name != s || got[0].Client != "" {
		t.Fatalf("silent peer = %+v, want it once by its address", got)
	}
}
