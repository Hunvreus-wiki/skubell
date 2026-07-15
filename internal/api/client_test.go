package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/i18n"
)

func TestClientGetParsesJSON(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodGet, request.Method)
		assert.Contains(t, request.UserAgent(), "Skubell/")
		assert.Contains(t, request.UserAgent(), "github.com/Hunvreus-wiki/skubell")
		_, _ = writer.Write([]byte(`{"query":{"ok":true}}`))
	}))
	defer testServer.Close()

	client, err := NewClient(1000, 3, nil)
	require.NoError(t, err)

	response, err := client.Get(testServer.URL, map[string]string{"action": "query"})
	require.NoError(t, err)

	query, ok := response["query"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, query["ok"])
}

func TestClientGetContextAppliesDefaultTimeoutWhenMissing(t *testing.T) {
	client, err := NewClient(1000, 0, nil)
	require.NoError(t, err)
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		deadline, ok := request.Context().Deadline()
		require.True(t, ok)
		require.InDelta(t, defaultRequestTimeout.Seconds(), time.Until(deadline).Seconds(), 2.0)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString(`{"query":{"ok":true}}`)),
		}, nil
	})

	_, err = client.GetContext(
		context.Background(),
		"http://example.test/api.php",
		map[string]string{"action": "query"},
	)
	require.NoError(t, err)
}

func TestClientGetContextPreservesExistingDeadline(t *testing.T) {
	client, err := NewClient(1000, 0, nil)
	require.NoError(t, err)
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		deadline, ok := request.Context().Deadline()
		require.True(t, ok)
		require.Less(t, time.Until(deadline), 2*time.Second)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString(`{"query":{"ok":true}}`)),
		}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err = client.GetContext(ctx, "http://example.test/api.php", map[string]string{"action": "query"})
	require.NoError(t, err)
}

func TestClientPostParsesJSON(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Contains(t, request.UserAgent(), "Skubell/")
		assert.Contains(t, request.UserAgent(), "github.com/Hunvreus-wiki/skubell")
		if !assert.NoError(t, request.ParseForm()) {
			return
		}
		assert.Equal(t, "login", request.PostForm.Get("action"))
		_, _ = writer.Write([]byte(`{"login":{"result":"Success"}}`))
	}))
	defer testServer.Close()

	client, err := NewClient(1000, 3, nil)
	require.NoError(t, err)

	response, err := client.Post(testServer.URL, map[string]string{"action": "login"})
	require.NoError(t, err)

	loginData, ok := response["login"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "Success", loginData["result"])
}

func TestCSRFTokenCachingAndBadTokenRefresh(t *testing.T) {
	var mu sync.Mutex
	tokenFetches := 0
	deleteCalls := 0

	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !assert.NoError(t, request.ParseForm()) {
			return
		}

		action := request.Form.Get("action")
		switch action {
		case "query":
			if request.Form.Get("meta") == "tokens" && request.Form.Get("type") == "csrf" {
				mu.Lock()
				tokenFetches++
				fetchNo := tokenFetches
				mu.Unlock()

				token := "first-token"
				if fetchNo > 1 {
					token = "second-token"
				}
				_, _ = writer.Write(fmt.Appendf(nil, `{"query":{"tokens":{"csrftoken":"%s"}}}`, token))
				return
			}
		case "delete":
			mu.Lock()
			deleteCalls++
			currentDelete := deleteCalls
			mu.Unlock()

			token := request.Form.Get("token")
			title := request.Form.Get("title")

			if currentDelete == 1 {
				assert.Equal(t, "first-token", token)
				_, _ = writer.Write([]byte(`{"delete":{"title":"PageOne"}}`))
				return
			}

			if currentDelete == 2 {
				assert.Equal(t, "first-token", token)
				_, _ = writer.Write([]byte(`{"delete":{"title":"PageTwo"}}`))
				return
			}

			if title == "NeedsRefresh" && token == "first-token" {
				_, _ = writer.Write([]byte(`{"error":{"code":"badtoken","info":"Invalid CSRF token"}}`))
				return
			}

			assert.Equal(t, "second-token", token)
			_, _ = writer.Write([]byte(`{"delete":{"title":"NeedsRefresh"}}`))
			return
		}

		http.Error(writer, "unexpected request", http.StatusBadRequest)
	}))
	defer testServer.Close()

	client, err := NewClient(1000, 3, nil)
	require.NoError(t, err)

	_, err = client.Post(testServer.URL, map[string]string{"action": "delete", "title": "PageOne"})
	require.NoError(t, err)
	_, err = client.Post(testServer.URL, map[string]string{"action": "delete", "title": "PageTwo"})
	require.NoError(t, err)
	_, err = client.Post(testServer.URL, map[string]string{"action": "delete", "title": "NeedsRefresh"})
	require.NoError(t, err)

	require.Equal(t, 2, tokenFetches)
	require.Equal(t, 4, deleteCalls)
}

func TestWriteThrottleAppliesToConsecutivePosts(t *testing.T) {
	var mu sync.Mutex
	deleteTimes := []time.Time{}

	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !assert.NoError(t, request.ParseForm()) {
			return
		}

		action := request.Form.Get("action")
		if action == "query" {
			_, _ = writer.Write([]byte(`{"query":{"tokens":{"csrftoken":"throttle-token"}}}`))
			return
		}

		if action == "delete" {
			mu.Lock()
			deleteTimes = append(deleteTimes, time.Now())
			mu.Unlock()
			_, _ = writer.Write([]byte(`{"delete":{"ok":true}}`))
			return
		}

		http.Error(writer, "unexpected action", http.StatusBadRequest)
	}))
	defer testServer.Close()

	client, err := NewClient(200, 1, nil)
	require.NoError(t, err)

	_, err = client.Post(testServer.URL, map[string]string{"action": "delete", "title": "A"})
	require.NoError(t, err)
	_, err = client.Post(testServer.URL, map[string]string{"action": "delete", "title": "B"})
	require.NoError(t, err)

	require.Len(t, deleteTimes, 2)
	require.GreaterOrEqual(t, deleteTimes[1].Sub(deleteTimes[0]), 200*time.Millisecond)
}

func TestRetryOnMaxlag(t *testing.T) {
	deleteAttempts := 0
	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !assert.NoError(t, request.ParseForm()) {
			return
		}

		switch request.Form.Get("action") {
		case "query":
			_, _ = writer.Write([]byte(`{"query":{"tokens":{"csrftoken":"token"}}}`))
		case "delete":
			deleteAttempts++
			if deleteAttempts == 1 {
				_, _ = writer.Write([]byte(`{"error":{"code":"maxlag","info":"Waiting for lagged replica"}}`))
				return
			}
			_, _ = writer.Write([]byte(`{"delete":{"ok":true}}`))
		default:
			http.Error(writer, "unexpected action", http.StatusBadRequest)
		}
	}))
	defer testServer.Close()

	client, err := NewClient(200, 2, nil)
	require.NoError(t, err)

	_, err = client.Post(testServer.URL, map[string]string{"action": "delete", "title": "A"})
	require.NoError(t, err)
	require.Equal(t, 2, deleteAttempts)
}

func TestRetryOnHTTP429WithRetryAfter(t *testing.T) {
	deleteAttempts := 0
	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !assert.NoError(t, request.ParseForm()) {
			return
		}

		switch request.Form.Get("action") {
		case "query":
			_, _ = writer.Write([]byte(`{"query":{"tokens":{"csrftoken":"token"}}}`))
		case "delete":
			deleteAttempts++
			if deleteAttempts == 1 {
				writer.Header().Set("Retry-After", "1")
				writer.WriteHeader(http.StatusTooManyRequests)
				_, _ = writer.Write([]byte(`{"error":{"code":"ratelimited"}}`))
				return
			}
			_, _ = writer.Write([]byte(`{"delete":{"ok":true}}`))
		default:
			http.Error(writer, "unexpected action", http.StatusBadRequest)
		}
	}))
	defer testServer.Close()

	client, err := NewClient(200, 2, nil)
	require.NoError(t, err)

	start := time.Now()
	_, err = client.Post(testServer.URL, map[string]string{"action": "delete", "title": "A"})
	require.NoError(t, err)

	require.Equal(t, 2, deleteAttempts)
	require.GreaterOrEqual(t, time.Since(start), time.Second)
}

func TestRetryOnTransientNetworkError(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !assert.NoError(t, request.ParseForm()) {
			return
		}
		if request.Form.Get("action") == "query" {
			_, _ = writer.Write([]byte(`{"query":{"tokens":{"csrftoken":"token"}}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"delete":{"ok":true}}`))
	}))
	defer testServer.Close()

	client, err := NewClient(200, 2, nil)
	require.NoError(t, err)

	baseTransport := http.DefaultTransport
	failFirstPost := true
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			if failFirstPost {
				failFirstPost = false
				return nil, &url.Error{Op: "Post", URL: request.URL.String(), Err: errors.New("connection reset")}
			}
		}
		return baseTransport.RoundTrip(request)
	})

	_, err = client.Post(testServer.URL, map[string]string{"action": "delete", "title": "A"})
	require.NoError(t, err)
	require.False(t, failFirstPost)
}

func TestFatalErrorNotRetried(t *testing.T) {
	deleteAttempts := 0
	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !assert.NoError(t, request.ParseForm()) {
			return
		}

		switch request.Form.Get("action") {
		case "query":
			_, _ = writer.Write([]byte(`{"query":{"tokens":{"csrftoken":"token"}}}`))
		case "delete":
			deleteAttempts++
			_, _ = writer.Write([]byte(`{"error":{"code":"permissiondenied","info":"missing delete right"}}`))
		default:
			http.Error(writer, "unexpected action", http.StatusBadRequest)
		}
	}))
	defer testServer.Close()

	client, err := NewClient(200, 3, nil)
	require.NoError(t, err)

	_, err = client.Post(testServer.URL, map[string]string{"action": "delete", "title": "A"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "permissiondenied")
	require.Equal(t, 1, deleteAttempts)
}

func TestHTTPSWarningBehavior(t *testing.T) {
	buffer := bytes.NewBuffer(nil)
	logger := logrus.New()
	logger.SetOutput(buffer)
	logger.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true, DisableColors: true})

	client, err := NewClient(200, 1, logger)
	require.NoError(t, err)

	httpServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(writer, `{"query":{"ok":true}}`)
	}))
	defer httpServer.Close()

	_, err = client.Get(httpServer.URL, map[string]string{"action": "query"})
	require.NoError(t, err)
	require.Contains(t, buffer.String(), "insecure HTTP API URL")

	buffer.Reset()
	httpsServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(writer, `{"query":{"ok":true}}`)
	}))
	defer httpsServer.Close()

	client.httpClient = httpsServer.Client()
	_, err = client.Get(httpsServer.URL, map[string]string{"action": "query"})
	require.NoError(t, err)
	require.NotContains(t, buffer.String(), "insecure HTTP API URL")
}

func TestClientGetContextCancelsRequest(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		select {
		case <-request.Context().Done():
			return
		case <-time.After(2 * time.Second):
			_, _ = writer.Write([]byte(`{"query":{"ok":true}}`))
		}
	}))
	defer testServer.Close()

	client, err := NewClient(1000, 0, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)

	start := time.Now()
	_, err = client.GetContext(ctx, testServer.URL, map[string]string{"action": "query"})
	require.ErrorIs(t, err, context.Canceled)
	require.Less(t, time.Since(start), 2*time.Second)
}

// TestClientRequestsLocalizedErrors pins the parameters that make MediaWiki answer in the operator's language: the
// default "bc" error format would reply in English whatever errorlang says.
func TestClientRequestsLocalizedErrors(t *testing.T) {
	previous := i18n.CurrentLanguage()
	defer i18n.SetLanguage(previous)
	i18n.SetLanguage("br")

	var seenErrorFormat, seenErrorLang string
	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !assert.NoError(t, request.ParseForm()) {
			return
		}
		seenErrorFormat = request.Form.Get("errorformat")
		seenErrorLang = request.Form.Get("errorlang")
		_, _ = writer.Write([]byte(`{"query":{"ok":true}}`))
	}))
	defer testServer.Close()

	client, err := NewClient(1000, 0, nil)
	require.NoError(t, err)

	_, err = client.Get(testServer.URL, map[string]string{"action": "query"})
	require.NoError(t, err)
	require.Equal(t, "plaintext", seenErrorFormat)
	require.Equal(t, "br", seenErrorLang)
}

// TestClientKeepsExplicitErrorParams lets a caller that needs another shape opt out of the defaults.
func TestClientKeepsExplicitErrorParams(t *testing.T) {
	var seenErrorFormat, seenErrorLang string
	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !assert.NoError(t, request.ParseForm()) {
			return
		}
		seenErrorFormat = request.Form.Get("errorformat")
		seenErrorLang = request.Form.Get("errorlang")
		_, _ = writer.Write([]byte(`{"query":{"ok":true}}`))
	}))
	defer testServer.Close()

	client, err := NewClient(1000, 0, nil)
	require.NoError(t, err)

	_, err = client.Get(testServer.URL, map[string]string{
		"action":      "query",
		"errorformat": "wikitext",
		"errorlang":   "content",
	})
	require.NoError(t, err)
	require.Equal(t, "wikitext", seenErrorFormat)
	require.Equal(t, "content", seenErrorLang)
}

// TestExtractAPIErrorReadsLocalizedAndLegacyShapes covers both replies: the errors list carries the localized text and
// hides maxlag's lag under "data", while wikis predating errorformat still answer with a single English error object.
func TestExtractAPIErrorReadsLocalizedAndLegacyShapes(t *testing.T) {
	t.Parallel()

	localized := map[string]any{"errors": []any{map[string]any{
		"code": "permissiondenied",
		"text": "N'ho peus ket ar gwirioù ret.",
	}}}
	apiErr := extractAPIError(localized)
	require.NotNil(t, apiErr)
	require.Equal(t, "permissiondenied", apiErr.Code)
	require.Equal(t, "N'ho peus ket ar gwirioù ret.", apiErr.Info)

	// Not every caller asks for formatversion=2, and formatversion=1 delivers the same localized text as "*".
	legacyFormatVersion := map[string]any{"errors": []any{map[string]any{
		"code": "badtoken",
		"*":    "Jeton CSRF non valide.",
	}}}
	apiErr = extractAPIError(legacyFormatVersion)
	require.NotNil(t, apiErr)
	require.Equal(t, "badtoken", apiErr.Code)
	require.Equal(t, "Jeton CSRF non valide.", apiErr.Info)

	lagging := map[string]any{"errors": []any{map[string]any{
		"code": "maxlag",
		"text": "Attente d'un serveur de base de données.",
		"data": map[string]any{"lag": 4.5},
	}}}
	apiErr = extractAPIError(lagging)
	require.NotNil(t, apiErr)
	require.True(t, isRetriableAPIError(apiErr))
	require.InDelta(t, 4.5, apiErr.Lag, 0.001)

	legacy := map[string]any{"error": map[string]any{
		"code": "maxlag",
		"info": "Waiting for a database server.",
		"lag":  2.0,
	}}
	apiErr = extractAPIError(legacy)
	require.NotNil(t, apiErr)
	require.Equal(t, "maxlag", apiErr.Code)
	require.Equal(t, "Waiting for a database server.", apiErr.Info)
	require.InDelta(t, 2.0, apiErr.Lag, 0.001)

	require.Nil(t, extractAPIError(map[string]any{"query": map[string]any{"ok": true}}))
	require.Nil(t, extractAPIError(map[string]any{"errors": []any{}}))
}

type roundTripFunc func(request *http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
