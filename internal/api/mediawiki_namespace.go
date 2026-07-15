package api

import (
	"strings"

	t "github.com/Hunvreus-wiki/skubell/internal/i18n"
)

const (
	// API error codes MediaWiki returns when a delete is denied for lack of the matching interface/site-config right.
	ProtectedNamespaceInterfaceErrorCode = "protectednamespace-interface"
	SiteCSSProtectedErrorCode            = "sitecssprotected"
	SiteJSProtectedErrorCode             = "sitejsprotected"
	SiteJSONProtectedErrorCode           = "sitejsonprotected"
)

// The messages below name MediaWiki's own labels (the bot-password grant, the interface-administrator group), so each
// translation quotes the wording that wiki shows rather than a literal rendering of the English. Right names such as
// "editsitecss" are identifiers and stay untranslated, as does the canonical Special:BotPasswords, which resolves on a
// wiki in any language.

// MediaWikiNamespaceDeleteGrantMessage names the bot-password grant that unlocks deleting in the MediaWiki namespace.
func MediaWikiNamespaceDeleteGrantMessage() string {
	return t.T(
		"del_grant_mediawiki_namespace",
		`Skubell cannot delete pages in the MediaWiki namespace with this bot password. In Special:BotPasswords, enable "Edit the MediaWiki namespace and sitewide/user JSON", then reconnect Skubell.`,
	)
}

// SiteCSSDeleteGrantMessage explains that deleting sitewide CSS needs both the editsitecss right and the CSS/JS grant.
func SiteCSSDeleteGrantMessage() string {
	return t.T(
		"del_grant_site_css",
		`Skubell cannot delete sitewide CSS pages (MediaWiki:*.css) with this session: it lacks the "editsitecss" right. That right belongs to interface administrators; add your account to that group and enable the bot password's sitewide CSS/JS grant, then reconnect Skubell.`,
	)
}

// SiteJSDeleteGrantMessage explains that deleting sitewide JavaScript needs the editsitejs right and the CSS/JS grant.
func SiteJSDeleteGrantMessage() string {
	return t.T(
		"del_grant_site_js",
		`Skubell cannot delete sitewide JavaScript pages (MediaWiki:*.js) with this session: it lacks the "editsitejs" right. That right belongs to interface administrators; add your account to that group and enable the bot password's sitewide CSS/JS grant, then reconnect Skubell.`,
	)
}

// SiteJSONDeleteGrantMessage explains that deleting sitewide JSON needs the editsitejson right and the JSON grant.
func SiteJSONDeleteGrantMessage() string {
	return t.T(
		"del_grant_site_json",
		`Skubell cannot delete sitewide JSON pages (MediaWiki:*.json) with this session: it lacks the "editsitejson" right. Enable the bot password's JSON grant (and ensure your account holds "editsitejson"), then reconnect Skubell.`,
	)
}

// HasRight reports whether the connected user session has an effective right.
func HasRight(caps WikiCapabilities, right string) bool {
	for _, current := range caps.UserRights {
		if strings.EqualFold(strings.TrimSpace(current), strings.TrimSpace(right)) {
			return true
		}
	}
	return false
}

// CanDeleteMediaWikiNamespace reports whether this session has the grant/right needed for namespace 8.
func CanDeleteMediaWikiNamespace(caps WikiCapabilities) bool {
	return HasRight(caps, RightEditInterface)
}

// RequiredDeleteRights returns the rights, beyond the base "delete" right, that MediaWiki requires to delete the page at
// title, derived from its namespace and content-model suffix.
//
// Only sitewide config pages (the MediaWiki namespace) gate deletion: every MediaWiki-namespace page needs
// "editinterface" ($wgNamespaceProtection), and .css/.js/.json pages also need editsitecss/editsitejs/editsitejson
// (PermissionManager::checkSiteConfigPermissions). User-space .css/.js/.json pages are deliberately not gated on delete
// (checkUserConfigPermissions is skipped for that action), and .css/.js elsewhere (e.g. User_talk) is plain wikitext, so
// both are omitted here.
func RequiredDeleteRights(caps WikiCapabilities, title string) []string {
	if !IsMediaWikiNamespaceTitle(caps, title) {
		return nil
	}
	rights := []string{RightEditInterface}
	switch {
	case strings.HasSuffix(title, ".css"):
		rights = append(rights, RightEditSiteCSS)
	case strings.HasSuffix(title, ".json"):
		rights = append(rights, RightEditSiteJSON)
	case strings.HasSuffix(title, ".js"):
		rights = append(rights, RightEditSiteJS)
	}
	return rights
}

// MissingDeleteRight returns the first right RequiredDeleteRights reports for title that the session lacks, or "" when
// the session can delete it.
func MissingDeleteRight(caps WikiCapabilities, title string) string {
	for _, right := range RequiredDeleteRights(caps, title) {
		if !HasRight(caps, right) {
			return right
		}
	}
	return ""
}

// DeleteAccessMessage returns a user-facing message explaining why this session cannot delete title, or "" when it can.
func DeleteAccessMessage(caps WikiCapabilities, title string) string {
	return deleteRightMessage(MissingDeleteRight(caps, title))
}

// deleteRightMessage maps a missing delete right to its user-facing message; unknown or empty yields "".
func deleteRightMessage(right string) string {
	switch right {
	case RightEditInterface:
		return MediaWikiNamespaceDeleteGrantMessage()
	case RightEditSiteCSS:
		return SiteCSSDeleteGrantMessage()
	case RightEditSiteJS:
		return SiteJSDeleteGrantMessage()
	case RightEditSiteJSON:
		return SiteJSONDeleteGrantMessage()
	default:
		return ""
	}
}

// IsMediaWikiNamespaceTitle reports whether title is in namespace 8.
func IsMediaWikiNamespaceTitle(caps WikiCapabilities, title string) bool {
	return TitleHasNamespace(caps, title, MediaWikiNamespaceID, "MediaWiki")
}

// IsProtectedNamespaceInterfaceError reports whether MediaWiki denied access to interface pages.
func IsProtectedNamespaceInterfaceError(apiErr *APIError) bool {
	return apiErr != nil && strings.EqualFold(apiErr.Code, ProtectedNamespaceInterfaceErrorCode)
}

// FriendlyErrorMessage returns a user-facing message for known API errors.
func FriendlyErrorMessage(apiErr *APIError) string {
	if apiErr == nil {
		return ""
	}
	switch {
	case IsProtectedNamespaceInterfaceError(apiErr):
		return MediaWikiNamespaceDeleteGrantMessage()
	case strings.EqualFold(apiErr.Code, SiteCSSProtectedErrorCode):
		return SiteCSSDeleteGrantMessage()
	case strings.EqualFold(apiErr.Code, SiteJSProtectedErrorCode):
		return SiteJSDeleteGrantMessage()
	case strings.EqualFold(apiErr.Code, SiteJSONProtectedErrorCode):
		return SiteJSONDeleteGrantMessage()
	}
	if strings.TrimSpace(apiErr.Info) != "" {
		return apiErr.Info
	}
	return apiErr.Code
}
