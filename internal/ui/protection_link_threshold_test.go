package ui

import (
	"testing"

	"fyne.io/fyne/v2/widget"
	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/api"
)

func TestParseQueryPageCounts(t *testing.T) {
	t.Parallel()

	// formatversion=2 object form; value as string; blank title skipped.
	obj := map[string]any{
		"query": map[string]any{
			"querypage": map[string]any{
				"name": "Mostlinkedtemplates",
				"results": []any{
					map[string]any{"title": "Template:A", "value": "21"},
					map[string]any{"title": "Template:B", "value": "11"},
					map[string]any{"title": "  ", "value": "5"},
				},
			},
		},
	}
	require.Equal(t, map[string]int{"Template:A": 21, "Template:B": 11}, parseQueryPageCounts(obj))

	// legacy batched-list form; value as a JSON number.
	list := map[string]any{
		"query": map[string]any{
			"querypage": []any{
				map[string]any{"results": []any{
					map[string]any{"title": "Template:C", "value": float64(3)},
				}},
			},
		},
	}
	require.Equal(t, map[string]int{"Template:C": 3}, parseQueryPageCounts(list))

	// no querypage key -> empty.
	require.Empty(t, parseQueryPageCounts(map[string]any{"query": map[string]any{}}))
}

func TestQueryPageValue(t *testing.T) {
	t.Parallel()
	require.Equal(t, 21, queryPageValue(float64(21)))
	require.Equal(t, 11, queryPageValue("11"))
	require.Equal(t, 0, queryPageValue("not-a-number"))
	require.Equal(t, 0, queryPageValue(nil))
}

func TestPartitionByCachedCounts(t *testing.T) {
	t.Parallel()

	counts := map[string]int{"Template:High": 21, "Template:Mid": 11, "Template:Low": 1}
	titles := []string{"Template:High", "Template:Mid", "Template:Low", "Template:Absent"}

	// Threshold above the cache floor (minCached=1): the cache is authoritative, so an absent title is below it.
	kept, needLive := partitionByCachedCounts(titles, counts, 1, true, 10)
	require.Equal(t, []string{"Template:High", "Template:Mid"}, kept)
	require.Empty(t, needLive)

	// Threshold below the cache floor (minCached=21): absent titles might still qualify -> verify live.
	kept, needLive = partitionByCachedCounts(titles, map[string]int{"Template:High": 21}, 21, true, 5)
	require.Equal(t, []string{"Template:High"}, kept)
	require.Equal(t, []string{"Template:Mid", "Template:Low", "Template:Absent"}, needLive)

	// Cache unavailable -> everything goes live.
	kept, needLive = partitionByCachedCounts(titles, nil, 0, false, 10)
	require.Empty(t, kept)
	require.Equal(t, titles, needLive)
}

func TestCountAtLeast(t *testing.T) {
	t.Parallel()

	// Transclusions metric (key: transcludedin).
	full := map[string]any{"query": map[string]any{"pages": []any{
		map[string]any{"transcludedin": []any{map[string]any{}, map[string]any{}, map[string]any{}}},
	}}}
	require.True(t, countAtLeast(full, "transcludedin", 3))
	require.False(t, countAtLeast(full, "transcludedin", 4))
	// Wrong key for this payload -> counts zero.
	require.False(t, countAtLeast(full, "linkshere", 1))

	// Inbound-links metric (key: linkshere): fewer than minCount in the batch, but a continue token means more exist.
	continued := map[string]any{
		"continue": map[string]any{"lhcontinue": "x"},
		"query":    map[string]any{"pages": []any{map[string]any{"linkshere": []any{map[string]any{}}}}},
	}
	require.True(t, countAtLeast(continued, "linkshere", 5))
}

func TestSelectedMetric(t *testing.T) {
	// Not parallel: it builds Fyne widgets, whose shared render caches are not safe for concurrent access.
	s := &protectionWorkflowScreen{}
	// Nil picker (not yet built) defaults to transclusions.
	require.Equal(t, metricTransclusions, s.selectedMetric())

	s.searchMetric = widget.NewSelect([]string{"Transclusions", "Incoming links"}, nil)
	s.searchMetric.SetSelectedIndex(0)
	m := s.selectedMetric()
	require.Equal(t, "Mostlinkedtemplates", m.queryPage)
	require.Equal(t, "transcludedin", m.prop)
	require.Equal(t, "tilimit", m.limitParam)

	s.searchMetric.SetSelectedIndex(1)
	m = s.selectedMetric()
	require.Equal(t, "Mostlinked", m.queryPage)
	require.Equal(t, "linkshere", m.prop)
	require.Equal(t, "lhlimit", m.limitParam)
}

func TestNormalizeAndPrefixFold(t *testing.T) {
	t.Parallel()
	require.Equal(t, "template talk", normalizeNSName("  Template_Talk "))
	require.True(t, hasPrefixFold("Infobox country", "info"))
	require.True(t, hasPrefixFold("Sidebar_Nav", "sidebar nav"))
	require.False(t, hasPrefixFold("Navbox", "info"))
}

func TestSplitNamespace(t *testing.T) {
	t.Parallel()
	s := &protectionWorkflowScreen{app: &App{currentCaps: api.WikiCapabilities{
		Namespaces: map[int]string{0: "", 1: "Talk", 10: "Template", 828: "Module"},
	}}}

	id, main := s.splitNamespace("Template:Infobox")
	require.Equal(t, 10, id)
	require.Equal(t, "Infobox", main)

	// Unknown prefix -> main namespace, full title preserved.
	id, main = s.splitNamespace("Foo:Bar")
	require.Equal(t, 0, id)
	require.Equal(t, "Foo:Bar", main)

	// No colon -> main namespace.
	id, main = s.splitNamespace("PlainTitle")
	require.Equal(t, 0, id)
	require.Equal(t, "PlainTitle", main)

	// Namespace name matched case/underscore-insensitively; a colon inside the main text is kept.
	id, main = s.splitNamespace("template:A:B")
	require.Equal(t, 10, id)
	require.Equal(t, "A:B", main)
}
