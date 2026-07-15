package api

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/i18n"
	"github.com/Hunvreus-wiki/skubell/locales"
)

// TestGrantMessagesKeepIdentifiersInEveryLanguage guards what must survive translation: a right name is an identifier
// the operator matches against MediaWiki, and Special:BotPasswords is the canonical title that resolves on a wiki in
// any language. Translating either would send the reader looking for something that does not exist.
func TestGrantMessagesKeepIdentifiersInEveryLanguage(t *testing.T) {
	i18n.Init(locales.FS, "", "en")
	defer i18n.SetLanguage("en")

	for _, lang := range i18n.AvailableLanguages() {
		i18n.SetLanguage(lang)
		require.Contains(t, MediaWikiNamespaceDeleteGrantMessage(), "Special:BotPasswords", lang)
		require.Contains(t, SiteCSSDeleteGrantMessage(), "editsitecss", lang)
		require.Contains(t, SiteJSDeleteGrantMessage(), "editsitejs", lang)
		require.Contains(t, SiteJSONDeleteGrantMessage(), "editsitejson", lang)
		require.Contains(t, customWikiUnreachableMessage(), "api.php", lang)
	}
}

// TestGrantMessagesAreTranslated proves the messages actually reach the locale files rather than always serving their
// English fallback.
func TestGrantMessagesAreTranslated(t *testing.T) {
	i18n.Init(locales.FS, "", "en")
	defer i18n.SetLanguage("en")

	english := MediaWikiNamespaceDeleteGrantMessage()
	require.Contains(t, english, "Skubell cannot delete pages")

	for _, lang := range []string{"fr", "br"} {
		i18n.SetLanguage(lang)
		require.NotEqual(t, english, MediaWikiNamespaceDeleteGrantMessage(), lang)
		require.NotEqual(t, english, SiteCSSDeleteGrantMessage(), lang)
	}
}
