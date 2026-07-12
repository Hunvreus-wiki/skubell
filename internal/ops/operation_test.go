package ops

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExecutionPlanConstruction(t *testing.T) {
	t.Parallel()

	plan := ExecutionPlan{
		Module:    "deletion",
		ReadPhase: 0,
		Operations: []Operation{
			{Type: OpQueryPageInfo, Params: map[string]string{"title": "Apple"}},
			{Type: OpDeletePage, Params: map[string]string{"title": "Apple", "reason": "Cleanup"}},
		},
	}

	require.Equal(t, "deletion", plan.Module)
	require.Equal(t, OpQueryPageInfo, plan.Operations[0].Type)
	require.Equal(t, OpDeletePage, plan.Operations[1].Type)
}
