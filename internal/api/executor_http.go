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

		method := strings.ToUpper(call.Method)
		if method != http.MethodGet && method != http.MethodPost {
			return results, fmt.Errorf("unsupported method %q for call index %d", call.Method, i)
		}
		response, err := e.runCall(ctx, method, params, strings.TrimSpace(call.MultiParam), call.Validate)
		if err != nil {
			if apiErr, ok := errors.AsType[*APIError](err); ok {
				errCopy := *apiErr
				results = append(results, APIResult{
					CallIndex: i,
					Success:   false,
					Response:  nil,
					Error:     &errCopy,
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

// runCall performs one call, re-splitting its declared multivalue parameter when the wiki rejects the batch as
// larger than the session's real cap ("toomanyvalues" shrinks Client.MultiValueCap; ForEachChunk then replays
// the values in chunks of that size). Each chunk's payload runs through validate (when set), so failures the
// wiki hides inside a successful response are caught per chunk. All chunks must succeed; the first failure
// aborts and is returned, so a re-split call can partially apply — same semantics as any multi-call operation.
// The returned payload is the last chunk's response: callers of write actions only inspect success/error, not
// the response body.
func (e *HttpExecutor) runCall(
	ctx context.Context, method string, params map[string]string, multiParam string,
	validate func(map[string]any) *APIError,
) (map[string]any, error) {
	checked := func(requestParams map[string]string) (map[string]any, error) {
		payload, err := e.do(ctx, method, requestParams)
		if err != nil {
			return nil, err
		}
		if validate != nil {
			if apiErr := validate(payload); apiErr != nil {
				return nil, apiErr
			}
		}
		return payload, nil
	}

	if multiParam == "" || strings.TrimSpace(params[multiParam]) == "" {
		return checked(params)
	}
	values := strings.Split(params[multiParam], "|")
	var last map[string]any
	err := e.client.ForEachChunk(params["action"], values, func(chunk []string) error {
		chunkParams := cloneParams(params)
		chunkParams[multiParam] = strings.Join(chunk, "|")
		payload, chunkErr := checked(chunkParams)
		if chunkErr != nil {
			return chunkErr
		}
		last = payload
		return nil
	})
	if err != nil {
		return nil, err
	}
	return last, nil
}

// do dispatches one request to the client; method has been validated as GET or POST by Execute.
func (e *HttpExecutor) do(ctx context.Context, method string, params map[string]string) (map[string]any, error) {
	if method == http.MethodGet {
		return e.client.GetContext(ctx, e.apiURL, params)
	}
	return e.client.PostContext(ctx, e.apiURL, params)
}
