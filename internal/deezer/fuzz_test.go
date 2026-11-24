package deezer

import (
	"bytes"
	"testing"
)

// Fuzz the BF_CBC_STRIPE decryptor: CDN bytes are untrusted, so decrypting must never
// panic on any input. Run locally with
// go test -run=^$ -fuzz=FuzzDecryptTrack -fuzztime=30s ./internal/deezer

// FuzzDecryptTrack checks that output length equals input length: full 2048-byte
// chunks decrypt in place and the trailing partial chunk passes through.
func FuzzDecryptTrack(f *testing.F) {
	f.Add("3135556", []byte("the quick brown fox jumps over the lazy dog 0123456789"))
	f.Add("0", []byte{})
	f.Add("", make([]byte, 2048*3+17)) // spans several full chunks + a partial
	f.Fuzz(func(t *testing.T, trackID string, data []byte) {
		out, err := DecryptTrack(trackID, data)
		if err != nil {
			return // an unusable key is a clean error, not a crash
		}
		if len(out) != len(data) {
			t.Fatalf("DecryptTrack: len(out)=%d != len(data)=%d (stripe must preserve length)", len(out), len(data))
		}
	})
}

// FuzzStripeChunking checks that feeding the buffer at once and as two arbitrary
// slices gives the same bytes.
func FuzzStripeChunking(f *testing.F) {
	f.Add("3135556", []byte("streaming in two halves should equal one whole feed"), uint16(7))
	f.Add("42", make([]byte, 2048*2), uint16(2048))
	f.Fuzz(func(t *testing.T, trackID string, data []byte, splitRaw uint16) {
		whole, err := newStripeOut(trackID, func(d *StripeDecryptor) []byte {
			out := d.Feed(data, nil)
			return d.Finish(out)
		})
		if err != nil {
			return
		}
		split := 0
		if len(data) > 0 {
			split = int(splitRaw) % (len(data) + 1)
		}
		parts, err := newStripeOut(trackID, func(d *StripeDecryptor) []byte {
			out := d.Feed(data[:split], nil)
			out = d.Feed(data[split:], out)
			return d.Finish(out)
		})
		if err != nil {
			return
		}
		if !bytes.Equal(whole, parts) {
			t.Fatalf("stripe output depends on chunking: split=%d len=%d", split, len(data))
		}
	})
}

func newStripeOut(trackID string, fn func(*StripeDecryptor) []byte) ([]byte, error) {
	d, err := NewStripeDecryptor(trackID)
	if err != nil {
		return nil, err
	}
	return fn(d), nil
}
