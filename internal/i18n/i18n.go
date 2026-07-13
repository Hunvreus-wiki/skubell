// Package i18n wraps github.com/nicksnyder/go-i18n/v2 with a thin, fallback-first API. Every call site carries its
// English text, so the application stays fully usable even when no translation files are present.
package i18n

import (
	"encoding/json"
	"io/fs"
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

// Init builds the message bundle from the shipped (embedded) locales and the user locale directory, then activates
// lang. It tolerates a nil FS or an empty/missing/unreadable user directory: any message without a translation falls
// back to its English default.
func Init(shipped fs.FS, userLocalesDir, lang string) {
	bundle = newBundle()
	loadFS(shipped)         // shipped files first, embedded in the binary
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

// loadFS loads the active.<lang>.json files from an fs.FS (typically the embedded shipped locales). A nil or
// unreadable FS is a no-op, leaving the affected messages to fall back to English.
func loadFS(fsys fs.FS) {
	if fsys == nil {
		return
	}
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return
	}
	for _, entry := range entries {
		if name := entry.Name(); !entry.IsDir() && isLocaleFile(name) {
			_, _ = bundle.LoadMessageFileFS(fsys, name)
		}
	}
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
		if name := entry.Name(); !entry.IsDir() && isLocaleFile(name) {
			_, _ = bundle.LoadMessageFile(filepath.Join(dir, name))
		}
	}
}

func isLocaleFile(name string) bool {
	return strings.HasPrefix(name, "active.") && strings.HasSuffix(name, ".json")
}

// active returns the current localizer, lazily creating an English-only one when Init/SetLanguage were never called.
func active() *goi18n.Localizer {
	if localizer == nil {
		SetLanguage("en")
	}
	return localizer
}
