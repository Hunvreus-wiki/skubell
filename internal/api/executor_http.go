package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// HttpExecutor executes APICalls against a real MediaWiki API endpoint.
type HttpExecutor struct {
	client *Client
	apiURL string
}

// NewHttpExecutor builds a real API executor.
func NewHttpExecutor(client *Client, apiURL string) (*HttpExecutor, error) {
	if client == nil {
		return nil, errors.New("api client is nil")
	}
	if strings.TrimSpace(apiURL) == "" {
		return nil, errors.New("api url is empty")
	}
	return &HttpExecutor{client: client, apiURL: apiURL}, nil
}

// Execute executes calls sequentially and returns one result per call.
func (e *HttpExecutor) Execute(ctx context.Context, calls []APICall) ([]APIResult, error) {
	results := make([]APIResult, 0, len(calls))
	for i, call := range calls {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		params := cloneParams(call.Params)
		if _, ok := params["action"]; !ok && call.Action != "" {
			params["action"] = call.Action
		}

		var (
			response map[string]any
			err      error
		)
		switch strings.ToUpper(call.Method) {
		case http.MethodGet:
			response, err = e.client.GetContext(ctx, e.apiURL, params)
		case http.MethodPost:
			response, err = e.client.PostContext(ctx, e.apiURL, params)
		default:
			return results, fmt.Errorf("unsupported method %q for call index %d", call.Method, i)
		}

		if err != nil {
			var apiErr *APIError
			if errors.As(err, &apiErr) {
				results = append(results, APIResult{
					CallIndex: i,
					Success:   false,
					Response:  nil,
					Error: &APIError{
						Code: apiErr.Code,
						Info: apiErr.Info,
					},
				})
				continue
			}
			return results, err
		}

		results = append(results, APIResult{
			CallIndex: i,
			Success:   true,
			Response:  response,
			Error:     nil,
		})
	}
	return results, nil
}
