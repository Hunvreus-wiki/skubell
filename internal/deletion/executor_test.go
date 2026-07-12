package deletion

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/api"
	"github.com/Hunvreus-wiki/skubell/internal/ops"
)

func TestDeletionEndToEndWithMockExecutor(t *testing.T) {
	t.Parallel()

	provider := &ops.MockDataProvider{
		SubjectPages: map[string]string{
			"Talk:Apple": "Apple",
			"Talk:Cider": "Cider",
		},
		TalkPages: map[string]string{
			"Apple": "Talk:Apple",
			"Cider": "Talk:Cider",
		},
		ExistingPages: map[string]struct{}{
			"Talk:Apple": {}, "Talk:Cider": {},
		},
		Redirects: map[string][]string{
			"Apple": {"Cider"}, // Cider redirects to Apple
		},
	}

	plan, err := BuildPlan(
		provider,
		[]string{"Apple"},
		PlanOptions{IncludeTalk: true, IncludeRedirect: true, Reason: "Cleanup"},
	)
	require.NoError(t, err)

	executor := api.NewMockExecutor(map[string]api.APIResult{})
	translator := api.DeleteTranslator{}

	results, err := ExecutePlan(
		context.Background(),
		plan.ExecutionPlan(),
		translator,
		api.WikiCapabilities{},
		executor,
	)
	require.NoError(t, err)
	require.Len(t, results, 2)

	calls := executor.RecordedCalls()
	require.Len(t, calls, 2)
	require.Equal(t, "delete", calls[0].Action)
	require.Equal(t, "POST", calls[0].Method)
	require.Equal(t, "Apple", calls[0].Params["title"])
	require.Equal(t, "Cleanup", calls[0].Params["reason"])
	require.Equal(t, "1", calls[0].Params["deletetalk"]) // Talk:Apple removed via deletetalk
	require.Equal(t, "Cider", calls[1].Params["title"])
	require.Equal(t, "1", calls[1].Params["deletetalk"]) // Talk:Cider removed via deletetalk
}
