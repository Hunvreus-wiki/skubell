package api

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"
)

// MockExecutor records calls and returns fixture-backed results.
type MockExecutor struct {
	mu         sync.Mutex
	fixtures   map[string]APIResult
	recorded   []APICall
	failOnMiss bool
}

// NewMockExecutor creates a mock executor with fixture results keyed by call key.
func NewMockExecutor(fixtures map[string]APIResult) *MockExecutor {
	cloned := make(map[string]APIResult, len(fixtures))
	maps.Copy(cloned, fixtures)
	return &MockExecutor{fixtures: cloned}
}

// SetFailOnMissingFixture configures whether Execute should return an error when a call key is missing.
func (m *MockExecutor) SetFailOnMissingFixture(enabled bool) {
	m.mu.Lock()
	m.failOnMiss = enabled
	m.mu.Unlock()
}

// RecordedCalls returns a copy of all calls seen by this executor.
func (m *MockExecutor) RecordedCalls() []APICall {
	m.mu.Lock()
	defer m.mu.Unlock()
	calls := make([]APICall, len(m.recorded))
	copy(calls, m.recorded)
	return calls
}

// Execute records calls and returns configured results.
func (m *MockExecutor) Execute(_ context.Context, calls []APICall) ([]APIResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	results := make([]APIResult, 0, len(calls))
	for index, call := range calls {
		m.recorded = append(m.recorded, call)
		key := APICallKey(call)
		if configured, ok := m.fixtures[key]; ok {
			configured.CallIndex = index
			results = append(results, configured)
			continue
		}

		if m.failOnMiss {
			return nil, fmt.Errorf("mock fixture not found for call key %q", key)
		}

		results = append(results, APIResult{
			CallIndex: index,
			Success:   false,
			Error:     &APIError{Code: "mock_missing_fixture", Info: key},
		})
	}

	return results, nil
}

// APICallKey computes a deterministic fixture key for an API call.
func APICallKey(call APICall) string {
	keys := make([]string, 0, len(call.Params))
	for key := range call.Params {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys)+2)
	parts = append(parts, strings.ToUpper(call.Method), call.Action)
	for _, key := range keys {
		parts = append(parts, key+"="+call.Params[key])
	}
	return strings.Join(parts, "|")
}
