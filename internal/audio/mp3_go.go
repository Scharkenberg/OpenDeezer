//go:build !cgo

package audio

import (
	"io"

	"github.com/hajimehoshi/go-mp3"
)

// readerOnly hides an io.ReadSeeker's Seek so go-mp3 treats the source as a stream
// and reads only the first frame, instead of scanning the whole stream to build its
// frame index. Decoding can then start before the download completes.
type readerOnly struct{ r io.Reader }

func (ro readerOnly) Read(p []byte) (int, error) { return ro.r.Read(p) }

func newMP3Decoder(r io.Reader) (mp3Decoder, error) { return mp3.NewDecoder(readerOnly{r}) }

func newSeekableMP3Decoder(r io.ReadSeeker) (mp3Decoder, error) { return mp3.NewDecoder(r) }
