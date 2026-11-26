package ui

import (
	"runtime"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/audio"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/config"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/i18n"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// setting is a Settings row: its label key, its current value and what enter does.
type setting struct {
	label  string
	value  func() string
	change func() tea.Cmd
}

// settings lists the Settings screen's rows; play reporting only for free accounts.
func (m *Model) settings() []setting {
	s := []setting{
		{"Audio quality", func() string {
			return i18n.T([]string{"Normal (MP3 128)", "High (MP3 320)", "HiFi (FLAC)"}[m.client.Quality()])
		}, func() tea.Cmd {
			next := (m.client.Quality() + 1) % 3
			m.client.SetQuality(next)
			if m.client.Quality() < next { // the plan stops below it: start over
				m.client.SetQuality(0)
			}
			q := m.client.Quality()
			m.savePlayback(func(pb *config.Playback) { pb.Quality = q })
			return nil
		}},
		{"ReplayGain", func() string { return boolLabel(m.player.ReplayGain()) }, func() tea.Cmd {
			on := !m.player.ReplayGain()
			m.player.SetReplayGain(on)
			m.savePlayback(func(pb *config.Playback) { pb.ReplayGain = on })
			return nil
		}},
		{"Equalizer", func() string { return boolLabel(m.player.EQEnabled()) }, func() tea.Cmd {
			m.player.SetEQEnabled(!m.player.EQEnabled())
			return nil
		}},
		{"EQ preset", m.player.EQPreset, func() tea.Cmd {
			// Wraps to the first after the last (a "custom" state restarts the cycle).
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
			return nil
		}},
		{"Mono downmix", func() string { return boolLabel(m.player.MonoDownmix()) }, func() tea.Cmd {
			m.player.SetMonoDownmix(!m.player.MonoDownmix())
			return nil
		}},
		{"Crossfade", func() string {
			if ms := m.player.CrossfadeMS(); ms > 0 {
				return i18n.Tf("%d s", ms/1000)
			}
			return i18n.T("off")
		}, func() tea.Cmd {
			next := map[int]int{0: 3000, 3000: 6000, 6000: 12000, 12000: 0}[m.player.CrossfadeMS()]
			m.player.SetCrossfadeMS(next)
			m.savePlayback(func(pb *config.Playback) { pb.CrossfadeMS = next })
			return nil
		}},
		{"Gapless", func() string { return boolLabel(m.player.Gapless()) }, func() tea.Cmd {
			on := !m.player.Gapless()
			m.player.SetGapless(on)
			m.savePlayback(func(pb *config.Playback) { pb.Gapless = on })
			return nil
		}},
		{"Output device", func() string { return "" }, func() tea.Cmd {
			m.loading = true
			m.status = i18n.T("Loading…")
			return m.navCmd(m.devicesCmd())
		}},
		{"Stream cache", func() string {
			if mb := config.LoadMedia().MediaCacheMB; mb > 0 {
				return i18n.Tf("%d GB", mb>>10)
			}
			return i18n.T("off")
		}, func() tea.Cmd {
			// off → 1 → 2 → 5 → 10 → 20 GB → off, applied at once.
			media := config.LoadMedia()
			next := 0
			for _, mb := range cacheSizesMB {
				if mb > media.MediaCacheMB {
					next = mb
					break
				}
			}
			if err := m.caches.SetMB(next); err != nil {
				m.status = i18n.Tf("Error: %s", err.Error())
			}
			return nil
		}},
		{"Theme", func() string { return palettes[m.theme].name }, func() tea.Cmd {
			m.cycleTheme()
			return nil
		}},
		{"Language", currentLanguageName, func() tea.Cmd {
			m.cycleLanguage()
			return nil
		}},
		{"Screen reader mode", func() string { return boolLabel(m.sr) }, m.toggleScreenReader},
	}
	if !m.acct.Premium {
		s = append(s, setting{"Play reporting", func() string { return boolLabel(!m.client.AdsDisabled()) }, func() tea.Cmd {
			off := !m.client.AdsDisabled()
			m.client.SetAdsDisabled(off)
			_ = config.SaveAdsDisabled(off)
			if off {
				m.status = i18n.T("Ads off — plays no longer reported (at your own risk: breaks Deezer's terms, denies artists their play count)")
			} else {
				m.status = i18n.T("Ads on — plays reported to Deezer (supports artists)")
			}
			return nil
		}})
	}
	return append(s, setting{"About", func() string { return "" }, func() tea.Cmd {
		m.toggleScreen(screenCredits)
		return nil
	}}, setting{"Sign out device", func() string { return "" }, func() tea.Cmd {
		m.signOutConfirm = true
		return nil
	}})
}

// signOutQuestion asks before the saved ARL goes; on Windows the desktop app reads the
// same file, so it says that app is signed out too.
func signOutQuestion() string {
	if runtime.GOOS == "windows" {
		return i18n.T("Sign out this device? The Windows app on this computer is signed out too. (y/n)")
	}
	return i18n.T("Sign out this device? (y/n)")
}

// signOut deletes the saved ARL and quits; a failure stays on screen.
func (m *Model) signOut() tea.Cmd {
	if err := DeleteARL(); err != nil {
		m.status = i18n.Tf("Error: %s", err.Error())
		return nil
	}
	m.signedOut = true
	return tea.Quit
}

// SignedOut reports whether the program ended by signing out.
func (m *Model) SignedOut() bool { return m.signedOut }

// showSettings lists the settings with their current values, the cursor on row i.
func (m *Model) showSettings(i int) {
	var items []list.Item
	for n, s := range m.settings() {
		items = append(items, row{kind: rowSetting, title: i18n.T(s.label), right: s.value(), setting: n})
	}
	m.setList(i18n.T("Settings"), items)
	m.list.Select(i)
}
