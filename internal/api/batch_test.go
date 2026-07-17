package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractAPIErrorMultiValueDetails(t *testing.T) {
	t.Parallel()

	// errorformat=plaintext shape: machine details under "data".
	localized := map[string]any{
		"errors": []any{map[string]any{
			"code": "toomanyvalues",
			"data": map[string]any{"parameter": "ids", "limit": float64(50), "highlimit": float64(500)},
			"*":    `Too many values supplied for parameter "ids". The limit is 50.`,
		}},
	}
	apiErr := extractAPIError(localized)
	require.NotNil(t, apiErr)
	require.Equal(t, "toomanyvalues", apiErr.Code)
	require.Equal(t, "ids", apiErr.Parameter)
	require.Equal(t, 50, apiErr.Limit)

	// Legacy shape: details directly in the "error" object.
	legacy := map[string]any{
		"error": map[string]any{
			"code": "toomanyvalues", "info": "Too many values...", "parameter": "titles", "limit": float64(500),
		},
	}
	apiErr = extractAPIError(legacy)
	require.NotNil(t, apiErr)
	require.Equal(t, "titles", apiErr.Parameter)
	require.Equal(t, 500, apiErr.Limit)
}

func TestMultiValueCapSemantics(t *testing.T) {
	t.Parallel()

	client, err := NewClient(200, 0, nil)
	require.NoError(t, err)

	require.Equal(t, 50, client.MultiValueCap("query"), "conservative default before capability detection")

	client.SetMultiValueCaps(500, map[string]int{"revisiondelete": 100})
	require.Equal(t, 100, client.MultiValueCap("revisiondelete"), "discovered per-action cap wins")
	require.Equal(t, 500, client.MultiValueCap("query"), "undiscovered actions use the rights-derived default")

	client.observeAPIError("query", &APIError{Code: "toomanyvalues", Limit: 50})
	require.Equal(t, 50, client.MultiValueCap("query"), "an observed rejection shrinks that action's cap")
	require.Equal(t, 100, client.MultiValueCap("revisiondelete"), "other actions keep their caps")

	client.observeAPIError("query", &APIError{Code: "toomanyvalues", Limit: 500})
	require.Equal(t, 50, client.MultiValueCap("query"), "observations never raise a cap")

	client.observeAPIError("query", &APIError{Code: "badtoken"})
	require.Equal(t, 50, client.MultiValueCap("query"), "unrelated errors leave the caps alone")

	client.SetMultiValueCaps(500, nil)
	require.Equal(t, 500, client.MultiValueCap("query"), "a reconnect may reset the caps")
}

// The full adaptive loop against a wiki whose real cap is lower than detected: the first oversized request is
// rejected with toomanyvalues, the cap shrinks to the reported limit, and the chunk is re-split and replayed.
func TestForEachChunkAdaptsToWikiReportedCap(t *testing.T) {
	t.Parallel()

	var requested [][]string
	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !assert.NoError(t, request.ParseForm()) {
			return
		}
		titles := strings.Split(request.URL.Query().Get("titles"), "|")
		if len(titles) > 50 {
			_, _ = writer.Write([]byte(`{"errors":[{"code":"toomanyvalues",` +
				`"data":{"parameter":"titles","limit":50,"lowlimit":50,"highlimit":500},` +
				`"*":"Too many values supplied for parameter \"titles\". The limit is 50."}]}`))
			return
		}
		requested = append(requested, titles)
		_, _ = writer.Write([]byte(`{"query":{}}`))
	}))
	defer testServer.Close()

	client, err := NewClient(200, 0, nil)
	require.NoError(t, err)
	client.SetMultiValueCaps(500, nil) // what the detected rights promised; the wiki disagrees

	values := make([]string, 120)
	for i := range values {
		values[i] = fmt.Sprintf("Page %d", i)
	}

	err = client.ForEachChunk("query", values, func(chunk []string) error {
		_, chunkErr := client.GetContext(context.Background(), testServer.URL, map[string]string{
			"action": "query", "titles": strings.Join(chunk, "|"),
		})
		if chunkErr != nil {
			return fmt.Errorf("query failed: %w", chunkErr) // wrapping must not hide the rejection
		}
		return nil
	})
	require.NoError(t, err)

	require.Equal(t, 50, client.MultiValueCap("query"), "the wiki's rejection shrank the action's cap")
	require.Len(t, requested, 3, "120 values re-split at the real cap: 50+50+20")
	served := []string{}
	for _, chunk := range requested {
		served = append(served, chunk...)
	}
	require.Equal(t, values, served, "every value served exactly once, in order")
}

// A rejection that does not shrink the cap below the chunk size must surface as an error, not loop forever.
func TestForEachChunkSurfacesNonShrinkingRejection(t *testing.T) {
	t.Parallel()

	client, err := NewClient(200, 0, nil)
	require.NoError(t, err)

	calls := 0
	err = client.ForEachChunk("query", []string{"a", "b"}, func([]string) error {
		calls++
		return &APIError{Code: "toomanyvalues", Limit: 50}
	})
	require.Error(t, err)
	require.Equal(t, 1, calls)
}

// An oversized translated write call: the executor re-splits the declared multivalue parameter at the cap the
// wiki reported and replays it, so the caller sees one successful result.
func TestHttpExecutorResplitsOversizedMultiParam(t *testing.T) {
	t.Parallel()

	var served [][]string
	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !assert.NoError(t, request.ParseForm()) {
			return
		}
		switch request.Form.Get("action") {
		case "query":
			_, _ = writer.Write([]byte(`{"query":{"tokens":{"csrftoken":"csrf-token"}}}`))
		case "revisiondelete":
			ids := strings.Split(request.Form.Get("ids"), "|")
			if len(ids) > 50 {
				_, _ = writer.Write([]byte(`{"error":{"code":"toomanyvalues",` +
					`"info":"Too many values supplied for parameter \"ids\". The limit is 50.",` +
					`"parameter":"ids","limit":50}}`))
				return
			}
			served = append(served, ids)
			_, _ = writer.Write([]byte(`{"revisiondelete":{"status":"Success"}}`))
		default:
			http.Error(writer, "unexpected call", http.StatusBadRequest)
		}
	}))
	defer testServer.Close()

	client, err := NewClient(200, 0, nil)
	require.NoError(t, err)
	client.SetMultiValueCaps(500, nil)
	executor, err := NewHttpExecutor(client, testServer.URL)
	require.NoError(t, err)

	ids := make([]string, 120)
	for i := range ids {
		ids[i] = strconv.Itoa(1000 + i)
	}
	results, err := executor.Execute(context.Background(), []APICall{{
		Action:     "revisiondelete",
		Method:     "POST",
		Params:     map[string]string{"type": "revision", "ids": strings.Join(ids, "|"), "hide": "content"},
		MultiParam: "ids",
	}})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.True(t, results[0].Success, "the re-split call reports one merged success")

	require.Len(t, served, 3, "120 ids re-split at the real cap: 50+50+20")
	applied := []string{}
	for _, chunk := range served {
		applied = append(applied, chunk...)
	}
	require.Equal(t, ids, applied, "every id applied exactly once, in order")
	require.Equal(t, 50, client.MultiValueCap("revisiondelete"), "the rejection shrank this action's cap")
}

// A call's Validate hook turns a failure hidden inside a successful response into a call-level error.
func TestHttpExecutorValidateRejectsHiddenFailures(t *testing.T) {
	t.Parallel()

	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !assert.NoError(t, request.ParseForm()) {
			return
		}
		if request.Form.Get("action") == "query" {
			_, _ = writer.Write([]byte(`{"query":{"tokens":{"csrftoken":"csrf-token"}}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"revisiondelete":{"status":"Success","items":[` +
			`{"status":"Success","id":210,"errors":[{"code":"revdelete-modify-no-access",` +
			`"*":"This item has been marked \"restricted\". You do not have access to it."}]}]}}`))
	}))
	defer testServer.Close()

	client, err := NewClient(200, 0, nil)
	require.NoError(t, err)
	executor, err := NewHttpExecutor(client, testServer.URL)
	require.NoError(t, err)

	results, err := executor.Execute(context.Background(), []APICall{{
		Action:     "revisiondelete",
		Method:     "POST",
		Params:     map[string]string{"type": "revision", "ids": "210", "show": "comment"},
		MultiParam: "ids",
		Validate:   ValidateRevisionDeleteResponse,
	}})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.False(t, results[0].Success)
	require.NotNil(t, results[0].Error)
	require.Equal(t, "revdelete-modify-no-access", results[0].Error.Code)
}
