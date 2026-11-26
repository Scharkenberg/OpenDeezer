package audio

import (
	"bytes"
	"testing"
)

// TestFLACProgressiveDecodeCorrect checks that the FLAC decoder yields the same PCM
// from a fully buffered stream and from a progressively fed one.
func TestFLACProgressiveDecodeCorrect(t *testing.T) {
	data := makeFLAC(20, 4096)

	// Reference: decode from a complete in-memory reader.
	ref, err := newFLACStream(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	want := decodeAll(t, ref)

	// Progressive: write a stream file in slices, decode concurrently.
	sb := newStreamFile(nil)
	defer sb.close()
	go func() {
		for off := 0; off < len(data); off += 4096 {
			end := off + 4096
			if end > len(data) {
				end = len(data)
			}
			_ = sb.write(data[off:end])
		}
		sb.finish(nil)
	}()
	prog, err := newFLACStream(sb.reader())
	if err != nil {
		t.Fatal(err)
	}
	got := decodeAll(t, prog)

	if !bytes.Equal(got, want) {
		t.Fatalf("progressive FLAC decode differs: got %d PCM bytes, want %d", len(got), len(want))
	}
}

// BenchmarkFLACDecode decodes a 100-frame (9.3 s) synthetic FLAC to PCM.
func BenchmarkFLACDecode(b *testing.B) {
	data := makeFLAC(100, 4096)
	buf := make([]byte, 16<<10)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		f, err := newFLACStream(bytes.NewReader(data))
		if err != nil {
			b.Fatal(err)
		}
		for {
			if _, err := f.Read(buf); err != nil {
				break
			}
		}
	}
}

func decodeAll(t *testing.T, s pcmStream) []byte {
	t.Helper()
	var out []byte
	buf := make([]byte, 8192)
	for {
		n, err := s.Read(buf)
		if n > 0 {
			out = append(out, buf[:n]...)
		}
		if err != nil {
			return out
		}
	}
}
