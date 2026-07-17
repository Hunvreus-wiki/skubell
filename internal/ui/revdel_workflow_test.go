package ui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/api"
	"github.com/Hunvreus-wiki/skubell/internal/ops"
	"github.com/Hunvreus-wiki/skubell/internal/revdel"
)

// revdelTestScreen builds a screen with the visibility panel constructed, holding the given user rights.
func revdelTestScreen(rights ...string) *revdelWorkflowScreen {
	s := &revdelWorkflowScreen{
		app:          &App{currentCaps: api.WikiCapabilities{UserRights: rights}},
		selected:     map[int64]struct{}{},
		fieldSelects: map[string]*widget.Select{},
	}
	_ = s.buildVisibilityPanel()
	return s
}

// The Suppressed checkbox is the operation's level: toggling it renames the dropdowns' hidden state between
// "deleted" and "suppressed" while keeping each dropdown's selection — it changes what hiding means, not which
// fields are targeted.
func TestRevdelSuppressRelabelsHiddenState(t *testing.T) {
	// Not parallel: builds Fyne widgets, whose shared render caches are not safe for concurrent access.
	s := revdelTestScreen("deleterevision", "suppressrevision")
	require.False(t, s.suppressCheck.Disabled())

	s.fieldSelects[revdel.FieldContent].SetSelectedIndex(2) // deleted
	s.suppressCheck.SetChecked(true)
	for _, field := range revdelFields {
		require.Equal(t, "suppressed", s.fieldSelects[field].Options[2], "field %s renames its hidden state", field)
		require.False(t, s.fieldSelects[field].Disabled())
	}
	require.Equal(t, 2, s.fieldSelects[revdel.FieldContent].SelectedIndex(), "the selection survives the relabel")

	s.suppressCheck.SetChecked(false)
	require.Equal(t, "deleted", s.fieldSelects[revdel.FieldContent].Options[2])
	require.Equal(t, 2, s.fieldSelects[revdel.FieldContent].SelectedIndex())
}

// Without suppressrevision the Suppressed checkbox is disabled (not hidden). A suppressor-only session can only
// act at the suppression level: the checkbox is forced checked and disabled; the dropdowns work either way.
func TestRevdelRightsGating(t *testing.T) {
	admin := revdelTestScreen("deleterevision")
	require.True(t, admin.suppressCheck.Disabled())
	require.False(t, admin.suppressCheck.Checked)
	for _, field := range revdelFields {
		require.False(t, admin.fieldSelects[field].Disabled())
	}

	suppressor := revdelTestScreen("suppressrevision")
	require.True(t, suppressor.suppressCheck.Disabled())
	require.True(t, suppressor.suppressCheck.Checked, "a suppressor-only session hides at the suppression level")
	for _, field := range revdelFields {
		require.False(t, suppressor.fieldSelects[field].Disabled())
		require.Equal(t, "suppressed", suppressor.fieldSelects[field].Options[2])
	}
}

func TestRevdelCaptureOptions(t *testing.T) {
	s := revdelTestScreen("deleterevision", "suppressrevision")
	s.dryRunCheck.SetChecked(false)

	// No selection: rejected before looking at the settings.
	require.NotEmpty(t, s.captureOptions())

	// A selection with all-no-change settings is still rejected: nothing would happen.
	s.selected[41] = struct{}{}
	require.NotEmpty(t, s.captureOptions())

	// Granular settings map dropdown positions to planner targets.
	s.fieldSelects[revdel.FieldComment].SetSelectedIndex(1) // visible
	s.fieldSelects[revdel.FieldUser].SetSelectedIndex(2)    // deleted

	// The "(none)" reason requires additional reason text.
	require.NotEmpty(t, s.captureOptions())
	s.reasonEntry.SetText("housekeeping")
	require.Empty(t, s.captureOptions())
	require.Equal(t, "housekeeping", s.settings.Reason)
	// Unchecked with the suppression right: hiding is plain deletion, and the bit is actively cleared.
	require.Equal(t, revdel.SuppressNo, s.settings.Suppress)
	require.Equal(t, revdel.FieldNoChange, s.settings.Content)
	require.Equal(t, revdel.FieldVisible, s.settings.Comment)
	require.Equal(t, revdel.FieldDeleted, s.settings.User)

	// Checking Suppressed keeps the field targets and raises the level.
	s.suppressCheck.SetChecked(true)
	require.Empty(t, s.captureOptions())
	require.Equal(t, revdel.SuppressYes, s.settings.Suppress)
	require.Equal(t, revdel.FieldDeleted, s.settings.User)
}

// A non-suppressor cannot touch a suppressed field, so the UI locks both directions: targeting a field blocks
// selecting revisions that have it suppressed, and selecting such a revision pins that field's dropdown to
// "(no change)".
func TestRevdelSuppressedFieldLocks(t *testing.T) {
	// Not parallel: builds Fyne widgets, whose shared render caches are not safe for concurrent access.
	s := revdelTestScreen("deleterevision")
	suppressedComment := revdel.Revision{ID: 2, CommentHidden: true, Suppressed: true}
	s.revisions = []revdel.Revision{{ID: 1}, suppressedComment, {ID: 3, CommentHidden: true}}
	s.fromEntry, s.toEntry, s.authorEntry = widget.NewEntry(), widget.NewEntry(), widget.NewEntry()
	s.manualIDs = widget.NewEntry()

	// Direction 1: a targeted field blocks revisions that have it suppressed — from every selection path.
	s.fieldSelects[revdel.FieldComment].SetSelectedIndex(1) // visible
	require.True(t, s.selectionBlocked(suppressedComment))
	require.False(t, s.selectionBlocked(s.revisions[2]), "plain-deleted fields are not locked")
	matched, err := s.applyCriteria()
	require.NoError(t, err)
	require.Equal(t, 2, matched, "criteria selection skips the blocked revision")
	require.NotContains(t, s.selected, int64(2))
	clear(s.selected)
	s.manualIDs.SetText("1\n2\n3")
	require.Equal(t, 1, s.ingestManualIDs(), "manual entry skips (and counts) the blocked revision")
	require.NotContains(t, s.selected, int64(2))
	clear(s.selected)
	s.fieldSelects[revdel.FieldComment].SetSelectedIndex(0)
	require.False(t, s.selectionBlocked(suppressedComment), "no lock while the field is at (no change)")

	// Direction 2: selecting the revision pins its suppressed field's dropdown to "(no change)".
	s.setRevisionSelected(2, true)
	require.True(t, s.fieldSelects[revdel.FieldComment].Disabled())
	require.Equal(t, 0, s.fieldSelects[revdel.FieldComment].SelectedIndex())
	require.False(t, s.fieldSelects[revdel.FieldContent].Disabled(), "only the suppressed field is pinned")
	s.setRevisionSelected(2, false)
	require.False(t, s.fieldSelects[revdel.FieldComment].Disabled())

	// A suppressor session gets no locks at all.
	suppressor := revdelTestScreen("deleterevision", "suppressrevision")
	suppressor.revisions = s.revisions
	suppressor.fieldSelects[revdel.FieldComment].SetSelectedIndex(1)
	require.False(t, suppressor.selectionBlocked(suppressedComment))
	suppressor.setRevisionSelected(2, true)
	require.False(t, suppressor.fieldSelects[revdel.FieldComment].Disabled())
}

// A session without suppressrevision must not touch the suppression bit: its operations say "nochange".
func TestRevdelCaptureOptionsWithoutSuppressRight(t *testing.T) {
	s := revdelTestScreen("deleterevision")
	s.dryRunCheck.SetChecked(false)
	s.selected[41] = struct{}{}
	s.fieldSelects[revdel.FieldContent].SetSelectedIndex(2)
	s.reasonEntry.SetText("housekeeping")
	require.Empty(t, s.captureOptions())
	require.Equal(t, revdel.SuppressNoChange, s.settings.Suppress)
}

func testRevisions() []revdel.Revision {
	ts := func(day int) time.Time { return time.Date(2026, 7, day, 12, 0, 0, 0, time.UTC) }
	return []revdel.Revision{
		{ID: 104, Timestamp: ts(4), User: "Mallory", Current: true},
		{ID: 103, Timestamp: ts(3), User: "Mallory"},
		{ID: 102, Timestamp: ts(2), User: "Alice"},
		{ID: 101, Timestamp: ts(1), User: "mallory"}, // a different user: usernames are case-sensitive
	}
}

func TestRevdelSelectMatching(t *testing.T) {
	s := revdelTestScreen("deleterevision")
	s.revisions = testRevisions()
	_ = s.buildSelectionTabs()

	// Defaults span the whole history, oldest to newest.
	require.Equal(t, "2026-07-01 12:00:00", s.fromEntry.Text)
	require.Equal(t, "2026-07-04 12:00:00", s.toEntry.Text)

	// Author criterion, exact match — MediaWiki usernames are case-sensitive, so "mallory" (101) is another user —
	// and the current revision (104, also Mallory's) can never be selected.
	s.authorEntry.SetText("Mallory")
	matched, err := s.applyCriteria()
	require.NoError(t, err)
	require.Equal(t, 1, matched)
	require.Equal(t, map[int64]struct{}{103: {}}, s.selected)

	// Timestamp bounds are inclusive and combine with the author criterion being cleared.
	clear(s.selected)
	s.authorEntry.SetText("")
	s.fromEntry.SetText("2026-07-02 12:00:00") // inclusive bound
	s.toEntry.SetText("2026-07-03 11:59:59")
	matched, err = s.applyCriteria()
	require.NoError(t, err)
	require.Equal(t, 1, matched)
	require.Equal(t, map[int64]struct{}{102: {}}, s.selected)

	// A malformed datetime is a criteria error, not a silent no-match.
	s.fromEntry.SetText("02/07/2026")
	_, err = s.applyCriteria()
	require.Error(t, err)
}

func TestRevdelSelectManualIDs(t *testing.T) {
	s := revdelTestScreen("deleterevision")
	s.revisions = testRevisions()
	_ = s.buildSelectionTabs()

	// 103 and 101 exist; 104 is the current revision (never selectable); 999 is not in this page's history.
	s.manualIDs.SetText("* 103\n101, 104\n999")
	skipped := s.ingestManualIDs()
	require.Equal(t, 2, skipped)
	require.Equal(t, map[int64]struct{}{101: {}, 103: {}}, s.selected)
	require.Empty(t, s.manualIDs.Text)
}

func TestParseRevdelTime(t *testing.T) {
	t.Parallel()

	parsed, err := parseRevdelTime(" 2026-07-16 08:15:59 ")
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 7, 16, 8, 15, 59, 0, time.UTC), parsed)

	empty, err := parseRevdelTime("  ")
	require.NoError(t, err)
	require.True(t, empty.IsZero()) // empty = unbounded

	_, err = parseRevdelTime("16/07/2026")
	require.Error(t, err)
}

func TestRevdelRowText(t *testing.T) {
	rev := revdel.Revision{
		ID: 42, Timestamp: time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC), User: "Alice", Comment: "fix typo",
	}
	require.Equal(t, "42 · 2026-07-01 09:00:00 · Alice · fix typo", revdelRowText(rev))

	rev.Current = true
	require.Equal(t, "(current) 42 · 2026-07-01 09:00:00 · Alice · fix typo", revdelRowText(rev))

	// Visibility state is not spelled out in the text — the glyph area carries it; only the empty-field
	// placeholder remains.
	hidden := revdel.Revision{ID: 43, UserHidden: true, CommentHidden: true}
	text := revdelRowText(hidden)
	require.Contains(t, text, "(hidden)")
	require.NotContains(t, text, "hidden:")

	suppressed := revdel.Revision{ID: 44, Suppressed: true, ContentHidden: true}
	require.NotContains(t, revdelRowText(suppressed), "suppressed")
}

// The glyph area strikes exactly the hidden fields' glyphs, in content/comment/user slot order: one diagonal
// for plain deletion, both diagonals (an X) when the revision's hidden fields are suppressed.
func TestRevdelGlyphArea(t *testing.T) {
	// Not parallel: it builds Fyne canvas objects, whose shared render caches are not safe for concurrent access.
	area := newRevdelGlyphArea()
	require.Len(t, area.Objects, len(revdelFields))

	marks := func() (strikes, crosses []bool) {
		for _, raw := range area.Objects {
			slot := raw.(*fyne.Container)
			require.Len(t, slot.Objects, 3)
			glyph := slot.Objects[0].(*canvas.Text)
			require.NotEmpty(t, glyph.Text)
			strikes = append(strikes, slot.Objects[1].Visible())
			crosses = append(crosses, slot.Objects[2].Visible())
		}
		return strikes, crosses
	}

	updateRevdelGlyphArea(area, revdel.Revision{ID: 1})
	strikes, crosses := marks()
	require.Equal(t, []bool{false, false, false}, strikes, "nothing hidden, nothing struck")
	require.Equal(t, []bool{false, false, false}, crosses)

	updateRevdelGlyphArea(area, revdel.Revision{ID: 2, ContentHidden: true, CommentHidden: true})
	strikes, crosses = marks()
	require.Equal(t, []bool{true, true, false}, strikes)
	require.Equal(t, []bool{false, false, false}, crosses, "plain deletion gets a single diagonal")

	// Suppressed: the hidden fields' glyphs get the X (both diagonals); visible fields stay unmarked.
	updateRevdelGlyphArea(area, revdel.Revision{ID: 3, Suppressed: true, ContentHidden: true})
	strikes, crosses = marks()
	require.Equal(t, []bool{true, false, false}, strikes)
	require.Equal(t, []bool{true, false, false}, crosses)

	updateRevdelGlyphArea(area, revdel.Revision{ID: 4, UserHidden: true})
	strikes, crosses = marks()
	require.Equal(t, []bool{false, false, true}, strikes, "recycled rows unstrike what no longer applies")
	require.Equal(t, []bool{false, false, false}, crosses, "recycled rows drop the suppression cross")
}

func TestRevdelPlanRowText(t *testing.T) {
	unchanged := revdel.PlanItem{Revision: revdel.Revision{ID: 1, User: "Alice"}}
	require.Equal(t, glyphUnchanged+" 1 · Alice", revdelPlanRowText(unchanged))

	skipped := revdel.PlanItem{Revision: revdel.Revision{ID: 2, User: "Alice", Current: true}, Skipped: true}
	require.Contains(t, revdelPlanRowText(skipped), glyphWarning)

	// A suppressing operation names the hidden state after its level.
	suppress := revdel.PlanItem{
		Revision: revdel.Revision{ID: 3, User: "Alice"},
		Suppress: true,
		Changed:  true,
		Changes:  []revdel.FieldChange{{Field: revdel.FieldContent, ToHidden: true}},
	}
	require.Equal(t, "3 · Alice  [content: suppressed]", revdelPlanRowText(suppress))

	granular := revdel.PlanItem{
		Revision: revdel.Revision{ID: 4, User: "Alice"},
		Changed:  true,
		Changes: []revdel.FieldChange{
			{Field: revdel.FieldContent, ToHidden: true},
			{Field: revdel.FieldUser, ToHidden: false},
		},
	}
	require.Equal(t, "4 · Alice  [content: deleted, username: visible]", revdelPlanRowText(granular))
}

func TestRevdelOpRowTitle(t *testing.T) {
	short := ops.Operation{Type: ops.OpRevisionDelete, Params: map[string]string{"ids": "1|2"}}
	require.Equal(t, "2 revisions (1, 2)", revdelOpRowTitle(short))

	long := ops.Operation{Type: ops.OpSuppress, Params: map[string]string{"ids": "1|2|3|4|5|6|7"}}
	require.Equal(t, "7 revisions (1, 2, 3, 4, 5, …)", revdelOpRowTitle(long))
}
