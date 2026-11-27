package pictures

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/config"
)

func TestKeepAndLoad(t *testing.T) {
	config.SetCacheBaseDir(t.TempDir())
	defer config.SetCacheBaseDir("")
	if _, ok := Load("https://cdn.test/a.jpg"); ok {
		t.Fatal("a picture never kept loaded")
	}
	Keep("https://cdn.test/a.jpg", []byte("a"))
	if b, ok := Load("https://cdn.test/a.jpg"); !ok || string(b) != "a" {
		t.Fatalf("Load = %q, %v", b, ok)
	}
	if _, ok := Load("https://cdn.test/b.jpg"); ok {
		t.Fatal("another URL loaded the same picture")
	}
}

// Pruning removes the least recently used pictures first.
func TestPrune(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-time.Hour)
	for i, name := range []string{"old", "mid", "new"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, make([]byte, 100), 0o600); err != nil {
			t.Fatal(err)
		}
		at := old.Add(time.Duration(i) * time.Minute)
		_ = os.Chtimes(p, at, at)
	}
	if left := prune(dir, 150); left != 100 {
		t.Fatalf("%d bytes left, want 100", left)
	}
	if _, err := os.Stat(filepath.Join(dir, "new")); err != nil {
		t.Fatalf("the newest picture went: %v", err)
	}
}
