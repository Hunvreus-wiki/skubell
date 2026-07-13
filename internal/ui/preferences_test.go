package ui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/i18n"
	"github.com/Hunvreus-wiki/skubell/locales"
)

// TestThemePreferenceLocalization verifies the theme dropdown shows localized labels while the stored value stays the
// canonical "system"/"light"/"dark", and that the label<->value mapping round-trips in every shipped language.
func TestThemePreferenceLocalization(t *testing.T) {
	i18n.Init(locales.FS, "", "en")
	defer i18n.SetLanguage("en")

	require.Equal(t, []string{"System", "Light", "Dark"}, themeLabels(), "English menu labels, in display order")

	for _, lang := range []string{"en", "fr", "br"} {
		i18n.SetLanguage(lang)
		for _, value := range []string{"system", "light", "dark"} {
			label := themeLabelFor(value)
			require.NotEmpty(t, label)
			require.Equal(t, value, themeValueFor(label), "%s: label %q must map back to its stored value", lang, label)
		}
	}

	// Localization actually happens rather than leaking the English default.
	i18n.SetLanguage("fr")
	require.Equal(t, "Sombre", themeLabelFor("dark"))
	i18n.SetLanguage("br")
	require.Equal(t, "Teñval", themeLabelFor("dark"))
}

// TestCurrentLanguageTracksSetLanguage guards the accessor Preferences relies on to restore the language on cancel.
func TestCurrentLanguageTracksSetLanguage(t *testing.T) {
	i18n.Init(locales.FS, "", "en")
	defer i18n.SetLanguage("en")

	i18n.SetLanguage("br")
	require.Equal(t, "br", i18n.CurrentLanguage())
	i18n.SetLanguage("fr")
	require.Equal(t, "fr", i18n.CurrentLanguage())
}
