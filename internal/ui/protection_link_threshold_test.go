package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestSelectedMetric(t *testing.T) {
	// Not parallel: it builds Fyne widgets, whose shared render caches are not safe for concurrent access.
	s := &protectionWorkflowScreen{}
	// Nil picker (not yet built) defaults to transclusions.
	require.Equal(t, queryPageTransclusions, s.selectedMetric())

	s.searchMetric = widget.NewSelect([]string{"Transclusions", "Incoming links"}, nil)
	s.searchMetric.SetSelectedIndex(0)
	require.Equal(t, "Mostlinkedtemplates", s.selectedMetric())

	s.searchMetric.SetSelectedIndex(1)
	require.Equal(t, "Mostlinked", s.selectedMetric())
}

// newSearchScreen wires a protection screen with its search widgets at their "(any)" defaults to a fake wiki server,
// for exercising the search paths without a real connection.
func newSearchScreen(t *testing.T, serverURL string) *protectionWorkflowScreen {
	t.Helper()
	client, err := api.NewClient(1000, 0, nil)
	require.NoError(t, err)
	s := &protectionWorkflowScreen{app: &App{client: client, apiURL: serverURL, currentCaps: api.WikiCapabilities{
		Namespaces: map[int]string{0: "", 10: "Template"},
	}}}
	s.searchNamespace = widget.NewSelect([]string{"(any)", "10: Template"}, nil)
	s.searchNamespace.SetSelectedIndex(0)
	s.searchPrefix = widget.NewEntry()
	s.searchLevel = widget.NewSelect([]string{"(any)", "sysop", "(no protection)"}, nil)
	s.searchLevel.SetSelectedIndex(0)
	s.searchExpiry = widget.NewSelect([]string{"(any)", "definite", "indefinite"}, nil)
	s.searchExpiry.SetSelectedIndex(0)
	s.searchCascade = widget.NewSelect([]string{"(any)", "cascading", "noncascading"}, nil)
	s.searchCascade.SetSelectedIndex(0)
	s.searchMetric = widget.NewSelect([]string{"Transclusions", "Incoming links"}, nil)
	s.searchMetric.SetSelectedIndex(0)
	return s
}

func TestApplySearchLevelRule(t *testing.T) {
	// Not parallel: it builds Fyne widgets, whose shared render caches are not safe for concurrent access.
	s := newSearchScreen(t, "")

	// A concrete level keeps the expiry/cascade filters usable.
	s.searchLevel.SetSelectedIndex(1) // sysop
	s.applySearchLevelRule()
	require.False(t, s.searchExpiry.Disabled())
	require.False(t, s.searchCascade.Disabled())

	// "(any)" level: expiry and cascade are independent protection filters (all temporary / all cascading pages), each
	// implying apprtype on its own, so they stay usable rather than being disabled.
	s.searchLevel.SetSelectedIndex(0)
	s.applySearchLevelRule()
	require.False(t, s.searchExpiry.Disabled())
	require.False(t, s.searchCascade.Disabled())

	// "(no protection)" is contradictory with an expiry/cascade filter — an unprotected page has neither — so they are
	// disabled and reset, leaking no stale refinement into the search parameters.
	s.searchExpiry.SetSelectedIndex(1)
	s.searchCascade.SetSelectedIndex(2)
	s.searchLevel.SetSelectedIndex(2) // "(no protection)"
	s.applySearchLevelRule()
	require.True(t, s.searchExpiry.Disabled())
	require.True(t, s.searchCascade.Disabled())
	require.Equal(t, 0, s.searchExpiry.SelectedIndex())
	require.Equal(t, 0, s.searchCascade.SelectedIndex())
}

func TestSearchCriterionAndSweep(t *testing.T) {
	// Not parallel: it builds Fyne widgets, whose shared render caches are not safe for concurrent access.
	s := newSearchScreen(t, "")

	// Nothing set: no criterion, and a bare search would sweep every namespace.
	require.False(t, s.hasSearchCriterion(0))
	require.True(t, s.sweepsAllNamespaces(0))

	// A count threshold alone is a criterion served from the cache: no allpages sweep to confirm.
	require.True(t, s.hasSearchCriterion(3))
	require.False(t, s.sweepsAllNamespaces(3))

	// A protection filter makes a threshold search visit allpages again.
	s.searchLevel.SetSelectedIndex(1) // sysop
	require.True(t, s.sweepsAllNamespaces(3))

	// Choosing a namespace ends the sweep and is a criterion by itself.
	s.searchNamespace.SetSelectedIndex(1)
	require.False(t, s.sweepsAllNamespaces(0))
	require.True(t, s.hasSearchCriterion(0))

	// A prefix alone is a criterion: apprefix filters server-side, unlike deletion's client-side prefix.
	prefixed := newSearchScreen(t, "")
	prefixed.searchPrefix.SetText("Infobox")
	require.True(t, prefixed.hasSearchCriterion(0))
	require.True(t, prefixed.sweepsAllNamespaces(0))
}

func TestAllPagesInNamespaceFollowsContinuation(t *testing.T) {
	// Not parallel: it builds Fyne widgets, whose shared render caches are not safe for concurrent access.
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("list") != "allpages" {
			t.Fatalf("unexpected request: %s", request.URL.String())
		}
		requests++
		if request.URL.Query().Get("apcontinue") == "" {
			writeJSON(t, writer, map[string]any{
				"continue": map[string]any{"apcontinue": "Page_C"},
				"query": map[string]any{"allpages": []map[string]any{
					{"title": "Page A"}, {"title": "Page B"},
				}},
			})
			return
		}
		writeJSON(t, writer, map[string]any{
			"query": map[string]any{"allpages": []map[string]any{{"title": "Page C"}}},
		})
	}))
	defer server.Close()

	s := newSearchScreen(t, server.URL)
	titles, err := s.allPagesInNamespace(context.Background(), protectSearchCriteria{}, 0)
	require.NoError(t, err)
	require.Equal(t, []string{"Page A", "Page B", "Page C"}, titles)
	require.Equal(t, 2, requests)
}

func TestAllPagesInNamespaceRefusesTooBroad(t *testing.T) {
	// Not parallel: it builds Fyne widgets, whose shared render caches are not safe for concurrent access.
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		// A namespace that never exhausts: every batch hands back another continuation token.
		writeJSON(t, writer, map[string]any{
			"continue": map[string]any{"apcontinue": "More"},
			"query":    map[string]any{"allpages": []map[string]any{{"title": "Page"}}},
		})
	}))
	defer server.Close()

	s := newSearchScreen(t, server.URL)
	_, err := s.allPagesInNamespace(context.Background(), protectSearchCriteria{}, 0)
	require.ErrorIs(t, err, errSearchTooBroad)
	require.Equal(t, maxAllPagesBatches, requests)
}

func TestSearchByMetricRejectsInsteadOfLiveCounting(t *testing.T) {
	// Not parallel: it builds Fyne widgets, whose shared render caches are not safe for concurrent access.
	cached := []map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// Only the querypage may be queried: rejection must happen before any allpages or per-page counting.
		if request.URL.Query().Get("list") != "querypage" {
			t.Fatalf("cache-rejected search issued another request: %s", request.URL.String())
		}
		writeJSON(t, writer, map[string]any{
			"query": map[string]any{"querypage": map[string]any{"results": cached}},
		})
	}))
	defer server.Close()
	s := newSearchScreen(t, server.URL)

	// Empty cache (miser-mode wiki without the cron): rejected outright.
	_, err := s.searchByMetric(context.Background(), protectSearchCriteria{minCount: 3, metric: queryPageTransclusions})
	require.ErrorIs(t, err, errNoCountCache)

	// Threshold at or below the cache floor: rejected. The cache may be truncated amid entries tied at the floor, so a
	// page with exactly that count can be missing; the error reports the lowest answerable threshold (floor + 1).
	cached = []map[string]any{
		{"title": "Template:A", "value": "21"},
		{"title": "Template:B", "value": "11"},
	}
	_, err = s.searchByMetric(context.Background(), protectSearchCriteria{minCount: 11, metric: queryPageTransclusions})
	var floorErr *countBelowCacheFloorError
	require.ErrorAs(t, err, &floorErr)
	require.Equal(t, 12, floorErr.Floor)

	// Threshold strictly above the floor: answerable from the cache alone, no other request allowed.
	titles, err := s.searchByMetric(
		context.Background(), protectSearchCriteria{minCount: 12, metric: queryPageTransclusions},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"Template:A"}, titles)
}

// A querypage continuation offset that MediaWiki serializes as a JSON number (formatversion=2) must be followed, not
// dropped: a string-only assertion would stop after the first batch and truncate large count caches.
func TestFetchQueryPageCountsFollowsNumericContinuation(t *testing.T) {
	// Not parallel: it builds Fyne widgets, whose shared render caches are not safe for concurrent access.
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("list") != "querypage" {
			t.Fatalf("unexpected request: %s", request.URL.String())
		}
		requests++
		if request.URL.Query().Get("qpoffset") == "" {
			writeJSON(t, writer, map[string]any{
				"continue": map[string]any{"qpoffset": 50}, // a JSON number, not a string
				"query": map[string]any{"querypage": map[string]any{"results": []any{
					map[string]any{"title": "Template:A", "value": "21"},
				}}},
			})
			return
		}
		if got := request.URL.Query().Get("qpoffset"); got != "50" {
			t.Errorf("expected qpoffset=50, got %q", got)
		}
		writeJSON(t, writer, map[string]any{
			"query": map[string]any{"querypage": map[string]any{"results": []any{
				map[string]any{"title": "Template:B", "value": "11"},
			}}},
		})
	}))
	defer server.Close()

	s := newSearchScreen(t, server.URL)
	counts, minCached, err := s.fetchQueryPageCounts(context.Background(), queryPageTransclusions)
	require.NoError(t, err)
	require.Equal(t, 2, requests, "numeric qpoffset must be followed, not dropped after the first batch")
	require.Equal(t, map[string]int{"Template:A": 21, "Template:B": 11}, counts)
	require.Equal(t, 11, minCached)
}

// "(no protection)" enumerates every page then confirms each is unprotected with prop=info. allpages' apprtype filter
// joins only direct page_restrictions, so a cascade-protected page (a sourced entry, no direct row) would slip through;
// prop=info reports the sourced entry, so both direct and cascade-protected pages are excluded.
func TestNoProtectionFilterExcludesDirectAndCascade(t *testing.T) {
	// Not parallel: it builds Fyne widgets, whose shared render caches are not safe for concurrent access.
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		q := request.URL.Query()
		switch {
		case q.Get("list") == "allpages":
			writeJSON(t, writer, map[string]any{"query": map[string]any{"allpages": []map[string]any{
				{"title": "Template:Free"}, {"title": "Template:Direct"}, {"title": "Template:Cascaded"},
			}}})
		case q.Get("prop") == "info":
			writeJSON(t, writer, map[string]any{"query": map[string]any{"pages": []any{
				map[string]any{"title": "Template:Free"}, // no protection key → unprotected
				map[string]any{"title": "Template:Direct", "protection": []any{
					map[string]any{"type": "edit", "level": "sysop", "expiry": "infinity"},
				}},
				map[string]any{"title": "Template:Cascaded", "protection": []any{
					map[string]any{"type": "edit", "level": "sysop", "source": "Template:Hub"}, // inherited via cascade
				}},
			}}})
		default:
			t.Errorf("unexpected request: %s", request.URL.String())
		}
	}))
	defer server.Close()

	s := newSearchScreen(t, server.URL)
	titles, err := s.allPagesInNamespace(context.Background(), protectSearchCriteria{noProtection: true}, 10)
	require.NoError(t, err)
	require.Equal(t, []string{"Template:Free"}, titles, "direct and cascade-protected pages are both excluded")
}

// A wiki whose configured types include move/create but omit edit must not crash: the move/create tabs must not offer
// "Same as Edit" (which would make captureOptions mirror to an absent edit tab and dereference nil widgets).
func TestOptionsWithoutEditTabDoesNotCrash(t *testing.T) {
	// Not parallel: it builds Fyne widgets, whose shared render caches are not safe for concurrent access.
	s := &protectionWorkflowScreen{
		app: &App{currentCaps: api.WikiCapabilities{
			RestrictionTypes:  []string{"move", "create"}, // no edit
			RestrictionLevels: []string{"", "autoconfirmed", "sysop"},
		}},
		levelSelects: map[string]*widget.Select{},
		expiryInputs: map[string]*expiryInput{},
		sameAsEdit:   map[string]*widget.Check{},
	}

	require.NotNil(t, s.buildOptionsContent())
	require.NotContains(t, s.levelSelects, "edit", "no edit tab is built")
	require.NotContains(t, s.sameAsEdit, "move", "no Same-as-Edit mirror without an edit tab")
	require.NotContains(t, s.sameAsEdit, "create")
	require.Empty(t, s.captureOptions(), "capturing options must not panic or error without an edit tab")
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

func TestNamespaceIDs(t *testing.T) {
	t.Parallel()
	s := &protectionWorkflowScreen{app: &App{currentCaps: api.WikiCapabilities{
		Namespaces: map[int]string{-2: "Media", -1: "Special", 0: "", 1: "Talk", 10: "Template", 828: "Module"},
	}}}
	// Excludes the negative Special/Media pseudo-namespaces (not listable), sorted ascending — an "(any)" search
	// iterates these, so it must include Template (10), the bug's missing namespace.
	require.Equal(t, []int{0, 1, 10, 828}, s.namespaceIDs())
}
