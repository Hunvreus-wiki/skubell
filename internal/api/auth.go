package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrAuthenticationFailed = errors.New("authentication failed")
	ErrBotPasswordsDisabled = errors.New("bot passwords are disabled")
)

// Login performs the MediaWiki two-step Bot Password login flow.
func Login(client *Client, apiURL, username, password string) (string, error) {
	return LoginContext(context.Background(), client, apiURL, username, password)
}

// LoginContext performs the MediaWiki two-step Bot Password login flow.
func LoginContext(ctx context.Context, client *Client, apiURL, username, password string) (string, error) {
	if client == nil {
		return "", errors.New("api client is nil")
	}

	loginTokenResponse, err := client.GetContext(ctx, apiURL, map[string]string{
		"action": "query",
		"meta":   "tokens",
		"type":   "login",
	})
	if err != nil {
		return "", fmt.Errorf("fetch login token: %w", err)
	}

	query, ok := loginTokenResponse["query"].(map[string]any)
	if !ok {
		return "", errors.New("missing query in login token response")
	}
	tokens, ok := query["tokens"].(map[string]any)
	if !ok {
		return "", errors.New("missing tokens in login token response")
	}
	loginToken, ok := tokens["logintoken"].(string)
	if !ok || loginToken == "" {
		return "", errors.New("missing login token in response")
	}

	loginResponse, err := client.PostContext(ctx, apiURL, map[string]string{
		"action":     "login",
		"lgname":     username,
		"lgpassword": password,
		"lgtoken":    loginToken,
	})
	if err != nil {
		return "", fmt.Errorf("execute login request: %w", err)
	}

	loginData, ok := loginResponse["login"].(map[string]any)
	if !ok {
		return "", errors.New("missing login field in response")
	}

	result, _ := loginData["result"].(string)
	switch result {
	case "Success":
		if authenticatedUser, ok := loginData["lgusername"].(string); ok && authenticatedUser != "" {
			return authenticatedUser, nil
		}
		return username, nil
	default:
		reason, _ := loginData["reason"].(string)
		if isBotPasswordDisabledReason(reason) {
			return "", fmt.Errorf("%w: %s", ErrBotPasswordsDisabled, reason)
		}
		if reason == "" {
			reason = "unknown login failure"
		}
		return "", fmt.Errorf("%w: %s", ErrAuthenticationFailed, reason)
	}
}

func isBotPasswordDisabledReason(reason string) bool {
	normalized := strings.ToLower(reason)
	return strings.Contains(normalized, "bot password") && strings.Contains(normalized, "disabled")
}

// Logout ends the MediaWiki session if possible.
func Logout(client *Client, apiURL string) error {
	return LogoutContext(context.Background(), client, apiURL)
}

// LogoutContext ends the MediaWiki session if possible.
func LogoutContext(ctx context.Context, client *Client, apiURL string) error {
	if client == nil {
		return errors.New("api client is nil")
	}

	// An intentional disconnect: stop asserting the login and never resurrect this session.
	client.DisableSessionRecovery()
	_, err := client.PostContext(ctx, apiURL, map[string]string{
		"action": "logout",
	})
	if err != nil {
		return fmt.Errorf("logout failed: %w", err)
	}
	return nil
}
