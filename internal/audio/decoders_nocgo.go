//go:build !cgo

package audio

import "io"

// Without cgo the FLAC decoder is the pure-Go one (mp3_go.go has the MP3 one).

type flacStream = goFLACStream

func newFLACStream(r io.ReadSeeker) (*flacStream, error) { return newGoFLACStream(r) }
