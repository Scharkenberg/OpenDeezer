package mediacache

import (
	"strconv"
	"testing"
	"time"
)

// Lowering the size limit over 2 000 entries evicts the least recent ones, and how long
// that takes under the lock (the deletes are stubbed).
func TestEvictionOverManyEntries(t *testing.T) {
	c, err := New(t.TempDir(), 1<<40)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	prev := removeFile
	removeFile = func(string) error { return nil }
	defer func() { removeFile = prev }()
	c.mu.Lock()
	for i := range 2000 {
		k := strconv.Itoa(i)
		c.entries[k] = entry{path: "f" + k, size: 10 << 20, accessSeq: uint64(2000 - i)}
		c.total += 10 << 20
	}
	c.max = 100 << 20 // room for 10 entries
	c.mu.Unlock()
	start := time.Now()
	c.evictIfNeeded()
	took := time.Since(start)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) != 10 {
		t.Fatalf("%d entries left, want 10", len(c.entries))
	}
	for k := range c.entries {
		if i, _ := strconv.Atoi(k); i >= 10 {
			t.Fatalf("entry %s kept: not among the 10 most recent", k)
		}
	}
	t.Logf("evicting 1 990 of 2 000 entries took %v", took)
}
