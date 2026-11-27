package update

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckInstallerAsset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v4.1.0","html_url":"https://example.test/r","assets":[
			{"name":"PurpleDisc-windows-x64.zip","browser_download_url":"https://example.test/zip","digest":"sha256:00"},
			{"name":"PurpleDisc-4.1.0-windows-x64.exe","browser_download_url":"https://example.test/exe","digest":"sha256:ABCDEF"},
			{"name":"SHA256SUMS.txt","browser_download_url":"https://example.test/sums","digest":"sha256:11"}]}`))
	}))
	defer srv.Close()
	defer func(u string) { latestURL = u }(latestURL)
	latestURL = srv.URL

	info, err := Check("4.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !info.HasUpdate || info.Installer != "https://example.test/exe" || info.InstallerSHA256 != "abcdef" {
		t.Fatalf("got %+v", info)
	}
}

func TestCheckInstallerNeedsDigest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v4.1.0","assets":[{"name":"PurpleDisc-4.1.0-windows-x64.exe","browser_download_url":"https://example.test/exe"}]}`))
	}))
	defer srv.Close()
	defer func(u string) { latestURL = u }(latestURL)
	latestURL = srv.URL

	info, err := Check("4.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if info.Installer != "" || info.InstallerSHA256 != "" {
		t.Fatalf("an asset without a digest was offered: %+v", info)
	}
}

func TestDownload(t *testing.T) {
	body := []byte("installer bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer srv.Close()
	sum := sha256.Sum256(body)
	path := filepath.Join(t.TempDir(), "PurpleDisc-4.1.0-windows-x64.exe")

	if err := Download(srv.URL, hex.EncodeToString(sum[:]), path); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(body) {
		t.Fatalf("file holds %q", got)
	}
	if done, total := Progress(); done != int64(len(body)) || total != int64(len(body)) {
		t.Fatalf("progress %d of %d, want %d of %d", done, total, len(body), len(body))
	}

	if err := Download(srv.URL, "00", path); err == nil {
		t.Fatal("a wrong SHA-256 was accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the file stayed after a wrong SHA-256 (%v)", err)
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.5.1", "1.5.0", true},
		{"1.5.0", "1.5.0", false},
		{"1.5.0", "1.5.1", false},
		{"2.0.0", "1.9.9", true},
		{"1.10.0", "1.9.0", true}, // numeric, not lexical
		{"1.5.1", "1.5.0", true},
		{"1.5.1-rc1", "1.5.0", true}, // pre-release suffix stripped
		{"v1.5.1", "v1.5.0", true},   // (parts strips no 'v'; caller trims; test the numeric path)
		{"3.1.0", "3.1.0-rc1", true}, // the release is newer than its rc
		{"3.1.0-rc1", "3.1.0", false},
	}
	for _, c := range cases {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%q,%q)=%v want %v", c.a, c.b, got, c.want)
		}
	}
}

// The release page the apps open must be this repository's; anything else becomes its
// releases page.
func TestCheckKeepsOnlyThisRepositorysURL(t *testing.T) {
	for _, c := range []struct{ html, want string }{
		{"https://github.com/" + Repo + "/releases/tag/v4.1.0", "https://github.com/" + Repo + "/releases/tag/v4.1.0"},
		{"https://example.test/r", "https://github.com/" + Repo + "/releases"},
		{"https://github.com/" + Repo + "evil/releases", "https://github.com/" + Repo + "/releases"},
		{"file:///C:/Windows/System32/calc.exe", "https://github.com/" + Repo + "/releases"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"tag_name":"v4.1.0","html_url":"` + c.html + `"}`))
		}))
		latestURL = srv.URL
		info, err := Check("4.0.0")
		srv.Close()
		if err != nil || info.URL != c.want {
			t.Errorf("html_url %q: URL %q, %v; want %q", c.html, info.URL, err, c.want)
		}
	}
	latestURL = "https://api.github.com/repos/" + Repo + "/releases/latest"
}