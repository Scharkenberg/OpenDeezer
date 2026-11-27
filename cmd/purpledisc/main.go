// Command purpledisc is a terminal Deezer client: log in with your ARL, browse liked
// songs, playlists and search, and stream (decrypt, decode and play locally). The ARL
// is sent only to Deezer.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/audio"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/config"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/i18n"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/impex"
	pdlog "github.com/Scharkenberg/PurpleDisc/v4/internal/log"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/mediacache"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/ui"
	version_ "github.com/Scharkenberg/PurpleDisc/v4/internal/version"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
)

// version defaults to the release number in internal/version and can be
// overridden at build time via -ldflags "-X main.version=...".
var version = version_.Number

// releaseVersion keeps a stamp that starts with a release number ("4.0.0", "v4.0.0-4-gabc").
// Anything else, such as a bare commit hash from an untagged checkout, follows the release
// number, so the update check never reads it as 0.0.0 and offers an older release.
func releaseVersion(v string) string {
	s := strings.TrimPrefix(v, "v")
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i > 0 && i < len(s) && s[i] == '.' {
		return v
	}
	return version_.Number + " (" + v + ")"
}

func main() {
	saveARL := flag.String("save-arl", "", "save this ARL to ~/.config/purpledisc/arl.txt and exit")
	showVer := flag.Bool("version", false, "print version and exit")
	exportPl := flag.String("export-playlist", "", "export the playlist with this id and exit (see -format, -o)")
	format := flag.String("format", "csv", "export format for -export-playlist: csv, m3u or json")
	outPath := flag.String("o", "", "output file for -export-playlist (default: stdout)")
	importPl := flag.String("import-playlist", "", "import this playlist file (CSV/M3U/plain text) into your Deezer account and exit")
	plTitle := flag.String("playlist-title", "", "title for the playlist created by -import-playlist (default: the file name)")
	screenReader := flag.Bool("screen-reader", false, "screen-reader mode for this run: a short plain-text view, no full-screen redraws")
	flag.Parse()
	version = releaseVersion(version)

	if *showVer {
		fmt.Println("purpledisc", version)
		return
	}

	if *saveARL != "" {
		if err := ui.SaveARL(*saveARL); err != nil {
			fmt.Fprintln(os.Stderr, "save-arl:", err)
			os.Exit(1)
		}
		fmt.Println("ARL saved.")
		return
	}

	if *exportPl != "" {
		if err := runExportPlaylist(*exportPl, *format, *outPath); err != nil {
			fmt.Fprintln(os.Stderr, "export-playlist:", err)
			os.Exit(1)
		}
		return
	}

	if *importPl != "" {
		if err := runImportPlaylist(*importPl, *plTitle); err != nil {
			fmt.Fprintln(os.Stderr, "import-playlist:", err)
			os.Exit(1)
		}
		return
	}

	// File logging (level via $PURPLEDISC_LOG), never to stdout so the TUI is unaffected.
	// If the log file cannot be opened, logs are discarded. The file stays open until exit.
	if base, err := os.UserConfigDir(); err == nil {
		_ = pdlog.OpenFile(filepath.Join(base, "purpledisc"))
	}
	pdlog.Info("purpledisc %s starting", version)

	// From here on an installer can ask this copy to quit: before the program runs (at the
	// ARL prompt) it just exits, so that the install folder can be replaced.
	var program atomic.Pointer[tea.Program]
	quitOnInstall(func() {
		if p := program.Load(); p != nil {
			p.Quit()
		} else {
			os.Exit(0)
		}
	})

	arl := ui.LoadARL()
	if arl == "" {
		fmt.Fprintln(os.Stderr, "No ARL found. Set $DEEZER_ARL or run:")
		fmt.Fprintln(os.Stderr, "  purpledisc -save-arl <your-arl>")
		fmt.Fprintln(os.Stderr, "\nYour ARL is the 'arl' cookie from an authenticated deezer.com session.")
		// In a terminal (a Start menu shortcut opens one that closes on exit) it can be
		// pasted here instead; it isn't echoed.
		if term.IsTerminal(os.Stdin.Fd()) {
			fmt.Fprint(os.Stderr, "\nPaste your ARL and press Enter (Enter alone quits): ")
			b, _ := term.ReadPassword(os.Stdin.Fd())
			fmt.Fprintln(os.Stderr)
			if arl = strings.TrimSpace(string(b)); arl != "" {
				if err := ui.SaveARL(arl); err != nil {
					fmt.Fprintln(os.Stderr, "save-arl:", err)
				}
			}
		}
		if arl == "" {
			os.Exit(1)
		}
	}

	audio.RemoveTempFiles() // stream files a crash left behind
	mediacache.RemoveOldCaches()
	player, err := audio.NewPlayer()
	if err != nil {
		fmt.Fprintln(os.Stderr, "audio:", err)
		os.Exit(1)
	}

	// Set the UI language before building the model (some strings, e.g. the
	// search placeholder, are captured at construction). The persisted language
	// wins; an empty setting means auto-detect from the locale environment.
	if lang := config.LoadLanguage(); lang != "" {
		i18n.SetLocale(lang)
	} else {
		i18n.SetLocale(i18n.DetectLocale())
	}

	ui.Version = version
	client := deezer.New(arl) // ui.New applies the saved playback settings
	model := ui.New(client, player)

	// Screen-reader mode draws inline without mouse reporting. Otherwise the
	// full-screen view with mouse cell-motion: wheel scrolling, click-to-select/activate
	// and progress-bar seeking (see internal/ui/mouse.go).
	sr := *screenReader || ui.ScreenReaderAtStart()
	model.SetScreenReader(sr)
	var opts []tea.ProgramOption
	if !sr {
		opts = append(opts, tea.WithAltScreen(), tea.WithMouseCellMotion())
	}
	p := tea.NewProgram(model, opts...)
	program.Store(p)
	model.StartMedia(p.Send) // OS media controls (MPRIS on Linux)
	if err := model.StartControl(p.Send); err != nil {
		pdlog.Warn("control api: %v", err) // non-fatal: TUI still runs
	}
	_, err = p.Run()
	// Save the resume point and history and close services however the program
	// ended: a quit key, SIGTERM or a recovered panic.
	model.Shutdown()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if model.SignedOut() {
		fmt.Println(i18n.T("Signed out."))
		if os.Getenv("DEEZER_ARL") != "" {
			fmt.Println(i18n.T("DEEZER_ARL is still set: remove it to stay signed out."))
		}
	}
}

// impexLogin logs in with the saved ARL exactly like the TUI does, with clear
// errors when there is no (working) ARL.
func impexLogin() (*deezer.Client, error) {
	arl := ui.LoadARL()
	if arl == "" {
		return nil, errors.New("not logged in — set $DEEZER_ARL or run: purpledisc -save-arl <your-arl>")
	}
	client := deezer.New(arl)
	if err := client.Login(); err != nil {
		if errors.Is(err, deezer.ErrARLExpired) {
			return nil, errors.New("ARL expired or invalid — refresh the 'arl' cookie from deezer.com, then `purpledisc -save-arl <arl>`")
		}
		return nil, fmt.Errorf("login: %w", err)
	}
	return client, nil
}

// runExportPlaylist implements -export-playlist: fetch the playlist and write
// it as CSV (Exportify style), extended M3U or JSON to -o (default stdout).
func runExportPlaylist(id, format, outPath string) error {
	client, err := impexLogin()
	if err != nil {
		return err
	}
	tracks, err := client.PlaylistTracks(id)
	if err != nil {
		return fmt.Errorf("fetch playlist %s: %w", id, err)
	}
	// The tracks endpoint doesn't return the title; look it up in the user's
	// own playlists (the common case) and fall back to a generic name.
	name := "Playlist " + id
	if ps, perr := client.Playlists(); perr == nil {
		for _, p := range ps {
			if p.ID == id {
				name = p.Name
				break
			}
		}
	}

	// Default to stdout, which we must never close; only a -o file is closed.
	var w io.Writer = os.Stdout
	var f *os.File
	if outPath != "" {
		f, err = os.Create(outPath)
		if err != nil {
			return err
		}
		w = f
	}
	switch format {
	case "csv":
		err = impex.ExportCSV(w, name, tracks)
	case "m3u":
		err = impex.ExportM3U(w, name, tracks)
	case "json":
		err = impex.ExportJSON(w, name, tracks)
	default:
		err = fmt.Errorf("unknown -format %q (want csv, m3u or json)", format)
	}
	// Closing a file opened for writing can surface a deferred write/flush error
	// (data flushed but not persisted). If the export itself succeeded, promote
	// the Close error so we don't claim success on a possibly-truncated file.
	if f != nil {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	if err != nil {
		return err
	}
	if outPath != "" {
		fmt.Printf("Exported %d tracks of %q to %s\n", len(tracks), name, outPath)
	}
	return nil
}

// runImportPlaylist implements -import-playlist: parse the file, resolve every
// row against Deezer (ISRC first, then fuzzy artist+title) with per-row
// progress, create the playlist, and summarize what didn't match.
func runImportPlaylist(path, title string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	rows, err := impex.ParseCSV(f)
	_ = f.Close() // read-only file; a Close error here is not actionable
	if err != nil {
		return err
	}
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	client, err := impexLogin()
	if err != nil {
		return err
	}

	fmt.Printf("Importing %d rows into %q…\n", len(rows), title)
	progress := func(i, total int, matched bool) {
		mark := "✓"
		if !matched {
			mark = "✗"
		}
		r := rows[i-1]
		label := r.Title
		if r.Artist != "" {
			label = r.Artist + " - " + r.Title
		}
		fmt.Printf("  [%d/%d] %s %s\n", i, total, mark, label)
	}
	res, err := impex.ImportPlaylist(context.Background(), impex.NewClientResolver(client), client, title, rows, progress)

	// Summary (also on failure: the partial result names what went wrong).
	if res != nil {
		fmt.Printf("\nMatched %d/%d rows.\n", len(res.Matched), len(rows))
		if len(res.Unmatched) > 0 {
			fmt.Println("Unmatched:")
			for _, r := range res.Unmatched {
				label := r.Title
				if r.Artist != "" {
					label = r.Artist + " - " + r.Title
				}
				fmt.Println("  - " + label)
			}
		}
	}
	if err != nil {
		return err
	}
	fmt.Printf("Created playlist %q (id %s) with %d tracks.\n", title, res.PlaylistID, len(res.Matched))
	return nil
}
