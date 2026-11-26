//go:build cgo

package audio

import "io"

// With cgo the decoders are minimp3 and dr_flac; the pure-Go ones stay for the builds
// without a C toolchain.

func newMP3Decoder(r io.Reader) (mp3Decoder, error) { return newMiniMP3(r) }

func newSeekableMP3Decoder(r io.ReadSeeker) (mp3Decoder, error) { return newSeekableMiniMP3(r) }
