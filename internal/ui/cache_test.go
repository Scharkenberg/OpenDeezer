package ui

import (
	"testing"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/config"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/mediacache"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/mediacache/mctest"
)

// cacheModel is an interaction model with its config and cache directories in
// fresh folders and a client that is not logged in: any network resolve fails.
func cacheModel(t *testing.T) *Model {
	t.Helper()
	config.SetBaseDir(t.TempDir())
	config.SetCacheBaseDir(t.TempDir())
	t.Cleanup(func() {
		config.SetBaseDir("")
		config.SetCacheBaseDir("")
	})
	m := interactionModel(screenMenu)
	m.client = deezer.New("arl")
	return m
}

// A track whose body and meta are cached resolves from the cache, with no
// request (the client is not logged in, so a network resolve would fail).
func TestStreamCmdResolvesFromCache(t *testing.T) {
	m := cacheModel(t)
	if err := m.caches.SetMB(1); err != nil {
		t.Fatal(err)
	}
	mc := m.caches.Current()
	mctest.PutBody(t, mc, "5.MP3_128")
	_ = mc.PutMeta("5.MP3_128", mediacache.StreamMeta{Format: "MP3_128", Encrypted: true})

	msg, _ := m.streamCmd(deezer.Track{ID: "5"}, 1)().(streamReadyMsg)
	if msg.err != nil || msg.plan == nil || msg.plan.CDNURL != "" || msg.plan.Format != "MP3_128" {
		t.Fatalf("streamCmd = %+v, want a cache-sourced plan", msg)
	}
}

// The Stream cache setting cycles the budget, saves it and applies it at once.
func TestCacheSettingCyclesBudget(t *testing.T) {
	m := cacheModel(t)
	want := []int{1 << 10, 2 << 10, 5 << 10, 10 << 10, 20 << 10, 0}
	for _, mb := range want {
		changeSetting(t, m, "Stream cache")
		if got := config.LoadMedia().MediaCacheMB; got != mb {
			t.Fatalf("budget after enter = %d MB, want %d", got, mb)
		}
		if on := m.caches.Current() != nil; on != (mb > 0) {
			t.Fatalf("cache attached %v with a budget of %d MB", on, mb)
		}
	}
}
