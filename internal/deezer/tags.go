package deezer

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-flac/flacpicture"
	"github.com/go-flac/flacvorbis"
	flac "github.com/go-flac/go-flac"

	pdlog "github.com/Scharkenberg/PurpleDisc/v4/internal/log"
)

// ArtworkFetcher fetches bytes for a Deezer artwork URL (or upgraded size).
// Supply a no-op or fake for tests so no network calls are made.
type ArtworkFetcher func(ctx context.Context, artworkURL string) ([]byte, error)

// upgradeArtworkURL follows existing patterns (gwImage uses 250x250; API
// responses use cover_medium / picture_medium). We request a reasonably large
// 500x500 variant suitable for embedded covers.
func upgradeArtworkURL(u string) string {
	if u == "" {
		return ""
	}
	for _, old := range []string{"250x250-000000-80-0-0.jpg", "250x250", "120x120", "56x56", "80x80"} {
		if strings.Contains(u, old) {
			rep := "500x500"
			if strings.Contains(old, "000000") {
				rep = "500x500-000000-80-0-0.jpg"
			}
			return strings.Replace(u, old, rep, 1)
		}
	}
	return u
}

// artworkTimeout bounds a whole cover download, so a stalled one cannot hold up a save.
var artworkTimeout = 30 * time.Second

func defaultArtworkFetcher(ctx context.Context, url string) ([]byte, error) {
	if url == "" {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := HTTPClient(artworkTimeout).Do(req)
	if err != nil {
		return nil, classifyNet(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("artwork CDN returned %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// tagFile writes metadata (and an optional cover) into a downloaded audio file. On
// error the audio is left untouched and the error returned. ext is "mp3" or "flac",
// since the temp path has no extension.
func tagFile(ctx context.Context, path, ext string, t Track, artworkURL string, trackNumber int, date string, fetch ArtworkFetcher) error {
	if fetch == nil {
		fetch = defaultArtworkFetcher
	}
	var art []byte
	if artworkURL != "" {
		if b, err := fetch(ctx, artworkURL); err == nil && len(b) > 0 {
			art = b
		} else if err != nil {
			pdlog.Debug("tag: artwork fetch for %s: %v (continuing without art)", path, err)
		}
	}

	if ext == "flac" {
		return tagFLAC(path, t, art, trackNumber, date)
	}
	return tagMP3(path, t, art, trackNumber, date)
}

// tagMP3 puts an ID3v2.4 tag in front of the audio, replacing one already there: UTF-8
// text frames (title, artist, album, date, track number) and the cover as the front
// cover picture. The tag and the audio go to a temp file renamed over path.
func tagMP3(path string, t Track, art []byte, trackNumber int, date string) error {
	var frames bytes.Buffer
	frame := func(id string, body []byte) {
		frames.WriteString(id)
		frames.Write(synchsafe(len(body)))
		frames.Write([]byte{0, 0}) // flags
		frames.Write(body)
	}
	text := func(id, s string) { frame(id, append(append([]byte{3}, s...), 0)) } // 3: UTF-8
	text("TIT2", t.Name)
	text("TPE1", t.ArtistLine())
	text("TALB", t.AlbumName)
	if date != "" {
		text("TDRC", date)
	}
	if trackNumber > 0 {
		text("TRCK", strconv.Itoa(trackNumber))
	}
	if len(art) > 0 {
		pic := append([]byte{3}, "image/jpeg\x00\x03Front cover\x00"...) // UTF-8, MIME, front cover, description
		frame("APIC", append(pic, art...))
	}

	src, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("id3v2 open: %w", err)
	}
	defer func() { _ = src.Close() }()
	skip, err := id3Size(src)
	if err != nil {
		return fmt.Errorf("id3v2 read: %w", err)
	}
	if _, err := src.Seek(skip, io.SeekStart); err != nil {
		return fmt.Errorf("id3v2 read: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tag-*")
	if err != nil {
		return fmt.Errorf("id3v2 save: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	w := bufio.NewWriter(tmp)
	_, _ = w.Write(append([]byte("ID3\x04\x00\x00"), synchsafe(frames.Len())...))
	_, _ = w.Write(frames.Bytes())
	_, err = io.Copy(w, src)
	if err == nil {
		err = w.Flush()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		_ = src.Close()
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		return fmt.Errorf("id3v2 save: %w", err)
	}
	return nil
}

// synchsafe is n as ID3v2.4 writes sizes: four bytes of seven bits each.
func synchsafe(n int) []byte {
	return []byte{byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
}

// id3Size is the length of the ID3v2 tag at the start of r (header, frames and a
// footer when its flag is set), 0 when r starts with none.
func id3Size(r io.Reader) (int64, error) {
	h := make([]byte, 10)
	if _, err := io.ReadFull(r, h); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return 0, nil
		}
		return 0, err
	}
	if string(h[:3]) != "ID3" {
		return 0, nil
	}
	n := int64(h[6]&0x7f)<<21 | int64(h[7]&0x7f)<<14 | int64(h[8]&0x7f)<<7 | int64(h[9]&0x7f)
	n += 10
	if h[5]&0x10 != 0 {
		n += 10
	}
	return n, nil
}

// tagFLAC replaces the file's comment and picture blocks: it reads only the
// metadata, writes the new metadata to a temp file, copies the frames after
// it and renames the temp file over path.
func tagFLAC(path string, t Track, art []byte, trackNumber int, date string) error {
	src, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("flac open: %w", err)
	}
	defer func() { _ = src.Close() }()
	f, err := flac.ParseMetadata(src)
	if err != nil {
		return fmt.Errorf("flac parse: %w", err)
	}
	var sync [2]byte
	if _, err := io.ReadFull(src, sync[:]); err != nil || sync[0] != 0xFF || sync[1]>>2 != 0x3E {
		return fmt.Errorf("flac parse: %w", flac.ErrorNoSyncCode)
	}

	// Preserve StreamInfo (must be first). Drop prior comment/picture blocks
	// so we control the final set.
	newMeta := make([]*flac.MetaDataBlock, 0, len(f.Meta)+2)
	if len(f.Meta) > 0 {
		newMeta = append(newMeta, f.Meta[0])
	}
	for _, m := range f.Meta[1:] {
		if m.Type != flac.VorbisComment && m.Type != flac.Picture {
			newMeta = append(newMeta, m)
		}
	}

	// Add only rejects malformed field names, and these are the library's own.
	cmts := flacvorbis.New()
	_ = cmts.Add(flacvorbis.FIELD_TITLE, t.Name)
	_ = cmts.Add(flacvorbis.FIELD_ARTIST, t.ArtistLine())
	_ = cmts.Add(flacvorbis.FIELD_ALBUM, t.AlbumName)
	if trackNumber > 0 {
		_ = cmts.Add(flacvorbis.FIELD_TRACKNUMBER, fmt.Sprintf("%d", trackNumber))
	}
	if date != "" {
		_ = cmts.Add(flacvorbis.FIELD_DATE, date)
	}
	cm := cmts.Marshal()
	newMeta = append(newMeta, &cm)

	if len(art) > 0 {
		p, perr := flacpicture.NewFromImageData(flacpicture.PictureTypeFrontCover, "Front cover", art, "image/jpeg")
		if perr == nil {
			pm := p.Marshal()
			newMeta = append(newMeta, &pm)
		}
	}

	// Write the tagged copy next to the original and rename it over path only on full
	// success, so a short write never destroys the downloaded audio.
	mode := os.FileMode(0o644)
	if fi, serr := src.Stat(); serr == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tag-*")
	if err != nil {
		return fmt.Errorf("flac tag temp: %w", err)
	}
	tmpName := tmp.Name()
	w := bufio.NewWriter(tmp)
	_, _ = w.WriteString("fLaC")
	for i, m := range newMeta {
		_, _ = w.Write(m.Marshal(i == len(newMeta)-1))
	}
	_, _ = w.Write(sync[:])
	_, err = io.Copy(w, src)
	if err == nil {
		err = w.Flush()
	}
	if err == nil {
		// The tagged copy reaches the disk before it replaces the original.
		err = tmp.Sync()
	}
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("flac tag write: %w", err)
	}
	// Windows cannot rename over a file that is open.
	_ = src.Close()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("flac tag close: %w", err)
	}
	_ = os.Chmod(tmpName, mode)
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("flac tag rename: %w", err)
	}
	return nil
}
