package api

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Hunvreus-wiki/skubell/internal/ops"
)

// revDelFieldOrder is the stable order in which revision fields are emitted into the API pipe-lists.
var revDelFieldOrder = []string{"content", "comment", "user"}

// RevDelTranslator translates revision-visibility operations (revision deletion and suppression — the latter is a
// level, not a separate action: MediaWiki keeps one revision-wide suppression bit over the hidden fields).
//
// OpRevisionDelete uses this normalized param vocabulary (matching the MediaWiki API's where they overlap):
//
//	ids       pipe-joined revision IDs to change, all carrying the same target visibility
//	hide      pipe-joined fields to hide: content, comment, user
//	show      pipe-joined fields to make visible again
//	suppress  level applied to the hidden fields: yes, no, or nochange (the default when absent)
//	reason    visibility-change reason
type RevDelTranslator struct{}

// Translate converts a revision-visibility operation into a single action=revisiondelete API call.
func (t RevDelTranslator) Translate(op ops.Operation, _ WikiCapabilities) ([]APICall, error) {
	if op.Type != ops.OpRevisionDelete {
		return nil, fmt.Errorf("unsupported operation type: %s", op.Type)
	}
	ids := strings.TrimSpace(op.Params["ids"])
	if ids == "" {
		return nil, errors.New("revision-visibility operation missing ids")
	}
	params := map[string]string{
		"type": "revision",
		"ids":  ids,
	}

	hide, err := normalizeRevDelFields(op.Params["hide"])
	if err != nil {
		return nil, err
	}
	show, err := normalizeRevDelFields(op.Params["show"])
	if err != nil {
		return nil, err
	}
	if hide == "" && show == "" {
		return nil, errors.New("revision-visibility operation changes nothing (no hide or show fields)")
	}
	if overlap := fieldOverlap(hide, show); overlap != "" {
		return nil, fmt.Errorf("field %q is both hidden and shown", overlap)
	}
	if hide != "" {
		params["hide"] = hide
	}
	if show != "" {
		params["show"] = show
	}

	suppress := op.Params["suppress"]
	if suppress == "" {
		suppress = "nochange"
	}
	if suppress != "yes" && suppress != "no" && suppress != "nochange" {
		return nil, fmt.Errorf("invalid suppress level %q", suppress)
	}
	params["suppress"] = suppress

	if reason := strings.TrimSpace(op.Params["reason"]); reason != "" {
		params["reason"] = reason
	}

	return []APICall{
		{
			Action:     "revisiondelete",
			Method:     "POST",
			Params:     params,
			MultiParam: "ids", // lets the executor re-split the batch if the wiki's real cap is smaller
			Validate:   ValidateRevisionDeleteResponse,
		},
	}, nil
}

// normalizeRevDelFields validates a pipe-list of revision fields and re-emits it deduplicated in revDelFieldOrder,
// so equivalent operations always produce the same API call. An empty list is valid and yields "".
func normalizeRevDelFields(raw string) (string, error) {
	present := map[string]struct{}{}
	for field := range strings.SplitSeq(raw, "|") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if !slices.Contains(revDelFieldOrder, field) {
			return "", fmt.Errorf("unknown revision field %q", field)
		}
		present[field] = struct{}{}
	}
	out := make([]string, 0, len(present))
	for _, field := range revDelFieldOrder {
		if _, ok := present[field]; ok {
			out = append(out, field)
		}
	}
	return strings.Join(out, "|"), nil
}

// fieldOverlap returns a field present in both normalized pipe-lists, or "" when they are disjoint.
func fieldOverlap(hide, show string) string {
	if hide == "" || show == "" {
		return ""
	}
	shown := strings.Split(show, "|")
	for field := range strings.SplitSeq(hide, "|") {
		if slices.Contains(shown, field) {
			return field
		}
	}
	return ""
}

// ValidateRevisionDeleteResponse surfaces the failure an action=revisiondelete response can hide: MediaWiki
// keeps both the call's and the item's status "Success" while attaching an "errors" list to items it could not
// change (e.g. revdelete-modify-no-access when a session without suppressrevision touches a restricted item),
// so the statuses alone cannot be trusted. Returns the first item's first error, or nil when every item is
// genuinely clean.
func ValidateRevisionDeleteResponse(response map[string]any) *APIError {
	payload, _ := response["revisiondelete"].(map[string]any)
	items, _ := payload["items"].([]any)
	for _, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		itemErrors, _ := item["errors"].([]any)
		if len(itemErrors) == 0 {
			continue
		}
		first, _ := itemErrors[0].(map[string]any)
		code, _ := first["code"].(string)
		if code == "" {
			code = "revdelete-item-error"
		}
		info, _ := first["*"].(string)
		if info == "" {
			info, _ = first["text"].(string) // formatversion=2 spelling of the same field
		}
		return &APIError{Code: code, Info: info}
	}
	return nil
}

// FriendlyRevDelErrorMessage is the user-facing message for a failed revision-visibility call: the API's
// human-readable info when present, else its error code. The interface/site-config grant guidance of the delete and
// protect variants doesn't apply here — action=revisiondelete is gated by rights, not by page namespace.
func FriendlyRevDelErrorMessage(apiErr *APIError) string {
	if apiErr == nil {
		return ""
	}
	return friendlyInfoOrCode(apiErr)
}
