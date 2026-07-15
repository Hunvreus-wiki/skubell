package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/Hunvreus-wiki/skubell/internal/config"
	t "github.com/Hunvreus-wiki/skubell/internal/i18n"
)

// customWikiUnreachableMessage guides the operator through the one URL a custom wiki cannot guess for them.
func customWikiUnreachableMessage() string {
	return t.T(
		"connect_custom_unreachable",
		`Could not reach the MediaWiki API. The URL must point to the directory containing api.php, which depends on the server's configuration. Common locations include the server root (e.g., http://example.com/) and the /w/ subdirectory (e.g., http://example.com/w/). Please verify the URL in wiki settings.`,
	)
}

// ConnectionResult contains outputs of the full connection sequence.
type ConnectionResult struct {
	Username       string
	Capabilities   WikiCapabilities
	ReasonDropdown map[string]ReasonDropdown
	Warnings       []string
}

// Connect performs login + capability detection + reason loading.
func Connect(client *Client, wikiEntry config.WikiEntry) (ConnectionResult, error) {
	return ConnectContext(context.Background(), client, wikiEntry)
}

// ConnectContext performs login + capability detection + reason loading.
func ConnectContext(ctx context.Context, client *Client, wikiEntry config.WikiEntry) (ConnectionResult, error) {
	result := ConnectionResult{
		Warnings: []string{},
	}
	if client == nil {
		return result, errors.New("api client is nil")
	}

	apiURL := strings.TrimSpace(wikiEntry.APIURL)
	if apiURL == "" {
		return result, errors.New("wiki api url is empty")
	}
	if strings.EqualFold(strings.TrimSpace(wikiEntry.Credential), "@keyring") {
		return result, errors.New("wiki credential is unresolved (@keyring marker)")
	}

	if parsed, err := url.Parse(apiURL); err == nil && strings.EqualFold(parsed.Scheme, "http") {
		result.Warnings = append(result.Warnings, "insecure HTTP API URL in use: "+apiURL)
	}

	username, err := LoginContext(ctx, client, apiURL, wikiEntry.Username, wikiEntry.Credential)
	if err != nil {
		if strings.EqualFold(wikiEntry.Farm, "custom") && isReachabilityError(err) {
			return result, fmt.Errorf("%s", customWikiUnreachableMessage())
		}
		return result, fmt.Errorf("connect/login failed: %w", err)
	}
	result.Username = username

	siteInfo, err := FetchSiteInfoContext(ctx, client, apiURL)
	if err != nil {
		return result, fmt.Errorf("connect/siteinfo failed: %w", err)
	}
	userInfo, err := FetchUserInfoContext(ctx, client, apiURL)
	if err != nil {
		return result, fmt.Errorf("connect/userinfo failed: %w", err)
	}

	caps := siteInfo
	caps.UserRights = userInfo.UserRights
	caps.UserGroups = userInfo.UserGroups
	caps.HasHighLimits = userInfo.HasHighLimits
	caps.SitewideBlock = userInfo.SitewideBlock
	caps.BlockReason = userInfo.BlockReason
	caps.BlockExpiry = userInfo.BlockExpiry
	result.Capabilities = caps

	reasons, err := FetchReasonDropdownsContext(ctx, client, apiURL, wikiEntry.AdminLanguage)
	if err != nil {
		return result, fmt.Errorf("connect/reason dropdowns failed: %w", err)
	}
	result.ReasonDropdown = reasons

	return result, nil
}

func isReachabilityError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "connection refused") ||
		strings.Contains(message, "could not resolve host") ||
		strings.Contains(message, "no such host") ||
		strings.Contains(message, "timeout")
}
