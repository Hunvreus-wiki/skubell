package protect

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/ops"
)

type fakeReader struct{ data map[string]PageProtection }

func (f fakeReader) PageProtections(titles []string) (map[string]PageProtection, error) {
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

	plan, err := BuildPlan(reader, []string{"Foo", "Bar"}, settings, []string{"sysop"})
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

	plan, err := BuildPlan(reader, []string{"Foo"}, settings, []string{"sysop"})
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

	plan, err := BuildPlan(reader, []string{"Foo"}, settings, []string{"sysop"})
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

	plan, err := BuildPlan(reader, []string{"Foo"}, settings, []string{"sysop"})
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

	invalid, err := BuildPlan(reader, []string{"Foo"},
		Settings{Cascade: true, ByType: map[string]TypeSetting{"edit": {Level: "autoconfirmed"}}}, cascading)
	require.NoError(t, err)
	require.Equal(t, 1, invalid.Invalid)
	require.True(t, itemFor(t, invalid, "Foo").Invalid)
	require.Equal(t, ops.Operation{}, itemFor(t, invalid, "Foo").Op)

	valid, err := BuildPlan(reader, []string{"Foo"}, Settings{
		Cascade: true,
		ByType:  map[string]TypeSetting{"edit": {Level: "sysop"}, "move": {Level: "sysop"}},
	}, cascading)
	require.NoError(t, err)
	require.Equal(t, 1, valid.Change)
	require.Equal(t, "true", itemFor(t, valid, "Foo").Op.Params["cascade"])
}
