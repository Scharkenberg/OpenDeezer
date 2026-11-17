package deezer

import (
	"bytes"
	"crypto/cipher"
	"testing"

	"golang.org/x/crypto/blowfish"
)

// Known Blowfish key vector for track 3135556.
func TestBlowfishKeyVector(t *testing.T) {
	want := []byte{108, 108, 102, 107, 57, 102, 44, 55, 101, 37, 117, 96, 60, 100, 52, 57}
	got := BlowfishKey("3135556")
	if !bytes.Equal(got, want) {
		t.Fatalf("key mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// Chunks 1 and 2 (index%3 != 0) pass through; chunk 0 is transformed.
func TestStripePassthrough(t *testing.T) {
	data := make([]byte, StripeChunk*3)
	for i := range data {
		data[i] = byte(i % 251)
	}
	out, err := DecryptTrack("3135556", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(data) {
		t.Fatalf("length changed: %d -> %d", len(data), len(out))
	}
	if !bytes.Equal(out[StripeChunk:], data[StripeChunk:]) {
		t.Error("chunks 1,2 should be unchanged")
	}
	if bytes.Equal(out[:StripeChunk], data[:StripeChunk]) {
		t.Error("chunk 0 should be transformed")
	}
}

// Feed in uneven pieces matches crypto/cipher's CBC decrypter on every third
// chunk.
func TestStripeMatchesCBC(t *testing.T) {
	data := make([]byte, StripeChunk*7+300)
	for i := range data {
		data[i] = byte(i*7 + i/13)
	}
	bf, err := blowfish.NewCipher(BlowfishKey("3135556"))
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Clone(data)
	for i := 0; (i+1)*StripeChunk <= len(want); i += 3 {
		c := want[i*StripeChunk : (i+1)*StripeChunk]
		cipher.NewCBCDecrypter(bf, stripeIV[:]).CryptBlocks(c, c)
	}
	d, err := NewStripeDecryptor("3135556")
	if err != nil {
		t.Fatal(err)
	}
	var got, out []byte
	for off, step := 0, 1; off < len(data); off, step = off+step, step*3+1 {
		end := min(off+step, len(data))
		out = d.Feed(data[off:end], out[:0])
		got = append(got, out...)
	}
	got = d.Finish(got)
	if !bytes.Equal(got, want) {
		t.Fatal("stripe decrypt differs from CBC reference")
	}
}

// BenchmarkStripeFeed decrypts 1 MB fed in 64 KB reads into a reused output
// slice, as the player and the downloader do.
func BenchmarkStripeFeed(b *testing.B) {
	data := make([]byte, 1<<20)
	var out []byte
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		d, _ := NewStripeDecryptor("3135556")
		for off := 0; off < len(data); off += 64 << 10 {
			out = d.Feed(data[off:off+64<<10], out[:0])
		}
	}
}

// DecryptAt from any chunk boundary, on any length, gives the bytes a whole
// decrypt gives there.
func TestStripeCipherDecryptAtAnyOffset(t *testing.T) {
	data := make([]byte, StripeChunk*10+700)
	for i := range data {
		data[i] = byte(i*7 + i/13)
	}
	want, err := DecryptTrack("3135556", data)
	if err != nil {
		t.Fatal(err)
	}
	sc := NewStripeCipher("3135556")
	for start := 0; start < len(data); start += StripeChunk {
		for _, n := range []int{StripeChunk, 3 * StripeChunk, len(data) - start} {
			end := min(start+n, len(data))
			b := bytes.Clone(data[start:end])
			sc.DecryptAt(b, int64(start))
			if !bytes.Equal(b, want[start:end]) {
				t.Fatalf("DecryptAt [%d,%d) differs from a whole decrypt", start, end)
			}
		}
	}
}

// A trailing partial chunk stays plaintext and isn't dropped.
func TestStripeTrailingPartial(t *testing.T) {
	data := make([]byte, StripeChunk+100)
	for i := range data {
		data[i] = byte(i % 251)
	}
	out, err := DecryptTrack("3135556", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(data) {
		t.Fatalf("length changed: %d -> %d", len(data), len(out))
	}
	if !bytes.Equal(out[StripeChunk:], data[StripeChunk:]) {
		t.Error("trailing partial chunk should be plaintext")
	}
}
