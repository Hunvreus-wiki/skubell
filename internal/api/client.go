package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"

	t "github.com/Hunvreus-wiki/skubell/internal/i18n"
	"github.com/Hunvreus-wiki/skubell/internal/version"
)

const (
	defaultWriteThrottle  = 1000 * time.Millisecond
	minimumWriteThrottle  = 200 * time.Millisecond
	defaultMaxRetries     = 3
	defaultBackoffBase    = 100 * time.Millisecond
	defaultRetryAfter     = 60 * time.Second
	defaultRequestTimeout = 30 * time.Second
)

// APIError represents an error returned by the MediaWiki API.
type APIError struct {
	Code      string
	Info      string
	Lag       float64
	Parameter string // multivalue parameter a "toomanyvalues" error names ("" otherwise)
	Limit     int    // value cap the wiki reported alongside "toomanyvalues" (0 otherwise)
}

func (e *APIError) Error() string {
	if e.Info == "" {
		return "mediawiki api error: " + e.Code
	}
	return fmt.Sprintf("mediawiki api error: %s (%s)", e.Code, e.Info)
}

// Client wraps HTTP communications with MediaWiki API.
type Client struct {
	httpClient    *http.Client
	logger        *logrus.Logger
	writeThrottle time.Duration
	maxRetries    int
	userAgent     string

	throttleMu sync.Mutex
	lastWrite  time.Time

	csrfMu    sync.Mutex
	csrfToken string

	warnMu         sync.Mutex
	warnedInsecure map[string]struct{}

	multiValueMu      sync.Mutex
	multiValueDefault int            // rights-derived fallback cap; 0 until SetMultiValueCaps
	multiValueCaps    map[string]int // per-action caps, wiki-discovered and rejection-shrunk; see MultiValueCap()

	sessionMu sync.Mutex       // guards session; never held across network calls
	session   *sessionRecovery // nil until EnableSessionRecovery
	recoverMu sync.Mutex       // serializes recoverSession runs (held across the re-login network calls)
}

// sessionRecovery is what the client needs to transparently re-login when the wiki reports the session gone:
// the credentials of the last successful connect, plus a generation counter so that when several in-flight
// requests see the same session die, only the first re-logs in and the others just retry.
type sessionRecovery struct {
	apiURL     string
	username   string
	credential string
	generation uint64 // bumped on each successful recovery
}

// noRecoveryContextKey marks a context whose requests must not attempt session recovery: the requests
// recoverSession itself makes (login, cap re-discovery) would otherwise recurse into it.
type noRecoveryContextKey struct{}

// NewClient creates an API client with cookie-jar session support.
func NewClient(writeThrottleMS, maxRetries int, logger *logrus.Logger) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}

	throttle := defaultWriteThrottle
	if writeThrottleMS > 0 {
		throttle = time.Duration(writeThrottleMS) * time.Millisecond
	}
	if throttle < minimumWriteThrottle {
		throttle = minimumWriteThrottle
	}

	retries := defaultMaxRetries
	if maxRetries >= 0 {
		retries = maxRetries
	}

	if logger == nil {
		logger = logrus.New()
	}

	return &Client{
		httpClient:     &http.Client{Jar: jar, Timeout: defaultRequestTimeout},
		logger:         logger,
		writeThrottle:  throttle,
		maxRetries:     retries,
		userAgent:      defaultUserAgent(),
		warnedInsecure: map[string]struct{}{},
	}, nil
}

func defaultUserAgent() string {
	return fmt.Sprintf("%s/%s (https://github.com/Hunvreus-wiki/skubell; bot)", version.AppName, version.Number)
}

// defaultMultiValueCap is the multivalue cap assumed before capability detection: the MediaWiki limit for a
// session without apihighlimits, safe against any wiki. highLimitsMultiValueCap is the limit apihighlimits raises
// it to.
const (
	defaultMultiValueCap    = 50
	highLimitsMultiValueCap = 500
)

// MultiValueCap is the number of values one multivalue parameter of the given action (query's titles,
// revisiondelete's ids, …) may carry on this session. Modules may cap their parameters individually, so the
// value is per-action: wiki-discovered at connect (SetMultiValueCaps) and shrunk for the action the moment one
// of its requests is rejected with "toomanyvalues" — the wiki's live answer overrides whatever discovery or
// rights implied. Actions without a discovered or learned cap use the rights-derived default. Batching callers
// must size their chunks with it at call time, not once up front.
func (c *Client) MultiValueCap(action string) int {
	c.multiValueMu.Lock()
	defer c.multiValueMu.Unlock()
	if actionCap, ok := c.multiValueCaps[action]; ok && actionCap > 0 {
		return actionCap
	}
	if c.multiValueDefault > 0 {
		return c.multiValueDefault
	}
	return defaultMultiValueCap
}

// SetMultiValueCaps resets the session's multivalue caps: defaultCap from the detected rights (500 with
// apihighlimits, 50 without) and perAction from wiki discovery (nil is fine). Call it on (re)connect and
// session recovery only; between those the caps move solely via shrinkMultiValueCap.
func (c *Client) SetMultiValueCaps(defaultCap int, perAction map[string]int) {
	c.multiValueMu.Lock()
	defer c.multiValueMu.Unlock()
	c.multiValueDefault = defaultCap
	c.multiValueCaps = maps.Clone(perAction)
	if c.multiValueCaps == nil {
		c.multiValueCaps = map[string]int{}
	}
}

// shrinkMultiValueCap lowers an action's cap to what the wiki just reported; it never raises one.
func (c *Client) shrinkMultiValueCap(action string, limit int) {
	if action == "" || limit <= 0 {
		return
	}
	c.multiValueMu.Lock()
	defer c.multiValueMu.Unlock()
	current := c.multiValueCaps[action]
	if current <= 0 {
		current = c.multiValueDefault
	}
	if current <= 0 {
		current = defaultMultiValueCap
	}
	if limit < current {
		if c.multiValueCaps == nil {
			c.multiValueCaps = map[string]int{}
		}
		c.multiValueCaps[action] = limit
	}
}

// observeAPIError updates session state from any API error the client sees: a multivalue overflow proves the
// wiki's real cap for that action, so every later batched call on it shrinks to the reported limit.
func (c *Client) observeAPIError(action string, apiErr *APIError) {
	if apiErr != nil && apiErr.Code == "toomanyvalues" {
		c.shrinkMultiValueCap(action, apiErr.Limit)
	}
}

// EnableSessionRecovery arms transparent session recovery: from now on every request asserts its login
// (assert=user, exempting the login flow itself) and a session the wiki reports gone is re-established by
// re-running the login flow with these credentials before the failed request is retried. MediaWiki checks the
// assertion before anything else — parameter counts, tokens — so an expired session surfaces as
// "assertuserfailed" instead of masquerading as, say, a multivalue-limit rejection. Call after a successful
// login; Logout disables it.
func (c *Client) EnableSessionRecovery(apiURL, username, credential string) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	generation := uint64(0)
	if c.session != nil {
		generation = c.session.generation
	}
	c.session = &sessionRecovery{apiURL: apiURL, username: username, credential: credential, generation: generation}
}

// DisableSessionRecovery forgets the stored credentials: requests stop asserting a login and a dead session
// is no longer resurrected. Called on logout.
func (c *Client) DisableSessionRecovery() {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	c.session = nil
}

// sessionSnapshot returns a copy of the recovery state and whether recovery is enabled.
func (c *Client) sessionSnapshot() (sessionRecovery, bool) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	if c.session == nil {
		return sessionRecovery{}, false
	}
	return *c.session, true
}

// recoverSession re-establishes a session the wiki reported gone: re-run the login flow with the stored
// credentials, drop the now-stale CSRF token, and re-learn the multivalue cap from the fresh session (a
// disconnection must not leave the cap small once the reconnection restores high limits — see batch.go).
// failedGeneration is the generation the failing request saw: when several requests watch the same session
// die, the first one recovers it and the rest return immediately to retry on the new session.
func (c *Client) recoverSession(ctx context.Context, failedGeneration uint64) error {
	c.recoverMu.Lock()
	defer c.recoverMu.Unlock()

	session, enabled := c.sessionSnapshot()
	if !enabled {
		return errors.New("session recovery not enabled")
	}
	if session.generation != failedGeneration {
		return nil // another request already recovered the session; just retry
	}

	ctx = context.WithValue(ctx, noRecoveryContextKey{}, true)
	if _, err := LoginContext(ctx, c, session.apiURL, session.username, session.credential); err != nil {
		return fmt.Errorf("re-login as %s: %w", session.username, err)
	}
	c.logger.Infof("session expired; re-logged in as %s", session.username)

	c.sessionMu.Lock()
	if c.session != nil {
		c.session.generation++
	}
	c.sessionMu.Unlock()

	// The old session's CSRF token died with it.
	c.csrfMu.Lock()
	c.csrfToken = ""
	c.csrfMu.Unlock()

	// The fresh session's rights decide the caps now: ask the wiki rather than keep what the dying session's
	// answers taught. Keep the current (safe) caps when the wiki cannot be asked.
	if perAction, err := FetchMultiValueCapsContext(ctx, c, session.apiURL); err == nil {
		c.multiValueMu.Lock()
		currentDefault := c.multiValueDefault
		c.multiValueMu.Unlock()
		c.SetMultiValueCaps(currentDefault, perAction)
	} else {
		c.logger.Warnf("could not re-learn multivalue caps after re-login: %v", err)
	}
	return nil
}

// isSessionLostError reports whether the API error means the login session is gone (the assert=user we attach
// to requests failed).
func isSessionLostError(apiErr *APIError) bool {
	return apiErr.Code == "assertuserfailed" || apiErr.Code == "assertnameduserfailed"
}

// isLoginFlowParams reports whether the request is part of the login/logout flow itself, which must run
// without a login assertion: it is establishing (or ending) the very session the assertion would test.
func isLoginFlowParams(params map[string]string) bool {
	switch params["action"] {
	case "login", "logout", "clientlogin":
		return true
	case "query":
		return params["meta"] == "tokens" && strings.Contains(params["type"], "login")
	default:
		return false
	}
}

// Get executes a GET request against api.php and decodes the JSON result.
func (c *Client) Get(apiURL string, params map[string]string) (map[string]any, error) {
	return c.GetContext(context.Background(), apiURL, params)
}

// GetContext executes a GET request against api.php and decodes the JSON result.
func (c *Client) GetContext(ctx context.Context, apiURL string, params map[string]string) (map[string]any, error) {
	return c.request(ctx, "GET", apiURL, params)
}

// Post executes a POST request against api.php and decodes the JSON result.
func (c *Client) Post(apiURL string, params map[string]string) (map[string]any, error) {
	return c.PostContext(context.Background(), apiURL, params)
}

// PostContext executes a POST request against api.php and decodes the JSON result.
func (c *Client) PostContext(ctx context.Context, apiURL string, params map[string]string) (map[string]any, error) {
	return c.request(ctx, "POST", apiURL, params)
}

// GetCSRFToken returns a cached token or fetches a fresh one when missing.
func GetCSRFToken(client *Client, apiURL string) (string, error) {
	if client == nil {
		return "", errors.New("api client is nil")
	}
	return client.getCSRFTokenContext(context.Background(), apiURL, false)
}

func (c *Client) request(ctx context.Context, method, apiURL string, params map[string]string) (map[string]any, error) {
	ctx, cancel := withDefaultRequestTimeout(ctx, defaultRequestTimeout)
	defer cancel()

	normalizedMethod := strings.ToUpper(method)
	if normalizedMethod != http.MethodGet && normalizedMethod != http.MethodPost {
		return nil, fmt.Errorf("unsupported method: %s", method)
	}

	workingParams := cloneParams(params)
	if _, ok := workingParams["format"]; !ok {
		workingParams["format"] = "json"
	}
	// MediaWiki's default error format ("bc") always answers in English and ignores errorlang; any other format honours
	// it, so ask for tag-free text in the language the operator reads. An unknown code degrades to English server-side.
	if _, ok := workingParams["errorformat"]; !ok {
		workingParams["errorformat"] = "plaintext"
	}
	if _, ok := workingParams["errorlang"]; !ok {
		workingParams["errorlang"] = t.CurrentLanguage()
	}

	// With recovery armed, every request asserts its login so a dead session fails fast and unambiguously
	// (assertuserfailed) instead of as whatever check the anonymous request trips first.
	session, recoveryEnabled := c.sessionSnapshot()
	if recoveryEnabled && !isLoginFlowParams(workingParams) {
		if _, ok := workingParams["assert"]; !ok {
			workingParams["assert"] = "user"
		}
	}

	c.warnIfInsecure(apiURL)

	badTokenRefreshed := false
	sessionRecovered := false
	attempts := c.maxRetries + 1
	for attempt := range attempts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if normalizedMethod == http.MethodPost {
			if err := c.applyWriteThrottle(ctx); err != nil {
				return nil, err
			}
		}

		responseBody, statusCode, headers, err := c.execute(ctx, normalizedMethod, apiURL, workingParams)
		if normalizedMethod == http.MethodPost && err == nil {
			c.recordWriteTime()
		}
		if err != nil {
			if attempt < c.maxRetries && isTransientNetworkError(err) {
				if err := sleepContext(ctx, backoffDuration(attempt)); err != nil {
					return nil, err
				}
				continue
			}
			return nil, err
		}

		if statusCode == http.StatusTooManyRequests {
			if attempt >= c.maxRetries {
				return nil, errors.New("http status 429 after retries")
			}
			if err := sleepContext(ctx, parseRetryAfter(headers.Get("Retry-After"))); err != nil {
				return nil, err
			}
			continue
		}

		if statusCode >= http.StatusBadRequest {
			return nil, fmt.Errorf("http status %d", statusCode)
		}

		apiErr := extractAPIError(responseBody)
		c.observeAPIError(workingParams["action"], apiErr)
		if apiErr != nil {
			if isSessionLostError(apiErr) && recoveryEnabled && !sessionRecovered &&
				ctx.Value(noRecoveryContextKey{}) == nil {
				if recoverErr := c.recoverSession(ctx, session.generation); recoverErr != nil {
					return nil, fmt.Errorf("session lost and recovery failed: %w", recoverErr)
				}
				sessionRecovered = true
				continue
			}

			if normalizedMethod == http.MethodPost && apiErr.Code == "badtoken" && !badTokenRefreshed &&
				shouldAttachCSRF(workingParams) {
				if _, refreshErr := c.getCSRFTokenContext(ctx, apiURL, true); refreshErr != nil {
					return nil, refreshErr
				}
				badTokenRefreshed = true
				continue
			}

			if attempt < c.maxRetries && isRetriableAPIError(apiErr) {
				if err := sleepContext(ctx, apiRetryDelay(apiErr, attempt)); err != nil {
					return nil, err
				}
				continue
			}

			return nil, apiErr
		}

		return responseBody, nil
	}

	return nil, errors.New("request retries exhausted")
}

func (c *Client) execute(
	ctx context.Context,
	method, apiURL string,
	params map[string]string,
) (map[string]any, int, http.Header, error) {
	requestParams := cloneParams(params)

	if method == http.MethodPost && shouldAttachCSRF(requestParams) {
		token, err := c.getCSRFTokenContext(ctx, apiURL, false)
		if err != nil {
			return nil, 0, nil, err
		}
		requestParams["token"] = token
	}

	request, err := buildRequest(ctx, method, apiURL, requestParams)
	if err != nil {
		return nil, 0, nil, err
	}
	if strings.TrimSpace(c.userAgent) != "" {
		request.Header.Set("User-Agent", c.userAgent)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, 0, nil, err
	}
	defer func() {
		_ = response.Body.Close()
	}()

	parsed, err := decodeResponse(response.Body)
	if err != nil {
		return nil, response.StatusCode, response.Header, err
	}

	return parsed, response.StatusCode, response.Header, nil
}

func buildRequest(ctx context.Context, method, apiURL string, params map[string]string) (*http.Request, error) {
	parsedURL, err := url.Parse(apiURL)
	if err != nil {
		return nil, fmt.Errorf("parse api url: %w", err)
	}

	if method == http.MethodGet {
		query := parsedURL.Query()
		for key, value := range params {
			query.Set(key, value)
		}
		parsedURL.RawQuery = query.Encode()
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
		if requestErr != nil {
			return nil, fmt.Errorf("build get request: %w", requestErr)
		}
		return request, nil
	}

	form := url.Values{}
	for key, value := range params {
		form.Set(key, value)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		parsedURL.String(),
		bytes.NewBufferString(form.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("build post request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return request, nil
}

func decodeResponse(body io.Reader) (map[string]any, error) {
	decoder := json.NewDecoder(body)
	result := map[string]any{}
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode json response: %w", err)
	}
	return result, nil
}

func (c *Client) applyWriteThrottle(ctx context.Context) error {
	c.throttleMu.Lock()
	defer c.throttleMu.Unlock()

	if c.lastWrite.IsZero() {
		return nil
	}

	nextAllowed := c.lastWrite.Add(c.writeThrottle)
	if delay := time.Until(nextAllowed); delay > 0 {
		if err := sleepContext(ctx, delay); err != nil {
			return err
		}
	}
	c.lastWrite = time.Now()
	return nil
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func withDefaultRequestTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return context.WithTimeout(context.Background(), timeout)
	}
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func (c *Client) recordWriteTime() {
	c.throttleMu.Lock()
	c.lastWrite = time.Now()
	c.throttleMu.Unlock()
}

func shouldAttachCSRF(params map[string]string) bool {
	if _, hasToken := params["token"]; hasToken {
		return false
	}
	return params["action"] != "login"
}

// extractAPIError reads the failure out of an API response, accepting both shapes: the "errors" list returned for the
// errorformat we request (its text is localized), and the legacy single "error" object, which wikis older than the
// errorformat parameter still reply with. Returns nil when the response reports no error.
func extractAPIError(payload map[string]any) *APIError {
	if apiErr := extractLocalizedAPIError(payload); apiErr != nil {
		return apiErr
	}
	return extractLegacyAPIError(payload)
}

// extractLocalizedAPIError parses the errorformat shape: a list of errors carrying localized "text", with machine
// details such as maxlag's lag moved under "data". Only the first entry is reported; it is the one that failed the
// request.
func extractLocalizedAPIError(payload map[string]any) *APIError {
	errorList, ok := payload["errors"].([]any)
	if !ok || len(errorList) == 0 {
		return nil
	}
	errorData, ok := errorList[0].(map[string]any)
	if !ok {
		return nil
	}

	apiErr := &APIError{}
	if code, ok := errorData["code"].(string); ok {
		apiErr.Code = code
	}
	// formatversion=2 names the localized text "text"; under formatversion=1 the same text arrives as "*".
	if text, ok := errorData["text"].(string); ok {
		apiErr.Info = text
	}
	if apiErr.Info == "" {
		apiErr.Info, _ = errorData["*"].(string)
	}
	if data, ok := errorData["data"].(map[string]any); ok {
		if lag, ok := data["lag"].(float64); ok {
			apiErr.Lag = lag
		}
		fillMultiValueDetails(apiErr, data)
	}
	return apiErr
}

// fillMultiValueDetails reads the multivalue-overflow details ("toomanyvalues") out of the error's machine data:
// under errorformat=plaintext they sit in the "data" object, in the legacy shape directly in the error object.
func fillMultiValueDetails(apiErr *APIError, data map[string]any) {
	if parameter, ok := data["parameter"].(string); ok {
		apiErr.Parameter = parameter
	}
	if limit, ok := data["limit"].(float64); ok {
		apiErr.Limit = int(limit)
	}
}

func extractLegacyAPIError(payload map[string]any) *APIError {
	errorData, ok := payload["error"].(map[string]any)
	if !ok {
		return nil
	}

	apiErr := &APIError{}
	if code, ok := errorData["code"].(string); ok {
		apiErr.Code = code
	}
	if info, ok := errorData["info"].(string); ok {
		apiErr.Info = info
	}
	if lag, ok := errorData["lag"].(float64); ok {
		apiErr.Lag = lag
	}
	fillMultiValueDetails(apiErr, errorData)
	return apiErr
}

func isRetriableAPIError(apiErr *APIError) bool {
	if apiErr == nil {
		return false
	}
	switch apiErr.Code {
	case "maxlag", "ratelimited":
		return true
	default:
		return false
	}
}

func apiRetryDelay(apiErr *APIError, attempt int) time.Duration {
	if apiErr != nil {
		switch apiErr.Code {
		case "maxlag":
			if apiErr.Lag > 0 {
				return time.Duration(apiErr.Lag * float64(time.Second))
			}
			return time.Second
		case "ratelimited":
			return defaultRetryAfter
		}
	}
	return backoffDuration(attempt)
}

func backoffDuration(attempt int) time.Duration {
	scale := math.Pow(2, float64(attempt))
	delay := time.Duration(float64(defaultBackoffBase) * scale)
	if delay > 2*time.Second {
		return 2 * time.Second
	}
	return delay
}

func isTransientNetworkError(err error) bool {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return isTransientNetworkError(urlErr.Err)
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	if errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "connection reset") ||
		strings.Contains(message, "connection refused") ||
		strings.Contains(message, "broken pipe") ||
		strings.Contains(message, "timeout")
}

func parseRetryAfter(value string) time.Duration {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return defaultRetryAfter
	}

	seconds, err := strconv.Atoi(trimmed)
	if err == nil {
		if seconds < 0 {
			return defaultRetryAfter
		}
		return time.Duration(seconds) * time.Second
	}

	dateValue, err := http.ParseTime(trimmed)
	if err != nil {
		return defaultRetryAfter
	}

	delay := time.Until(dateValue)
	if delay < 0 {
		return 0
	}
	return delay
}

func cloneParams(params map[string]string) map[string]string {
	cloned := map[string]string{}
	maps.Copy(cloned, params)
	return cloned
}

func (c *Client) warnIfInsecure(apiURL string) {
	parsedURL, err := url.Parse(apiURL)
	if err != nil {
		return
	}
	if !strings.EqualFold(parsedURL.Scheme, "http") {
		return
	}

	c.warnMu.Lock()
	defer c.warnMu.Unlock()

	if _, exists := c.warnedInsecure[apiURL]; exists {
		return
	}
	c.warnedInsecure[apiURL] = struct{}{}
	c.logger.Warnf("insecure HTTP API URL in use: %s", apiURL)
}

func (c *Client) getCSRFTokenContext(ctx context.Context, apiURL string, forceRefresh bool) (string, error) {
	c.csrfMu.Lock()
	if c.csrfToken != "" && !forceRefresh {
		token := c.csrfToken
		c.csrfMu.Unlock()
		return token, nil
	}
	c.csrfMu.Unlock()

	response, err := c.request(ctx, http.MethodGet, apiURL, map[string]string{
		"action": "query",
		"meta":   "tokens",
		"type":   "csrf",
	})
	if err != nil {
		return "", err
	}

	query, ok := response["query"].(map[string]any)
	if !ok {
		return "", errors.New("missing query in csrf response")
	}
	tokens, ok := query["tokens"].(map[string]any)
	if !ok {
		return "", errors.New("missing tokens in csrf response")
	}
	token, ok := tokens["csrftoken"].(string)
	if !ok || token == "" {
		return "", errors.New("missing csrftoken in csrf response")
	}

	c.csrfMu.Lock()
	c.csrfToken = token
	c.csrfMu.Unlock()
	return token, nil
}
