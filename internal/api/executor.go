package api

import "context"

// Executor executes translated API calls.
type Executor interface {
	Execute(ctx context.Context, calls []APICall) ([]APIResult, error)
}

// APIResult contains the result of an individual API call.
type APIResult struct {
	CallIndex int
	Success   bool
	Response  map[string]any
	Error     *APIError
}
