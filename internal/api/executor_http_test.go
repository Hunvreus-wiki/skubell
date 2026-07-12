package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHttpExecutorDeleteFlowFetchesCSRFAndExecutesDelete(t *testing.T) {
	t.Parallel()

	tokenCalls := 0
	deleteCalls := 0

	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !assert.NoError(t, request.ParseForm()) {
			return
		}

		switch request.Form.Get("action") {
		case "query":
			if request.Form.Get("meta") == "tokens" && request.Form.Get("type") == "csrf" {
				tokenCalls++
				_, _ = writer.Write([]byte(`{"query":{"tokens":{"csrftoken":"csrf-token"}}}`))
				return
			}
		case "delete":
			deleteCalls++
			assert.Equal(t, "csrf-token", request.Form.Get("token"))
			assert.Equal(t, "Apple", request.Form.Get("title"))
			_, _ = writer.Write([]byte(`{"delete":{"title":"Apple"}}`))
			return
		}

		http.Error(writer, "unexpected call", http.StatusBadRequest)
	}))
	defer testServer.Close()

	client, err := NewClient(200, 1, nil)
	require.NoError(t, err)
	executor, err := NewHttpExecutor(client, testServer.URL)
	require.NoError(t, err)

	results, err := executor.Execute(context.Background(), []APICall{{
		Action: "delete",
		Method: "POST",
		Params: map[string]string{
			"title":  "Apple",
			"reason": "Cleanup",
		},
		SourceOp: 0,
	}})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.True(t, results[0].Success)
	require.Nil(t, results[0].Error)
	require.Equal(t, 1, tokenCalls)
	require.Equal(t, 1, deleteCalls)
}
