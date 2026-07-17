// Package revdel builds revision-visibility change plans: given the selected revisions of one page and the user's
// target visibility, it applies the per-field targets — at the operation's suppression level — over each revision's
// current visibility, drops no-ops (and, defensively, the page's current revision — the UI already forbids selecting
// it), and batches the changing revision IDs into OpRevisionDelete operations.
//
// MediaWiki's model, smoke-tested against 1.46: rev_deleted holds one bit per field (content/comment/user) plus ONE
// revision-wide "restricted" (suppression) bit covering every hidden field of that revision. Suppression is therefore
// not a separate whole-revision action but a level at which fields are hidden; changing only the level requires
// re-sending the hidden fields in "hide" (the API refuses hide/show-less calls).
package revdel

import (
	"strconv"
	"strings"
	"time"

	"github.com/Hunvreus-wiki/skubell/internal/ops"
)

// Revision field names, shared with the translator's normalized vocabulary.
const (
	FieldContent = "content"
	FieldComment = "comment"
	FieldUser    = "user"
)

// FieldTarget is the user's target visibility for one revision field.
type FieldTarget int

const (
	FieldNoChange FieldTarget = iota // leave the field as it currently is
	FieldVisible                     // make the field visible (unhide)
	FieldDeleted                     // hide the field
)

// Revision is one revision of the target page, with its current visibility.
type Revision struct {
	ID            int64
	Timestamp     time.Time
	User          string // empty when UserHidden and the session cannot see through it
	Comment       string // empty when CommentHidden and the session cannot see through it
	Current       bool   // the page's live revision; its visibility can never be changed
	ContentHidden bool
	CommentHidden bool
	UserHidden    bool
	Suppressed    bool // hidden from admins too (the suppression bit is set)
}

// SuppressLevel is the operation's suppression setting, applied revision-wide to the fields it hides. The values are
// the MediaWiki API's own vocabulary for the "suppress" parameter.
type SuppressLevel string

const (
	SuppressNoChange SuppressLevel = "nochange" // leave each revision's suppression bit alone (sessions without the right)
	SuppressYes      SuppressLevel = "yes"      // hidden fields are suppressed: hidden from administrators too
	SuppressNo       SuppressLevel = "no"       // hidden fields are plain-deleted; clears the suppression bit
)

// Settings is the target visibility configured in the Options step, applied to every selected revision.
type Settings struct {
	Suppress SuppressLevel // level at which the targeted fields are hidden; zero value "" is treated as SuppressNoChange
	Content  FieldTarget
	Comment  FieldTarget
	User     FieldTarget
	Reason   string
}

// level is the effective suppression level, defaulting the zero value to SuppressNoChange.
func (s Settings) level() SuppressLevel {
	if s.Suppress == "" {
		return SuppressNoChange
	}
	return s.Suppress
}

// fieldTargets pairs each field name with its target, in the canonical field order.
func (s Settings) fieldTargets() [3]struct {
	Field  string
	Target FieldTarget
} {
	return [3]struct {
		Field  string
		Target FieldTarget
	}{
		{FieldContent, s.Content},
		{FieldComment, s.Comment},
		{FieldUser, s.User},
	}
}

// ChangesNothing reports whether the settings are a no-op for every possible revision: every field left as-is. The
// level alone cannot act — the API requires at least one hide/show field — so it does not count as a change. The UI
// rejects such settings before planning.
func (s Settings) ChangesNothing() bool {
	return s.Content == FieldNoChange && s.Comment == FieldNoChange && s.User == FieldNoChange
}

// FieldChange is one field's change, for the verification display; the field was in the opposite state before.
type FieldChange struct {
	Field    string
	ToHidden bool
}

// PlanItem is one selected revision's visibility change (or non-change).
type PlanItem struct {
	Revision Revision
	Skipped  bool          // the current revision, which can never be changed; defensive — never planned
	Suppress bool          // the operation hides fields at the suppression level (for display wording)
	Changes  []FieldChange // per-field changes (empty when unchanged)
	Changed  bool
}

// Plan is the outcome of BuildPlan: items in the given revision order, counts, and the batched operations.
type Plan struct {
	Items     []PlanItem
	Change    int
	Unchanged int
	Skipped   int
	Ops       []ops.Operation
}

// defaultBatchSize is the ids-per-operation cap used when the caller passes no positive batch size: the MediaWiki
// multi-value limit for a session without apihighlimits.
const defaultBatchSize = 50

// BuildPlan computes the visibility plan for the selected revisions. batchSize caps how many revision IDs one
// operation carries (50 without apihighlimits, 500 with); items keep the caller's revision order.
func BuildPlan(revisions []Revision, settings Settings, batchSize int) Plan {
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	plan := Plan{}
	changing := []Revision{}
	for _, rev := range revisions {
		item := buildItem(rev, settings)
		plan.Items = append(plan.Items, item)
		switch {
		case item.Skipped:
			plan.Skipped++
		case item.Changed:
			plan.Change++
			changing = append(changing, rev)
		default:
			plan.Unchanged++
		}
	}

	for start := 0; start < len(changing); start += batchSize {
		end := min(start+batchSize, len(changing))
		plan.Ops = append(plan.Ops, buildOp(changing[start:end], settings))
	}
	return plan
}

func buildItem(rev Revision, settings Settings) PlanItem {
	item := PlanItem{Revision: rev, Suppress: settings.level() == SuppressYes}
	if rev.Current {
		item.Skipped = true
		return item
	}
	// The suppression bit is revision-wide, so a level flip makes every field kept-or-made hidden a change even
	// when its hidden bit already matches (that is how "escalate to suppressed"/"downgrade to deleted" is asked:
	// the field is re-sent in hide with the new level).
	levelChanges := settings.level() != SuppressNoChange && rev.Suppressed != (settings.level() == SuppressYes)
	for _, ft := range settings.fieldTargets() {
		if ft.Target == FieldNoChange {
			continue
		}
		toHidden := ft.Target == FieldDeleted
		if fieldHidden(rev, ft.Field) == toHidden && (!toHidden || !levelChanges) {
			continue
		}
		item.Changes = append(item.Changes, FieldChange{Field: ft.Field, ToHidden: toHidden})
		item.Changed = true
	}
	return item
}

// fieldHidden reports a field's current hidden state on a revision.
func fieldHidden(rev Revision, field string) bool {
	switch field {
	case FieldContent:
		return rev.ContentHidden
	case FieldComment:
		return rev.CommentHidden
	default:
		return rev.UserHidden
	}
}

// buildOp emits one batch of changing revisions as a single operation. All selected revisions carry the same target,
// so the hide/show lists come straight from the settings; a field already in its target state on some revision of
// the batch is a per-revision no-op the API tolerates (and, on a level change, exactly how the new level is applied
// to it).
func buildOp(batch []Revision, settings Settings) ops.Operation {
	ids := make([]string, len(batch))
	for i, rev := range batch {
		ids[i] = strconv.FormatInt(rev.ID, 10)
	}
	params := map[string]string{"ids": strings.Join(ids, "|"), "suppress": string(settings.level())}
	if reason := strings.TrimSpace(settings.Reason); reason != "" {
		params["reason"] = reason
	}

	hide, show := []string{}, []string{}
	for _, ft := range settings.fieldTargets() {
		switch ft.Target {
		case FieldDeleted:
			hide = append(hide, ft.Field)
		case FieldVisible:
			show = append(show, ft.Field)
		case FieldNoChange:
		}
	}
	if len(hide) > 0 {
		params["hide"] = strings.Join(hide, "|")
	}
	if len(show) > 0 {
		params["show"] = strings.Join(show, "|")
	}
	return ops.Operation{Type: ops.OpRevisionDelete, Params: params, Description: describe(settings, len(batch))}
}

func describe(settings Settings, count int) string {
	if settings.level() == SuppressYes {
		return "Suppress fields of " + strconv.Itoa(count) + " revision(s)"
	}
	return "Change visibility of " + strconv.Itoa(count) + " revision(s)"
}
