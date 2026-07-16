package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/Hunvreus-wiki/skubell/internal/api"
	"github.com/Hunvreus-wiki/skubell/internal/protect"
)

// protectionProvider reads current protection and existence from the live wiki. It implements protect.Reader.
type protectionProvider struct {
	client *api.Client
	apiURL string
}

// PageProtections queries prop=info&inprop=protection for the titles (batched) and returns each page's existence and
// per-type current protection. Results are keyed by both the MediaWiki-normalized title and the caller's input title,
// so a lookup by the original spelling still hits.
func (p *protectionProvider) PageProtections(
	ctx context.Context, titles []string,
) (map[string]protect.PageProtection, error) {
	const chunkSize = 50
	out := map[string]protect.PageProtection{}
	for start := 0; start < len(titles); start += chunkSize {
		end := min(start+chunkSize, len(titles))
		chunk := titles[start:end]

		payload, err := p.client.GetContext(ctx, p.apiURL, map[string]string{
			"action":        "query",
			"prop":          "info",
			"inprop":        "protection",
			"titles":        strings.Join(chunk, "|"),
			"formatversion": "2",
		})
		if err != nil {
			return nil, fmt.Errorf("query protection: %w", err)
		}
		query, _ := payload["query"].(map[string]any)

		pages, _ := query["pages"].([]any)
		for _, raw := range pages {
			page, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			title, _ := page["title"].(string)
			if strings.TrimSpace(title) == "" {
				continue
			}
			out[title] = parsePageProtection(title, page)
		}

		// MediaWiki may normalize input titles (e.g. first-letter case); alias the normalized result back to the
		// spelling the caller passed, so BuildPlan's lookup by the original title succeeds.
		if normalized, ok := query["normalized"].([]any); ok {
			for _, raw := range normalized {
				m, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				from, _ := m["from"].(string)
				to, _ := m["to"].(string)
				if from != "" && to != "" {
					if pp, ok := out[to]; ok {
						out[from] = pp
					}
				}
			}
		}
	}
	return out, nil
}

func parsePageProtection(title string, page map[string]any) protect.PageProtection {
	pp := protect.PageProtection{
		Title:       title,
		Exists:      page["missing"] != true, // fv2: a nonexistent page carries "missing": true
		Protections: map[string]protect.TypeProtection{},
	}
	protections, ok := page["protection"].([]any)
	if !ok {
		return pp
	}
	for _, raw := range protections {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := entry["type"].(string)
		if strings.TrimSpace(typ) == "" {
			continue
		}
		// A "source" field marks a restriction inherited from another page's cascade — not a direct protection on this
		// page. Resending it as a direct restriction (or previewing its removal) would be wrong: it can only be changed
		// on its source page and remains inherited regardless. Skip it so only the page's own protection is modelled.
		if source, _ := entry["source"].(string); strings.TrimSpace(source) != "" {
			continue
		}
		level, _ := entry["level"].(string)
		expiry, _ := entry["expiry"].(string)
		_, cascade := entry["cascade"] // fv2 emits the key only when the protection cascades
		pp.Protections[typ] = protect.TypeProtection{Level: level, Expiry: expiry, Cascade: cascade}
	}
	return pp
}
