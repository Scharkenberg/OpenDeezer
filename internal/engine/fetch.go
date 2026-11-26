package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
)

// maxFetch bounds Fetch: the length goes out to C as an int, and artwork is far smaller.
const maxFetch = 32 << 20

// Fetch downloads raw bytes (cover art) for hosts without an HTTP stack, with the web
// player's User-Agent. A body cut short or over maxFetch is an error; offline mode
// refuses it.
func Fetch(url string) ([]byte, error) {
	if OfflineMode() {
		return nil, ErrOfflineMode
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", deezer.UserAgent)
	resp, err := deezer.HTTPClient(15 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxFetch+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxFetch {
		return nil, fmt.Errorf("over %d bytes", maxFetch)
	}
	return b, nil
}
