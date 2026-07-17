package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recoveryTestServer is a stateful fake wiki: it rejects asserted requests while logged out, accepts the
// bot-password login flow, and reports a session-aware multivalue cap via paraminfo.
type recoveryTestServer struct {
	mu         sync.Mutex
	loggedIn   bool
	loginCalls int
	loginFails bool
	server     *httptest.Server
}

func newRecoveryTestServer(t *testing.T) *recoveryTestServer {
	t.Helper()
	s := &recoveryTestServer{}
	s.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !assert.NoError(t, request.ParseForm()) {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()

		action := request.Form.Get("action")
		isLoginFlow := action == "login" ||
			(action == "query" && request.Form.Get("meta") == "tokens" && request.Form.Get("type") == "login")
		if isLoginFlow {
			assert.Empty(t, request.Form.Get("assert"), "login-flow requests must not assert a login")
		}

		// The assertion is checked before anything else, like MediaWiki does.
		if request.Form.Get("assert") == "user" && !s.loggedIn {
			_, _ = writer.Write([]byte(`{"errors":[{"code":"assertuserfailed",` +
				`"*":"You are no longer logged in, so the action could not be completed."}]}`))
			return
		}

		switch action {
		case "query":
			switch {
			case request.Form.Get("meta") == "tokens" && request.Form.Get("type") == "login":
				_, _ = writer.Write([]byte(`{"query":{"tokens":{"logintoken":"login-token"}}}`))
			case request.Form.Get("meta") == "tokens" && request.Form.Get("type") == "csrf":
				_, _ = writer.Write([]byte(`{"query":{"tokens":{"csrftoken":"csrf-token"}}}`))
			default:
				_, _ = writer.Write([]byte(`{"query":{"pages":[]}}`))
			}
		case "login":
			s.loginCalls++
			if s.loginFails {
				_, _ = writer.Write([]byte(`{"login":{"result":"Failed","reason":"Incorrect username or password."}}`))
				return
			}
			s.loggedIn = true
			_, _ = writer.Write([]byte(`{"login":{"result":"Success","lgusername":"TestAdmin"}}`))
		case "paraminfo":
			// Session-aware: the fresh session has high limits, reported per module.
			_, _ = writer.Write([]byte(`{"paraminfo":{"modules":[` +
				`{"name":"query","parameters":[{"name":"titles","multi":true,"limit":500}]},` +
				`{"name":"revisiondelete","parameters":[{"name":"ids","multi":true,"limit":500}]}]}}`))
		case "delete":
			assert.Equal(t, "csrf-token", request.Form.Get("token"))
			_, _ = writer.Write([]byte(`{"delete":{"title":"Apple"}}`))
		default:
			http.Error(writer, "unexpected call", http.StatusBadRequest)
		}
	}))
	t.Cleanup(s.server.Close)
	return s
}

// An expired session is recovered transparently: the failing GET triggers one re-login, the request is
// retried, and the multivalue cap is re-learned from the fresh session — a cap shrunk by the dying session's
// answers does not outlive the reconnection.
func TestSessionRecoveryTransparentlyRelogins(t *testing.T) {
	t.Parallel()

	wiki := newRecoveryTestServer(t)
	client, err := NewClient(200, 1, nil)
	require.NoError(t, err)
	client.EnableSessionRecovery(wiki.server.URL, "TestAdmin@App", "botpass")
	// As if the dying session's rejections had shrunk an action's cap.
	client.SetMultiValueCaps(500, map[string]int{"revisiondelete": 50})

	payload, err := client.GetContext(context.Background(), wiki.server.URL, map[string]string{
		"action": "query", "prop": "info", "titles": "Apple",
	})
	require.NoError(t, err)
	require.Contains(t, payload, "query")
	require.Equal(t, 1, wiki.loginCalls, "one transparent re-login")
	require.Equal(t, 500, client.MultiValueCap("revisiondelete"),
		"the caps are re-learned from the recovered session")
	require.Equal(t, 500, client.MultiValueCap("query"))
}

// A POST whose session died recovers through the CSRF-token fetch (which is asserted too) and then completes
// with a fresh token.
func TestSessionRecoveryCoversPostTokenFetch(t *testing.T) {
	t.Parallel()

	wiki := newRecoveryTestServer(t)
	client, err := NewClient(200, 1, nil)
	require.NoError(t, err)
	client.EnableSessionRecovery(wiki.server.URL, "TestAdmin@App", "botpass")

	payload, err := client.PostContext(context.Background(), wiki.server.URL, map[string]string{
		"action": "delete", "title": "Apple", "reason": "Cleanup",
	})
	require.NoError(t, err)
	require.Contains(t, payload, "delete")
	require.Equal(t, 1, wiki.loginCalls)
}

// When the re-login itself fails (e.g. the bot password was revoked), the caller gets a recovery error, after
// exactly one attempt.
func TestSessionRecoveryFailureSurfaces(t *testing.T) {
	t.Parallel()

	wiki := newRecoveryTestServer(t)
	wiki.loginFails = true
	client, err := NewClient(200, 1, nil)
	require.NoError(t, err)
	client.EnableSessionRecovery(wiki.server.URL, "TestAdmin@App", "botpass")

	_, err = client.GetContext(context.Background(), wiki.server.URL, map[string]string{
		"action": "query", "prop": "info", "titles": "Apple",
	})
	require.ErrorContains(t, err, "session lost and recovery failed")
	require.Equal(t, 1, wiki.loginCalls)
}

// Without EnableSessionRecovery nothing changes: no assertion is attached and no re-login is attempted.
func TestNoAssertWithoutSessionRecovery(t *testing.T) {
	t.Parallel()

	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !assert.NoError(t, request.ParseForm()) {
			return
		}
		assert.Empty(t, request.Form.Get("assert"))
		_, _ = writer.Write([]byte(`{"query":{"pages":[]}}`))
	}))
	defer testServer.Close()

	client, err := NewClient(200, 1, nil)
	require.NoError(t, err)
	_, err = client.GetContext(context.Background(), testServer.URL, map[string]string{
		"action": "query", "prop": "info", "titles": "Apple",
	})
	require.NoError(t, err)
}

func TestParseMultiValueCaps(t *testing.T) {
	t.Parallel()

	caps, err := parseMultiValueCaps(map[string]any{
		"paraminfo": map[string]any{"modules": []any{
			map[string]any{"name": "revisiondelete", "parameters": []any{
				map[string]any{"name": "type"},
				map[string]any{
					"name": "ids", "lowlimit": float64(50), "highlimit": float64(500), "limit": float64(500),
				},
			}},
			// A module capping its parameter individually is reported as-is.
			map[string]any{"name": "query", "parameters": []any{
				map[string]any{"name": "titles", "limit": float64(100)},
			}},
		}},
	})
	require.NoError(t, err)
	require.Equal(t, map[string]int{"revisiondelete": 500, "query": 100}, caps)

	_, err = parseMultiValueCaps(map[string]any{"paraminfo": map[string]any{"modules": []any{}}})
	require.Error(t, err)

	// A wiki whose paraminfo omits every session-aware limit is reported as an error, so callers fall back.
	_, err = parseMultiValueCaps(map[string]any{
		"paraminfo": map[string]any{"modules": []any{map[string]any{
			"name": "revisiondelete", "parameters": []any{map[string]any{"name": "ids"}},
		}}},
	})
	require.Error(t, err)
}
