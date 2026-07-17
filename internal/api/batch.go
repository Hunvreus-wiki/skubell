package api

import (
	"errors"
)

// This file is the adaptive-batching framework for multivalue API parameters (titles, ids, …).
//
// MediaWiki caps how many values one multivalue parameter may carry: 50 per request, 500 with apihighlimits.
// Skubell detects apihighlimits at connect, but the wiki's answer at execution time is the only authority: a
// session can silently lose its login (MediaWiki then validates parameter counts BEFORE authentication, so an
// expired session surfaces as "toomanyvalues: the limit is 50", not as an auth error). The framework therefore
// treats the cap as a live session property, not a constant:
//
//   - Client.MultiValueCap holds the session's PER-ACTION caps (modules may cap their parameters
//     individually): discovered from the wiki at connect (SetMultiValueCaps, paraminfo) and shrunk
//     automatically whenever a response reports "toomanyvalues" for that action (observeAPIError) — that
//     error carries the wiki's real limit.
//   - Client.ForEachChunk (below) is the batching loop: it sizes chunks from the live cap and replays a chunk
//     that the wiki rejected as too large, now split at the reported limit. Once re-split, the underlying
//     failure (badtoken, permissions, …) is no longer masked by the size rejection and surfaces normally.
//   - APICall.MultiParam gives translated write calls the same behavior inside HttpExecutor: an oversized
//     call is re-split at the shrunken cap and replayed transparently.
//
// Callers must size batches at call time via MultiValueCap (or better, not size them at all and go through
// ForEachChunk / MultiParam) instead of freezing 50-or-500 up front from HasHighLimits.

// ForEachChunk invokes fn once per chunk of values, sized to the session's live multivalue cap for the given
// action. When fn fails with the wiki's "toomanyvalues" rejection and the action's cap has just shrunk below
// the chunk's size, the same chunk is re-split at the new cap and retried; any other error aborts the run. fn
// is expected to perform one request of that action (or one continuation-following sequence) for its chunk
// through this client, so the rejection has already shrunk the cap by the time it is inspected here.
func (c *Client) ForEachChunk(action string, values []string, fn func(chunk []string) error) error {
	start := 0
	for start < len(values) {
		end := min(start+c.MultiValueCap(action), len(values))
		if err := fn(values[start:end]); err != nil {
			if apiErr, ok := errors.AsType[*APIError](err); ok &&
				apiErr.Code == "toomanyvalues" && c.MultiValueCap(action) < end-start {
				continue // the wiki just taught us its real cap; re-split this chunk at that size
			}
			return err
		}
		start = end
	}
	return nil
}
