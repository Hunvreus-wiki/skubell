package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMockExecutorRecordsCallsAndReturnsFixtures(t *testing.T) {
	t.Parallel()

	call := APICall{
		Action: "query",
		Method: "GET",
		Params: map[string]string{
			"action": "query",
			"meta":   "siteinfo",
		},
		SourceOp: 0,
	}

	mock := NewMockExecutor(map[string]APIResult{
		APICallKey(call): {Success: true, Response: map[string]any{"query": map[string]any{"ok": true}}},
	})

	results, err := mock.Execute(context.Background(), []APICall{call})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.True(t, results[0].Success)
	require.Nil(t, results[0].Error)
	require.Equal(t, 0, results[0].CallIndex)

	recorded := mock.RecordedCalls()
	require.Equal(t, []APICall{call}, recorded)
}

func TestMockExecutorReturnsConfiguredFailure(t *testing.T) {
	t.Parallel()

	call := APICall{
		Action: "delete",
		Method: "POST",
		Params: map[string]string{"action": "delete", "title": "Apple"},
	}

	mock := NewMockExecutor(map[string]APIResult{
		APICallKey(call): {
			Success: false,
			Error:   &APIError{Code: "blocked", Info: "Blocked from editing"},
		},
	})

	results, err := mock.Execute(context.Background(), []APICall{call})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.False(t, results[0].Success)
	require.NotNil(t, results[0].Error)
	require.Equal(t, "blocked", results[0].Error.Code)
}
