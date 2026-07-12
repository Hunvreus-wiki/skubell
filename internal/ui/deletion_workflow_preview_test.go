package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/api"
	"github.com/Hunvreus-wiki/skubell/internal/deletion"
)

func TestPreviewRowText(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		row  previewRow
		want string
	}{
		{
			name: "selected page with talk",
			row:  previewRow{item: deletion.PlanItem{Title: "Apple", HasTalkPage: true}},
			want: "Apple 💬",
		},
		{
			name: "derived redirect without talk",
			row:  previewRow{item: deletion.PlanItem{Title: "Cider", Derived: true}},
			want: " ↳ Cider",
		},
		{
			name: "derived orphan talk page",
			row:  previewRow{item: deletion.PlanItem{Title: "Talk:Pomme", Derived: true, TalkPage: true}},
			want: " ↳ Talk:Pomme",
		},
		{
			name: "category not empty",
			row: previewRow{
				item:             deletion.PlanItem{Title: "Category:Foo"},
				categoryNotEmpty: true,
				remainingMembers: 3,
			},
			want: "Category:Foo  ⚠ category not empty (3 members remaining) — deletion will fail",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, previewRowText(tc.row))
		})
	}
}

func TestDeletionDataProviderGetSubjectPageTitle(t *testing.T) {
	t.Parallel()

	provider := &deletionDataProvider{
		caps: api.WikiCapabilities{
			Namespaces: map[int]string{
				0:  "",
				1:  "Talk",
				14: "Category",
				15: "Category talk",
			},
		},
	}

	cases := []struct{ in, want string }{
		{"Talk:Apple", "Apple"},                   // Talk (1) -> main (0), no prefix
		{"Category talk:Fruit", "Category:Fruit"}, // cross-namespace: 15 -> 14
		{"Apple", ""},                             // main namespace: not a talk page
		{"Category:Fruit", ""},                    // even (subject) namespace: not a talk page
		{"", ""},
	}
	for _, tc := range cases {
		got, err := provider.GetSubjectPageTitle(tc.in)
		require.NoError(t, err)
		require.Equal(t, tc.want, got, "GetSubjectPageTitle(%q)", tc.in)
	}
}

func TestCategoryMembersOutsideSet(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		assert.Equal(t, "categorymembers", query.Get("list"))
		assert.Equal(t, "Category:Foo", query.Get("cmtitle"))
		if query.Get("cmcontinue") == "" {
			writeJSON(t, writer, map[string]any{
				"query": map[string]any{
					"categorymembers": []map[string]any{
						{"title": "Article A", "ns": 0},
						{"title": "Article B", "ns": 0},
					},
				},
				"continue": map[string]any{"cmcontinue": "page-2"},
			})
			return
		}
		writeJSON(t, writer, map[string]any{
			"query": map[string]any{
				"categorymembers": []map[string]any{
					{"title": "Category:Sub", "ns": 14},
				},
			},
		})
	}))
	defer server.Close()

	client, err := api.NewClient(1000, 0, nil)
	require.NoError(t, err)
	screen := &deleteWorkflowScreen{app: &App{client: client, apiURL: server.URL}}

	// Article A is being deleted; Article B and Category:Sub are not.
	deleted := map[string]struct{}{"Article A": {}}
	remaining, err := screen.categoryMembersOutsideSet(context.Background(), "Category:Foo", deleted)
	require.NoError(t, err)
	require.Equal(t, 2, remaining)

	// When every member is also being deleted by the workflow, nothing remains,
	// so the category will be empty by the time it is deleted (categories last).
	allDeleted := map[string]struct{}{"Article A": {}, "Article B": {}, "Category:Sub": {}}
	remaining, err = screen.categoryMembersOutsideSet(context.Background(), "Category:Foo", allDeleted)
	require.NoError(t, err)
	require.Equal(t, 0, remaining)
}
