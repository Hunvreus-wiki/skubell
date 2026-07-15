package ui

import (
	"testing"

	"github.com/Hunvreus-wiki/skubell/internal/api"
)

func TestNormalizeManualTitle(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  string
	}{
		{input: "* [[Foo|Bar]]", want: "Foo"},
		{input: "# Baz", want: "Baz"},
		{input: "[[Talk:Apple]]", want: "Talk:Apple"},
		{input: "  ", want: ""},
	}

	for _, tc := range cases {
		if got := normalizeManualTitle(tc.input); got != tc.want {
			t.Fatalf("normalizeManualTitle(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestReasonSelectOptionsAlwaysIncludesNone(t *testing.T) {
	t.Parallel()

	screen := &deleteWorkflowScreen{
		reasons: []string{"Reason B", "Reason A"},
	}

	options := screen.reasonSelectOptions()
	if len(options) < 3 {
		t.Fatalf("expected at least 3 options, got %v", options)
	}
	if options[0] != "(none)" {
		t.Fatalf("expected first option to be (none), got %q", options[0])
	}
}

func TestValidateOptionsRequiresTextWhenNone(t *testing.T) {
	t.Parallel()

	screen := &deleteWorkflowScreen{
		optionReasonChoice:   "(none)",
		optionReasonFreeText: "",
	}
	if err := screen.validateOptions(); err == nil {
		t.Fatalf("expected validation error when (none) is selected without additional text")
	}

	screen.optionReasonFreeText = "Custom reason"
	if err := screen.validateOptions(); err != nil {
		t.Fatalf("expected validation success with additional text, got %v", err)
	}
}

func TestValidateMediaWikiNamespaceDeleteAccessRequiresEditInterface(t *testing.T) {
	t.Parallel()

	screen := &deleteWorkflowScreen{
		app: &App{
			currentCaps: api.WikiCapabilities{
				Namespaces: map[int]string{
					api.MediaWikiNamespaceID: "MediaWiki",
				},
				UserRights: []string{"delete"},
			},
		},
	}

	msg := screen.validateMediaWikiNamespaceDeleteAccess([]string{"MediaWiki:Sidebar"})
	if msg == "" {
		t.Fatalf("expected validation error without editinterface")
	}
	if msg != api.MediaWikiNamespaceDeleteGrantMessage() {
		t.Fatalf("unexpected message: %v", msg)
	}

	msg = screen.validateMediaWikiNamespaceDeleteAccess([]string{"Apple"})
	if msg != "" {
		t.Fatalf("expected non-MediaWiki title to pass, got %v", msg)
	}
}

func TestValidateMediaWikiNamespaceDeleteAccessAllowsEditInterfaceOrDryRun(t *testing.T) {
	t.Parallel()

	screen := &deleteWorkflowScreen{
		app: &App{
			currentCaps: api.WikiCapabilities{
				UserRights: []string{"delete", "editinterface"},
			},
		},
	}
	if msg := screen.validateMediaWikiNamespaceDeleteAccess([]string{"MediaWiki:Sidebar"}); msg != "" {
		t.Fatalf("expected editinterface to pass, got %v", msg)
	}

	screen.app.currentCaps.UserRights = []string{"delete"}
	screen.optionDryRun = true
	if msg := screen.validateMediaWikiNamespaceDeleteAccess([]string{"MediaWiki:Sidebar"}); msg != "" {
		t.Fatalf("expected dry-run to pass, got %v", msg)
	}
}

// A sysop with editinterface can delete MediaWiki message/JSON pages but must
// still be blocked on sitewide CSS/JS, which need editsitecss/editsitejs.
func TestValidateMediaWikiNamespaceDeleteAccessGatesSiteCSSAndJS(t *testing.T) {
	t.Parallel()

	screen := &deleteWorkflowScreen{
		app: &App{
			currentCaps: api.WikiCapabilities{
				UserRights: []string{"delete", "editinterface", "editsitejson"},
			},
		},
	}

	if msg := screen.validateMediaWikiNamespaceDeleteAccess([]string{"MediaWiki:Gadgets-definition.json"}); msg != "" {
		t.Fatalf("expected JSON delete to pass with editsitejson, got %v", msg)
	}
	if msg := screen.validateMediaWikiNamespaceDeleteAccess(
		[]string{"MediaWiki:Common.css"},
	); msg != api.SiteCSSDeleteGrantMessage() {
		t.Fatalf("expected sitecss message, got %v", msg)
	}
	if msg := screen.validateMediaWikiNamespaceDeleteAccess(
		[]string{"MediaWiki:Common.js"},
	); msg != api.SiteJSDeleteGrantMessage() {
		t.Fatalf("expected sitejs message, got %v", msg)
	}
	// A batch mixing an allowed page and a blocked one is blocked.
	if msg := screen.validateMediaWikiNamespaceDeleteAccess(
		[]string{"Apple", "MediaWiki:Common.js"},
	); msg != api.SiteJSDeleteGrantMessage() {
		t.Fatalf("expected batch with sitejs page to be blocked, got %v", msg)
	}
	// User-space scripts are not gated on delete.
	if msg := screen.validateMediaWikiNamespaceDeleteAccess([]string{"User:Toto/common.js"}); msg != "" {
		t.Fatalf("expected user-space js delete to pass, got %v", msg)
	}
}
