package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadFixtureSiteInfoSample(t *testing.T) {
	t.Parallel()

	fixture, err := LoadFixture("siteinfo_wmf_1.45.json")
	require.NoError(t, err)

	query, ok := fixture["query"].(map[string]any)
	require.True(t, ok)

	general, ok := query["general"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "Wikipedia", general["sitename"])
	require.Equal(t, "MediaWiki 1.45.0-wmf.3", general["generator"])
}
