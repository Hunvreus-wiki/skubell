package ui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/api"
)

// TestGetRedirectsAsksWhatRedirectsHere pins the question being asked, because the wrong one is easy to reach for and
// its answers look right. list=backlinks with blfilterredir=redirects returns "pages that link here and are redirects",
// which includes redirects pointing somewhere else entirely: deleting "Cat" on Wikipedia took "Kucing" with it, a
// redirect to Kuching, a city in Malaysia. prop=redirects returns what redirects here, and nothing else.
func TestGetRedirectsAsksWhatRedirectsHere(t *testing.T) {
	t.Parallel()

	var seen url.Values
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.NoError(t, request.ParseForm())
		seen = request.Form
		// Answer as prop=redirects does: the redirects hang off the queried page, not off a flat list.
		writeJSON(t, writer, map[string]any{
			"query": map[string]any{
				"pages": []map[string]any{{
					"title": "Cat",
					"redirects": []map[string]any{
						{"title": "Cats"},
						{"title": "Domestic cat"},
					},
				}},
			},
		})
	}))
	defer server.Close()

	client, err := api.NewClient(1000, 0, nil)
	require.NoError(t, err)
	provider := &deletionDataProvider{client: client, apiURL: server.URL}

	redirects, err := provider.GetRedirects("Cat")
	require.NoError(t, err)
	require.Equal(t, []string{"Cats", "Domestic cat"}, redirects)

	require.Equal(t, "redirects", seen.Get("prop"))
	require.Equal(t, "Cat", seen.Get("titles"))
	require.Empty(t, seen.Get("list"), "backlinks answers a different question and must not come back")
	require.Empty(t, seen.Get("blfilterredir"))
}

// TestGetRedirectsFollowsContinuation covers a page with more redirects than one response carries.
func TestGetRedirectsFollowsContinuation(t *testing.T) {
	t.Parallel()

	call := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.NoError(t, request.ParseForm())
		call++
		if call == 1 {
			assert.Empty(t, request.Form.Get("rdcontinue"))
			writeJSON(t, writer, map[string]any{
				"query": map[string]any{"pages": []map[string]any{{
					"title": "Cat", "redirects": []map[string]any{{"title": "Cats"}},
				}}},
				"continue": map[string]any{"rdcontinue": "next"},
			})
			return
		}
		assert.Equal(t, "next", request.Form.Get("rdcontinue"))
		writeJSON(t, writer, map[string]any{
			"query": map[string]any{"pages": []map[string]any{{
				"title": "Cat", "redirects": []map[string]any{{"title": "Felis catus"}},
			}}},
		})
	}))
	defer server.Close()

	client, err := api.NewClient(1000, 0, nil)
	require.NoError(t, err)
	provider := &deletionDataProvider{client: client, apiURL: server.URL}

	redirects, err := provider.GetRedirects("Cat")
	require.NoError(t, err)
	require.Equal(t, []string{"Cats", "Felis catus"}, redirects)
	require.Equal(t, 2, call)
}

// TestGetRedirectsOnPageWithNone covers the ordinary case: a page nothing redirects to, and one that does not exist.
func TestGetRedirectsOnPageWithNone(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(t, writer, map[string]any{
			"query": map[string]any{"pages": []map[string]any{{"title": "Nowhere", "missing": true}}},
		})
	}))
	defer server.Close()

	client, err := api.NewClient(1000, 0, nil)
	require.NoError(t, err)
	provider := &deletionDataProvider{client: client, apiURL: server.URL}

	redirects, err := provider.GetRedirects("Nowhere")
	require.NoError(t, err)
	require.Empty(t, redirects)
}
