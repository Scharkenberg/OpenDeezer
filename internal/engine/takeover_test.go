package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A host playing another track is passed over; the one playing it gives its queue, with
// the index of the playing row, its position, shuffle and repeat.
func TestTakeOver_FindsTheHostPlayingTheTrack(t *testing.T) {
	host := func(status string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/status" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(status))
		}))
	}
	other := host(`{"state":"playing","track":{"id":"9","title":"Other"},"queue":[{"id":"9"}]}`)
	defer other.Close()
	playing := host(`{"state":"playing","track":{"id":"2","title":"Two"},"positionMs":61000,"shuffle":true,"repeat":"all",` +
		`"queue":[{"id":"1","title":"One"},{"id":"2","title":"Two"},{"id":"3","title":"Three"}]}`)
	defer playing.Close()

	var got struct {
		Tracks []struct {
			ID string `json:"id"`
		} `json:"tracks"`
		Index      int    `json:"index"`
		PositionMS int64  `json:"positionMs"`
		Shuffle    bool   `json:"shuffle"`
		Repeat     string `json:"repeat"`
	}
	s := takeOver("2", "42", []string{other.URL, playing.URL})
	if err := json.Unmarshal([]byte(s), &got); err != nil {
		t.Fatalf("reply %q: %v", s, err)
	}
	if len(got.Tracks) != 3 || got.Tracks[2].ID != "3" || got.Index != 1 || got.PositionMS != 61000 || !got.Shuffle || got.Repeat != "all" {
		t.Errorf("got %+v; want the 3-track queue at index 1, 61 s, shuffle, repeat all", got)
	}
	if s := takeOver("7", "42", []string{other.URL, playing.URL}); s != "" {
		t.Errorf("no host plays 7: got %q, want \"\"", s)
	}
}
