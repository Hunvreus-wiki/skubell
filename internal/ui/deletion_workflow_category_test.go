package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/Hunvreus-wiki/skubell/internal/api"
	"github.com/Hunvreus-wiki/skubell/internal/ops"
)

func TestSearchPagesCategoryRecursiveToggle(t *testing.T) {
	testApp := fynetest.NewApp()
	defer testApp.Quit()

	allPagesRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		list := query.Get("list")
		switch list {
		case "allpages":
			allPagesRequests++
			t.Fatalf("category-only search should not query allpages: %s", request.URL.String())
		case "categorymembers":
			switch query.Get("cmtitle") {
			case "Category:Root":
				writeJSON(t, writer, map[string]any{
					"query": map[string]any{
						"categorymembers": []map[string]any{
							{"title": "RootPage", "ns": 0},
							{"title": "Category:Sub", "ns": 14},
						},
					},
				})
				return
			case "Category:Sub":
				writeJSON(t, writer, map[string]any{
					"query": map[string]any{
						"categorymembers": []map[string]any{
							{"title": "SubPage", "ns": 0},
						},
					},
				})
				return
			}
		}
		t.Fatalf("unexpected request: %s", request.URL.String())
	}))
	defer server.Close()

	client, err := api.NewClient(1000, 0, nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	screen := &deleteWorkflowScreen{
		app: &App{
			client: client,
			apiURL: server.URL,
		},
		searchPrefixEntry:      widget.NewEntry(),
		searchCategoryEntry:    widget.NewEntry(),
		searchCategoryRecurChk: widget.NewCheck("Recursive search", nil),
		searchCategoryInclChk:  widget.NewCheck("Include categories", nil),
		searchCreatorEntry:     widget.NewEntry(),
		searchLinkedFromEntry:  widget.NewEntry(),
		searchTemplateEntry:    widget.NewEntry(),
		searchMinSizeEntry:     widget.NewEntry(),
		searchMaxSizeEntry:     widget.NewEntry(),
		searchRedirectsCheck:   widget.NewCheck("Redirects only", nil),
		searchBrokenRedirCheck: widget.NewCheck("Broken redirects", nil),
	}
	screen.searchCategoryEntry.SetText("Root")

	screen.searchCategoryRecurChk.SetChecked(false)
	results, _, err := screen.searchPages(context.Background(), screen.collectSearchCriteria())
	if err != nil {
		t.Fatalf("search without recursion: %v", err)
	}
	if got := resultTitles(results); !slices.Equal(got, []string{"RootPage"}) {
		t.Fatalf("search without recursion returned %v", got)
	}

	screen.searchCategoryRecurChk.SetChecked(true)
	results, _, err = screen.searchPages(context.Background(), screen.collectSearchCriteria())
	if err != nil {
		t.Fatalf("search with recursion: %v", err)
	}
	if got := resultTitles(results); !slices.Equal(got, []string{"RootPage", "SubPage"}) {
		t.Fatalf("search with recursion returned %v", got)
	}

	screen.searchCategoryInclChk.SetChecked(true)
	results, _, err = screen.searchPages(context.Background(), screen.collectSearchCriteria())
	if err != nil {
		t.Fatalf("search with recursion + include categories: %v", err)
	}
	if got := resultTitles(
		results,
	); !slices.Equal(
		got,
		[]string{"Category:Root", "Category:Sub", "RootPage", "SubPage"},
	) {
		t.Fatalf("search with recursion + include categories returned %v", got)
	}
	if allPagesRequests != 0 {
		t.Fatalf("expected no allpages requests, got %d", allPagesRequests)
	}
}

func TestFetchCategoryMembersRecursiveAvoidsCycles(t *testing.T) {
	testApp := fynetest.NewApp()
	defer testApp.Quit()

	requestCount := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		if query.Get("list") != "categorymembers" {
			t.Fatalf("unexpected list=%q", query.Get("list"))
		}
		category := query.Get("cmtitle")
		requestCount[category]++

		switch category {
		case "Category:Root":
			writeJSON(t, writer, map[string]any{
				"query": map[string]any{
					"categorymembers": []map[string]any{
						{"title": "Category:Child", "ns": 14},
					},
				},
			})
			return
		case "Category:Child":
			writeJSON(t, writer, map[string]any{
				"query": map[string]any{
					"categorymembers": []map[string]any{
						{"title": "ChildPage", "ns": 0},
						{"title": "Category:Root", "ns": 14},
					},
				},
			})
			return
		default:
			t.Fatalf("unexpected category: %s", category)
		}
	}))
	defer server.Close()

	client, err := api.NewClient(1000, 0, nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	screen := &deleteWorkflowScreen{
		app: &App{
			client: client,
			apiURL: server.URL,
		},
	}

	members, categories, categoryParents, err := screen.fetchCategoryMembers(
		context.Background(),
		"Root",
		true,
		0,
		false,
	)
	if err != nil {
		t.Fatalf("fetchCategoryMembers: %v", err)
	}

	if _, ok := members["ChildPage"]; !ok {
		t.Fatalf("expected ChildPage in recursive members, got %v", members)
	}
	if _, ok := categories["Category:Root"]; !ok {
		t.Fatalf("expected Category:Root in categories, got %v", categories)
	}
	if _, ok := categories["Category:Child"]; !ok {
		t.Fatalf("expected Category:Child in categories, got %v", categories)
	}
	if _, ok := categoryParents["Category:Child"]["Category:Root"]; !ok {
		t.Fatalf("expected Category:Child -> Category:Root relationship, got %v", categoryParents)
	}
	if requestCount["Category:Root"] != 1 {
		t.Fatalf("expected Category:Root queried once, got %d", requestCount["Category:Root"])
	}
	if requestCount["Category:Child"] != 1 {
		t.Fatalf("expected Category:Child queried once, got %d", requestCount["Category:Child"])
	}
}

func TestFetchCategoryMembersAcceptsLocalizedNamespace(t *testing.T) {
	testApp := fynetest.NewApp()
	defer testApp.Quit()

	var gotTitle string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotTitle = request.URL.Query().Get("cmtitle")
		writeJSON(t, writer, map[string]any{
			"query": map[string]any{
				"categorymembers": []map[string]any{
					{"title": "Page A", "ns": 0},
				},
			},
		})
	}))
	defer server.Close()

	client, err := api.NewClient(1000, 0, nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	screen := &deleteWorkflowScreen{
		app: &App{
			client: client,
			apiURL: server.URL,
			currentCaps: api.WikiCapabilities{
				Namespaces: map[int]string{14: "Rummad"},
			},
		},
	}

	members, _, _, err := screen.fetchCategoryMembers(context.Background(), "Rummad:Root", false, 0, false)
	if err != nil {
		t.Fatalf("fetchCategoryMembers: %v", err)
	}
	if gotTitle != "Rummad:Root" {
		t.Fatalf("expected localized category title preserved, got %q", gotTitle)
	}
	if _, ok := members["Page A"]; !ok {
		t.Fatalf("expected Page A in members, got %v", members)
	}
}

func TestFinalTitlesLocalizedCategoryNamespaceSortedLast(t *testing.T) {
	t.Parallel()

	screen := &deleteWorkflowScreen{
		app: &App{
			currentCaps: api.WikiCapabilities{
				Namespaces: map[int]string{14: "Rummad"},
			},
		},
		selectedTitles: map[string]struct{}{
			"Rummad:Zeta":  {},
			"Apple":        {},
			"Rummad:Alpha": {},
			"Banana":       {},
		},
	}

	got := screen.finalTitles()
	want := []string{"Apple", "Banana", "Rummad:Alpha", "Rummad:Zeta"}
	if !slices.Equal(got, want) {
		t.Fatalf("finalTitles() = %v, want %v", got, want)
	}
}

func TestFetchEmbeddedInAcceptsLocalizedTemplateNamespaceAlias(t *testing.T) {
	testApp := fynetest.NewApp()
	defer testApp.Quit()

	gotTitle := ""
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotTitle = request.URL.Query().Get("eititle")
		writeJSON(t, writer, map[string]any{
			"query": map[string]any{
				"embeddedin": []map[string]any{
					{"title": "Page A"},
				},
			},
		})
	}))
	defer server.Close()

	client, err := api.NewClient(1000, 0, nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	screen := &deleteWorkflowScreen{
		app: &App{
			client: client,
			apiURL: server.URL,
			currentCaps: api.WikiCapabilities{
				Namespaces:       map[int]string{10: "Patrom"},
				NamespaceAliases: map[int][]string{10: {"Template"}},
			},
		},
	}

	results, err := screen.fetchEmbeddedIn(context.Background(), "Template:Infobox", 0, false)
	if err != nil {
		t.Fatalf("fetchEmbeddedIn: %v", err)
	}
	if gotTitle != "Template:Infobox" {
		t.Fatalf("expected template alias preserved, got %q", gotTitle)
	}
	if _, ok := results["Page A"]; !ok {
		t.Fatalf("expected Page A in embeddedin results, got %v", results)
	}
}

func TestGetTalkPageTitleUsesLocalizedNamespaceAndAliases(t *testing.T) {
	t.Parallel()

	provider := &deletionDataProvider{
		caps: api.WikiCapabilities{
			Namespaces: map[int]string{
				1:  "Kaozeadenn",
				10: "Patrom",
				11: "Kaozeadenn Batrom",
			},
			NamespaceAliases: map[int][]string{
				1:  {"Talk"},
				10: {"Template"},
				11: {"Template talk"},
			},
		},
	}

	talk, err := provider.GetTalkPageTitle("Template:Infobox")
	if err != nil {
		t.Fatalf("GetTalkPageTitle(template alias): %v", err)
	}
	if talk != "Kaozeadenn Batrom:Infobox" {
		t.Fatalf("expected localized template talk title, got %q", talk)
	}

	talk, err = provider.GetTalkPageTitle("Page")
	if err != nil {
		t.Fatalf("GetTalkPageTitle(main): %v", err)
	}
	if talk != "Kaozeadenn:Page" {
		t.Fatalf("expected localized talk title, got %q", talk)
	}

	talk, err = provider.GetTalkPageTitle("Talk:Page")
	if err != nil {
		t.Fatalf("GetTalkPageTitle(talk alias): %v", err)
	}
	if talk != "" {
		t.Fatalf("expected no talk page for talk-namespace input, got %q", talk)
	}
}

func TestSearchPagesRejectsBroadSearchesThatNeedAllPages(t *testing.T) {
	t.Parallel()

	screen := &deleteWorkflowScreen{
		app:                    &App{},
		searchPrefixEntry:      widget.NewEntry(),
		searchCategoryEntry:    widget.NewEntry(),
		searchCategoryRecurChk: widget.NewCheck("Recursive search", nil),
		searchCategoryInclChk:  widget.NewCheck("Include categories", nil),
		searchCreatorEntry:     widget.NewEntry(),
		searchLinkedFromEntry:  widget.NewEntry(),
		searchTemplateEntry:    widget.NewEntry(),
		searchMinSizeEntry:     widget.NewEntry(),
		searchMaxSizeEntry:     widget.NewEntry(),
		searchRedirectsCheck:   widget.NewCheck("Redirects only", nil),
		searchBrokenRedirCheck: widget.NewCheck("Broken redirects", nil),
	}

	screen.searchPrefixEntry.SetText("A")

	_, _, err := screen.searchPages(context.Background(), screen.collectSearchCriteria())
	if err == nil {
		t.Fatalf("expected broad search to be rejected")
	}
	if !strings.Contains(err.Error(), "too broad") {
		t.Fatalf("expected too broad error, got %v", err)
	}
}

func TestFinalTitlesCategoryNamespaceSortedLast(t *testing.T) {
	t.Parallel()

	screen := &deleteWorkflowScreen{
		selectedTitles: map[string]struct{}{
			"Category:Zeta":  {},
			"Apple":          {},
			"Category:Alpha": {},
			"Banana":         {},
		},
	}

	got := screen.finalTitles()
	want := []string{"Apple", "Banana", "Category:Alpha", "Category:Zeta"}
	if !slices.Equal(got, want) {
		t.Fatalf("finalTitles() = %v, want %v", got, want)
	}
}

func TestFinalTitlesCategoryNamespaceTopologicalOrder(t *testing.T) {
	t.Parallel()

	screen := &deleteWorkflowScreen{
		selectedTitles: map[string]struct{}{
			"Apple":               {},
			"Category:Parent":     {},
			"Category:ChildA":     {},
			"Category:ChildB":     {},
			"Category:Grandchild": {},
		},
		categoryParents: map[string]map[string]struct{}{
			"Category:ChildA": {
				"Category:Parent": {},
			},
			"Category:ChildB": {
				"Category:Parent": {},
			},
			"Category:Grandchild": {
				"Category:ChildB": {},
			},
		},
	}

	got := screen.finalTitles()
	want := []string{
		"Apple",
		"Category:ChildA",
		"Category:Grandchild",
		"Category:ChildB",
		"Category:Parent",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("finalTitles() = %v, want %v", got, want)
	}
}

func TestFinalTitlesSiblingSubcategoriesAlphabetical(t *testing.T) {
	t.Parallel()

	screen := &deleteWorkflowScreen{
		selectedTitles: map[string]struct{}{
			"Category:Parent": {},
			"Category:Zulu":   {},
			"Category:Alpha":  {},
			"Category:Mike":   {},
		},
		categoryParents: map[string]map[string]struct{}{
			"Category:Zulu": {
				"Category:Parent": {},
			},
			"Category:Alpha": {
				"Category:Parent": {},
			},
			"Category:Mike": {
				"Category:Parent": {},
			},
		},
	}

	got := screen.finalTitles()
	want := []string{
		"Category:Alpha",
		"Category:Mike",
		"Category:Zulu",
		"Category:Parent",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("finalTitles() = %v, want %v", got, want)
	}
}

func TestOrderDeletionOperationsPlacesCategoryRedirectsRightBeforeTargets(t *testing.T) {
	t.Parallel()

	screen := &deleteWorkflowScreen{
		selectedTitles: map[string]struct{}{
			"Apple":                    {},
			"Category:Parent":          {},
			"Category:Child":           {},
			"Banana Redirect":          {},
			"Category:Child Redirect":  {},
			"Category:Parent Redirect": {},
		},
		categoryParents: map[string]map[string]struct{}{
			"Category:Child": {
				"Category:Parent": {},
			},
		},
	}

	opsIn := []ops.Operation{
		{Params: map[string]string{"title": "Apple"}},
		{Params: map[string]string{"title": "Category:Child"}},
		{Params: map[string]string{"title": "Banana Redirect"}},
		{Params: map[string]string{"title": "Category:Parent"}},
		{Params: map[string]string{"title": "Category:AAA Child Redirect", "redirect_target": "Category:Child"}},
		{Params: map[string]string{"title": "Category:Child Redirect", "redirect_target": "Category:Child"}},
		{Params: map[string]string{"title": "Category:ZZZ Parent Redirect", "redirect_target": "Category:Parent"}},
		{Params: map[string]string{"title": "Category:Parent Redirect", "redirect_target": "Category:Parent"}},
	}

	ordered := screen.orderDeletionOperations(opsIn, screen.categoryParents)
	got := make([]string, 0, len(ordered))
	for _, op := range ordered {
		got = append(got, op.Params["title"])
	}

	want := []string{
		"Apple",
		"Banana Redirect",
		"Category:AAA Child Redirect",
		"Category:Child Redirect",
		"Category:Child",
		"Category:Parent Redirect",
		"Category:ZZZ Parent Redirect",
		"Category:Parent",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("orderDeletionOperations() = %v, want %v", got, want)
	}
}

func resultTitles(results []searchResult) []string {
	titles := make([]string, 0, len(results))
	for _, result := range results {
		titles = append(titles, result.Title)
	}
	return titles
}

func writeJSON(t *testing.T, writer http.ResponseWriter, payload map[string]any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(payload); err != nil {
		t.Fatalf("encode json: %v", err)
	}
}
