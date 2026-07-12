package deletion

import (
	"context"
	"errors"

	"github.com/Hunvreus-wiki/skubell/internal/api"
	"github.com/Hunvreus-wiki/skubell/internal/ops"
)

// ExecutePlan translates and executes a deletion plan.
func ExecutePlan(
	ctx context.Context,
	plan ops.ExecutionPlan,
	translator api.Translator,
	caps api.WikiCapabilities,
	executor api.Executor,
) ([]api.APIResult, error) {
	if translator == nil || executor == nil {
		return nil, errors.New("translator or executor is nil")
	}

	calls := make([]api.APICall, 0, len(plan.Operations))
	for idx, op := range plan.Operations {
		apiCalls, err := translator.Translate(op, caps)
		if err != nil {
			return nil, err
		}
		for _, call := range apiCalls {
			call.SourceOp = idx
			calls = append(calls, call)
		}
	}
	return executor.Execute(ctx, calls)
}
