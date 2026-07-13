package api

import "github.com/Hunvreus-wiki/skubell/internal/ops"

// APICall represents a single call to the MediaWiki API.
type APICall struct {
	Action   string
	Method   string
	Params   map[string]string
	SourceOp int
}

// WikiCapabilities summarizes detected capabilities of the target wiki.
type WikiCapabilities struct {
	Version          string
	VersionMajMin    [2]int
	Namespaces       map[int]string
	NamespaceAliases map[int][]string
	Extensions       []string
	UserRights       []string
	UserGroups       []string
	HasHighLimits    bool
	SitewideBlock    bool
	BlockReason      string
	BlockExpiry      string
	PageCount        int // total pages, from siteinfo statistics (0 when unavailable)
	ActiveUsers      int // recently-active users, from siteinfo statistics (0 when unavailable)

	// Protection: what the wiki offers, from siteinfo restrictions. RestrictionLevels keeps the "" entry ("" = no
	// restriction / everyone). CascadingLevels are the levels that permit cascade protection (normally just "sysop").
	RestrictionTypes    []string
	RestrictionLevels   []string
	CascadingLevels     []string
	SemiProtectedLevels []string
}

// Translator converts semantic operations into API calls.
type Translator interface {
	Translate(op ops.Operation, caps WikiCapabilities) ([]APICall, error)
}
