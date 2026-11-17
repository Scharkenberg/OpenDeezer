package audio

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
	"github.com/hajimehoshi/go-mp3"
)

// TestMain points the config and cache directories, and with the latter the
// stream temp directory, at directories of their own.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "od-audio-test-")
	if err != nil {
		panic(err)
	}
	config.SetBaseDir(filepath.Join(dir, "config"))
	config.SetCacheBaseDir(filepath.Join(dir, "cache"))
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// fixture is an MP3 or FLAC stream generated for a test, plain and encrypted
// with a test key.
type fixture struct {
	format string
	id     string
	plain  []byte
	cipher []byte
}

func fixtures() []fixture {
	var fs []fixture
	for _, f := range []struct {
		format string
		plain  []byte
	}{
		{"MP3_128", makeMP3(400)},     // 10.4 s
		{"FLAC", makeFLAC(100, 4096)}, // 9.3 s
	} {
		c := bytes.Clone(f.plain)
		encryptStripe("fx-"+f.format, c)
		fs = append(fs, fixture{f.format, "fx-" + f.format, f.plain, c})
	}
	return fs
}

func stripeFile(id string) *streamFile { return newStreamFile(deezer.NewStripeCipher(id)) }

// writeSlowly appends data in uneven pieces with pauses, then finishes.
func writeSlowly(b *streamFile, data []byte) {
	for off, step := 0, 1000; off < len(data); off, step = off+step, step*2+333 {
		end := min(off+step, len(data))
		if err := b.write(data[off:end]); err != nil {
			b.finish(err)
			return
		}
		time.Sleep(time.Millisecond)
	}
	b.finish(nil)
}

// pcmOf decodes a whole stream with the real decoder of its format.
func pcmOf(t *testing.T, format string, r io.ReadSeeker) []byte {
	t.Helper()
	var dec io.Reader
	if format == "FLAC" {
		fs, err := newFLACStream(r)
		if err != nil {
			t.Fatal(err)
		}
		dec = fs
	} else {
		d, err := mp3.NewDecoder(r)
		if err != nil {
			t.Fatal(err)
		}
		dec = d
	}
	pcm, err := io.ReadAll(dec)
	if err != nil {
		t.Fatal(err)
	}
	return pcm
}

// Both decoders, reading an encrypted stream as it is written, give the PCM
// of the plaintext.
func TestStreamFileDecodesWhileWritten(t *testing.T) {
	for _, fx := range fixtures() {
		t.Run(fx.format, func(t *testing.T) {
			want := pcmOf(t, fx.format, bytes.NewReader(fx.plain))
			b := stripeFile(fx.id)
			defer b.close()
			go writeSlowly(b, fx.cipher)
			if got := pcmOf(t, fx.format, b.reader()); !bytes.Equal(got, want) {
				t.Fatalf("PCM %d bytes, want %d bytes of the plaintext's", len(got), len(want))
			}
		})
	}
}

// A seek to any offset reads the bytes a whole decrypt has there, also for
// several readers at once.
func TestStreamFileSeekMatchesWholeDecrypt(t *testing.T) {
	for _, fx := range fixtures() {
		t.Run(fx.format, func(t *testing.T) {
			b := stripeFile(fx.id)
			defer b.close()
			go writeSlowly(b, fx.cipher)
			done := make(chan error, 3)
			for k := 0; k < 3; k++ {
				go func(k int) {
					r := b.reader()
					for _, off := range []int{len(fx.plain) - 10, 0, 4095 + k, 70001, 2048 * 3, len(fx.plain) / 2} {
						if _, err := r.Seek(int64(off), io.SeekStart); err != nil {
							done <- err
							return
						}
						got := make([]byte, min(5000, len(fx.plain)-off))
						if _, err := io.ReadFull(r, got); err != nil {
							done <- err
							return
						}
						if !bytes.Equal(got, fx.plain[off:off+len(got)]) {
							done <- errors.New("bytes at a seek differ from a whole decrypt")
							return
						}
					}
					done <- nil
				}(k)
			}
			for k := 0; k < 3; k++ {
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// A waiting reader wakes on the writer's error, and the first error stays.
func TestStreamFileReaderGetsWriterError(t *testing.T) {
	b := newStreamFile(nil)
	defer b.close()
	got := make(chan error, 1)
	go func() {
		r := b.reader()
		_, err := io.ReadAll(r)
		got <- err
	}()
	_ = b.write([]byte("abc"))
	time.Sleep(20 * time.Millisecond)
	fail := errors.New("disk full")
	b.finish(fail)
	b.finish(nil)
	select {
	case err := <-got:
		if !errors.Is(err, fail) {
			t.Fatalf("reader got %v, want %v", err, fail)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reader did not wake on the writer's error")
	}
}

// An encrypted stream cut inside chunk 3 (the fourth, an encrypted one) hands
// out only whole chunks: the cut chunk's ciphertext never reaches the decoder
// as audio, whether the writer failed or ended short of the known length.
func TestCutStreamEndsOnChunkBoundary(t *testing.T) {
	fx := fixtures()[0]
	cut := 3*deezer.StripeChunk + 1000
	for _, tc := range []struct {
		name  string
		total int64
		err   error
	}{
		{"writer error", 0, errors.New("connection reset")},
		{"short of the length", int64(len(fx.cipher)), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := stripeFile(fx.id)
			defer b.close()
			if tc.total > 0 {
				b.setTotal(tc.total)
			}
			if err := b.write(fx.cipher[:cut]); err != nil {
				t.Fatal(err)
			}
			b.finish(tc.err)
			got, err := io.ReadAll(b.reader())
			if len(got) != 3*deezer.StripeChunk || !bytes.Equal(got, fx.plain[:len(got)]) {
				t.Fatalf("read %d bytes (plaintext prefix %v), want the %d of the whole chunks", len(got), bytes.Equal(got, fx.plain[:len(got)]), 3*deezer.StripeChunk)
			}
			if err != tc.err {
				t.Fatalf("read error %v, want %v", err, tc.err)
			}
		})
	}
}

// The stream file can be deleted while its handle is still in use (a reader
// in ReadAt when the stream closes keeps it), so close never leaves it behind.
func TestStreamFileDeletableWhileOpen(t *testing.T) {
	b := newStreamFile(nil)
	if err := b.write([]byte("some bytes")); err != nil {
		t.Fatal(err)
	}
	name := b.f.Name()
	if err := os.Remove(name); err != nil {
		t.Fatalf("remove while the stream's handle is open: %v", err)
	}
	b.close()
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatalf("stream file after close: %v", err)
	}
}

// Close while a reader waits returns io.ErrClosedPipe and deletes the file.
func TestStreamFileCloseWakesReaderAndDeletesFile(t *testing.T) {
	b := stripeFile("x")
	_ = b.write(make([]byte, 3000)) // one full chunk readable, the rest waits
	r := b.reader()
	if _, err := r.Read(make([]byte, 2048)); err != nil {
		t.Fatal(err)
	}
	got := make(chan error, 1)
	go func() {
		_, err := r.Read(make([]byte, 10))
		got <- err
	}()
	time.Sleep(20 * time.Millisecond)
	name := b.f.Name()
	b.close()
	select {
	case err := <-got:
		if err != io.ErrClosedPipe {
			t.Fatalf("reader got %v, want io.ErrClosedPipe", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("close left the reader waiting")
	}
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatalf("stream file after close: %v", err)
	}
}

// The file holds the bytes as received: ciphertext for an encrypted stream.
func TestStreamFileStoresCiphertext(t *testing.T) {
	fx := fixtures()[1]
	b := stripeFile(fx.id)
	defer b.close()
	writeSlowly(b, fx.cipher)
	onDisk, err := os.ReadFile(b.f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(onDisk, fx.cipher) {
		t.Fatal("the file does not hold the ciphertext as received")
	}
	base, _ := config.CacheBaseDir()
	if dir := filepath.Join(base, "tmp"); filepath.Dir(b.f.Name()) != dir {
		t.Fatalf("stream file in %s, want %s", filepath.Dir(b.f.Name()), dir)
	}
}

// A plain stream (a podcast) is downloaded into a file in the temp directory,
// decodes from it, and the file goes with the source.
func TestPlainSourceStreamsThroughFile(t *testing.T) {
	for _, fx := range fixtures() {
		t.Run(fx.format, func(t *testing.T) {
			cdn := newCountingCDN(t, fx.plain)
			s := newSource(&deezer.StreamPlan{CDNURL: cdn.srv.URL, TrackID: "ep", Format: fx.format}, 10000)
			s.download()
			if e := s.lastErr(); e != "" {
				t.Fatal(e)
			}
			name := s.sb.f.Name()
			onDisk, err := os.ReadFile(name)
			if err != nil || !bytes.Equal(onDisk, fx.plain) {
				t.Fatalf("stream file: %d bytes, err %v; want the body", len(onDisk), err)
			}
			if got, want := pcmOf(t, fx.format, s.sb.reader()), pcmOf(t, fx.format, bytes.NewReader(fx.plain)); !bytes.Equal(got, want) {
				t.Fatalf("PCM %d bytes, want %d", len(got), len(want))
			}
			s.kill()
			if _, err := os.Stat(name); !os.IsNotExist(err) {
				t.Fatalf("stream file after kill: %v", err)
			}
		})
	}
}

// RemoveTempFiles deletes what a crashed process left: stream files and old
// podcast spill files.
func TestRemoveTempFiles(t *testing.T) {
	b := newStreamFile(nil)
	_ = b.write([]byte("left by a crash"))
	_ = b.f.Close() // the process died: nobody closes the stream
	spill, err := os.CreateTemp("", "opdeezer-podcast-*.tmp")
	if err != nil {
		t.Fatal(err)
	}
	_ = spill.Close()
	RemoveTempFiles()
	for _, p := range []string{b.f.Name(), spill.Name()} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s after RemoveTempFiles: %v", p, err)
		}
	}
}
