package audio

import (
	"bytes"
	"io"
	"testing"
	"time"
)

// TestSetContentLengthHuge verifies that a server-controlled huge
// Content-Length/Content-Range total is recorded in full and allocates nothing:
// a seek past what has arrived resolves to the end once the stream finishes.
func TestSetContentLengthHuge(t *testing.T) {
	sb := newStreamFile(nil)
	defer sb.close()
	sb.setTotal(1 << 60)
	if total := sb.Total(); total != 1<<60 {
		t.Fatalf("total should record the advertised length in full, got %d", total)
	}
	_ = sb.write([]byte("data"))
	sb.finish(nil)
	if off, _ := sb.reader().Seek(1<<40, io.SeekStart); off != 4 {
		t.Fatalf("seek past the data = %d, want 4", off)
	}
	if string(drainStreamFile(sb)) != "data" {
		t.Fatal("stream data corrupted after a huge total")
	}
}

// TestWaitWatermarkUnblocksOnFinish verifies the watermark wait also returns when
// the stream finishes before the margin is reached (short tracks).
func TestWaitWatermarkUnblocksOnFinish(t *testing.T) {
	sb := newStreamFile(nil)
	defer sb.close()
	done := make(chan struct{})
	go func() {
		sb.waitWatermark(1 << 20) // 1MB margin, never reached
		close(done)
	}()
	_ = sb.write(make([]byte, 100))
	sb.finish(nil)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("waitWatermark did not return when the stream finished below the margin")
	}
}

// TestRingShortReadLeavesTail: a short pcmRing.read must not write past n
// (readPCM scales only out[:n]; the device plays the whole buffer).
func TestRingShortReadLeavesTail(t *testing.T) {
	r := newPCMRing(64)
	r.write(bytes.Repeat([]byte{0xAA}, 64), r.seq())
	r.read(make([]byte, 64)) // drain; the 0xAA bytes stay in the backing array
	r.write([]byte{1, 2, 3, 4}, r.seq())
	p := make([]byte, 32)
	n := r.read(p)
	for i, b := range p[n:] {
		if b != 0 {
			t.Fatalf("read returned %d but wrote stale byte %#x at %d", n, b, n+i)
		}
	}
}
