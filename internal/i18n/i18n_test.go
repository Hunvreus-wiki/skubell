package i18n

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHelpersFallBackToEnglishWithoutFiles(t *testing.T) {
	Init("", "", "fr") // no locale files loaded

	require.Equal(t, "Delete", T("delete_button", "Delete"))
	require.Equal(t, "Connected to frwiki as Bot", Td("connected_to",
		"Connected to {{.Wiki}} as {{.User}}", map[string]any{"Wiki": "frwiki", "User": "Bot"}))
	require.Equal(t, "Delete 1 page", Tp("del", "Delete {{.Count}} page", "Delete {{.Count}} pages", 1))
	require.Equal(t, "Delete 3 pages", Tp("del", "Delete {{.Count}} page", "Delete {{.Count}} pages", 3))
	require.Equal(t, "Deleting 2 pages (1/2)", Tpd("prog",
		"Deleting {{.Count}} page ({{.Current}}/{{.Total}})",
		"Deleting {{.Count}} pages ({{.Current}}/{{.Total}})",
		2, map[string]any{"Current": 1, "Total": 2}))
}

func TestTranslationsLoadAndSwitch(t *testing.T) {
	dir := t.TempDir()
	writeLocale(t, dir, "active.fr.json", `{
		"delete_button": {"other": "Supprimer"},
		"connected_to": {"other": "Connecté à {{.Wiki}} en tant que {{.User}}"},
		"del": {"one": "Supprimer {{.Count}} page", "other": "Supprimer {{.Count}} pages"}
	}`)

	Init(dir, "", "fr")

	require.Equal(t, "Supprimer", T("delete_button", "Delete"))
	require.Equal(t, "Connecté à frwiki en tant que Bot", Td("connected_to",
		"Connected to {{.Wiki}} as {{.User}}", map[string]any{"Wiki": "frwiki", "User": "Bot"}))
	require.Equal(t, "Supprimer 1 page", Tp("del", "Delete {{.Count}} page", "Delete {{.Count}} pages", 1))
	require.Equal(t, "Supprimer 2 pages", Tp("del", "Delete {{.Count}} page", "Delete {{.Count}} pages", 2))

	// A key missing from French still falls back to the English default.
	require.Equal(t, "Cancel", T("cancel_button", "Cancel"))

	require.Contains(t, AvailableLanguages(), "fr")
	require.Contains(t, AvailableLanguages(), "en")

	// Switching back to English uses the English defaults.
	SetLanguage("en")
	require.Equal(t, "Delete", T("delete_button", "Delete"))
}

func TestUserDirOverridesShippedFile(t *testing.T) {
	appDir := t.TempDir()
	userDir := t.TempDir()
	writeLocale(t, appDir, "active.fr.json", `{"delete_button": {"other": "Supprimer"}}`)
	writeLocale(t, userDir, "active.fr.json", `{"delete_button": {"other": "Effacer"}}`)

	Init(appDir, userDir, "fr")
	require.Equal(t, "Effacer", T("delete_button", "Delete"), "user locale must override the shipped one")
}

// TestShippedLocalesLoad loads the real repository locales/ directory and verifies the fr and br files translate. It
// asserts the mechanism (a non-English, non-empty result; no English leak on a "few"-category count) rather than
// specific vocabulary, so it stays green while the translations are still being reviewed/edited.
func TestShippedLocalesLoad(t *testing.T) {
	Init(filepath.Join("..", "..", "locales"), "", "fr")
	for _, lang := range []string{"fr", "br"} {
		require.Contains(t, AvailableLanguages(), lang)
		SetLanguage(lang)

		deleteLabel := T("common_delete", "Delete")
		require.NotEqual(t, "Delete", deleteLabel, "%s should translate common_delete", lang)
		require.NotEmpty(t, deleteLabel)

		// A "few"-category count (3) must resolve in-language, never leaking the English plural default.
		few := Tp("del_results_count", "{{.Count}} result", "{{.Count}} results", 3)
		require.Contains(t, few, "3")
		require.NotContains(t, few, "result", "%s plural leaked the English default", lang)
	}

	SetLanguage("en")
	require.Equal(t, "Delete", T("common_delete", "Delete"))
}

func writeLocale(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
