package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Hunvreus-wiki/skubell/internal/api"
)

func TestCategoryHasMembers(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		if query.Get("list") != "categorymembers" {
			t.Fatalf("unexpected list=%q", query.Get("list"))
		}
		switch query.Get("cmtitle") {
		case "Category:NonEmpty":
			_, _ = writer.Write([]byte(`{"query":{"categorymembers":[{"title":"Page A","ns":0}]}}`))
		case "Category:Empty":
			_, _ = writer.Write([]byte(`{"query":{"categorymembers":[]}}`))
		default:
			t.Fatalf("unexpected cmtitle=%q", query.Get("cmtitle"))
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

	hasMembers, err := screen.categoryHasMembers(context.Background(), "Category:NonEmpty")
	if err != nil {
		t.Fatalf("categoryHasMembers(non-empty) error: %v", err)
	}
	if !hasMembers {
		t.Fatalf("expected Category:NonEmpty to be non-empty")
	}

	hasMembers, err = screen.categoryHasMembers(context.Background(), "Category:Empty")
	if err != nil {
		t.Fatalf("categoryHasMembers(empty) error: %v", err)
	}
	if hasMembers {
		t.Fatalf("expected Category:Empty to be empty")
	}
}

func TestCategoryHasMembersIgnoresNonCategoryTitles(t *testing.T) {
	t.Parallel()

	screen := &deleteWorkflowScreen{}
	hasMembers, err := screen.categoryHasMembers(context.Background(), "Apple")
	if err != nil {
		t.Fatalf("categoryHasMembers(non-category) error: %v", err)
	}
	if hasMembers {
		t.Fatalf("expected non-category title to return hasMembers=false")
	}
}
