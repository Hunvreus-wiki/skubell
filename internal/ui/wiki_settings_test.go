package ui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequiredValidator(t *testing.T) {
	t.Parallel()

	v := requiredValidator("needed")
	require.Error(t, v(""))
	require.Error(t, v("   "))
	require.NoError(t, v("x"))
}

func TestValidateURL(t *testing.T) {
	t.Parallel()

	// Well-known mode: the URL is derived from farm/project, so the field is never required or validated.
	wellKnown := &WikiSettingsScreen{state: wikiFormState{selectedFarm: farmWikimedia}}
	require.NoError(t, wellKnown.validateURL(""))
	require.NoError(t, wellKnown.validateURL("anything"))

	// Custom mode: the URL is required and must resolve to a usable API endpoint.
	custom := &WikiSettingsScreen{state: wikiFormState{selectedFarm: farmCustom}}
	require.Error(t, custom.validateURL(""), "empty custom URL is invalid")
	require.NoError(t, custom.validateURL("https://example.org/w/api.php"))
}
