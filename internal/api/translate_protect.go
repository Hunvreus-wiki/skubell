package api

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Hunvreus-wiki/skubell/internal/ops"
)

// protectionTypeOrder is the stable order in which protection types are emitted into the API pipe-lists.
var protectionTypeOrder = []string{"edit", "create", "move", "upload"}

// ProtectTranslator translates protection-change operations.
//
// OpProtectPage uses this normalized param vocabulary (NOT the MediaWiki API vocabulary):
//
//	title            the page title
//	protect_<type>   target level for that restriction type, one key per type being set on this page
//	                 (protect_edit, protect_move, protect_create, protect_upload). The value is a wiki level
//	                 (e.g. "autoconfirmed", "sysop") or "all"/"" to remove protection for that type.
//	expiry_<type>    expiry paired with each protect_<type>: "infinite", an RFC3339 timestamp, or a relative
//	                 duration ("1 week"). Defaults to "infinite" when absent.
//	cascade          "true" to cascade-protect (valid only when every level is a cascading level).
//	reason           protection reason.
//
// The planner decides which types are present per page (existing pages get edit/move, missing titles get create),
// so the translator emits exactly the types it is given.
type ProtectTranslator struct{}

// Translate converts a protection operation into a single action=protect API call.
func (t ProtectTranslator) Translate(op ops.Operation, _ WikiCapabilities) ([]APICall, error) {
	if op.Type != ops.OpProtectPage {
		return nil, fmt.Errorf("unsupported operation type: %s", op.Type)
	}

	title := strings.TrimSpace(op.Params["title"])
	if title == "" {
		return nil, errors.New("protect operation missing title")
	}

	var protections, expiries []string
	for _, typ := range protectionTypesInParams(op.Params) {
		level := strings.TrimSpace(op.Params["protect_"+typ])
		if level == "" {
			level = "all" // "" and "all" both mean "no restriction" to the API
		}
		protections = append(protections, typ+"="+level)

		expiry := strings.TrimSpace(op.Params["expiry_"+typ])
		if expiry == "" {
			expiry = "infinite"
		}
		expiries = append(expiries, expiry)
	}
	if len(protections) == 0 {
		return nil, errors.New("protect operation has no protections")
	}

	params := map[string]string{
		"title":       title,
		"protections": strings.Join(protections, "|"),
		"expiry":      strings.Join(expiries, "|"),
	}
	if op.Params["cascade"] == "true" {
		params["cascade"] = "1"
	}
	if reason := strings.TrimSpace(op.Params["reason"]); reason != "" {
		params["reason"] = reason
	}

	return []APICall{
		{
			Action: "protect",
			Method: "POST",
			Params: params,
		},
	}, nil
}

// protectionTypesInParams lists the restriction types present as protect_<type> keys: the canonical types first (in
// protectionTypeOrder), then any wiki-specific custom types sorted for determinism. Emitting every present type — not
// just the canonical four — is what keeps a preserved custom restriction from being dropped by action=protect's
// whole-set replacement.
func protectionTypesInParams(params map[string]string) []string {
	seen := map[string]struct{}{}
	types := make([]string, 0, len(params))
	for _, typ := range protectionTypeOrder {
		if _, ok := params["protect_"+typ]; ok {
			types = append(types, typ)
			seen[typ] = struct{}{}
		}
	}
	extra := []string{}
	for key := range params {
		typ, ok := strings.CutPrefix(key, "protect_")
		if !ok {
			continue
		}
		if _, known := seen[typ]; !known {
			extra = append(extra, typ)
		}
	}
	sort.Strings(extra)
	return append(types, extra...)
}
