package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Hunvreus-wiki/skubell/internal/api"
	"github.com/Hunvreus-wiki/skubell/internal/revdel"
)

// revisionBatchSize is rvlimit per request. The enumeration is unbounded: the revision list claims to be the page's
// whole history (mass selection over a partial one would silently miss revisions), so a long history costs more
// batches rather than failing — the load reports progress and stays cancellable.
const revisionBatchSize = 500

// errPageMissing rejects a title with no page behind it; the workflow screen maps it to localized guidance.
var errPageMissing = errors.New("the page does not exist")

// revdelProvider reads a page's revision history from the live wiki.
type revdelProvider struct {
	client *api.Client
	apiURL string
}

// pageRevisions lists every revision of title, newest first, with its current visibility; the page's live revision is
// marked Current. Content visibility is probed through sha1 (rvprop=content would fetch every revision's text).
// progress, when non-nil, is called after each batch with the number of revisions loaded so far; it runs on the
// caller's goroutine.
func (p *revdelProvider) pageRevisions(
	ctx context.Context, title string, progress func(loaded int),
) ([]revdel.Revision, error) {
	params := map[string]string{
		"action":        "query",
		"prop":          "info|revisions",
		"titles":        title,
		"rvprop":        "ids|timestamp|user|comment|sha1",
		"rvlimit":       strconv.Itoa(revisionBatchSize),
		"formatversion": "2",
	}
	revisions := []revdel.Revision{}
	lastRevID := int64(0)
	for {
		payload, err := p.client.GetContext(ctx, p.apiURL, params)
		if err != nil {
			return nil, fmt.Errorf("query revisions: %w", err)
		}
		page, err := firstPage(payload)
		if err != nil {
			return nil, err
		}
		if id := jsonInt64(page["lastrevid"]); id != 0 {
			lastRevID = id
		}
		rawRevisions, _ := page["revisions"].([]any)
		for _, raw := range rawRevisions {
			if entry, ok := raw.(map[string]any); ok {
				revisions = append(revisions, parseRevision(entry, lastRevID))
			}
		}
		if progress != nil {
			progress(len(revisions))
		}
		continueMap, _ := payload["continue"].(map[string]any)
		next, _ := continueMap["rvcontinue"].(string)
		if next == "" {
			return revisions, nil
		}
		params["rvcontinue"] = next
	}
}

// firstPage extracts the single page object of a titles= query, rejecting a missing or invalid title.
func firstPage(payload map[string]any) (map[string]any, error) {
	query, _ := payload["query"].(map[string]any)
	pages, _ := query["pages"].([]any)
	if len(pages) == 0 {
		return nil, errPageMissing
	}
	page, ok := pages[0].(map[string]any)
	if !ok || page["missing"] == true || page["invalid"] == true {
		return nil, errPageMissing
	}
	return page, nil
}

// parseRevision reads one fv2 revision entry. The *hidden keys are emitted (as true) only when that field is hidden;
// sha1hidden stands in for the content, whose visibility is otherwise only reported when fetching the text itself.
func parseRevision(entry map[string]any, lastRevID int64) revdel.Revision {
	id := jsonInt64(entry["revid"])
	user, _ := entry["user"].(string)
	comment, _ := entry["comment"].(string)
	timestamp, _ := entry["timestamp"].(string)
	parsed, _ := time.Parse(time.RFC3339, timestamp)
	return revdel.Revision{
		ID:            id,
		Timestamp:     parsed,
		User:          user,
		Comment:       comment,
		Current:       lastRevID != 0 && id == lastRevID,
		ContentHidden: entry["sha1hidden"] == true,
		CommentHidden: entry["commenthidden"] == true,
		UserHidden:    entry["userhidden"] == true,
		Suppressed:    entry["suppressed"] == true,
	}
}

// jsonInt64 reads a numeric JSON value that decoding may have produced as float64 or json.Number; other types yield 0.
func jsonInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	case string:
		i, _ := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		return i
	default:
		return 0
	}
}
