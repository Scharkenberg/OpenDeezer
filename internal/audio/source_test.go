package audio

import (
	"testing"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
)

// A source parked after decoder EOF wakes with a pending seek when one is requested,
// and wakes reporting "dead" when killed so the decode goroutine exits.
func TestSourceWaitSeekWakesOnSeek(t *testing.T) {
	s := newSource(&deezer.StreamPlan{Format: "MP3"}, 1000)
	done := make(chan bool, 1)
	go func() { done <- s.waitSeekOrDead() }()
	s.requestSeek(4096)
	if !<-done {
		t.Fatal("waitSeekOrDead should return true (seek pending) after requestSeek")
	}
}

func TestSourceWaitSeekWakesOnKill(t *testing.T) {
	s := newSource(&deezer.StreamPlan{Format: "MP3"}, 1000)
	done := make(chan bool, 1)
	go func() { done <- s.waitSeekOrDead() }()
	s.kill()
	if <-done {
		t.Fatal("waitSeekOrDead should return false (dead) after kill")
	}
	if !s.eof.Load() {
		t.Fatal("kill() must set eof so manage() never waits on a killed source forever")
	}
}
