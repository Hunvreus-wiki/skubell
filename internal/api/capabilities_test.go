package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSiteInfoFromFixture(t *testing.T) {
	t.Parallel()

	fixture, err := LoadFixture("siteinfo_mw143.json")
	require.NoError(t, err)

	caps, err := parseSiteInfoResponse(fixture)
	require.NoError(t, err)
	require.Equal(t, "1.43.9", caps.Version)
	require.Equal(t, [2]int{1, 43}, caps.VersionMajMin)
	require.Equal(t, "Talk", caps.Namespaces[1])
	require.Contains(t, caps.Extensions, "Abuse Filter")
}

func TestParseSiteInfoParsesRestrictions(t *testing.T) {
	t.Parallel()

	withRestrictions, err := parseSiteInfoResponse(map[string]any{
		"query": map[string]any{
			"general": map[string]any{"generator": "MediaWiki 1.46.0"},
			"restrictions": map[string]any{
				"types":               []any{"create", "edit", "move", "upload"},
				"levels":              []any{"", "autoconfirmed", "extendedconfirmed", "sysop"},
				"cascadinglevels":     []any{"sysop"},
				"semiprotectedlevels": []any{"autoconfirmed"},
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"create", "edit", "move", "upload"}, withRestrictions.RestrictionTypes)
	// The "" (no-restriction) level is preserved, and a wiki's custom/extended level comes through.
	require.Equal(t, []string{"", "autoconfirmed", "extendedconfirmed", "sysop"}, withRestrictions.RestrictionLevels)
	require.Equal(t, []string{"sysop"}, withRestrictions.CascadingLevels)
	require.Equal(t, []string{"autoconfirmed"}, withRestrictions.SemiProtectedLevels)

	// Restrictions are optional: absence yields nil, not an error.
	without, err := parseSiteInfoResponse(map[string]any{
		"query": map[string]any{"general": map[string]any{"generator": "MediaWiki 1.43.9"}},
	})
	require.NoError(t, err)
	require.Nil(t, without.RestrictionTypes)
	require.Nil(t, without.RestrictionLevels)
}

func TestParseSiteInfoParsesStatistics(t *testing.T) {
	t.Parallel()

	withStats, err := parseSiteInfoResponse(map[string]any{
		"query": map[string]any{
			"general":    map[string]any{"generator": "MediaWiki 1.43.9"},
			"statistics": map[string]any{"pages": float64(1234), "activeusers": float64(56)},
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1234, withStats.PageCount)
	require.Equal(t, 56, withStats.ActiveUsers)

	// Statistics are optional: absence yields zero values, not an error.
	withoutStats, err := parseSiteInfoResponse(map[string]any{
		"query": map[string]any{"general": map[string]any{"generator": "MediaWiki 1.43.9"}},
	})
	require.NoError(t, err)
	require.Zero(t, withoutStats.PageCount)
	require.Zero(t, withoutStats.ActiveUsers)
}

func TestParseSiteInfoIncludesNamespaceAliases(t *testing.T) {
	t.Parallel()

	fixture := map[string]any{
		"query": map[string]any{
			"general": map[string]any{
				"generator": "MediaWiki 1.45.1",
			},
			"namespaces": map[string]any{
				"10": map[string]any{
					"id":        10,
					"*":         "Patrom",
					"canonical": "Template",
				},
			},
			"namespacealiases": []any{
				map[string]any{
					"id": 10,
					"*":  "Skeudenn",
				},
			},
		},
	}

	caps, err := parseSiteInfoResponse(fixture)
	require.NoError(t, err)
	require.Equal(t, "Patrom", caps.Namespaces[10])
	require.ElementsMatch(t, []string{"Template", "Skeudenn"}, caps.NamespaceAliases[10])
}

func TestParseSiteInfoRejectsUnsupportedVersion(t *testing.T) {
	t.Parallel()

	// Both 1.38 and 1.39 are below the current 1.43 floor and must be rejected.
	cases := []struct {
		fixture string
		version string
	}{
		{"siteinfo_mw138.json", "1.38.4"},
		{"siteinfo_mw139.json", "1.39.17"},
	}
	for _, tc := range cases {
		t.Run(tc.version, func(t *testing.T) {
			fixture, err := LoadFixture(tc.fixture)
			require.NoError(t, err)

			_, err = parseSiteInfoResponse(fixture)
			require.Error(t, err)

			var versionErr *ErrUnsupportedMediaWikiVersion
			require.ErrorAs(t, err, &versionErr)
			require.Equal(t, tc.version, versionErr.Version)
		})
	}
}

func TestParseUserInfoHighLimitsDetection(t *testing.T) {
	t.Parallel()

	withFixture, err := LoadFixture("userinfo_with_highlimits.json")
	require.NoError(t, err)
	withCaps, err := parseUserInfoResponse(withFixture)
	require.NoError(t, err)
	require.True(t, withCaps.HasHighLimits)
	require.Contains(t, withCaps.UserRights, "delete")
	require.Contains(t, withCaps.UserGroups, "sysop")

	withoutFixture, err := LoadFixture("userinfo_without_highlimits.json")
	require.NoError(t, err)
	withoutCaps, err := parseUserInfoResponse(withoutFixture)
	require.NoError(t, err)
	require.False(t, withoutCaps.HasHighLimits)
}

func TestParseUserInfoBlockClassification(t *testing.T) {
	t.Parallel()

	userinfoResponse := func(userinfo map[string]any) map[string]any {
		return map[string]any{"query": map[string]any{"userinfo": userinfo}}
	}

	cases := []struct {
		name         string
		userinfo     map[string]any
		wantSitewide bool
	}{
		{"not blocked", map[string]any{"name": "U"}, false},
		{"sitewide block (no field)", map[string]any{"name": "U", "blockid": float64(1)}, true},
		// formatversion=2: blockpartial is a real boolean, present for every block.
		{
			"fv2 sitewide (blockpartial false)",
			map[string]any{"name": "U", "blockid": float64(2), "blockpartial": false},
			true,
		},
		{
			"fv2 partial (blockpartial true)",
			map[string]any{"name": "U", "blockid": float64(3), "blockpartial": true},
			false,
		},
		// formatversion=1: blockpartial is present (empty string) only for partial blocks.
		{
			"fv1 partial (blockpartial empty string)",
			map[string]any{"name": "U", "blockid": float64(4), "blockpartial": ""},
			false,
		},
		// An explicit blocksitewide boolean is authoritative when present.
		{
			"explicit blocksitewide false",
			map[string]any{"name": "U", "blockid": float64(5), "blocksitewide": false},
			false,
		},
		{
			"explicit blocksitewide true",
			map[string]any{"name": "U", "blockid": float64(6), "blocksitewide": true},
			true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			caps, err := parseUserInfoResponse(userinfoResponse(tc.userinfo))
			require.NoError(t, err)
			require.Equal(t, tc.wantSitewide, caps.SitewideBlock)
		})
	}
}
