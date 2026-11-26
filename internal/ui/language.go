package ui

import (
	"github.com/Scharkenberg/PurpleDisc/v4/internal/config"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/i18n"
)

// languageOrder is the cycle the Language setting steps through. "" is Auto
// (detect from the environment); the rest are shipped locale codes in the same
// order as i18n.Available().
var languageOrder = []string{"", "en", "zh", "hi", "es", "fr", "ar", "ru"}

// applyLanguage activates a saved language code, falling back to environment
// detection when it is empty (Auto).
func applyLanguage(code string) {
	if code == "" {
		i18n.SetLocale(i18n.DetectLocale())
		return
	}
	i18n.SetLocale(code)
}

// languageName is the native display name for a locale code, or "Auto" when the
// code is empty.
func languageName(code string) string {
	if code == "" {
		return i18n.T("Auto")
	}
	for _, l := range i18n.Available() {
		if l.Code == code {
			return l.Name
		}
	}
	return code
}

// currentLanguageName is the native display name for the Language setting's own
// persisted selection (the file, not $PURPLEDISC_LANG), or "Auto" when unset.
func currentLanguageName() string {
	return languageName(config.LanguageSetting())
}

// cycleLanguage switches to the next language in languageOrder, saves and applies it.
// It starts from config.LanguageSetting, so a set $PURPLEDISC_LANG cannot freeze the
// cycle.
func (m *Model) cycleLanguage() {
	cur := config.LanguageSetting()
	idx := 0
	for i, c := range languageOrder {
		if c == cur {
			idx = i
			break
		}
	}
	next := languageOrder[(idx+1)%len(languageOrder)]
	_ = config.SaveLanguage(next)
	applyLanguage(next)
}
