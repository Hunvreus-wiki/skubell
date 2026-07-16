//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/api"
	"github.com/Hunvreus-wiki/skubell/internal/protect"
)

// protReader reads live protection for the planner (a lean stand-in for the UI's protectionProvider).
type protReader struct {
	c   *api.Client
	url string
}

func (r protReader) PageProtections(ctx context.Context, titles []string) (map[string]protect.PageProtection, error) {
	payload, err := r.c.GetContext(ctx, r.url, map[string]string{
		"action": "query", "prop": "info", "inprop": "protection",
		"titles": strings.Join(titles, "|"), "formatversion": "2",
	})
	if err != nil {
		return nil, err
	}
	out := map[string]protect.PageProtection{}
	query, _ := payload["query"].(map[string]any)
	pages, _ := query["pages"].([]any)
	for _, raw := range pages {
		page, _ := raw.(map[string]any)
		title, _ := page["title"].(string)
		pp := protect.PageProtection{
			Title: title, Exists: page["missing"] != true,
			Protections: map[string]protect.TypeProtection{},
		}
		if prots, ok := page["protection"].([]any); ok {
			for _, e := range prots {
				m, _ := e.(map[string]any)
				typ, _ := m["type"].(string)
				level, _ := m["level"].(string)
				expiry, _ := m["expiry"].(string)
				if typ != "" {
					pp.Protections[typ] = protect.TypeProtection{Level: level, Expiry: expiry}
				}
			}
		}
		out[title] = pp
	}
	// Alias MediaWiki-normalized titles (e.g. underscores -> spaces) back to the input spelling, matching the real
	// protectionProvider, so BuildPlan's lookup by the caller's title succeeds.
	if normalized, ok := query["normalized"].([]any); ok {
		for _, raw := range normalized {
			m, _ := raw.(map[string]any)
			from, _ := m["from"].(string)
			to, _ := m["to"].(string)
			if from != "" && to != "" {
				if pp, ok := out[to]; ok {
					out[from] = pp
				}
			}
		}
	}
	return out, nil
}

// runProtectionPlan exercises the whole chain: BuildPlan -> ProtectTranslator -> HttpExecutor.
func runProtectionPlan(
	t *testing.T, c *api.Client, url string, caps api.WikiCapabilities, titles []string, s protect.Settings,
) []api.APIResult {
	t.Helper()
	plan, err := protect.BuildPlan(
		context.Background(), protReader{c: c, url: url}, titles, s, caps.CascadingLevels, caps.RestrictionTypes,
	)
	require.NoError(t, err)
	executor, err := api.NewHttpExecutor(c, url)
	require.NoError(t, err)
	tr := api.ProtectTranslator{}
	var results []api.APIResult
	for _, item := range plan.Items {
		if !item.Changed || item.Invalid {
			continue
		}
		calls, err := tr.Translate(item.Op, caps)
		require.NoError(t, err)
		res, err := executor.Execute(context.Background(), calls)
		require.NoError(t, err)
		results = append(results, res...)
	}
	return results
}

func liveProtection(t *testing.T, c *api.Client, url, title string) map[string]protect.TypeProtection {
	t.Helper()
	got, err := protReader{c: c, url: url}.PageProtections(context.Background(), []string{title})
	require.NoError(t, err)
	return got[title].Protections
}

func keep() protect.TypeSetting { return protect.TypeSetting{KeepLevel: true, KeepExpiry: true} }

// P11 — protect an existing page, then make it permanent, then unprotect. Verified via prop=info.
func TestProtectionRoundTrip(t *testing.T) {
	url := apiURL(t)
	c, res := connectBot(t, url, botAdmin, adminBotPassword(url))
	admin := loginWeb(t, url, webAdminUser, webAdminPass)

	title := uniqueTitle(t)
	editPage(t, admin, url, title, "protection round-trip")

	// Temporary full edit protection.
	results := runProtectionPlan(t, c, url, res.Capabilities, []string{title}, protect.Settings{
		ByType: map[string]protect.TypeSetting{"edit": {Level: "sysop", Expiry: "1 week"}, "move": keep()},
		Reason: "IT protect",
	})
	require.NotEmpty(t, results)
	require.True(t, results[0].Success, "protect failed: %+v", results[0].Error)
	require.Equal(t, "sysop", liveProtection(t, c, url, title)["edit"].Level)

	// Make permanent (keep level, expiry infinite).
	runProtectionPlan(t, c, url, res.Capabilities, []string{title}, protect.Settings{
		ByType: map[string]protect.TypeSetting{"edit": {KeepLevel: true, Expiry: "infinite"}, "move": keep()},
		Reason: "IT permanent",
	})
	require.Equal(t, "infinity", liveProtection(t, c, url, title)["edit"].Expiry)

	// Unprotect.
	runProtectionPlan(t, c, url, res.Capabilities, []string{title}, protect.Settings{
		ByType: map[string]protect.TypeSetting{"edit": {Level: ""}, "move": keep()},
		Reason: "IT unprotect",
	})
	require.NotContains(t, liveProtection(t, c, url, title), "edit", "edit protection should be gone")
}

// P11 — a mixed batch of an existing page and a missing title succeeds: the planner sends edit/move to the existing
// page and create to the missing one, so MediaWiki never rejects with create-titleexists / missingtitle-createonly.
func TestProtectionExistenceFiltering(t *testing.T) {
	url := apiURL(t)
	c, res := connectBot(t, url, botAdmin, adminBotPassword(url))
	admin := loginWeb(t, url, webAdminUser, webAdminPass)

	existing := uniqueTitle(t)
	editPage(t, admin, url, existing, "existence filtering")
	missing := uniqueTitle(t) // never created

	results := runProtectionPlan(t, c, url, res.Capabilities, []string{existing, missing}, protect.Settings{
		ByType: map[string]protect.TypeSetting{
			"edit": {Level: "sysop"}, "move": {Level: "sysop"}, "create": {Level: "sysop"},
		},
		Reason: "IT mixed",
	})
	require.Len(t, results, 2)
	for _, r := range results {
		require.True(t, r.Success, "no page should be rejected: %+v", r.Error)
	}
	ep := liveProtection(t, c, url, existing)
	require.Equal(t, "sysop", ep["edit"].Level)
	require.Equal(t, "sysop", ep["move"].Level)
	require.NotContains(t, ep, "create", "existing page must not get create protection")
	require.Equal(t, "sysop", liveProtection(t, c, url, missing)["create"].Level)
}

// P11 — a non-admin account (no protect right) cannot protect, even though its bot password carries the grant.
func TestProtectionInsufficientRights(t *testing.T) {
	url := apiURL(t)
	c, res := connectBot(t, url, botEditor, botEditorPass)
	admin := loginWeb(t, url, webAdminUser, webAdminPass)

	title := uniqueTitle(t)
	editPage(t, admin, url, title, "insufficient rights")

	results := runProtectionPlan(t, c, url, res.Capabilities, []string{title}, protect.Settings{
		ByType: map[string]protect.TypeSetting{"edit": {Level: "sysop"}, "move": keep()},
		Reason: "IT denied",
	})
	require.NotEmpty(t, results)
	require.False(t, results[0].Success, "protect without the protect right must fail")
	require.NotNil(t, results[0].Error)
}
