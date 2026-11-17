// Package mctest holds test helpers for packages that use a media cache.
package mctest

import (
	"testing"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/mediacache"
)

// PutBody commits a placeholder body under key; the cache answers meta only
// next to a body.
func PutBody(t testing.TB, c *mediacache.Cache, key string) {
	t.Helper()
	w, err := c.Put(key)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("ciphertext"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}
