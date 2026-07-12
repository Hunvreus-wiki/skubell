package api

import "strings"

const (
	MediaWikiNamespaceID = 8
	RightEditInterface   = "editinterface"
	RightEditSiteCSS     = "editsitecss"
	RightEditSiteJS      = "editsitejs"
	RightEditSiteJSON    = "editsitejson"
)

// TitleHasNamespace reports whether title uses a known prefix for namespaceID.
func TitleHasNamespace(caps WikiCapabilities, title string, namespaceID int, fallback string) bool {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return false
	}
	for _, prefix := range NamespacePrefixes(caps, namespaceID, fallback) {
		if hasNamespacePrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

// NormalizeTitleForNamespace adds the preferred namespace prefix when title has no matching prefix.
func NormalizeTitleForNamespace(caps WikiCapabilities, title string, namespaceID int, fallback string) string {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return ""
	}
	for _, prefix := range NamespacePrefixes(caps, namespaceID, fallback) {
		if hasNamespacePrefix(trimmed, prefix) {
			return trimmed
		}
	}
	return PreferredNamespacePrefix(caps, namespaceID, fallback) + ":" + trimmed
}

// NamespacePrefixes returns the local namespace name, aliases, and fallback prefix.
func NamespacePrefixes(caps WikiCapabilities, namespaceID int, fallback string) []string {
	prefixes := []string{}
	if name := strings.TrimSpace(caps.Namespaces[namespaceID]); name != "" {
		prefixes = append(prefixes, name)
	}
	for _, alias := range caps.NamespaceAliases[namespaceID] {
		if !containsFold(prefixes, alias) {
			prefixes = append(prefixes, alias)
		}
	}
	if strings.TrimSpace(fallback) != "" && !containsFold(prefixes, fallback) {
		prefixes = append(prefixes, fallback)
	}
	return prefixes
}

// PreferredNamespacePrefix returns the wiki's primary namespace prefix or fallback.
func PreferredNamespacePrefix(caps WikiCapabilities, namespaceID int, fallback string) string {
	prefixes := NamespacePrefixes(caps, namespaceID, fallback)
	if len(prefixes) == 0 {
		return fallback
	}
	return prefixes[0]
}

// SplitKnownNamespace splits a title using the namespace names known from siteinfo.
func SplitKnownNamespace(caps WikiCapabilities, title string) (int, string, bool) {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return 0, "", false
	}
	parts := strings.SplitN(trimmed, ":", 2)
	if len(parts) != 2 {
		return 0, trimmed, false
	}
	prefix := strings.TrimSpace(parts[0])
	remainder := strings.TrimSpace(parts[1])
	for namespaceID := range caps.Namespaces {
		for _, candidate := range NamespacePrefixes(caps, namespaceID, "") {
			if strings.EqualFold(prefix, strings.TrimSpace(candidate)) {
				return namespaceID, remainder, true
			}
		}
	}
	return 0, trimmed, false
}

func hasNamespacePrefix(title, prefix string) bool {
	title = strings.TrimSpace(title)
	prefix = strings.TrimSpace(prefix)
	if title == "" || prefix == "" {
		return false
	}
	return strings.HasPrefix(strings.ToLower(title), strings.ToLower(prefix)+":")
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}
