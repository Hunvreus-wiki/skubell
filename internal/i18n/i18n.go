// Package i18n wraps github.com/nicksnyder/go-i18n/v2 with a thin, fallback-first API. Every call site carries its
// English text, so the application stays fully usable even when no translation files are present.
package i18n

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

var (
	bundle    *goi18n.Bundle
	localizer *goi18n.Localizer
)

// Init builds the message bundle from the shipped and user locale directories and activates lang. It tolerates empty,
// missing, or unreadable directories: any message without a translation falls back to its English default.
func Init(appLocalesDir, userLocalesDir, lang string) {
	bundle = newBundle()
	loadDir(appLocalesDir)  // shipped files first
	loadDir(userLocalesDir) // user files override or complete them
	SetLanguage(lang)
}

// SetLanguage switches the active language at runtime; an unknown language falls back to English per message.
func SetLanguage(lang string) {
	if bundle == nil {
		bundle = newBundle()
	}
	localizer = goi18n.NewLocalizer(bundle, lang, "en")
}

// AvailableLanguages returns the sorted ISO codes of the loaded translation files, always including "en".
func AvailableLanguages() []string {
	seen := map[string]struct{}{"en": {}}
	if bundle != nil {
		for _, tag := range bundle.LanguageTags() {
			seen[tag.String()] = struct{}{}
		}
	}
	codes := make([]string, 0, len(seen))
	for code := range seen {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

func newBundle() *goi18n.Bundle {
	b := goi18n.NewBundle(language.English)
	b.RegisterUnmarshalFunc("json", json.Unmarshal)
	return b
}

func loadDir(dir string) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "active.") || !strings.HasSuffix(name, ".json") {
			continue
		}
		_, _ = bundle.LoadMessageFile(filepath.Join(dir, name))
	}
}

// active returns the current localizer, lazily creating an English-only one when Init/SetLanguage were never called.
func active() *goi18n.Localizer {
	if localizer == nil {
		SetLanguage("en")
	}
	return localizer
}
