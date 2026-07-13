package api

import "strings"

const (
	// API error codes MediaWiki returns when a delete is denied for lack of the matching interface/site-config right.
	ProtectedNamespaceInterfaceErrorCode = "protectednamespace-interface"
	SiteCSSProtectedErrorCode            = "sitecssprotected"
	SiteJSProtectedErrorCode             = "sitejsprotected"
	SiteJSONProtectedErrorCode           = "sitejsonprotected"

	MediaWikiNamespaceDeleteGrantMessage = "Skubell cannot delete pages in the MediaWiki namespace with this bot password. In Special:BotPasswords, enable \"Edit the MediaWiki namespace and sitewide/user JSON\", then reconnect Skubell."
	SiteCSSDeleteGrantMessage            = "Skubell cannot delete sitewide CSS pages (MediaWiki:*.css) with this session: it lacks the \"editsitecss\" right. That right belongs to interface administrators; add your account to that group and enable the bot password's sitewide CSS/JS grant, then reconnect Skubell."
	SiteJSDeleteGrantMessage             = "Skubell cannot delete sitewide JavaScript pages (MediaWiki:*.js) with this session: it lacks the \"editsitejs\" right. That right belongs to interface administrators; add your account to that group and enable the bot password's sitewide CSS/JS grant, then reconnect Skubell."
	SiteJSONDeleteGrantMessage           = "Skubell cannot delete sitewide JSON pages (MediaWiki:*.json) with this session: it lacks the \"editsitejson\" right. Enable the bot password's JSON grant (and ensure your account holds \"editsitejson\"), then reconnect Skubell."

	MediaWikiNamespaceProtectGrantMessage = "Skubell cannot protect pages in the MediaWiki namespace with this session: it lacks the \"editinterface\" right. In Special:BotPasswords, enable \"Edit the MediaWiki namespace and sitewide/user JSON\" (and ensure your account holds it), then reconnect Skubell."
	SiteCSSProtectGrantMessage            = "Skubell cannot protect sitewide CSS pages (MediaWiki:*.css) with this session: it lacks the \"editsitecss\" right. That right belongs to interface administrators; add your account to that group and enable the bot password's sitewide CSS/JS grant, then reconnect Skubell."
	SiteJSProtectGrantMessage             = "Skubell cannot protect sitewide JavaScript pages (MediaWiki:*.js) with this session: it lacks the \"editsitejs\" right. That right belongs to interface administrators; add your account to that group and enable the bot password's sitewide CSS/JS grant, then reconnect Skubell."
	SiteJSONProtectGrantMessage           = "Skubell cannot protect sitewide JSON pages (MediaWiki:*.json) with this session: it lacks the \"editsitejson\" right. Enable the bot password's JSON grant (and ensure your account holds \"editsitejson\"), then reconnect Skubell."
)

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

// RequiredMediaWikiNamespaceRights returns the rights, beyond the base action right, that MediaWiki requires to act on
// the page at title, derived from its namespace and content-model suffix. The same rights gate both **deleting** and
// **protecting** a page: every MediaWiki-namespace page needs "editinterface" ($wgNamespaceProtection), and .css/.js/
// .json pages additionally need editsitecss/editsitejs/editsitejson (PermissionManager::checkSiteConfigPermissions).
// User-space .css/.js/.json pages are deliberately not gated for these actions (checkUserConfigPermissions is skipped),
// and .css/.js elsewhere (e.g. User_talk) is plain wikitext, so both are omitted here.
func RequiredMediaWikiNamespaceRights(caps WikiCapabilities, title string) []string {
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

// RequiredDeleteRights is RequiredMediaWikiNamespaceRights for the delete action.
func RequiredDeleteRights(caps WikiCapabilities, title string) []string {
	return RequiredMediaWikiNamespaceRights(caps, title)
}

// firstMissingRight returns the first of rights the session lacks, or "" when it holds them all.
func firstMissingRight(caps WikiCapabilities, rights []string) string {
	for _, right := range rights {
		if !HasRight(caps, right) {
			return right
		}
	}
	return ""
}

// MissingDeleteRight returns the first right needed to delete title that the session lacks, or "".
func MissingDeleteRight(caps WikiCapabilities, title string) string {
	return firstMissingRight(caps, RequiredMediaWikiNamespaceRights(caps, title))
}

// MissingProtectRight returns the first right needed to protect title that the session lacks, or "". Protecting a page
// requires the same MediaWiki-namespace/site-config rights as deleting it (you must be able to edit the page to protect
// it), so the two share RequiredMediaWikiNamespaceRights.
func MissingProtectRight(caps WikiCapabilities, title string) string {
	return firstMissingRight(caps, RequiredMediaWikiNamespaceRights(caps, title))
}

// DeleteAccessMessage returns a user-facing message explaining why this session cannot delete title, or "" when it can.
func DeleteAccessMessage(caps WikiCapabilities, title string) string {
	return deleteRightMessage(MissingDeleteRight(caps, title))
}

// ProtectAccessMessage returns a user-facing message explaining why this session cannot protect title, or "" when it can.
func ProtectAccessMessage(caps WikiCapabilities, title string) string {
	return protectRightMessage(MissingProtectRight(caps, title))
}

// deleteRightMessage maps a missing delete right to its user-facing message; unknown or empty yields "".
func deleteRightMessage(right string) string {
	switch right {
	case RightEditInterface:
		return MediaWikiNamespaceDeleteGrantMessage
	case RightEditSiteCSS:
		return SiteCSSDeleteGrantMessage
	case RightEditSiteJS:
		return SiteJSDeleteGrantMessage
	case RightEditSiteJSON:
		return SiteJSONDeleteGrantMessage
	default:
		return ""
	}
}

// protectRightMessage maps a missing protect right to its user-facing message; unknown or empty yields "".
func protectRightMessage(right string) string {
	switch right {
	case RightEditInterface:
		return MediaWikiNamespaceProtectGrantMessage
	case RightEditSiteCSS:
		return SiteCSSProtectGrantMessage
	case RightEditSiteJS:
		return SiteJSProtectGrantMessage
	case RightEditSiteJSON:
		return SiteJSONProtectGrantMessage
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
		return MediaWikiNamespaceDeleteGrantMessage
	case strings.EqualFold(apiErr.Code, SiteCSSProtectedErrorCode):
		return SiteCSSDeleteGrantMessage
	case strings.EqualFold(apiErr.Code, SiteJSProtectedErrorCode):
		return SiteJSDeleteGrantMessage
	case strings.EqualFold(apiErr.Code, SiteJSONProtectedErrorCode):
		return SiteJSONDeleteGrantMessage
	}
	if strings.TrimSpace(apiErr.Info) != "" {
		return apiErr.Info
	}
	return apiErr.Code
}
