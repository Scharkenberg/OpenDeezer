package ui

import (
	"errors"
	"strings"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/audio"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/control"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/deezer"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/i18n"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/listens"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// menuRows is the home screen. A "Resume" row is prepended when a saved
// playback position exists.
func (m *Model) menuRows() []list.Item {
	var rows []list.Item
	if r := LoadResume(); r != nil {
		rows = append(rows, row{
			kind: rowMenu, action: actResume,
			title: "▶  " + i18n.T("Resume") + " — " + r.Name,
			desc:  r.ArtistLine + " · " + fmtMS(r.PositionMS) + " / " + fmtMS(r.DurationMS),
		})
	}
	rows = append(rows,
		row{kind: rowMenu, title: "❤  " + i18n.T("Liked Songs"), desc: i18n.T("favorites"), action: actLiked},
		row{kind: rowMenu, title: "≡  " + i18n.T("My Playlists"), desc: i18n.T("your playlists"), action: actPlaylists},
		row{kind: rowMenu, title: "⚡ Flow", desc: i18n.T("non-stop mix"), action: actFlow},
		row{kind: rowMenu, title: "📈 " + i18n.T("Charts"), desc: i18n.T("top tracks, albums & artists"), action: actCharts},
		row{kind: rowMenu, title: "🎙 " + i18n.T("Podcasts"), desc: i18n.T("search shows & episodes"), action: actPodcasts},
		row{kind: rowMenu, title: "🔍 " + i18n.T("Search"), desc: i18n.T("tracks, albums, artists, playlists"), action: actSearch},
		row{kind: rowMenu, title: "📊 " + i18n.T("Stats"), desc: i18n.T("recently played & top tracks"), action: actStats},
		row{kind: rowMenu, title: "📡 " + i18n.T("Remote control"), desc: i18n.T("drive another OpenDeezer client"), action: actRemote},
		row{kind: rowMenu, title: "📱 " + i18n.T("Web Remote"), desc: i18n.T("control from your phone over Wi-Fi"), action: actWebRemote},
		row{kind: rowMenu, title: "🌐 " + i18n.T("Language") + ": " + currentLanguageName(), desc: i18n.T("interface language"), action: actLanguage},
	)
	return rows
}

// setList shows a new list. The filter of the previous list is cleared first:
// left applied, it would hide the new items.
func (m *Model) setList(title string, items []list.Item) {
	m.list.ResetFilter()
	m.list.Title = title
	m.list.SetItems(items)
	m.list.ResetSelected()
	m.plConfirm = false
}

// showMenu returns to the home screen.
func (m *Model) showMenu() {
	m.setList("OpenDeezer", m.menuRows())
	m.screen = screenMenu
}

// onListScreen reports whether m.list is the visible body, so list keys
// (Enter, g/G, cursor keys, D on a row) may act on it.
func (m *Model) onListScreen() bool {
	switch m.screen {
	case screenMenu, screenList, screenDevices, screenRemote, screenPlaylistPick:
		return true
	}
	return false
}

// navMsg is the result of a navigation request, tagged with the navigation
// generation it was made in.
type navMsg struct {
	gen int
	msg tea.Msg
}

// navCmd starts a navigation request. Its result is dropped when the user has
// navigated or made another request since.
func (m *Model) navCmd(c tea.Cmd) tea.Cmd {
	m.nav++
	gen := m.nav
	m.navReq = gen
	return func() tea.Msg { return navMsg{gen: gen, msg: c()} }
}

// Update handles all messages. A screen change makes pending navigation results stale,
// drops a delete confirmation and detaches from a peer the new screen does not drive.
// Spinner and UI ticks run only while needed.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	prev := m.screen
	_, cmd := m.update(msg)
	if m.screen != prev {
		m.nav++
		m.plConfirm = false
		if m.remote != nil && m.peer() == nil {
			m.detachRemote()
		}
	}
	if m.loading && !m.spinning {
		m.spinning = true
		cmd = tea.Batch(cmd, m.spinner.Tick)
	}
	if st := m.player.State(); !m.ticking && (m.playing || st == audio.Loading || st == audio.Playing || m.peer() != nil) {
		m.ticking = true
		cmd = tea.Batch(cmd, tickCmd())
	}
	return m, cmd
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case navMsg:
		if msg.gen == m.nav {
			return m.update(msg.msg)
		}
		if msg.gen == m.navReq {
			m.loading = false // no newer request is loading
		}
		// An explicit play still plays; it just does not take over the screen.
		if p, ok := msg.msg.(playNowMsg); ok && len(p.tracks) > 0 {
			m.q.Set(p.tracks, 0)
			m.episodeMode = p.episodes
			m.playCtx = p.ctx
			return m, m.playCurrent()
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// View sizes the list against the footer it actually renders. Give it a
		// usable interim size here so resize messages never leave a zero viewport.
		m.list.SetSize(max(1, msg.Width), max(1, msg.Height))
		m.ready = true
		m.refreshCover()
		return m, nil

	case loginDoneMsg:
		m.loading = false
		if msg.err != nil {
			switch {
			case errors.Is(msg.err, deezer.ErrNoNetwork):
				// Connectivity loss, not an auth problem: show the No-Internet
				// screen with a retry rather than telling the user to re-login.
				m.screen = screenNoInternet
				m.status = ""
			case errors.Is(msg.err, deezer.ErrARLExpired):
				m.status = i18n.T("ARL expired or invalid — refresh the 'arl' cookie from deezer.com, then `opendeezer -save-arl <arl>`")
			default:
				m.status = i18n.Tf("Login failed: %s", msg.err.Error())
			}
			return m, nil
		}
		m.acct = m.client.Account()
		m.publishAccount()                                // identity snapshot for the control API (race-free read)
		m.client.SetAdsDisabled(config.LoadAdsDisabled()) // free-tier ads opt-out
		target := m.client.GainTarget()
		m.player.SetReplayGainTarget(target)
		m.savePlayback(func(pb *config.Playback) { pb.GainTarget = target })
		m.listens.Flush() // listens kept while offline
		m.showMenu()
		if m.acct.Name != "" {
			m.status = i18n.Tf("Logged in as %s · %s", m.acct.Name, m.acct.Offer)
		} else {
			m.status = i18n.Tf("Logged in · %s", m.acct.Offer)
		}
		switch {
		case !m.acct.Premium:
			// Free account: streams full tracks at 128 kbps, as Deezer's web player
			// does; 320/FLAC and downloads need a paid plan. A track
			// with no full-length source falls back to a 30-second preview.
			m.status += "  " + i18n.T("(free account · 128 kbps; downloads need a paid plan)")
		case (m.client.Quality() == 2 && !m.acct.CanHiFi) || (m.client.Quality() == 1 && !m.acct.CanHQ):
			// Warn if the chosen quality exceeds the plan's entitlement.
			m.status += "  " + i18n.T("(plan can't stream that quality — will fall back)")
		}
		// Seed the liked-ids cache in the background so the 'f' toggle knows the
		// current state before the Liked Songs view is ever opened; learn the web
		// player's build for the next start.
		return m, tea.Batch(m.likedIDsCmd(), m.webReleaseCmd())

	case likedIDsMsg:
		m.likedIDs = msg.ids
		return m, nil
	case radioMoreMsg:
		if msg.ctx == m.playCtx { // else another list is playing
			m.q.Append(msg.tracks...)
		}
		return m, nil

	case tracksMsg:
		m.loading = false
		items := make([]list.Item, len(msg.tracks))
		for i, t := range msg.tracks {
			items[i] = trackRow(t)
		}
		if msg.favorites {
			// The Liked Songs list is the authoritative liked set: refresh the cache.
			m.likedIDs = likedSet(msg.tracks)
		}
		// Browsing must not touch the live play queue: hold the tracks aside and
		// only commit to the queue on an explicit play (see activate).
		m.browse, m.browseCtx = msg.tracks, msg.ctx
		m.ownPlaylists = false
		m.pageArtist = deezer.ArtistInfo{}
		m.setList(msg.title, items)
		m.screen = screenList
		m.status = ""
		return m, nil

	case artistPageMsg:
		m.loading = false
		p := msg.page
		var items []list.Item
		if len(p.Top) > 0 {
			items = append(items, sectionRow(i18n.T("Top tracks")))
			for _, t := range p.Top {
				items = append(items, trackRow(t))
			}
		}
		if len(p.Albums) > 0 {
			items = append(items, sectionRow(i18n.T("Albums")))
			for _, a := range p.Albums {
				items = append(items, albumRow(a))
			}
		}
		if len(p.Related) > 0 {
			items = append(items, sectionRow(i18n.T("Related artists")))
			for _, a := range p.Related {
				items = append(items, artistRow(a))
			}
		}
		// Only the top tracks are the playable context (playBrowsed maps rows to
		// the browse slice by id, so header/album/artist rows never shift plays).
		m.browse, m.browseCtx = p.Top, listens.Context{Type: "artist_top", ID: p.Artist.ID}
		m.ownPlaylists = false
		m.pageArtist = p.Artist // 'm' with no seed row starts this artist's radio
		m.setList("♪ "+p.Artist.Name, items)
		if len(items) > 1 {
			m.list.Select(1) // start on the first entry, not the section header
		}
		m.screen = screenList
		m.status = ""
		return m, nil

	case podcastsMsg:
		m.loading = false
		items := make([]list.Item, len(msg.podcasts))
		for i, p := range msg.podcasts {
			items[i] = podcastRow(p)
		}
		m.ownPlaylists = false
		m.pageArtist = deezer.ArtistInfo{}
		m.setList(msg.title, items)
		m.screen = screenList
		m.status = ""
		return m, nil

	case episodesMsg:
		m.loading = false
		items := make([]list.Item, len(msg.episodes))
		browse := make([]deezer.Track, len(msg.episodes))
		for i, e := range msg.episodes {
			items[i] = episodeRow(e)
			browse[i] = e.AsTrack()
		}
		// Held aside until the user plays a row: see tracksMsg.
		m.browse, m.browseCtx = browse, msg.ctx
		m.ownPlaylists = false
		m.pageArtist = deezer.ArtistInfo{}
		m.setList(msg.title, items)
		m.screen = screenList
		m.status = ""
		return m, nil

	case statusMsg:
		m.loading = false
		m.status = msg.text
		return m, nil

	case downloadDoneMsg:
		m.downloading = false
		switch {
		case msg.err == nil:
			m.status = i18n.Tf("Saved to %s", msg.path)
		case deezer.IsPremiumRequired(msg.err):
			m.status = i18n.T("Downloads need a paid Deezer plan")
		default:
			m.status = i18n.Tf("Download failed: %s", msg.err.Error())
		}
		return m, nil

	case playlistsMsg:
		m.loading = false
		items := make([]list.Item, len(msg.playlists))
		for i, p := range msg.playlists {
			items[i] = playlistRow(p)
		}
		// This list is the user's own playlists (the only producer is
		// playlistsCmd), so the N/R/X playlist write ops apply here.
		m.ownPlaylists = true
		m.pageArtist = deezer.ArtistInfo{}
		m.setList(msg.title, items)
		m.screen = screenList
		m.status = ""
		return m, nil

	case favToggleMsg:
		m.loading = false
		if msg.err != nil {
			// Roll back the optimistic liked-set flip.
			if m.likedIDs != nil {
				if msg.liked {
					delete(m.likedIDs, msg.id)
				} else {
					m.likedIDs[msg.id] = true
				}
			}
			m.status = i18n.Tf("Error: %s", msg.err.Error())
			return m, nil
		}
		if msg.liked {
			m.status = "❤ " + i18n.Tf("Liked: %s", msg.name)
		} else {
			m.status = i18n.Tf("Unliked: %s", msg.name)
		}
		return m, nil

	case playlistPickMsg:
		// Snapshot the list the picker replaces (it borrows m.list), exactly like
		// the device picker: see devicesMsg for the prevScreen rationale.
		m.openPicker(screenPlaylistPick)
		m.pickTrack = msg.track
		items := []list.Item{row{
			kind: rowMenu, action: actNewPlaylist,
			title: "✚  " + i18n.T("New playlist…"), desc: i18n.T("create a playlist with this track"),
		}}
		for _, p := range msg.playlists {
			items = append(items, playlistRow(p))
		}
		m.setList("➕ "+i18n.T("Add to playlist"), items)
		m.screen = screenPlaylistPick
		m.loading = false
		m.status = ""
		return m, nil

	case playlistOpMsg:
		m.loading = false
		if msg.err != nil {
			m.status = i18n.Tf("Error: %s", msg.err.Error())
			return m, nil
		}
		m.status = msg.status
		// Reload the playlists list so the op is visible immediately, but only
		// when it is what's on screen (create-from-picker returns to a browse list).
		if msg.refresh && m.screen == screenList && m.ownPlaylists {
			m.loading = true
			return m, m.navCmd(m.playlistsCmd())
		}
		return m, nil

	case searchMsg:
		m.loading = false
		var items []list.Item
		for _, t := range msg.results.Tracks {
			items = append(items, trackRow(t))
		}
		for _, a := range msg.results.Artists {
			items = append(items, artistRow(a))
		}
		for _, a := range msg.results.Albums {
			items = append(items, albumRow(a))
		}
		for _, p := range msg.results.Playlists {
			items = append(items, playlistRow(p))
		}
		// Tracks are the playable context, held aside until an explicit play.
		m.browse, m.browseCtx = msg.results.Tracks, msg.ctx
		m.ownPlaylists = false
		m.pageArtist = deezer.ArtistInfo{}
		m.setList(i18n.T("Results"), items)
		m.screen = screenList
		m.status = ""
		return m, nil

	case streamReadyMsg:
		// Drop a stale resolve: the user has picked again (maybe the same
		// track) or stopped.
		if msg.seq != m.pickSeq.Load() {
			return m, nil
		}
		if msg.err != nil {
			m.resolving = false
			return m.update(errMsg{msg.err})
		}
		var seek int64
		if m.resumeID == msg.track.ID {
			seek = m.resumeMS
			m.resumeID, m.resumeMS = "", 0
		}
		return m, m.playerCmd(func() tea.Msg {
			if m.pickSeq.Load() != msg.seq {
				return nil // a newer pick or a stop came first
			}
			err := m.player.Play(msg.plan, msg.track.DurationMS)
			if err == nil && seek > 0 {
				m.player.SeekMS(seek) // resume
			}
			return playStartedMsg{track: msg.track, seq: msg.seq, err: err}
		})

	case playStartedMsg:
		if msg.seq != m.pickSeq.Load() {
			return m, nil
		}
		m.resolving = false
		m.loading = false
		if msg.err != nil {
			m.status = i18n.Tf("Playback error: %s", msg.err.Error())
			return m, nil
		}
		m.playing = true
		m.status = ""
		cmd := m.onTrackChanged(msg.track)
		// Ask for the next track for a gapless/crossfaded transition.
		m.requestPreload()
		return m, cmd

	case devicesMsg:
		m.openPicker(screenDevices)
		items := make([]list.Item, len(msg.devices))
		cur := m.player.CurrentDevice()
		for i, d := range msg.devices {
			items[i] = deviceRow(d.ID, d.Name, d.ID == cur)
		}
		m.setList(i18n.T("Output device"), items)
		m.screen = screenDevices
		m.loading = false
		m.status = ""
		return m, nil

	case lyricsMsg:
		m.loading = false
		if msg.err != nil {
			m.status = i18n.Tf("Lyrics: %s", msg.err.Error())
			return m, nil
		}
		if msg.trackID == m.lyricsTrack {
			m.lyrics = msg.lyrics
		}
		return m, nil

	case artMsg:
		if msg.img != nil && msg.trackID == m.curImgTrack {
			m.curImg = msg.img
			m.refreshCover()
		}
		return m, nil

	case trackFinishedMsg:
		// Branch on msg.swapped (captured at the finish signal), not the live player
		// state, which a pause/stop/selection racing the signal could have changed.
		switch {
		case m.resolving:
			// A pick is still resolving: it decides what plays next.
			return m, m.waitFinish()
		case msg.swapped && m.player.State() != audio.Stopped:
			// The player swapped in the preloaded linear next (PeekNext). Move the cursor to that
			// track, not via Next(), which could pick another under shuffle; no re-Play, so a pause
			// survives.
			m.histMarkFull() // the outgoing track played to its natural end
			m.q.AdvanceLinear()
			m.playing = true
			m.preloads.Take() // the player consumed it
			if t, ok := m.q.Current(); ok {
				m.requestPreload()
				return m, tea.Batch(m.onTrackChanged(t), m.waitFinish())
			}
			return m, tea.Batch(m.historyFlushCmd(), m.waitFinish())
		case m.player.State() == audio.Stopped:
			// Track ended with no preload and nothing new started: advance + play.
			m.histMarkFull() // natural end; advance() flushes if playback stops here
			return m, tea.Batch(m.advance(), m.waitFinish())
		default:
			// A user selection is already loading/playing: don't advance over it.
			return m, m.waitFinish()
		}

	case errMsg:
		m.loading = false
		// A call failed because the network dropped: show the No-Internet screen (remembering
		// where to return) instead of an error in the footer. The session stays valid.
		if errors.Is(msg.err, deezer.ErrNoNetwork) && m.screen != screenNoInternet {
			m.prevScreen = m.screen
			m.screen = screenNoInternet
			m.status = ""
			return m, nil
		}
		m.status = i18n.Tf("Error: %s", msg.err.Error())
		return m, nil

	case updateCheckMsg:
		// Note whether this reply answers a manual re-check before clearing the flag; the
		// status line is localized, so it cannot be compared with "Checking…".
		wasManual := m.updateChecking
		m.updateChecking = false
		m.updateChecked = true
		if msg.err == nil {
			m.updateInfo = msg.info
		}
		// The silent startup check never touches m.status: the footer notice (see
		// footer()) is enough when an update exists. Only a manual re-check (About
		// screen) surfaces feedback here.
		if wasManual {
			switch {
			case msg.err != nil:
				m.status = i18n.T("Update check failed (network?)")
			case msg.info.HasUpdate:
				m.status = ""
			default:
				m.status = i18n.T("You're on the latest version.")
			}
		}
		return m, nil

	case tickMsg:
		m.ticking = false // Update schedules the next one while it is wanted
		var histFlush tea.Cmd
		// The player can stop without firing onFinish (end-of-track sleep timer) or
		// fail (Errored), leaving m.playing stale. Reconcile it so the footer stops
		// showing "playing" and quit won't persist a bogus resume point.
		if st := m.player.State(); m.playing && (st == audio.Stopped || st == audio.Errored) {
			m.playing = false
			if st == audio.Errored {
				m.status = i18n.Tf("Playback error: %s", m.player.LastError())
			}
			// Close the listening session at the last observed position.
			histFlush = m.historyFlushCmd()
		}
		m.histSyncPos() // keep the session's listened-position sample fresh
		m.publishMedia()
		m.publishControl()
		// Poll the peer on the remote-control screen and while viewing its lyrics
		// (so synced lyrics scroll with the peer's position).
		if m.peer() != nil {
			return m, tea.Batch(m.remotePoll(), histFlush)
		}
		return m, histFlush

	case remoteConnMsg:
		m.loading = false
		if msg.err != nil {
			m.status = i18n.Tf("Remote: %s", msg.err.Error())
			return m, nil
		}
		m.remote = msg.client
		m.remoteAddr = msg.addr
		m.remoteName = msg.name
		m.remoteClient = msg.clientType
		m.remoteVersion = msg.version
		m.remoteState = msg.state
		m.screen = screenRemoteCtl
		name := msg.name
		if name == "" {
			name = msg.addr
		}
		m.status = i18n.Tf("Connected to %s", name)
		_ = SaveLastPeer(msg.addr)
		return m, nil

	case remoteStateMsg:
		if msg.poll {
			m.remotePolling = false
		}
		if msg.client != m.remote {
			return m, nil // a reply for a peer we have detached from
		}
		if msg.err != nil {
			m.status = i18n.Tf("Remote: %s", msg.err.Error())
			return m, nil
		}
		m.remoteState = msg.state
		if t := msg.state.Track; t != nil && m.screen == screenLyrics && t.ID != m.lyricsTrack {
			m.lyrics = nil
			m.lyricsTrack = t.ID
			m.lyricsOffset = 0
			return m, m.lyricsCmd(remoteTrack(t))
		}
		return m, nil

	case webRemoteMsg:
		m.loading = false
		if msg.errStr != "" {
			m.status = i18n.Tf("Web Remote: %s", msg.errStr)
			return m, nil
		}
		// When the cmd had to close the old loopback server and rebind on LAN,
		// replace m.ctrl with the new server so publishControl etc. reach it.
		if msg.replacedCtrl && msg.srv != nil {
			m.ctrl = msg.srv
		}
		if msg.srv != nil {
			m.webRemoteSrv = msg.srv
		}
		if msg.enabled && msg.replacedCtrl {
			m.webRemoteRebound = true
		}
		if msg.closed {
			// The LAN server is gone: the loopback API comes back as configured.
			m.ctrl, m.webRemoteSrv, m.webRemoteRebound = nil, nil, false
			if m.ctrlSend != nil {
				_ = m.StartControl(m.ctrlSend)
			}
		}
		m.webRemoteActive = msg.enabled
		m.webRemoteURL = msg.url
		m.webRemoteQR = msg.qr
		if msg.enabled {
			m.status = ""
		} else {
			m.status = i18n.T("Web remote disabled")
		}
		return m, nil

	case devicesDiscoveredMsg:
		m.loading = false
		items := []list.Item{row{
			kind: rowMenu, action: actRemoteManual,
			title: "✎  " + i18n.T("Enter address…"), desc: i18n.T("type a host:port manually"),
		}}
		for _, p := range msg.peers {
			items = append(items, peerRow(p))
		}
		m.setList(i18n.T("Connect to a device"), items)
		m.screen = screenRemote
		if len(msg.peers) == 0 {
			m.status = i18n.T("No devices found — enable OPENDEEZER_CONTROL=:7654 on the target.")
		} else {
			m.status = ""
		}
		return m, nil

	case mediaCmdMsg:
		var cmd tea.Cmd
		switch msg.kind {
		case "playpause":
			m.player.TogglePause()
		case "next":
			cmd = m.next()
		case "prev":
			cmd = m.prev()
		case "stop":
			cmd = m.stop()
		case "seek":
			cmd = m.seekCmd(m.player.PositionMS() + msg.arg/1000)
		case "setpos":
			cmd = m.seekCmd(msg.arg / 1000)
		}
		m.publishMedia()
		return m, cmd

	case controlCmdMsg:
		var cmd tea.Cmd
		switch msg.kind {
		case "playpause":
			m.player.TogglePause()
		case "next":
			cmd = m.next()
		case "prev":
			cmd = m.prev()
		case "stop":
			cmd = m.stop()
		case "restart":
			cmd = m.seekCmd(0)
		case "repeat":
			if rc := m.peer(); rc != nil {
				m.publishMedia()
				m.publishControl()
				return m, remoteCmd(rc, rc.CycleRepeat)
			}
			m.q.CycleRepeat()
			m.invalidatePreload() // upcoming track may differ under the new mode
		case "shuffle":
			if rc := m.peer(); rc != nil {
				m.publishMedia()
				m.publishControl()
				return m, remoteCmd(rc, rc.ToggleShuffle)
			}
			m.q.ToggleShuffle()
			m.invalidatePreload()
		case "repeat-set":
			// SET variant: absolute repeat mode from a GUI/web/SDK controller.
			if rc := m.peer(); rc != nil {
				m.publishMedia()
				m.publishControl()
				return m, remoteCmd(rc, func() (control.State, error) { return rc.SetRepeat(msg.mode) })
			}
			m.q.SetRepeat(parseRepeatMode(msg.mode))
			m.invalidatePreload() // upcoming track may differ under the new mode
		case "shuffle-set":
			// SET variant: absolute shuffle state from a GUI/web/SDK controller.
			if rc := m.peer(); rc != nil {
				m.publishMedia()
				m.publishControl()
				return m, remoteCmd(rc, func() (control.State, error) { return rc.SetShuffle(msg.on) })
			}
			m.q.SetShuffle(msg.on)
			m.invalidatePreload()
		case "seek":
			cmd = m.seekCmd(msg.ms)
		case "volume":
			m.player.SetVolume(msg.vol)
		case "playtrack":
			cmd = m.playTrackByIDCmd(msg.id)
		case "playplaylist":
			cmd = m.playPlaylistByIDCmd(msg.id)
		case "sleep":
			m.player.SetSleepTimer(time.Duration(msg.ms)*time.Minute, msg.eot)
			m.status = sleepStatus(m.player)
		case "sleepcancel":
			m.player.CancelSleepTimer()
			m.status = i18n.T("Sleep timer off")
		}
		m.publishMedia()
		m.publishControl()
		return m, cmd

	case playNowMsg:
		if len(msg.tracks) == 0 {
			return m, nil
		}
		items := make([]list.Item, len(msg.tracks))
		for i, t := range msg.tracks {
			items[i] = trackRow(t)
		}
		m.q.Set(msg.tracks, 0)
		m.browse = msg.tracks // keep row activation consistent with the shown list
		m.browseCtx, m.playCtx = msg.ctx, msg.ctx
		m.episodeMode = msg.episodes
		m.ownPlaylists = false
		m.pageArtist = deezer.ArtistInfo{}
		m.setList(i18n.T("Now Playing"), items)
		m.screen = screenList
		return m, m.playCurrent()

	case statsMsg:
		m.loading = false
		m.browse = nil // stats rows play by id, not through the browse slice
		m.browseCtx = listens.Context{}
		m.ownPlaylists = false
		m.pageArtist = deezer.ArtistInfo{}
		m.setList("📊 "+i18n.T("Stats"), statsItems(msg))
		m.screen = screenList
		m.status = ""
		return m, nil

	case controlQueueEditMsg:
		return m.handleControlQueueEdit(msg)

	case spinner.TickMsg:
		if !m.loading {
			m.spinning = false
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Delegate to the active sub-component.
	return m.delegate(msg)
}

func (m *Model) delegate(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.screen == screenSearch {
		m.search, cmd = m.search.Update(msg)
		return m, cmd
	}
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// openPicker saves the list a picker is about to replace and the return screen, unless
// already on that picker. An overlay is not recorded as prevScreen, to avoid an esc
// loop.
func (m *Model) openPicker(s screen) {
	if m.screen == s {
		return
	}
	m.devSavedItems = m.list.Items()
	m.devSavedTitle = m.list.Title
	m.devSavedIndex = m.list.Index()
	switch m.screen {
	case screenNowPlaying, screenCredits, screenQueue, screenLyrics, screenHelp:
	default:
		m.prevScreen = m.screen
	}
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Ctrl+C quits from every screen and input; main saves state after the
	// program returns (see Shutdown).
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	// No-Internet screen: retry logs in again, which doubles as a connectivity check; on
	// success loginDoneMsg returns to the app.
	if m.screen == screenNoInternet {
		switch msg.String() {
		case "r", "enter":
			if m.loading {
				return m, nil
			}
			m.loading = true
			m.status = ""
			return m, m.loginCmd()
		case "q":
			return m, tea.Quit
		}
		return m, nil
	}

	// Remote-control screens own their keys.
	if m.screen == screenRemoteCtl {
		return m.handleRemoteKey(msg)
	}
	if m.screen == screenRemoteInput {
		switch msg.String() {
		case "esc":
			m.showMenu()
			m.status = ""
			return m, nil
		case "enter":
			if m.search.Value() == "" {
				return m, nil
			}
			m.loading = true
			m.status = i18n.T("Connecting…")
			return m, m.navCmd(m.remoteConnectCmd(m.search.Value(), true)) // manual: trusted, may use token
		}
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		return m, cmd
	}

	if m.screen == screenWebRemote {
		return m.handleWebRemoteKey(msg)
	}

	// Playlist title prompt (create / rename): a text input like remote entry.
	if m.screen == screenPlaylistPrompt {
		switch msg.String() {
		case "esc":
			m.search.Blur()
			m.plPrompt = plNone
			m.screen = m.plReturn
			m.status = ""
			return m, nil
		case "enter":
			title := strings.TrimSpace(m.search.Value())
			if title == "" {
				return m, nil
			}
			m.search.Blur()
			prompt := m.plPrompt
			m.plPrompt = plNone
			m.screen = m.plReturn
			m.loading = true
			m.status = i18n.T("Loading…")
			switch prompt {
			case plCreateWithTrack:
				return m, m.createPlaylistCmd(title, []string{m.pickTrack.ID})
			case plCreateEmpty:
				return m, m.createPlaylistCmd(title, nil)
			case plRename:
				return m, m.renamePlaylistCmd(m.plTarget, title)
			}
			m.loading = false
			return m, nil
		}
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		return m, cmd
	}

	// Search input captures most keys; handle it first.
	if m.screen == screenSearch {
		switch msg.String() {
		case "esc":
			m.search.Blur()
			m.screen = m.searchReturn
			return m, nil
		case "enter":
			q := m.search.Value()
			if q == "" {
				return m, nil
			}
			m.loading = true
			m.status = i18n.T("Searching…")
			if m.searchPodcast {
				return m, m.navCmd(m.podcastSearchCmd(q))
			}
			return m, m.navCmd(m.searchCmd(q))
		}
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		return m, cmd
	}

	// Help is taller than many terminals. Scroll it directly instead of moving
	// the hidden list underneath the overlay.
	if m.screen == screenHelp {
		switch msg.String() {
		case "down", "j":
			m.helpOffset++
			return m, nil
		case "up", "k":
			m.helpOffset = max(0, m.helpOffset-1)
			return m, nil
		case "pgdown":
			m.helpOffset += max(1, m.height/2)
			return m, nil
		case "pgup":
			m.helpOffset = max(0, m.helpOffset-max(1, m.height/2))
			return m, nil
		case "home", "g":
			m.helpOffset = 0
			return m, nil
		case "end", "G":
			m.helpOffset = 1 << 30 // clamped to the last page by helpView
			return m, nil
		}
	}

	// Interactive queue view: cursor navigation + in-place editing. Unhandled
	// keys fall through to the global bindings (space, n/p, u to close, …).
	if m.screen == screenQueue {
		n := m.q.Len()
		switch msg.String() {
		case "down", "j":
			if m.queueSel < n-1 {
				m.queueSel++
			}
			return m, nil
		case "up", "k":
			if m.queueSel > 0 {
				m.queueSel--
			}
			return m, nil
		case "pgdown":
			m.queueSel = min(max(0, n-1), m.queueSel+max(1, m.height/2))
			return m, nil
		case "pgup":
			m.queueSel = max(0, m.queueSel-max(1, m.height/2))
			return m, nil
		case "g", "home":
			m.queueSel = 0
			return m, nil
		case "G", "end":
			m.queueSel = max(0, n-1)
			return m, nil
		case "enter":
			return m, m.queueJump(m.queueSel)
		case "x":
			return m, m.queueRemove(m.queueSel)
		case "J":
			return m, m.queueMove(m.queueSel, m.queueSel+1)
		case "K":
			return m, m.queueMove(m.queueSel, m.queueSel-1)
		}
	}

	// Let the list own keys while filtering (so typing works).
	if m.list.FilterState() == list.Filtering {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}

	// A playlist delete is awaiting confirmation: y deletes, n/esc cancels,
	// everything else is swallowed so stray keys can't act while it's pending.
	if m.plConfirm {
		switch msg.String() {
		case "y", "Y":
			m.plConfirm = false
			m.loading = true
			m.status = i18n.T("Deleting…")
			return m, m.deletePlaylistCmd(m.plTarget)
		case "n", "N", "esc":
			m.plConfirm = false
		}
		return m, nil
	}

	// Lyrics opened from the remote screen: transport keys drive the peer.
	if rc := m.peer(); rc != nil {
		if cmd, ok := m.remoteKey(rc, msg.String()); ok {
			return m, cmd
		}
	}

	switch msg.String() {
	case "q":
		return m, tea.Quit
	case " ":
		if m.player.State() == audio.Errored {
			return m, m.playCurrent() // the track failed: try it again
		}
		m.player.TogglePause()
		return m, nil
	case "n":
		// On a browse list with a track row selected, n queues it to play next
		// (see also e). Everywhere else it stays "next track".
		if t, ok := m.selectedBrowseTrack(); ok {
			m.q.InsertAfterCurrent(t)
			m.publishControl()
			m.status = i18n.Tf("Playing next: %s", t.Name)
			m.invalidatePreload()
			return m, nil
		}
		return m, m.next()
	case "e":
		// Add the selected browse track to the end of the queue.
		if t, ok := m.selectedBrowseTrack(); ok {
			m.q.Append(t)
			m.publishControl()
			m.status = i18n.Tf("Added to queue: %s", t.Name)
			m.invalidatePreload()
			return m, nil
		}
		return m, nil
	case "p":
		return m, m.prev()
	case "m":
		// Start radio: a mix seeded from the selected track/artist (browse lists,
		// queue view) or the current track (now playing) replaces the queue.
		return m.startRadio()
	case "f":
		// Toggle like on the highlighted track row, else the current track. The
		// cached liked set flips optimistically; favToggleMsg rolls back on error.
		var t deezer.Track
		if it, ok := m.list.SelectedItem().(row); ok && it.kind == rowTrack && m.screen == screenList {
			t = it.track
		} else if ct, ok := m.q.Current(); ok && !m.episodeMode {
			t = ct
		} else {
			return m, nil
		}
		like := !m.likedIDs[t.ID]
		if m.likedIDs == nil {
			m.likedIDs = map[string]bool{}
		}
		if like {
			m.likedIDs[t.ID] = true
			m.status = i18n.T("Liking…")
		} else {
			delete(m.likedIDs, t.ID)
			m.status = i18n.T("Unliking…")
		}
		return m, m.favToggleCmd(t, like)
	case "a":
		// Add the highlighted track row to a playlist (opens the picker).
		if it, ok := m.list.SelectedItem().(row); ok && it.kind == rowTrack && m.screen == screenList {
			m.loading = true
			m.status = i18n.T("Loading…")
			return m, m.navCmd(m.playlistPickCmd(it.track))
		}
		return m, nil
	case "r":
		m.status = i18n.Tf("Repeat: %s", i18n.T(m.q.CycleRepeat().String()))
		m.publishControl()
		// The upcoming track may have changed: drop the stale linear preload and preload again
		// for the new mode (nothing when it is not deterministic).
		m.invalidatePreload()
		return m, nil
	case "z":
		if m.q.ToggleShuffle() {
			m.status = i18n.T("Shuffle on")
		} else {
			m.status = i18n.T("Shuffle off")
		}
		m.publishControl()
		m.invalidatePreload()
		return m, nil
	case "g":
		if m.onListScreen() {
			m.list.Select(0)
		}
		return m, nil
	case "G":
		if n := len(m.list.VisibleItems()); n > 0 && m.onListScreen() {
			m.list.Select(n - 1)
		}
		return m, nil
	case "u":
		// On the About screen, "u" is a manual "check for updates" instead
		// of the usual queue shortcut (pointless there anyway).
		if m.screen == screenCredits {
			if m.updateChecking {
				return m, nil
			}
			m.updateChecking = true
			m.status = i18n.T("Checking for updates…")
			return m, m.updateCheckCmd()
		}
		m.toggleScreen(screenQueue)
		if m.screen == screenQueue {
			m.queueSel = max(0, m.q.Index()) // open with the cursor on the playing track
		}
		return m, nil
	case "t":
		m.status = i18n.Tf("Theme: %s", m.cycleTheme())
		return m, nil
	case "T":
		// Cycle the sleep timer: off → 15 → 30 → 45 → 60 min → end-of-track → off.
		m.cycleSleepTimer()
		m.status = sleepStatus(m.player)
		return m, nil
	case "R":
		// On the playlists screen, R renames the highlighted playlist.
		if p, ok := m.selectedOwnPlaylist(); ok {
			m.plPrompt = plRename
			m.plTarget = p
			m.plReturn = screenList
			m.search.SetValue(p.Name)
			m.search.Focus()
			m.screen = screenPlaylistPrompt
			m.status = ""
			return m, nil
		}
		on := !m.player.ReplayGain()
		m.player.SetReplayGain(on)
		m.savePlayback(func(pb *config.Playback) { pb.ReplayGain = on })
		if on {
			m.status = i18n.T("ReplayGain on (loudness normalization)")
		} else {
			m.status = i18n.T("ReplayGain off")
		}
		return m, nil
	case "E":
		on := !m.player.EQEnabled()
		m.player.SetEQEnabled(on)
		if on {
			m.status = i18n.Tf("Equalizer on (%s)", m.player.EQPreset())
		} else {
			m.status = i18n.T("Equalizer off")
		}
		return m, nil
	case "ctrl+e":
		// Cycle the EQ preset; wraps to the first after the last (a "custom"
		// state restarts the cycle).
		names := audio.EQPresetNames
		cur := m.player.EQPreset()
		next := names[0]
		for i, n := range names {
			if n == cur {
				next = names[(i+1)%len(names)]
				break
			}
		}
		_ = m.player.SetEQPreset(next)
		if m.player.EQEnabled() {
			m.status = i18n.Tf("EQ preset: %s", next)
		} else {
			m.status = i18n.Tf("EQ preset: %s (equalizer off — E to enable)", next)
		}
		return m, nil
	case "M":
		on := !m.player.MonoDownmix()
		m.player.SetMonoDownmix(on)
		if on {
			m.status = i18n.T("Mono downmix on")
		} else {
			m.status = i18n.T("Mono downmix off")
		}
		return m, nil
	case "d":
		// Output device picker.
		m.loading = true
		m.status = i18n.T("Loading…")
		return m, m.navCmd(m.devicesCmd())
	case "x":
		// Cycle crossfade: 0 → 3s → 6s → 12s → 0.
		next := map[int]int{0: 3000, 3000: 6000, 6000: 12000, 12000: 0}[m.player.CrossfadeMS()]
		m.player.SetCrossfadeMS(next)
		m.savePlayback(func(pb *config.Playback) { pb.CrossfadeMS = next })
		if next == 0 {
			m.status = i18n.T("Crossfade off")
		} else {
			m.status = i18n.Tf("Crossfade %ds", next/1000)
		}
		return m, nil
	case "C":
		// Cycle the stream cache size: off → 1 → 2 → 5 → 10 → 20 GB → off,
		// applied at once.
		media := config.LoadMedia()
		next := 0
		for _, mb := range cacheSizesMB {
			if mb > media.MediaCacheMB {
				next = mb
				break
			}
		}
		err := m.caches.SetMB(next)
		switch {
		case err != nil:
			m.status = i18n.Tf("Error: %s", err.Error())
		case next == 0:
			m.status = i18n.T("Stream cache off")
		default:
			m.status = i18n.Tf("Stream cache %d GB", next>>10)
		}
		return m, nil
	case "ctrl+g":
		on := !m.player.Gapless()
		m.player.SetGapless(on)
		m.savePlayback(func(pb *config.Playback) { pb.Gapless = on })
		if on {
			m.status = i18n.T("Gapless on")
		} else {
			m.status = i18n.T("Gapless off")
		}
		return m, nil
	case "l":
		// Show synced lyrics for the current track (the peer's, on its lyrics screen).
		var t deezer.Track
		if m.peer() != nil && m.remoteState.Track != nil {
			t = remoteTrack(m.remoteState.Track)
		} else if ct, ok := m.q.Current(); ok {
			t = ct
		} else {
			return m, nil
		}
		m.toggleScreen(screenLyrics)
		if m.screen == screenLyrics && (m.lyrics == nil || m.lyricsTrack != t.ID) {
			m.lyricsTrack = t.ID
			m.loading = true
			m.status = i18n.T("Loading…")
			return m, m.lyricsCmd(t)
		}
		return m, nil
	case "+", "=":
		m.status = volStatus(m.player.AddVolume(0.1))
		return m, nil
	case "-", "_":
		m.status = volStatus(m.player.AddVolume(-0.1))
		return m, nil
	case "left":
		return m, m.seekCmd(m.player.PositionMS() - 10000)
	case "right":
		return m, m.seekCmd(m.player.PositionMS() + 10000)
	case "h":
		q := (m.client.Quality() + 1) % 3
		m.client.SetQuality(q)
		m.savePlayback(func(pb *config.Playback) { pb.Quality = q })
		switch q {
		case 2:
			m.status = i18n.T("Audio quality: HiFi (FLAC)")
		case 1:
			m.status = i18n.T("Audio quality: High (MP3 320)")
		default:
			m.status = i18n.T("Audio quality: Normal (MP3 128)")
		}
		return m, nil
	case "D":
		// Download a full track to disk (premium-only). Prefer the highlighted
		// track row; otherwise fall back to the current track (not podcasts).
		var t deezer.Track
		if it, ok := m.list.SelectedItem().(row); ok && it.kind == rowTrack && m.onListScreen() {
			t = it.track
		} else if ct, ok := m.q.Current(); ok && !m.episodeMode {
			t = ct
		} else {
			return m, nil
		}
		if !m.acct.Premium {
			m.status = i18n.T("Downloads need a paid Deezer plan")
			return m, nil
		}
		if m.downloading {
			m.status = i18n.T("A download is already in progress…")
			return m, nil
		}
		m.downloading = true
		m.status = i18n.Tf("Downloading %s…", t.Name)
		return m, m.downloadCmd(t)
	case "A":
		// Toggle Deezer Free's play-reporting/ads (free accounts only). Paid plans
		// have no ads, so it's a no-op there.
		if m.acct.Premium {
			m.status = i18n.T("Your plan has no ads")
			return m, nil
		}
		off := !m.client.AdsDisabled()
		m.client.SetAdsDisabled(off)
		_ = config.SaveAdsDisabled(off)
		if off {
			m.status = i18n.T("Ads off — plays no longer reported (at your own risk: breaks Deezer's terms, denies artists their play count)")
		} else {
			m.status = i18n.T("Ads on — plays reported to Deezer (supports artists)")
		}
		return m, nil
	case "s":
		return m, m.stop()
	case "/":
		m.openSearch(false)
		return m, nil
	case "c":
		m.toggleScreen(screenNowPlaying)
		return m, nil
	case "?":
		if m.screen != screenHelp {
			m.helpOffset = 0
		}
		m.toggleScreen(screenHelp)
		return m, nil
	case "i":
		m.toggleScreen(screenCredits)
		return m, nil
	case "U":
		// Open the available update's release page. Never downloads or
		// installs anything itself.
		if m.updateInfo.HasUpdate && m.updateInfo.URL != "" {
			openInBrowser(m.updateInfo.URL)
			m.updateDismissed = true
		}
		return m, nil
	case "X":
		// On the playlists screen, X deletes the highlighted playlist (after a
		// y/n confirm the footer shows). Elsewhere it dismisses the update notice.
		if p, ok := m.selectedOwnPlaylist(); ok {
			m.plConfirm = true
			m.plTarget = p
			return m, nil
		}
		// Dismiss the footer's "update available" notice for this session.
		m.updateDismissed = true
		return m, nil
	case "N":
		// New empty playlist, from the playlists screen.
		if m.screen == screenList && m.ownPlaylists {
			m.plPrompt = plCreateEmpty
			m.plReturn = screenList
			m.search.SetValue("")
			m.search.Focus()
			m.screen = screenPlaylistPrompt
			m.status = ""
			return m, nil
		}
		return m, nil
	case "ctrl+f":
		// Start the local list filter (the list's Filter binding is ctrl+f, "/"
		// being Deezer search), but only where the list is the visible body, so
		// an overlay can't put the hidden list into filtering mode.
		if m.onListScreen() {
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			return m, cmd
		}
		return m, nil
	case "esc", "backspace":
		// An applied local filter clears first; a second esc then navigates.
		// (While typing the filter, the FilterState()==Filtering block above owns
		// esc via the list's cancel binding.)
		if m.list.FilterState() == list.FilterApplied && m.onListScreen() {
			m.list.ResetFilter()
			return m, nil
		}
		switch m.screen {
		case screenNowPlaying, screenCredits, screenQueue, screenLyrics, screenHelp:
			m.screen = m.prevScreen
		case screenDevices, screenPlaylistPick:
			m.restoreList() // the picker replaced m.list; put the browse list back
			m.screen = m.prevScreen
		case screenList, screenRemote:
			m.showMenu()
		}
		return m, nil
	case "enter":
		if m.onListScreen() {
			return m.activate()
		}
		return m, nil
	}

	if !m.onListScreen() {
		return m, nil
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// startRadio handles 'm': the selected track or artist row seeds a song or artist
// radio; otherwise the artist page's artist, the queue cursor row or the current track
// does. Podcast episodes have no mixes.
func (m *Model) startRadio() (tea.Model, tea.Cmd) {
	radio := func(cmd tea.Cmd) (tea.Model, tea.Cmd) {
		m.loading = true
		m.status = i18n.T("Starting radio…")
		return m, m.navCmd(cmd)
	}
	if m.screen == screenList {
		if it, ok := m.list.SelectedItem().(row); ok {
			switch {
			case it.kind == rowTrack:
				return radio(m.trackMixCmd(it.track.ID))
			case it.kind == rowArtist:
				return radio(m.artistMixCmd(it.artist.ID))
			case it.kind == rowHistory && it.histID != "":
				return radio(m.trackMixCmd(it.histID))
			}
		}
		if m.pageArtist.ID != "" {
			return radio(m.artistMixCmd(m.pageArtist.ID))
		}
		return m, nil
	}
	if m.screen == screenQueue && !m.episodeMode {
		if ts := m.q.Tracks(); m.queueSel >= 0 && m.queueSel < len(ts) {
			return radio(m.trackMixCmd(ts[m.queueSel].ID))
		}
		return m, nil
	}
	if t, ok := m.q.Current(); ok && !m.episodeMode {
		return radio(m.trackMixCmd(t.ID))
	}
	return m, nil
}

// openSearch records where the search overlay was opened and resets its mode.
// Keeping a dedicated return screen avoids corrupting prevScreen, which belongs
// to now-playing/help/lyrics overlays that may themselves open search.
func (m *Model) openSearch(podcasts bool) {
	m.searchReturn = m.screen
	m.searchPodcast = podcasts
	m.search.SetValue("")
	m.search.Focus()
	m.status = ""
	m.screen = screenSearch
}

// activate handles Enter on the selected row.
func (m *Model) activate() (tea.Model, tea.Cmd) {
	it, ok := m.list.SelectedItem().(row)
	if !ok {
		return m, nil
	}
	load := func(cmd tea.Cmd) (tea.Model, tea.Cmd) {
		m.status = i18n.T("Loading…")
		m.loading = true
		return m, m.navCmd(cmd)
	}
	switch it.kind {
	case rowMenu:
		switch it.action {
		case actLiked:
			return load(m.favoritesCmd())
		case actPlaylists:
			return load(m.playlistsCmd())
		case actCharts:
			return load(m.chartsCmd())
		case actFlow:
			return load(m.flowCmd())
		case actSearch:
			m.openSearch(false)
			return m, nil
		case actStats:
			return load(m.statsCmd())
		case actPodcasts:
			m.openSearch(true)
			return m, nil
		case actRemote:
			m.loading = true
			m.status = i18n.T("Scanning…")
			return m, m.navCmd(m.discoverDevicesCmd())
		case actWebRemote:
			m.webCtrlCfg = config.LoadControl()
			m.screen = screenWebRemote
			m.status = ""
			return m, nil
		case actLanguage:
			// Cycle the UI language and rebuild the home menu so every row
			// re-renders in the new language (the list holds a static snapshot of
			// its items; overlays/footer re-read T() every frame on their own).
			name := m.cycleLanguage()
			idx := m.list.Index()
			m.setList("OpenDeezer", m.menuRows())
			if idx >= 0 && idx < len(m.list.Items()) {
				m.list.Select(idx)
			}
			m.status = i18n.Tf("Language: %s", name)
			return m, nil
		case actRemoteManual:
			m.search.SetValue(LoadLastPeer())
			m.search.Focus()
			m.screen = screenRemoteInput
			m.status = ""
			return m, nil
		case actResume:
			if r := LoadResume(); r != nil {
				m.q.Set([]deezer.Track{r.Track()}, 0)
				m.episodeMode = r.Episode
				m.resumeID, m.resumeMS = r.ID, r.PositionMS
				m.playCtx = listens.Context{}
				return m, m.playCurrent()
			}
			return m, nil
		case actNewPlaylist:
			// "New playlist…" inside the add-to-playlist picker: put the browsed
			// list back and prompt for a title; the new playlist is seeded with
			// the picked track.
			m.plPrompt = plCreateWithTrack
			m.plReturn = m.prevScreen
			m.restoreList()
			m.search.SetValue("")
			m.search.Focus()
			m.screen = screenPlaylistPrompt
			m.status = ""
			return m, nil
		}
	case rowTrack:
		// Explicit play: commit the browsed list to the play queue at this row.
		// This is the only place browsing turns into the live queue.
		m.playBrowsed(it.track, false)
		return m, m.playCurrent()
	case rowHistory:
		// Stats rows carry only the track id: fetch the full track and play it
		// (same path as the control API's "play track <id>").
		if it.histID == "" {
			return m, nil
		}
		return load(m.playTrackByIDCmd(it.histID))
	case rowArtist:
		return load(m.artistPageCmd(it.artist))
	case rowPodcast:
		return load(m.episodesCmd(it.podcast))
	case rowEpisode:
		// Explicit play: commit the browsed episodes to the play queue at this row.
		m.playBrowsed(it.episode.AsTrack(), true)
		return m, m.playCurrent()
	case rowDevice:
		m.restoreList() // the picker replaced m.list; put the browse list back
		m.screen = m.prevScreen
		return m, m.setDeviceCmd(it.deviceID, it.title)
	case rowPlaylist:
		// Inside the add-to-playlist picker, enter adds the picked track here
		// instead of opening the playlist.
		if m.screen == screenPlaylistPick {
			t := m.pickTrack
			m.restoreList()
			m.screen = m.prevScreen
			m.loading = true
			m.status = i18n.T("Loading…")
			return m, m.addToPlaylistCmd(it.playlist, t)
		}
		return load(m.playlistTracksCmd(it.playlist))
	case rowAlbum:
		return load(m.albumTracksCmd(it.album))
	case rowPeer:
		m.loading = true
		m.status = i18n.Tf("Connecting to %s…", it.title)
		return m, m.navCmd(m.remoteConnectCmd(it.peerAddr, false)) // discovered: account-only
	}
	return m, nil
}

// playBrowsed replaces the queue with the browsed list at the activated row (by index,
// else by ID when filtering shifts rows) and sets the episode mode. A row not in the
// list plays alone. Only an explicit play calls it.
func (m *Model) playBrowsed(t deezer.Track, episodes bool) {
	idx := m.list.Index()
	if idx < 0 || idx >= len(m.browse) || m.browse[idx].ID != t.ID {
		idx = m.findInBrowse(t.ID)
	}
	if idx < 0 {
		m.q.Set([]deezer.Track{t}, 0)
	} else {
		m.q.Set(m.browse, idx)
	}
	m.episodeMode = episodes
	m.playCtx = m.browseCtx
}

func (m *Model) findInBrowse(id string) int {
	for i, t := range m.browse {
		if t.ID == id {
			return i
		}
	}
	return -1
}

// restoreList puts back the list the device picker temporarily replaced.
func (m *Model) restoreList() {
	if m.devSavedItems == nil {
		return
	}
	m.setList(m.devSavedTitle, m.devSavedItems)
	if m.devSavedIndex >= 0 && m.devSavedIndex < len(m.devSavedItems) {
		m.list.Select(m.devSavedIndex)
	}
	m.devSavedItems = nil
}

// playCurrent resolves + plays the queue's current track.
func (m *Model) playCurrent() tea.Cmd {
	// The player is still on the outgoing track here (the queue cursor already
	// moved): capture its final position for the listening-history session,
	// which onTrackChanged flushes once the new stream is live.
	m.histSyncPos()
	t, ok := m.q.Current()
	if !ok {
		return nil
	}
	if m.resumeID != t.ID {
		m.resumeID, m.resumeMS = "", 0 // the resume offset belongs to another track
	}
	// Until this pick plays, the finish handler must not advance over it, and a
	// preload of the old next track must not be swapped in.
	m.resolving = true
	seq := m.pickSeq.Add(1)
	m.preloads.Clear()
	m.status = i18n.Tf("Loading: %s", t.Name)
	m.loading = true
	if m.episodeMode {
		return m.episodeStreamCmd(t, seq)
	}
	return m.streamCmd(t, seq)
}

// stop halts local playback and cancels a pick that is still resolving.
func (m *Model) stop() tea.Cmd {
	m.histSyncPos() // sample the position before the player forgets it
	m.resolving = false
	m.pickSeq.Add(1)
	m.loading = false
	m.playing = false
	return tea.Batch(m.playerCmd(func() tea.Msg { m.player.Stop(); return nil }), m.historyFlushCmd())
}

// playerCmd runs a player call that can block (fade-outs, device switches) off
// the update loop, one at a time.
func (m *Model) playerCmd(f func() tea.Msg) tea.Cmd {
	return func() tea.Msg {
		m.playerMu.Lock()
		defer m.playerMu.Unlock()
		return f()
	}
}

// seekCmd seeks the local player to ms.
func (m *Model) seekCmd(ms int64) tea.Cmd {
	return m.playerCmd(func() tea.Msg { m.player.SeekMS(ms); return nil })
}

// setDeviceCmd switches the output device and reports the result.
func (m *Model) setDeviceCmd(id, title string) tea.Cmd {
	return m.playerCmd(func() tea.Msg {
		if err := m.player.SetDevice(id); err != nil {
			return statusMsg{i18n.Tf("Device error: %s", err.Error())}
		}
		_ = SaveAudioDevice(id)
		return statusMsg{i18n.Tf("Output: %s", title)}
	})
}

// playTrackByIDCmd fetches a track by id and plays it (control API).
func (m *Model) playTrackByIDCmd(id string) tea.Cmd {
	return func() tea.Msg {
		t, err := m.client.Track(id)
		if err != nil {
			return errMsg{err}
		}
		return playNowMsg{tracks: []deezer.Track{t}, ctx: listens.Context{Type: "track_page", ID: id}}
	}
}

// playPlaylistByIDCmd loads a playlist by id and plays it from the top.
func (m *Model) playPlaylistByIDCmd(id string) tea.Cmd {
	return func() tea.Msg {
		ts, err := m.client.PlaylistTracks(id)
		if err != nil {
			return errMsg{err}
		}
		return playNowMsg{tracks: ts, ctx: listens.Context{Type: "playlist_page", ID: id}}
	}
}

// selectedBrowseTrack returns the highlighted track row on a browse list, for
// the n/e queue-building keys. Episode queues are excluded: mixing a music
// track into a plain-stream episode queue would break playback resolution.
func (m *Model) selectedBrowseTrack() (deezer.Track, bool) {
	if m.screen != screenList || m.episodeMode {
		return deezer.Track{}, false
	}
	it, ok := m.list.SelectedItem().(row)
	if !ok || it.kind != rowTrack {
		return deezer.Track{}, false
	}
	return it.track, true
}

// selectedOwnPlaylist returns the highlighted playlist when the user's own
// playlists screen is showing: the gate for the N/R/X write ops.
func (m *Model) selectedOwnPlaylist() (deezer.Playlist, bool) {
	if m.screen != screenList || !m.ownPlaylists {
		return deezer.Playlist{}, false
	}
	it, ok := m.list.SelectedItem().(row)
	if !ok || it.kind != rowPlaylist {
		return deezer.Playlist{}, false
	}
	return it.playlist, true
}

// invalidatePreload drops a possibly-stale gapless preload after the upcoming
// track changed (queue edit, repeat/shuffle change) and asks for the next
// track of the new order.
func (m *Model) invalidatePreload() {
	m.preloads.Clear()
	m.requestPreload()
}

// requestPreload asks for the queue's deterministic next track to follow the
// playing one gaplessly or with a crossfade; the player's near-end hook
// resolves it. Nothing for podcast episodes or an undetermined next track.
func (m *Model) requestPreload() {
	if m.episodeMode {
		return
	}
	if t, ok := m.q.PeekNext(); ok {
		m.preloads.Request(t.ID, t.DurationMS)
	}
}

// queueJump plays the queue entry under the queue-view cursor.
func (m *Model) queueJump(i int) tea.Cmd {
	if i < 0 || i >= m.q.Len() || i == m.q.Index() {
		return nil
	}
	m.q.SetIndex(i)
	m.publishControl()
	return m.playCurrent()
}

// queueRemove deletes the queue entry under the queue-view cursor. The playing
// track can't be removed: the queue cursor must keep matching the audio.
func (m *Model) queueRemove(i int) tea.Cmd {
	if i < 0 || i >= m.q.Len() {
		return nil
	}
	if i == m.q.Index() {
		m.status = i18n.T("Can't remove the playing track")
		return nil
	}
	name := m.q.Tracks()[i].Name
	if !m.q.Remove(i) {
		return nil
	}
	if i < m.queueSel {
		m.queueSel-- // keep the cursor on its track
	}
	if m.queueSel >= m.q.Len() {
		m.queueSel = max(0, m.q.Len()-1)
	}
	m.publishControl()
	m.status = i18n.Tf("Removed from queue: %s", name)
	m.invalidatePreload()
	return nil
}

// queueMove moves the queue entry at i to j. The cursor follows its track:
// the moved one (J/K) or the one the move shifted.
func (m *Model) queueMove(i, j int) tea.Cmd {
	if !m.q.Move(i, j) {
		return nil
	}
	switch {
	case i == m.queueSel:
		m.queueSel = j
	case i < m.queueSel && j >= m.queueSel:
		m.queueSel--
	case i > m.queueSel && j <= m.queueSel:
		m.queueSel++
	}
	m.publishControl()
	m.invalidatePreload()
	return nil
}

func (m *Model) next() tea.Cmd {
	if m.q.Next() {
		return m.playCurrent()
	}
	return nil
}

// prevRestartMS is where Previous restarts the track instead of going back, as
// the engine and Deezer's Android app do.
const prevRestartMS = 5000

func (m *Model) prev() tea.Cmd {
	// While a pick resolves, the position is the outgoing track's: step back.
	if !m.resolving && m.player != nil && m.player.PositionMS() >= prevRestartMS {
		return m.seekCmd(0)
	}
	if m.q.Prev() {
		return m.playCurrent()
	}
	return nil
}

// advance is called when a track finishes naturally.
func (m *Model) advance() tea.Cmd {
	if m.q.AdvanceAuto() {
		return m.playCurrent()
	}
	m.playing = false
	// End of the queue: nothing else will flush the session, so record it here
	// (histMarkFull already ran on the finish message).
	return m.historyFlushCmd()
}

// onTrackChanged refreshes now-playing state (media, lyrics, cover) for a newly
// active track, records the outgoing track's listening-history session, and
// returns the follow-up commands (history write, cover and lyrics fetch).
func (m *Model) onTrackChanged(t deezer.Track) tea.Cmd {
	flush := m.historyFlushCmd() // record the outgoing track (if any)
	m.histStartSession(t)
	m.listens.Started(listens.Start{ID: t.ID, Episode: m.episodeMode, Context: m.playCtx, Shuffle: m.q.Shuffle(), Repeat: int(m.q.Repeat())})
	m.status = ""
	m.publishMedia()
	m.publishControl()
	m.lyrics = nil
	m.lyricsTrack = ""
	m.lyricsOffset = 0
	var lyrics tea.Cmd
	if m.screen == screenLyrics && m.peer() == nil {
		m.lyricsTrack = t.ID
		lyrics = m.lyricsCmd(t)
	}
	m.curImg = nil
	m.curCover = ""
	m.curImgTrack = t.ID
	var cover tea.Cmd
	if artworkSupported() && t.ArtworkURL != "" {
		cover = m.coverCmd(t.ID, t.ArtworkURL)
	}
	// A queue played from Flow or a mix is topped up as it runs low.
	var radio tea.Cmd
	if left := m.q.Len() - m.q.Index() - 1; m.q.Len() > 0 && left <= 4 && deezer.IsRadio(m.playCtx.Type) && m.peer() == nil {
		ctx, queued := m.playCtx, append([]deezer.Track(nil), m.q.Tracks()...)
		radio = func() tea.Msg {
			more := m.radio.More(m.client, ctx.Type, ctx.ID, left, func() []deezer.Track { return queued })
			if len(more) == 0 {
				return nil
			}
			return radioMoreMsg{ctx: ctx, tracks: more}
		}
	}
	return tea.Batch(flush, cover, lyrics, radio)
}

// toggleScreen flips to dst (remembering the screen to return to) or back.
func (m *Model) toggleScreen(dst screen) {
	if m.screen == dst {
		m.screen = m.prevScreen
		return
	}
	if m.screen == screenDevices || m.screen == screenPlaylistPick {
		m.restoreList() // leaving a picker for an overlay; put the borrowed list back
	}
	// Don't stack overlay-on-overlay (incl. the pickers) as the return target.
	switch m.screen {
	case screenNowPlaying, screenCredits, screenQueue, screenLyrics, screenHelp,
		screenDevices, screenPlaylistPick:
	default:
		m.prevScreen = m.screen
	}
	m.screen = dst
}

// saveResume persists the current track + position so it can be resumed later.
func (m *Model) saveResume() {
	if t, ok := m.q.Current(); ok && m.playing {
		_ = SaveResume(t, m.player.PositionMS(), m.episodeMode)
	}
}

// Shutdown saves the resume point and the listening session and closes every
// background service. main calls it once the program has returned, whatever
// ended it (a quit key, a signal, a recovered panic). Later calls do nothing.
func (m *Model) Shutdown() {
	if m.shutDown {
		return
	}
	m.shutDown = true
	m.histSyncPos()
	m.historyFlushNow() // synchronous: a goroutine could be killed mid-write on quit
	m.saveResume()
	m.player.Stop()
	if m.media != nil {
		m.media.Close()
	}
	if m.discord != nil {
		m.discord.Close()
	}
	if m.ctrl != nil {
		m.ctrl.Close()
	}
	if m.webRemoteSrv != nil && m.webRemoteSrv != m.ctrl {
		m.webRemoteSrv.Close()
	}
	if m.advertiser != nil {
		m.advertiser.Close()
	}
}

func volStatus(v float64) string {
	return i18n.Tf("Volume %d%%", int(v*100+0.5))
}
