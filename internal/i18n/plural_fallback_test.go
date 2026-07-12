package i18n

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// A translation that provides only one/other must still resolve every count in its own language: CLDR categories the
// file omits (e.g. Breton's two/few) fall back to the translation's "other" form, never to the English default.
func TestPluralMissingFormFallsBackToTranslationOther(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "active.br.json"),
		[]byte(`{"probe":{"one":"UNAN {{.Count}}","other":"LIES {{.Count}}"}}`), 0o644))
	Init(dir, "", "br")

	require.Equal(t, "UNAN 1", Tp("probe", "{{.Count}} one", "{{.Count}} other", 1)) // one
	require.Equal(t, "LIES 2", Tp("probe", "{{.Count}} one", "{{.Count}} other", 2)) // two -> other
	require.Equal(t, "LIES 3", Tp("probe", "{{.Count}} one", "{{.Count}} other", 3)) // few -> other
	require.Equal(t, "LIES 9", Tp("probe", "{{.Count}} one", "{{.Count}} other", 9)) // few -> other
	require.Equal(t, "LIES 5", Tp("probe", "{{.Count}} one", "{{.Count}} other", 5)) // other
}
