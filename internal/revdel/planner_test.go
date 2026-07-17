package revdel

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/ops"
)

func TestBuildPlanGranularChangeDetection(t *testing.T) {
	t.Parallel()

	revisions := []Revision{
		{ID: 1},                      // content visible → hidden: changes
		{ID: 2, ContentHidden: true}, // content already hidden, comment already visible: no-op
		{ID: 3, ContentHidden: true, UserHidden: true}, // untouched user stays hidden; content no-op: no-op
		{ID: 4, CommentHidden: true},                   // comment hidden → visible, content → hidden: changes
	}
	settings := Settings{Content: FieldDeleted, Comment: FieldVisible, Reason: "Cleanup"}

	plan := BuildPlan(revisions, settings, 50)
	require.Len(t, plan.Items, 4)
	require.Equal(t, 2, plan.Change)
	require.Equal(t, 2, plan.Unchanged)
	require.Equal(t, 0, plan.Skipped)

	require.True(t, plan.Items[0].Changed)
	require.Equal(t, []FieldChange{{Field: FieldContent, ToHidden: true}}, plan.Items[0].Changes)
	require.False(t, plan.Items[1].Changed)
	require.False(t, plan.Items[2].Changed)
	require.True(t, plan.Items[3].Changed)
	require.Equal(t, []FieldChange{
		{Field: FieldContent, ToHidden: true},
		{Field: FieldComment, ToHidden: false},
	}, plan.Items[3].Changes)

	// Only the changing revisions are batched; hide/show come from the settings, the reason rides along.
	require.Len(t, plan.Ops, 1)
	op := plan.Ops[0]
	require.Equal(t, ops.OpRevisionDelete, op.Type)
	require.Equal(t, "1|4", op.Params["ids"])
	require.Equal(t, "content", op.Params["hide"])
	require.Equal(t, "comment", op.Params["show"])
	require.Equal(t, "nochange", op.Params["suppress"])
	require.Equal(t, "Cleanup", op.Params["reason"])
}

func TestBuildPlanSuppression(t *testing.T) {
	t.Parallel()

	revisions := []Revision{
		{ID: 10}, // fully visible → all three fields change
		{
			ID:            11,
			Suppressed:    true,
			ContentHidden: true,
			CommentHidden: true,
			UserHidden:    true,
		}, // fully suppressed already: no-op
		{
			ID:            12,
			Suppressed:    true,
			ContentHidden: true,
		}, // suppressed but partially hidden → the visible fields change; hidden-and-suppressed content does not
	}
	settings := Settings{
		Suppress: SuppressYes, Content: FieldDeleted, Comment: FieldDeleted, User: FieldDeleted,
		Reason: "Oversight",
	}

	plan := BuildPlan(revisions, settings, 50)
	require.Equal(t, 2, plan.Change)
	require.Equal(t, 1, plan.Unchanged)
	require.True(t, plan.Items[0].Suppress)
	require.Len(t, plan.Items[0].Changes, 3)
	require.Equal(t, []FieldChange{
		{Field: FieldComment, ToHidden: true},
		{Field: FieldUser, ToHidden: true},
	}, plan.Items[2].Changes)

	require.Len(t, plan.Ops, 1)
	op := plan.Ops[0]
	require.Equal(t, ops.OpRevisionDelete, op.Type)
	require.Equal(t, "10|12", op.Params["ids"])
	require.Equal(t, "content|comment|user", op.Params["hide"])
	require.Equal(t, "yes", op.Params["suppress"])
}

// The suppression bit is revision-wide and flips only when a field is (re-)sent in hide: a level flip makes an
// already-hidden targeted field a change (escalate deleted→suppressed, downgrade suppressed→deleted), while a
// matching level keeps it a no-op.
func TestBuildPlanLevelChanges(t *testing.T) {
	t.Parallel()

	hidden := Revision{ID: 40, ContentHidden: true}
	suppressed := Revision{ID: 41, ContentHidden: true, Suppressed: true}

	escalate := BuildPlan([]Revision{hidden, suppressed}, Settings{Suppress: SuppressYes, Content: FieldDeleted}, 50)
	require.Equal(t, 1, escalate.Change)
	require.True(t, escalate.Items[0].Changed, "deleted → suppressed is a change")
	require.Equal(t, []FieldChange{{Field: FieldContent, ToHidden: true}}, escalate.Items[0].Changes)
	require.False(t, escalate.Items[1].Changed, "already suppressed")
	require.Equal(t, "content", escalate.Ops[0].Params["hide"])
	require.Equal(t, "yes", escalate.Ops[0].Params["suppress"])

	downgrade := BuildPlan([]Revision{hidden, suppressed}, Settings{Suppress: SuppressNo, Content: FieldDeleted}, 50)
	require.False(t, downgrade.Items[0].Changed, "already plain-deleted")
	require.True(t, downgrade.Items[1].Changed, "suppressed → deleted is a change")
	require.Equal(t, "no", downgrade.Ops[0].Params["suppress"])

	// Without the suppression right the level is untouched, so bit-matching fields stay no-ops.
	nochange := BuildPlan(
		[]Revision{hidden, suppressed},
		Settings{Suppress: SuppressNoChange, Content: FieldDeleted},
		50,
	)
	require.Equal(t, 0, nochange.Change)
	require.Empty(t, nochange.Ops)
}

// The current revision can never be changed: even if one slips past the UI's guards, the planner skips it and never
// puts its ID into an operation.
func TestBuildPlanSkipsCurrentRevision(t *testing.T) {
	t.Parallel()

	revisions := []Revision{
		{ID: 20, Current: true},
		{ID: 21},
	}
	plan := BuildPlan(revisions, Settings{Content: FieldDeleted}, 50)
	require.Equal(t, 1, plan.Skipped)
	require.Equal(t, 1, plan.Change)
	require.True(t, plan.Items[0].Skipped)
	require.Len(t, plan.Ops, 1)
	require.Equal(t, "21", plan.Ops[0].Params["ids"])
}

func TestBuildPlanBatchesByLimit(t *testing.T) {
	t.Parallel()

	revisions := make([]Revision, 120)
	for i := range revisions {
		revisions[i] = Revision{ID: int64(i + 1)}
	}
	plan := BuildPlan(revisions, Settings{User: FieldDeleted}, 50)
	require.Equal(t, 120, plan.Change)
	require.Len(t, plan.Ops, 3)
	require.Len(t, strings.Split(plan.Ops[0].Params["ids"], "|"), 50)
	require.Len(t, strings.Split(plan.Ops[1].Params["ids"], "|"), 50)
	require.Len(t, strings.Split(plan.Ops[2].Params["ids"], "|"), 20)

	// A non-positive batch size falls back to the conservative non-apihighlimits cap instead of one giant call.
	fallback := BuildPlan(revisions, Settings{User: FieldDeleted}, 0)
	require.Len(t, fallback.Ops, 3)
}

func TestBuildPlanOmitsEmptyReasonAndEmptySettings(t *testing.T) {
	t.Parallel()

	plan := BuildPlan([]Revision{{ID: 30}}, Settings{Content: FieldDeleted, Reason: "  "}, 50)
	_, hasReason := plan.Ops[0].Params["reason"]
	require.False(t, hasReason)

	// All-no-change settings change nothing; the UI rejects them, and the planner emits no ops for them either.
	// The level alone counts for nothing: the API cannot apply it without a hide/show field.
	noop := Settings{}
	require.True(t, noop.ChangesNothing())
	require.True(t, Settings{Suppress: SuppressYes}.ChangesNothing())
	require.False(t, Settings{Comment: FieldVisible}.ChangesNothing())
	empty := BuildPlan([]Revision{{ID: 31}}, noop, 50)
	require.Equal(t, 1, empty.Unchanged)
	require.Empty(t, empty.Ops)
}
