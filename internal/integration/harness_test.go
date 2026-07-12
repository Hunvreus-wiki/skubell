//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/api"
	"github.com/Hunvreus-wiki/skubell/internal/config"
	"github.com/Hunvreus-wiki/skubell/internal/deletion"
	"github.com/Hunvreus-wiki/skubell/internal/ops"
)

// Provisioned test credentials (see README.integration.md). Bot passwords for TestEditor/TestBlocked/TestPartial are
// identical on both wikis; TestAdmin's differs per wiki (see adminBotPassword).
const (
	webAdminUser = "TestAdmin"
	webAdminPass = "TestAdminPass!"

	botAdmin       = "TestAdmin@SkubellTest"
	botEditor      = "TestEditor@SkubellTest"
	botEditorPass  = "testeditor00botpass00skubell0002"
	botBlocked     = "TestBlocked@SkubellTest"
	botBlockedPass = "testblocked0botpass00skubell0003"
	botPartial     = "TestPartial@SkubellTest"
	botPartialPass = "testpartial0botpass00skubell0004"
)

var titleCounter atomic.Int64

// apiURL returns the target wiki's api.php, or skips the test when unset.
func apiURL(t *testing.T) string {
	t.Helper()
	u := strings.TrimSpace(os.Getenv("SKUBELL_TEST_API"))
	if u == "" {
		t.Skip("set SKUBELL_TEST_API (e.g. http://localhost:8081/api.php) to run integration tests")
	}
	return u
}

func adminBotPassword(apiURL string) string {
	if strings.Contains(apiURL, ":8082") {
		return "r7elmkikmc1mqehngiqo8rrqcs2kktpu"
	}
	return "ovgj07dt13opeuti773d17i96hamrg7g"
}

func newClient(t *testing.T) *api.Client {
	t.Helper()
	c, err := api.NewClient(0, 2, nil)
	require.NoError(t, err)
	return c
}

// connectBot runs the full Skubell connection sequence (login + capabilities + reasons) via a bot password.
func connectBot(t *testing.T, apiURL, username, credential string) (*api.Client, api.ConnectionResult) {
	t.Helper()
	c := newClient(t)
	res, err := api.ConnectContext(context.Background(), c, config.WikiEntry{
		Farm:       "custom",
		APIURL:     apiURL,
		Username:   username,
		Credential: credential,
	})
	require.NoError(t, err, "connect %s", username)
	return c, res
}

// loginWeb logs in with an ordinary account password (full account rights, not restricted by bot-password grants),
// used for test setup/teardown.
func loginWeb(t *testing.T, apiURL, username, password string) *api.Client {
	t.Helper()
	c := newClient(t)
	_, err := api.LoginContext(context.Background(), c, apiURL, username, password)
	require.NoError(t, err, "web login %s", username)
	return c
}

// uniqueTitle returns a run-unique throwaway page title.
func uniqueTitle(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("SkubellIT_%d_%d", os.Getpid(), titleCounter.Add(1))
}

func editPage(t *testing.T, c *api.Client, apiURL, title, text string) {
	t.Helper()
	resp, err := c.PostContext(context.Background(), apiURL, map[string]string{
		"action":  "edit",
		"title":   title,
		"text":    text,
		"summary": "integration test seed",
	})
	require.NoError(t, err, "edit %s", title)
	edit, _ := resp["edit"].(map[string]any)
	result, _ := edit["result"].(string)
	require.Equal(t, "Success", result, "edit %s: %v", title, resp)
}

// deleteAsAdmin removes a page via a full admin web session; best-effort cleanup.
func deleteAsAdmin(t *testing.T, admin *api.Client, apiURL, title string) {
	t.Helper()
	_, _ = admin.PostContext(context.Background(), apiURL, map[string]string{
		"action": "delete",
		"title":  title,
		"reason": "integration test cleanup",
	})
}

func pageExists(t *testing.T, c *api.Client, apiURL, title string) bool {
	t.Helper()
	exists, err := (&liveProvider{client: c, apiURL: apiURL, ctx: context.Background()}).PagesExist([]string{title})
	require.NoError(t, err)
	return exists[title]
}

// deleteViaPlan runs the real workflow execution path (BuildPlan → DeleteTranslator → HttpExecutor), one result per op.
func deleteViaPlan(t *testing.T, c *api.Client, apiURL string, caps api.WikiCapabilities, titles []string, reason string) []api.APIResult {
	t.Helper()
	provider := &liveProvider{client: c, apiURL: apiURL, caps: caps, ctx: context.Background()}
	plan, err := deletion.BuildPlan(provider, titles, deletion.PlanOptions{Reason: reason})
	require.NoError(t, err)
	executor, err := api.NewHttpExecutor(c, apiURL)
	require.NoError(t, err)
	results, err := deletion.ExecutePlan(context.Background(), plan.ExecutionPlan(), api.DeleteTranslator{}, caps, executor)
	require.NoError(t, err)
	return results
}

// allPagesWithPrefix lists ns0 pages under prefix, forcing continuation via a tiny page size — a missed page means a
// continuation token was ignored.
func allPagesWithPrefix(t *testing.T, c *api.Client, apiURL, prefix string, limit int) []string {
	t.Helper()
	params := map[string]string{
		"action":        "query",
		"list":          "allpages",
		"apprefix":      prefix,
		"apnamespace":   "0",
		"aplimit":       strconv.Itoa(limit),
		"formatversion": "2",
	}
	var titles []string
	for {
		payload, err := c.GetContext(context.Background(), apiURL, params)
		require.NoError(t, err)
		query, _ := payload["query"].(map[string]any)
		pages, _ := query["allpages"].([]any)
		for _, raw := range pages {
			entry, _ := raw.(map[string]any)
			if ti, _ := entry["title"].(string); ti != "" {
				titles = append(titles, ti)
			}
		}
		cont, _ := payload["continue"].(map[string]any)
		next, _ := cont["apcontinue"].(string)
		if next == "" {
			break
		}
		params["apcontinue"] = next
	}
	return titles
}

// liveProvider is a minimal ops.DataProvider backed by the live API, implementing only what deletion.BuildPlan uses
// (existence, redirects, talk/subject mapping).
type liveProvider struct {
	client *api.Client
	apiURL string
	caps   api.WikiCapabilities
	ctx    context.Context
}

func (p *liveProvider) GetRevisions(string) ([]ops.Revision, error)        { return nil, nil }
func (p *liveProvider) GetPageInfo(string) (*ops.PageInfo, error)          { return nil, nil }
func (p *liveProvider) GetDeletedRevisions(string) ([]ops.Revision, error) { return nil, nil }
func (p *liveProvider) GetBlockInfo(string) (*ops.BlockInfo, error)        { return nil, nil }
func (p *liveProvider) GetUserInfo() (*ops.UserInfo, error)                { return nil, nil }
func (p *liveProvider) GetSiteInfo() (*ops.SiteInfo, error)                { return nil, nil }

func (p *liveProvider) GetTalkPageTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", nil
	}
	nsID, remainder, ok := api.SplitKnownNamespace(p.caps, title)
	if ok && nsID%2 == 1 {
		return "", nil
	}
	if !ok {
		return api.PreferredNamespacePrefix(p.caps, 1, "Talk") + ":" + title, nil
	}
	if nsID < 0 {
		return "", nil
	}
	prefix := api.PreferredNamespacePrefix(p.caps, nsID+1, "")
	if prefix == "" {
		return "", nil
	}
	return prefix + ":" + remainder, nil
}

func (p *liveProvider) GetSubjectPageTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", nil
	}
	nsID, remainder, ok := api.SplitKnownNamespace(p.caps, title)
	if !ok || nsID%2 == 0 {
		return "", nil
	}
	if nsID-1 == 0 {
		return remainder, nil
	}
	prefix := api.PreferredNamespacePrefix(p.caps, nsID-1, "")
	if prefix == "" {
		return "", nil
	}
	return prefix + ":" + remainder, nil
}

func (p *liveProvider) PagesExist(titles []string) (map[string]bool, error) {
	result := make(map[string]bool, len(titles))
	batchSize := 50
	if p.caps.HasHighLimits {
		batchSize = 500
	}
	for start := 0; start < len(titles); start += batchSize {
		end := start + batchSize
		if end > len(titles) {
			end = len(titles)
		}
		payload, err := p.client.GetContext(p.ctx, p.apiURL, map[string]string{
			"action":        "query",
			"prop":          "info",
			"titles":        strings.Join(titles[start:end], "|"),
			"formatversion": "2",
		})
		if err != nil {
			return nil, err
		}
		query, _ := payload["query"].(map[string]any)
		requestedFor := map[string]string{}
		if normalized, ok := query["normalized"].([]any); ok {
			for _, raw := range normalized {
				entry, _ := raw.(map[string]any)
				from, _ := entry["from"].(string)
				to, _ := entry["to"].(string)
				if from != "" && to != "" {
					requestedFor[to] = from
				}
			}
		}
		pages, _ := query["pages"].([]any)
		for _, raw := range pages {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			pageTitle, _ := entry["title"].(string)
			key := pageTitle
			if original, ok := requestedFor[pageTitle]; ok {
				key = original
			}
			_, missing := entry["missing"]
			result[key] = !missing
		}
	}
	return result, nil
}

func (p *liveProvider) GetRedirects(title string) ([]string, error) {
	params := map[string]string{
		"action":        "query",
		"list":          "backlinks",
		"bltitle":       title,
		"bllimit":       "max",
		"blfilterredir": "redirects",
		"formatversion": "2",
	}
	var redirects []string
	for {
		payload, err := p.client.GetContext(p.ctx, p.apiURL, params)
		if err != nil {
			return nil, err
		}
		query, _ := payload["query"].(map[string]any)
		items, _ := query["backlinks"].([]any)
		for _, raw := range items {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if name, _ := entry["title"].(string); strings.TrimSpace(name) != "" {
				redirects = append(redirects, name)
			}
		}
		cont, _ := payload["continue"].(map[string]any)
		next, _ := cont["blcontinue"].(string)
		if next == "" {
			break
		}
		params["blcontinue"] = next
	}
	return redirects, nil
}
