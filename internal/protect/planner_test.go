package protect

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/ops"
)

type fakeReader struct{ data map[string]PageProtection }

func (f fakeReader) PageProtections(_ context.Context, titles []string) (map[string]PageProtection, error) {
	out := map[string]PageProtection{}
	for _, t := range titles {
		if p, ok := f.data[t]; ok {
			out[t] = p
		}
	}
	return out, nil
}

func itemFor(t *testing.T, plan Plan, title string) PlanItem {
	t.Helper()
	for _, it := range plan.Items {
		if it.Title == title {
			return it
		}
	}
	t.Fatalf("no plan item for %q", title)
	return PlanItem{}
}

// Existing pages take edit/move and never send create; missing titles take create and never send edit/move — matching
// MediaWiki's atomic type↔existence rule (create-titleexists / missingtitle-createonly).
func TestBuildPlanFiltersTypesByExistence(t *testing.T) {
	t.Parallel()

	reader := fakeReader{data: map[string]PageProtection{
		"Foo": {Title: "Foo", Exists: true},  // existing, currently unprotected
		"Bar": {Title: "Bar", Exists: false}, // missing title
	}}
	settings := Settings{ByType: map[string]TypeSetting{
		"edit":   {Level: "sysop"},
		"move":   {Level: "sysop"},
		"create": {Level: "sysop"},
	}}

	plan, err := BuildPlan(context.Background(), reader, []string{"Foo", "Bar"}, settings, []string{"sysop"}, nil)
	require.NoError(t, err)

	foo := itemFor(t, plan, "Foo").Op.Params
	require.Equal(t, "sysop", foo["protect_edit"])
	require.Equal(t, "sysop", foo["protect_move"])
	require.NotContains(t, foo, "protect_create", "existing page must not send create")

	bar := itemFor(t, plan, "Bar").Op.Params
	require.Equal(t, "sysop", bar["protect_create"])
	require.NotContains(t, bar, "protect_edit", "missing title must not send edit")
	require.NotContains(t, bar, "protect_move")
}

// Make-permanent: keep the current level, set expiry to infinite. A temporary sysop protection becomes permanent.
func TestBuildPlanMakePermanent(t *testing.T) {
	t.Parallel()

	reader := fakeReader{data: map[string]PageProtection{
		"Foo": {Title: "Foo", Exists: true, Protections: map[string]TypeProtection{
			"edit": {Level: "sysop", Expiry: "2026-12-31T00:00:00Z"},
		}},
	}}
	settings := Settings{ByType: map[string]TypeSetting{
		"edit": {KeepLevel: true, Expiry: "infinite"},
		"move": {KeepLevel: true, KeepExpiry: true}, // preserve (currently none)
	}}

	plan, err := BuildPlan(context.Background(), reader, []string{"Foo"}, settings, []string{"sysop"}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, plan.Change)
	op := itemFor(t, plan, "Foo").Op.Params
	require.Equal(t, "sysop", op["protect_edit"])
	require.Equal(t, "infinite", op["expiry_edit"])
}

// Two already-at-target pages produce no operations.
func TestBuildPlanNoOp(t *testing.T) {
	t.Parallel()

	reader := fakeReader{data: map[string]PageProtection{
		"Foo": {Title: "Foo", Exists: true, Protections: map[string]TypeProtection{
			"edit": {Level: "sysop", Expiry: "infinity"},
		}},
	}}
	settings := Settings{ByType: map[string]TypeSetting{
		"edit": {Level: "sysop", Expiry: "infinite"}, // same as current (permanent spellings fold together)
		"move": {KeepLevel: true, KeepExpiry: true},
	}}

	plan, err := BuildPlan(context.Background(), reader, []string{"Foo"}, settings, []string{"sysop"}, nil)
	require.NoError(t, err)
	require.Equal(t, 0, plan.Change)
	require.Equal(t, 1, plan.Unchanged)
	require.False(t, itemFor(t, plan, "Foo").Changed)
	require.Equal(t, ops.Operation{}, itemFor(t, plan, "Foo").Op, "unchanged pages emit no op")
}

// Unprotect: removing edit protection on a currently-protected page is a change and sends an empty level.
func TestBuildPlanUnprotect(t *testing.T) {
	t.Parallel()

	reader := fakeReader{data: map[string]PageProtection{
		"Foo": {Title: "Foo", Exists: true, Protections: map[string]TypeProtection{
			"edit": {Level: "sysop", Expiry: "infinity"},
		}},
	}}
	settings := Settings{ByType: map[string]TypeSetting{
		"edit": {Level: ""}, // remove
		"move": {KeepLevel: true, KeepExpiry: true},
	}}

	plan, err := BuildPlan(context.Background(), reader, []string{"Foo"}, settings, []string{"sysop"}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, plan.Change)
	require.Empty(t, itemFor(t, plan, "Foo").Op.Params["protect_edit"])
}

// Cascade is only valid with a cascading level; a non-cascading level flags the page invalid (no op), a cascading one
// sets cascade on the op.
func TestBuildPlanCascadeValidity(t *testing.T) {
	t.Parallel()

	reader := fakeReader{data: map[string]PageProtection{
		"Foo": {Title: "Foo", Exists: true},
	}}
	cascading := []string{"sysop"}

	invalid, err := BuildPlan(context.Background(), reader, []string{"Foo"},
		Settings{Cascade: true, ByType: map[string]TypeSetting{"edit": {Level: "autoconfirmed"}}}, cascading, nil)
	require.NoError(t, err)
	require.Equal(t, 1, invalid.Invalid)
	require.True(t, itemFor(t, invalid, "Foo").Invalid)
	require.Equal(t, "autoconfirmed", itemFor(t, invalid, "Foo").InvalidLevel, "the blocking level is reported")
	require.Equal(t, ops.Operation{}, itemFor(t, invalid, "Foo").Op)

	valid, err := BuildPlan(context.Background(), reader, []string{"Foo"}, Settings{
		Cascade: true,
		ByType:  map[string]TypeSetting{"edit": {Level: "sysop"}, "move": {Level: "sysop"}},
	}, cascading, nil)
	require.NoError(t, err)
	require.Equal(t, 1, valid.Change)
	require.Equal(t, "true", itemFor(t, valid, "Foo").Op.Params["cascade"])
}

// A File page's upload protection is preserved when edit/move change: action=protect replaces the whole set, so the
// upload type must be resent or it would be silently removed.
func TestBuildPlanPreservesUnmanagedProtection(t *testing.T) {
	t.Parallel()

	reader := fakeReader{data: map[string]PageProtection{
		"File:Logo.png": {Title: "File:Logo.png", Exists: true, Protections: map[string]TypeProtection{
			"edit":   {Level: "autoconfirmed", Expiry: "infinity"},
			"upload": {Level: "sysop", Expiry: "infinity"},
		}},
	}}
	settings := Settings{ByType: map[string]TypeSetting{
		"edit": {Level: "sysop"}, // changes edit
		"move": {KeepLevel: true, KeepExpiry: true},
	}}

	plan, err := BuildPlan(context.Background(), reader, []string{"File:Logo.png"}, settings, []string{"sysop"}, nil)
	require.NoError(t, err)
	op := itemFor(t, plan, "File:Logo.png").Op.Params
	require.Equal(t, "sysop", op["protect_edit"])
	require.Equal(t, "sysop", op["protect_upload"], "upload protection must be preserved in the full replacement")
	require.Equal(t, "infinity", op["expiry_upload"])
}

// Toggling cascade off while keeping level and expiry is a change and emits an op, even though no level/expiry differs.
func TestBuildPlanCascadeOnlyChange(t *testing.T) {
	t.Parallel()

	reader := fakeReader{data: map[string]PageProtection{
		"Foo": {Title: "Foo", Exists: true, Protections: map[string]TypeProtection{
			"edit": {Level: "sysop", Expiry: "infinity", Cascade: true},
		}},
	}}
	settings := Settings{Cascade: false, ByType: map[string]TypeSetting{
		"edit": {KeepLevel: true, KeepExpiry: true}, // keep edit exactly as-is
		"move": {KeepLevel: true, KeepExpiry: true},
	}}

	plan, err := BuildPlan(context.Background(), reader, []string{"Foo"}, settings, []string{"sysop"}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, plan.Change)
	item := itemFor(t, plan, "Foo")
	require.True(t, item.Changed)
	require.True(t, item.FromCascade)
	require.False(t, item.ToCascade)
	require.NotContains(t, item.Op.Params, "cascade", "removing cascade omits the flag so action=protect drops it")
}

// Cascade is a property of an existing page's edit restriction. Requesting it on a change that leaves edit unprotected
// (a move-only change) neither sets the flag nor invalidates the change — MediaWiki would simply discard the flag, and
// the non-cascading move level is irrelevant to cascade eligibility.
func TestBuildPlanCascadeIgnoredWithoutEditProtection(t *testing.T) {
	t.Parallel()

	reader := fakeReader{data: map[string]PageProtection{"Foo": {Title: "Foo", Exists: true}}}
	settings := Settings{Cascade: true, ByType: map[string]TypeSetting{
		"move": {Level: "autoconfirmed"}, // move only; edit stays unprotected
	}}

	plan, err := BuildPlan(context.Background(), reader, []string{"Foo"}, settings, []string{"sysop"}, nil)
	require.NoError(t, err)
	item := itemFor(t, plan, "Foo")
	require.False(t, item.Invalid, "a non-cascading move level must not invalidate a cascade request")
	require.False(t, item.ToCascade)
	require.NotContains(t, item.Op.Params, "cascade")
}

// Cascade is disabled for missing titles: requesting it on a create protection neither sets the flag nor invalidates
// the change, even when the create level is non-cascading.
func TestBuildPlanCascadeIgnoredForMissingTitle(t *testing.T) {
	t.Parallel()

	reader := fakeReader{data: map[string]PageProtection{"Bar": {Title: "Bar", Exists: false}}}
	settings := Settings{Cascade: true, ByType: map[string]TypeSetting{
		"create": {Level: "autoconfirmed"},
	}}

	plan, err := BuildPlan(context.Background(), reader, []string{"Bar"}, settings, []string{"sysop"}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, plan.Change) // the create protection itself is still a change
	item := itemFor(t, plan, "Bar")
	require.False(t, item.Invalid, "cascade is disabled for missing titles, so it must not reject a create change")
	require.False(t, item.ToCascade)
	require.NotContains(t, item.Op.Params, "cascade")
}

// A wiki that doesn't offer "move" must not have move planned: naming an unsupported type makes action=protect reject
// the whole request.
func TestBuildPlanRestrictsToWikiTypes(t *testing.T) {
	t.Parallel()

	reader := fakeReader{data: map[string]PageProtection{"Foo": {Title: "Foo", Exists: true}}}
	settings := Settings{ByType: map[string]TypeSetting{
		"edit": {Level: "sysop"},
		"move": {Level: "sysop"},
	}}

	plan, err := BuildPlan(context.Background(), reader, []string{"Foo"}, settings, []string{"sysop"}, []string{"edit"})
	require.NoError(t, err)
	op := itemFor(t, plan, "Foo").Op.Params
	require.Equal(t, "sysop", op["protect_edit"])
	require.NotContains(t, op, "protect_move", "a wiki that doesn't offer move must not plan it")
}

// Aliases that MediaWiki normalizes to one page emit a single write, titled by the normalized page.
func TestBuildPlanDeduplicatesNormalizedTitles(t *testing.T) {
	t.Parallel()

	page := PageProtection{Title: "Foo bar", Exists: true}
	reader := fakeReader{data: map[string]PageProtection{
		"Foo bar": page,
		"Foo_bar": page, // the underscore alias resolves to the same normalized page
	}}
	settings := Settings{ByType: map[string]TypeSetting{"edit": {Level: "sysop"}}}

	plan, err := BuildPlan(
		context.Background(), reader, []string{"Foo_bar", "Foo bar"}, settings, []string{"sysop"}, nil,
	)
	require.NoError(t, err)
	require.Len(t, plan.Items, 1)
	require.Equal(t, "Foo bar", plan.Items[0].Title)
}
