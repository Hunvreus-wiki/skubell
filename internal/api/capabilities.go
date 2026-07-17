package api

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var versionPattern = regexp.MustCompile(`(?i)^MediaWiki\s+(\d+)\.(\d+)(?:\.(\d+))?.*$`)

// minSupportedMajMin is the oldest MediaWiki version Skubell connects to. It
// tracks the current LTS (1.43, EOL Dec 2027); 1.39 reached end-of-life.
var minSupportedMajMin = [2]int{1, 43}

// ErrUnsupportedMediaWikiVersion indicates that the wiki version is below supported minimum.
type ErrUnsupportedMediaWikiVersion struct {
	Version string
}

func (e *ErrUnsupportedMediaWikiVersion) Error() string {
	return fmt.Sprintf(
		"unsupported MediaWiki version %q (minimum supported is %d.%d)",
		e.Version, minSupportedMajMin[0], minSupportedMajMin[1],
	)
}

// FetchSiteInfo loads and parses site capabilities from meta=siteinfo.
func FetchSiteInfo(client *Client, apiURL string) (WikiCapabilities, error) {
	return FetchSiteInfoContext(context.Background(), client, apiURL)
}

// FetchSiteInfoContext loads and parses site capabilities from meta=siteinfo.
func FetchSiteInfoContext(ctx context.Context, client *Client, apiURL string) (WikiCapabilities, error) {
	response, err := client.GetContext(ctx, apiURL, map[string]string{
		"action":        "query",
		"meta":          "siteinfo",
		"siprop":        "general|namespaces|namespacealiases|extensions|statistics|restrictions",
		"formatversion": "2",
	})
	if err != nil {
		return WikiCapabilities{}, fmt.Errorf("fetch siteinfo: %w", err)
	}

	caps, err := parseSiteInfoResponse(response)
	if err != nil {
		return WikiCapabilities{}, err
	}
	return caps, nil
}

func parseSiteInfoResponse(response map[string]any) (WikiCapabilities, error) {
	query, ok := response["query"].(map[string]any)
	if !ok {
		return WikiCapabilities{}, errors.New("missing query field in siteinfo response")
	}

	general, ok := query["general"].(map[string]any)
	if !ok {
		return WikiCapabilities{}, errors.New("missing query.general in siteinfo response")
	}

	generator, _ := general["generator"].(string)
	version, majMin, err := parseMediaWikiVersion(generator)
	if err != nil {
		return WikiCapabilities{}, err
	}
	if compareMajMin(majMin, minSupportedMajMin) < 0 {
		return WikiCapabilities{}, &ErrUnsupportedMediaWikiVersion{Version: version}
	}

	namespaces := map[int]string{}
	namespaceAliases := map[int][]string{}
	switch raw := query["namespaces"].(type) {
	case []any:
		for _, entry := range raw {
			namespace, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			id, ok := extractNamespaceID(namespace)
			if !ok {
				continue
			}
			name := extractNamespaceName(namespace)
			namespaces[id] = name
			namespaceAliases[id] = mergeNamespaceAliases(
				namespaceAliases[id],
				extractNamespaceAliases(namespace, name)...)
		}
	case map[string]any:
		for key, value := range raw {
			namespace, ok := value.(map[string]any)
			if !ok {
				continue
			}
			id, err := strconv.Atoi(key)
			if err != nil {
				continue
			}
			name := extractNamespaceName(namespace)
			namespaces[id] = name
			namespaceAliases[id] = mergeNamespaceAliases(
				namespaceAliases[id],
				extractNamespaceAliases(namespace, name)...)
		}
	}

	if raw := query["namespacealiases"]; raw != nil {
		switch aliases := raw.(type) {
		case []any:
			for _, entry := range aliases {
				alias, ok := entry.(map[string]any)
				if !ok {
					continue
				}
				id, ok := extractNamespaceID(alias)
				if !ok {
					continue
				}
				if name := extractNamespaceName(alias); name != "" {
					namespaceAliases[id] = mergeNamespaceAliases(namespaceAliases[id], name)
				}
			}
		}
	}

	extensions := []string{}
	switch raw := query["extensions"].(type) {
	case []any:
		for _, entry := range raw {
			extension, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			if name, ok := extension["name"].(string); ok && name != "" {
				extensions = append(extensions, name)
			}
		}
	}

	pageCount, activeUsers := 0, 0
	if stats, ok := query["statistics"].(map[string]any); ok {
		pageCount = statInt(stats["pages"])
		activeUsers = statInt(stats["activeusers"])
	}

	restrictionTypes, restrictionLevels, cascadingLevels, semiLevels := parseRestrictions(query["restrictions"])

	return WikiCapabilities{
		Version:             version,
		VersionMajMin:       majMin,
		Namespaces:          namespaces,
		NamespaceAliases:    namespaceAliases,
		Extensions:          extensions,
		PageCount:           pageCount,
		ActiveUsers:         activeUsers,
		RestrictionTypes:    restrictionTypes,
		RestrictionLevels:   restrictionLevels,
		CascadingLevels:     cascadingLevels,
		SemiProtectedLevels: semiLevels,
	}, nil
}

// parseRestrictions reads the siteinfo "restrictions" block: the protection types, levels, cascading levels, and
// semi-protected levels the wiki offers. Levels preserve the "" entry (no restriction); the others never contain "".
func parseRestrictions(raw any) (types, levels, cascading, semi []string) {
	block, ok := raw.(map[string]any)
	if !ok {
		return nil, nil, nil, nil
	}
	return parseStringList(block["types"]), parseLevelList(block["levels"]),
		parseStringList(block["cascadinglevels"]), parseStringList(block["semiprotectedlevels"])
}

// parseLevelList is parseStringList that keeps the "" entry, since "" is a meaningful protection level (no restriction).
func parseLevelList(raw any) []string {
	values := []string{}
	list, ok := raw.([]any)
	if !ok {
		return values
	}
	for _, entry := range list {
		if value, ok := entry.(string); ok {
			values = append(values, value)
		}
	}
	return values
}

// statInt reads a numeric siteinfo statistics value, tolerating the JSON number types the API may use.
func statInt(raw any) int {
	switch v := raw.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}

func extractNamespaceID(namespace map[string]any) (int, bool) {
	switch raw := namespace["id"].(type) {
	case float64:
		return int(raw), true
	case int:
		return raw, true
	case string:
		id, err := strconv.Atoi(raw)
		if err != nil {
			return 0, false
		}
		return id, true
	default:
		return 0, false
	}
}

func extractNamespaceName(namespace map[string]any) string {
	if name, ok := namespace["name"].(string); ok {
		return name
	}
	if name, ok := namespace["*"].(string); ok {
		return name
	}
	if canonical, ok := namespace["canonical"].(string); ok {
		return canonical
	}
	return ""
}

func extractNamespaceAliases(namespace map[string]any, primary string) []string {
	aliases := []string{}
	if canonical, ok := namespace["canonical"].(string); ok && canonical != "" &&
		!strings.EqualFold(strings.TrimSpace(canonical), strings.TrimSpace(primary)) {
		aliases = append(aliases, canonical)
	}
	return aliases
}

func mergeNamespaceAliases(existing []string, additions ...string) []string {
	out := append([]string{}, existing...)
	for _, addition := range additions {
		trimmed := strings.TrimSpace(addition)
		if trimmed == "" {
			continue
		}
		found := false
		for _, item := range out {
			if strings.EqualFold(strings.TrimSpace(item), trimmed) {
				found = true
				break
			}
		}
		if !found {
			out = append(out, trimmed)
		}
	}
	return out
}

func parseMediaWikiVersion(generator string) (string, [2]int, error) {
	matches := versionPattern.FindStringSubmatch(strings.TrimSpace(generator))
	if len(matches) < 3 {
		return "", [2]int{}, fmt.Errorf("could not parse MediaWiki version from %q", generator)
	}
	major, err := strconv.Atoi(matches[1])
	if err != nil {
		return "", [2]int{}, fmt.Errorf("invalid major version in %q", generator)
	}
	minor, err := strconv.Atoi(matches[2])
	if err != nil {
		return "", [2]int{}, fmt.Errorf("invalid minor version in %q", generator)
	}

	normalized := matches[1] + "." + matches[2]
	if len(matches) > 3 && matches[3] != "" {
		normalized += "." + matches[3]
	}
	return normalized, [2]int{major, minor}, nil
}

func compareMajMin(a, b [2]int) int {
	if a[0] != b[0] {
		if a[0] < b[0] {
			return -1
		}
		return 1
	}
	if a[1] < b[1] {
		return -1
	}
	if a[1] > b[1] {
		return 1
	}
	return 0
}

// FetchUserInfo loads rights/groups and highlimits capability.
func FetchUserInfo(client *Client, apiURL string) (WikiCapabilities, error) {
	return FetchUserInfoContext(context.Background(), client, apiURL)
}

// FetchUserInfoContext loads rights/groups and highlimits capability.
func FetchUserInfoContext(ctx context.Context, client *Client, apiURL string) (WikiCapabilities, error) {
	response, err := client.GetContext(ctx, apiURL, map[string]string{
		"action":        "query",
		"meta":          "userinfo",
		"uiprop":        "rights|groups|blockinfo",
		"formatversion": "2",
	})
	if err != nil {
		return WikiCapabilities{}, fmt.Errorf("fetch userinfo: %w", err)
	}

	caps, err := parseUserInfoResponse(response)
	if err != nil {
		return WikiCapabilities{}, err
	}
	return caps, nil
}

// multiValueParams maps each action Skubell batches multivalue calls on to the batched parameter, for
// paraminfo discovery. Extend it when a new batched action appears; unlisted actions fall back to the
// rights-derived default cap (and to the live per-rejection shrink in batch.go).
var multiValueParams = map[string]string{
	"query":          "titles",
	"revisiondelete": "ids",
}

// FetchMultiValueCapsContext asks the wiki, in one paraminfo request, how many values this session may put in
// the multivalue parameter of each action Skubell batches on (multiValueParams). Each reported "limit" is
// computed for the current session's rights — so the answers already account for apihighlimits, for
// per-module overrides, and for any future MediaWiki change to the 50/500 defaults. Modules the wiki does not
// answer for are simply absent from the result; an error means nothing usable came back and callers keep the
// rights-derived caps.
func FetchMultiValueCapsContext(ctx context.Context, client *Client, apiURL string) (map[string]int, error) {
	modules := slices.Sorted(maps.Keys(multiValueParams))
	response, err := client.GetContext(ctx, apiURL, map[string]string{
		"action":        "paraminfo",
		"modules":       strings.Join(modules, "|"),
		"formatversion": "2",
	})
	if err != nil {
		return nil, fmt.Errorf("fetch paraminfo: %w", err)
	}
	return parseMultiValueCaps(response)
}

func parseMultiValueCaps(response map[string]any) (map[string]int, error) {
	paraminfo, _ := response["paraminfo"].(map[string]any)
	modules, _ := paraminfo["modules"].([]any)
	caps := map[string]int{}
	for _, rawModule := range modules {
		module, ok := rawModule.(map[string]any)
		if !ok {
			continue
		}
		action, _ := module["name"].(string)
		paramName, tracked := multiValueParams[action]
		if !tracked {
			continue
		}
		parameters, _ := module["parameters"].([]any)
		for _, raw := range parameters {
			parameter, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if name, _ := parameter["name"].(string); name != paramName {
				continue
			}
			if limit, ok := parameter["limit"].(float64); ok && limit > 0 {
				caps[action] = int(limit)
			}
			break
		}
	}
	if len(caps) == 0 {
		return nil, errors.New("paraminfo did not report any multivalue limit")
	}
	return caps, nil
}

func parseUserInfoResponse(response map[string]any) (WikiCapabilities, error) {
	query, ok := response["query"].(map[string]any)
	if !ok {
		return WikiCapabilities{}, errors.New("missing query field in userinfo response")
	}
	userinfo, ok := query["userinfo"].(map[string]any)
	if !ok {
		return WikiCapabilities{}, errors.New("missing query.userinfo in userinfo response")
	}

	rights := parseStringList(userinfo["rights"])
	groups := parseStringList(userinfo["groups"])

	hasHighLimits := slices.Contains(rights, "apihighlimits")

	blockReason, _ := userinfo["blockedby"].(string)
	if reason, ok := userinfo["blockedreason"].(string); ok && reason != "" {
		blockReason = reason
	}
	blockExpiry, _ := userinfo["blockexpiry"].(string)
	sitewideBlock := false
	if _, blocked := userinfo["blockid"]; blocked {
		// Blocked. Treat as sitewide unless MediaWiki marks it partial. "blockpartial" is a real boolean under
		// formatversion=2 (present as false for sitewide) but present-only (empty string) under formatversion=1, where
		// it appears only for partial blocks. An explicit "blocksitewide" boolean, when present, is authoritative.
		sitewideBlock = true
		switch partial := userinfo["blockpartial"].(type) {
		case bool:
			sitewideBlock = !partial
		case string:
			sitewideBlock = false
		}
		if value, ok := userinfo["blocksitewide"].(bool); ok {
			sitewideBlock = value
		}
	}

	return WikiCapabilities{
		UserRights:    rights,
		UserGroups:    groups,
		HasHighLimits: hasHighLimits,
		SitewideBlock: sitewideBlock,
		BlockReason:   blockReason,
		BlockExpiry:   blockExpiry,
	}, nil
}

func parseStringList(raw any) []string {
	values := []string{}
	list, ok := raw.([]any)
	if !ok {
		return values
	}
	for _, entry := range list {
		if value, ok := entry.(string); ok && value != "" {
			values = append(values, value)
		}
	}
	return values
}
