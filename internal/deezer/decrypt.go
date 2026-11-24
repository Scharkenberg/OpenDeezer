// Package deezer: Blowfish BF_CBC_STRIPE decryption for Deezer CDN streams.
package deezer

import (
	"crypto/md5"
	"encoding/hex"

	"golang.org/x/crypto/blowfish"
)

// StripeChunk is the BF_CBC_STRIPE chunk size: chunk i of an encrypted
// stream is encrypted when i%3 == 0, so any chunk boundary decrypts on its
// own.
const StripeChunk = 2048

// secret is the fixed key-derivation secret.
var secret = []byte("g4el58wc0zvf9na1")

// stripeIV is the per-chunk CBC IV (reset each chunk).
var stripeIV = [8]byte{0, 1, 2, 3, 4, 5, 6, 7}

// BlowfishKey derives the per-track Blowfish key:
// key[i] = md5hex[i] ^ md5hex[i+16] ^ secret[i].
func BlowfishKey(trackID string) []byte {
	sum := md5.Sum([]byte(trackID))
	h := []byte(hex.EncodeToString(sum[:])) // 32 lowercase hex chars
	key := make([]byte, 16)
	for i := 0; i < 16; i++ {
		key[i] = h[i] ^ h[i+16] ^ secret[i]
	}
	return key
}

// StripeDecryptor streams BF_CBC_STRIPE plaintext out of ciphertext fed in
// arbitrary-sized pieces. Mirrors C++ DeezerStripeDecryptor.
type StripeDecryptor struct {
	cipher     *blowfish.Cipher
	buf        []byte
	chunkIndex int
}

// NewStripeDecryptor builds a decryptor keyed for trackID.
func NewStripeDecryptor(trackID string) (*StripeDecryptor, error) {
	c, err := blowfish.NewCipher(BlowfishKey(trackID))
	if err != nil {
		return nil, err
	}
	return &StripeDecryptor{cipher: c, buf: make([]byte, 0, StripeChunk)}, nil
}

// decryptChunk undoes CBC with the fixed IV over one full 2048-byte chunk, in
// place.
func decryptChunk(c *blowfish.Cipher, b []byte) {
	prev := stripeIV
	var ct [8]byte
	for off := 0; off+8 <= StripeChunk; off += 8 {
		blk := b[off : off+8]
		copy(ct[:], blk)
		c.Decrypt(blk, blk)
		for k := 0; k < 8; k++ {
			blk[k] ^= prev[k]
		}
		prev = ct
	}
}

// StripeCipher decrypts BF_CBC_STRIPE ciphertext in place from any chunk
// boundary, so a stream can be read from any offset.
type StripeCipher struct{ cipher *blowfish.Cipher }

// NewStripeCipher builds a StripeCipher keyed for trackID.
func NewStripeCipher(trackID string) *StripeCipher {
	c, _ := blowfish.NewCipher(BlowfishKey(trackID)) // a 16-byte key never fails
	return &StripeCipher{cipher: c}
}

// DecryptAt decrypts b in place. b holds the stream from byte off, a multiple
// of 2048: chunk i of the stream is encrypted when i%3 == 0 and it is a full
// chunk, so a trailing partial chunk is left as it is (plaintext).
func (s *StripeCipher) DecryptAt(b []byte, off int64) {
	i := off / StripeChunk
	for p := 0; p+StripeChunk <= len(b); p += StripeChunk {
		if i%3 == 0 {
			decryptChunk(s.cipher, b[p:p+StripeChunk])
		}
		i++
	}
}

// Feed appends decrypted/passthrough output for n input bytes.
func (d *StripeDecryptor) Feed(data []byte, out []byte) []byte {
	i := 0
	for i < len(data) {
		need := StripeChunk - len(d.buf)
		take := need
		if rem := len(data) - i; rem < need {
			take = rem
		}
		d.buf = append(d.buf, data[i:i+take]...)
		i += take
		if len(d.buf) == StripeChunk {
			n := len(out)
			out = append(out, d.buf...)
			if d.chunkIndex%3 == 0 {
				decryptChunk(d.cipher, out[n:])
			}
			d.chunkIndex++
			d.buf = d.buf[:0]
		}
	}
	return out
}

// Consumed reports how many ciphertext bytes were fed (emitted chunks plus the buffered
// partial). The cipher is 1:1, so a dropped download resumes with
// `Range: bytes=<Consumed()>-` into the same decryptor.
func (d *StripeDecryptor) Consumed() int64 {
	return int64(d.chunkIndex)*StripeChunk + int64(len(d.buf))
}

// Finish flushes the trailing partial chunk (always plaintext).
func (d *StripeDecryptor) Finish(out []byte) []byte {
	if len(d.buf) > 0 {
		out = append(out, d.buf...)
		d.buf = d.buf[:0]
	}
	return out
}

// DecryptTrack decrypts a whole in-memory buffer (used by tests).
func DecryptTrack(trackID string, data []byte) ([]byte, error) {
	d, err := NewStripeDecryptor(trackID)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(data))
	out = d.Feed(data, out)
	out = d.Finish(out)
	return out, nil
}
