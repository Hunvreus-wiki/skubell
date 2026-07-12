//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/api"
)

// N2 — successful login and full connection sequence.
func TestN2ConnectionSequenceSucceeds(t *testing.T) {
	url := apiURL(t)
	_, res := connectBot(t, url, botAdmin, adminBotPassword(url))

	require.Equal(t, "TestAdmin", res.Username)
	// The SkubellTest bot password grants basic,highvolume,delete — so the session holds delete but not editinterface
	// (that is the SkubellIface app).
	require.True(t, api.HasRight(res.Capabilities, "delete"), "admin should hold delete")
	require.False(t, res.Capabilities.SitewideBlock)
	require.NotEmpty(t, res.Capabilities.Namespaces, "siteinfo namespaces should be populated")
	require.Positive(t, res.Capabilities.PageCount, "siteinfo statistics should report the seeded pages")
	require.True(t, api.EvaluateWorkflowAvailability(res.Capabilities.UserRights)[api.WorkflowDeletion].Available)
}

// N3 — authentication failure with a wrong (but well-formed) bot password.
func TestN3WrongPassword(t *testing.T) {
	url := apiURL(t)
	c := newClient(t)
	// 32 chars, valid bot-password charset, but not the real password.
	_, err := api.LoginContext(context.Background(), c, url, botAdmin, strings.Repeat("a", 32))
	require.Error(t, err)
	require.ErrorIs(t, err, api.ErrAuthenticationFailed)
}

// N4 — authentication failure for a nonexistent user. MediaWiki deliberately returns the same generic reason as a wrong
// password (no user enumeration), so we assert the same error class rather than a distinguishable one.
func TestN4NonexistentUser(t *testing.T) {
	url := apiURL(t)
	c := newClient(t)
	_, err := api.LoginContext(context.Background(), c, url, "NoSuchUser@SkubellTest", strings.Repeat("a", 32))
	require.Error(t, err)
	require.ErrorIs(t, err, api.ErrAuthenticationFailed)
}

// N5 — a credential that is not a bot-password login (bare account name, no @AppId). MediaWiki does not treat it as a
// bot login, and it fails to authenticate.
func TestN5NonBotPasswordFormat(t *testing.T) {
	url := apiURL(t)
	c := newClient(t)
	_, err := api.LoginContext(context.Background(), c, url, webAdminUser, adminBotPassword(url))
	require.Error(t, err)
	require.ErrorIs(t, err, api.ErrAuthenticationFailed)
}

// N6 — insufficient rights: a default user (no delete) is reported unavailable for deletion, and forcing a delete
// produces a clean failure, not a panic.
func TestN6InsufficientRightsDeletionDenied(t *testing.T) {
	url := apiURL(t)
	c, res := connectBot(t, url, botEditor, botEditorPass)

	require.False(t, api.HasRight(res.Capabilities, "delete"))
	require.False(t, api.EvaluateWorkflowAvailability(res.Capabilities.UserRights)[api.WorkflowDeletion].Available)

	admin := loginWeb(t, url, webAdminUser, webAdminPass)
	title := uniqueTitle(t)
	editPage(t, admin, url, title, "integration N6")
	defer deleteAsAdmin(t, admin, url, title)

	results := deleteViaPlan(t, c, url, res.Capabilities, []string{title}, "N6 forced delete")
	require.Len(t, results, 1)
	require.False(t, results[0].Success)
	require.NotNil(t, results[0].Error)
	require.True(t, pageExists(t, admin, url, title), "page must survive a denied delete")
}

// N8 — a sitewide-blocked user: block detected at connection; a forced delete fails with the 'blocked' code, not a panic.
func TestN8SitewideBlockedUser(t *testing.T) {
	url := apiURL(t)
	c, res := connectBot(t, url, botBlocked, botBlockedPass)
	require.True(t, res.Capabilities.SitewideBlock, "sitewide block should be detected at connect")

	admin := loginWeb(t, url, webAdminUser, webAdminPass)
	title := uniqueTitle(t)
	editPage(t, admin, url, title, "integration N8")
	defer deleteAsAdmin(t, admin, url, title)

	results := deleteViaPlan(t, c, url, res.Capabilities, []string{title}, "N8 forced delete")
	require.Len(t, results, 1)
	require.False(t, results[0].Success)
	require.NotNil(t, results[0].Error)
	require.Contains(t, strings.ToLower(results[0].Error.Code), "blocked")
	require.True(t, pageExists(t, admin, url, title))
}

// N8b — a partially blocked (namespace 0) user: succeeds outside ns0, fails inside.
func TestN8bPartiallyBlockedUser(t *testing.T) {
	url := apiURL(t)
	c, res := connectBot(t, url, botPartial, botPartialPass)
	require.False(t, res.Capabilities.SitewideBlock, "partial block is not a sitewide block")

	admin := loginWeb(t, url, webAdminUser, webAdminPass)
	mainPage := uniqueTitle(t)                 // namespace 0 — restricted
	projectPage := "Project:" + uniqueTitle(t) // namespace 4 — allowed
	editPage(t, admin, url, mainPage, "integration N8b main")
	editPage(t, admin, url, projectPage, "integration N8b project")
	defer deleteAsAdmin(t, admin, url, mainPage)
	defer deleteAsAdmin(t, admin, url, projectPage)

	blocked := deleteViaPlan(t, c, url, res.Capabilities, []string{mainPage}, "N8b restricted")
	require.Len(t, blocked, 1)
	require.False(t, blocked[0].Success, "delete in the restricted namespace must fail")
	require.True(t, pageExists(t, admin, url, mainPage))

	allowed := deleteViaPlan(t, c, url, res.Capabilities, []string{projectPage}, "N8b allowed")
	require.Len(t, allowed, 1)
	require.True(t, allowed[0].Success, "delete outside the restricted namespace must succeed: %+v", allowed[0].Error)
	require.False(t, pageExists(t, admin, url, projectPage))
}

// N9 — the deletion round-trip: seed a page, delete it via the real workflow path, confirm it is gone. This is the core
// end-to-end verification.
func TestN9DeletionRoundTrip(t *testing.T) {
	url := apiURL(t)
	c, res := connectBot(t, url, botAdmin, adminBotPassword(url))
	admin := loginWeb(t, url, webAdminUser, webAdminPass)

	title := uniqueTitle(t)
	editPage(t, admin, url, title, "integration N9 round-trip")
	require.True(t, pageExists(t, admin, url, title), "seed page should exist before deletion")

	results := deleteViaPlan(t, c, url, res.Capabilities, []string{title}, "N9 round-trip")
	require.Len(t, results, 1)
	require.True(t, results[0].Success, "delete failed: %+v", results[0].Error)
	require.False(t, pageExists(t, admin, url, title), "page should be gone after deletion")
}

// N10 — deleting an already-deleted page: the failure is recorded, not fatal.
func TestN10DeleteAlreadyDeleted(t *testing.T) {
	url := apiURL(t)
	c, res := connectBot(t, url, botAdmin, adminBotPassword(url))
	admin := loginWeb(t, url, webAdminUser, webAdminPass)

	title := uniqueTitle(t)
	editPage(t, admin, url, title, "integration N10")

	first := deleteViaPlan(t, c, url, res.Capabilities, []string{title}, "N10 first")
	require.Len(t, first, 1)
	require.True(t, first[0].Success, "first delete should succeed: %+v", first[0].Error)

	second := deleteViaPlan(t, c, url, res.Capabilities, []string{title}, "N10 second")
	require.Len(t, second, 1)
	require.False(t, second[0].Success, "second delete of a missing page should fail")
	require.NotNil(t, second[0].Error)
}

// N11 — continuation coverage: with a tiny page size, every seeded page must still be collected. A missing page means a
// continuation token was ignored.
func TestN11PaginationContinuation(t *testing.T) {
	url := apiURL(t)
	admin := loginWeb(t, url, webAdminUser, webAdminPass)

	prefix := uniqueTitle(t) // unique ns0 prefix for this run
	const n = 5
	seeded := make([]string, 0, n)
	for i := 0; i < n; i++ {
		title := fmt.Sprintf("%s_%02d", prefix, i)
		editPage(t, admin, url, title, "integration N11 pagination")
		seeded = append(seeded, title)
	}
	defer func() {
		for _, title := range seeded {
			deleteAsAdmin(t, admin, url, title)
		}
	}()

	got := allPagesWithPrefix(t, admin, url, prefix, 2) // aplimit=2 forces continuation
	require.Len(t, got, n, "continuation token ignored — expected %d pages, got %d", n, len(got))
}
