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
	Code string
	Info string
	Lag  float64
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
}

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

	c.warnIfInsecure(apiURL)

	badTokenRefreshed := false
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
		if apiErr != nil {
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
	}
	return apiErr
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
