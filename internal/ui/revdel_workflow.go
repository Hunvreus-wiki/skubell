package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/Hunvreus-wiki/skubell/internal/api"
	t "github.com/Hunvreus-wiki/skubell/internal/i18n"
	"github.com/Hunvreus-wiki/skubell/internal/ops"
	"github.com/Hunvreus-wiki/skubell/internal/revdel"
)

// revdelFields are the granular visibility fields the Options step exposes, in dropdown order.
var revdelFields = []string{revdel.FieldContent, revdel.FieldComment, revdel.FieldUser}

// Field glyphs for the per-row visibility area. They live here rather than in the messages because they are
// visual vocabulary, not language.
const (
	glyphContent = "📄"
	glyphComment = "💬"
	glyphUser    = "🧍"
)

// revdelFieldGlyph is the glyph standing for a visibility field in the per-row area.
func revdelFieldGlyph(field string) string {
	switch field {
	case revdel.FieldContent:
		return glyphContent
	case revdel.FieldComment:
		return glyphComment
	default:
		return glyphUser
	}
}

// revdelTimeLayout is the second-precision datetime format of the from/to criteria, interpreted as UTC (MediaWiki
// stores revision timestamps in UTC).
const revdelTimeLayout = "2006-01-02 15:04:05"

// revdelWorkflowScreen renders the "Change the visibility of versions" (revision deletion / suppression) workflow.
type revdelWorkflowScreen struct {
	app  *App
	wf   *workflowController
	root fyne.CanvasObject
	step int

	// Selection widgets + the loaded page.
	pageEntry   *widget.Entry
	loadedTitle string            // page whose history is currently loaded; "" until the first load
	revisions   []revdel.Revision // the loaded history, newest first
	loadCancel  context.CancelFunc

	// Options: revision list + mass selection.
	selected      map[int64]struct{} // selected revision IDs
	revList       *widget.List
	selCountLabel *widget.Label
	fromEntry     *widget.Entry
	toEntry       *widget.Entry
	authorEntry   *widget.Entry
	manualIDs     *widget.Entry

	// Options: visibility settings.
	suppressCheck  *widget.Check
	fieldSelects   map[string]*widget.Select
	reasonSelect   *widget.Select
	reasonEntry    *widget.Entry
	dryRunCheck    *widget.Check
	optionsContent fyne.CanvasObject // built per loaded page and retained, so Back preserves the operator's choices
	reasons        []string

	// Captured target + plan.
	settings revdel.Settings
	dryRun   bool
	plan     revdel.Plan

	// Verification/execution state.
	executionInfo     *widget.Label
	executionProgress *widget.ProgressBar // determinate: revisions processed / revisions to change
	executionList     *widget.List
	executionRows     []executionRow
	executionRunning  bool
	executionDone     bool
	cancelExecution   context.CancelFunc
	downloadsBox      *fyne.Container
	journalEntries    []ops.JournalEntry
}

// NewRevDelWorkflowScreen creates the Change-the-visibility-of-versions workflow screen.
func NewRevDelWorkflowScreen(app *App) *revdelWorkflowScreen {
	s := &revdelWorkflowScreen{
		app:            app,
		selected:       map[int64]struct{}{},
		fieldSelects:   map[string]*widget.Select{},
		dryRun:         app.config.Preferences.DryRunByDefault,
		journalEntries: []ops.JournalEntry{},
	}
	s.wf = newWorkflowController(app, s.title(), s.onBack, s.onHome, s.onCancel, s.onProceed)
	s.root = s.wf.Canvas()
	s.showSelectionStep()
	return s
}

// Canvas returns the root canvas object.
func (s *revdelWorkflowScreen) Canvas() fyne.CanvasObject { return s.root }

func (s *revdelWorkflowScreen) title() string {
	return t.T("revdel_title", "Change the visibility of versions")
}

// canRevDel reports whether the session may change regular revision visibility (the three granular dropdowns).
func (s *revdelWorkflowScreen) canRevDel() bool {
	return slices.Contains(s.app.currentCaps.UserRights, "deleterevision")
}

// canSuppress reports whether the session may suppress revisions (the whole-revision checkbox).
func (s *revdelWorkflowScreen) canSuppress() bool {
	return slices.Contains(s.app.currentCaps.UserRights, "suppressrevision")
}

// ---- navigation ----

func (s *revdelWorkflowScreen) onBack() {
	switch s.step {
	case workflowStepOptions:
		s.showSelectionStep()
	case workflowStepVerification:
		s.showOptionsStep()
	case workflowStepExecution:
		if s.executionRunning {
			return
		}
		s.showVerificationStep()
	}
}

func (s *revdelWorkflowScreen) onHome() {
	if s.executionRunning {
		return
	}
	s.app.openWelcome()
}

func (s *revdelWorkflowScreen) onCancel() {
	if s.loadCancel != nil {
		s.loadCancel()
		return
	}
	if s.executionRunning && s.cancelExecution != nil {
		s.cancelExecution()
	}
}

func (s *revdelWorkflowScreen) onProceed() {
	switch s.step {
	case workflowStepSelection:
		s.proceedFromSelection()
	case workflowStepOptions:
		if msg := s.captureOptions(); msg != "" {
			s.app.showMessage(s.title(), msg)
			return
		}
		s.showVerificationStep()
	case workflowStepVerification:
		if s.plan.Change == 0 {
			msg := t.T("revdel_nothing_to_change", "No revision would change; adjust the settings or selection.")
			s.app.showMessage(s.title(), msg)
			return
		}
		s.showExecutionStep()
	case workflowStepExecution:
		if s.executionDone {
			s.app.openWelcome()
			return
		}
		if s.executionRunning {
			return
		}
		s.startExecution()
	}
}

// ---- selection ----

func (s *revdelWorkflowScreen) showSelectionStep() {
	s.step = workflowStepSelection
	s.wf.SetStep(s.step)
	s.wf.SetButtons(workflowButtonState{
		HomeEnabled:    true,
		ProceedEnabled: true,
		ProceedLabel:   t.T("common_proceed", "Proceed"),
	})
	s.wf.SetContent(s.buildSelectionContent())
}

func (s *revdelWorkflowScreen) buildSelectionContent() fyne.CanvasObject {
	if s.pageEntry == nil {
		s.pageEntry = widget.NewEntry()
		s.pageEntry.SetPlaceHolder(t.T("revdel_placeholder_page", "Page title"))
		s.pageEntry.OnSubmitted = func(string) { s.onProceed() }
	}
	form := container.NewVBox(
		widget.NewLabel(t.T("revdel_selection_intro",
			"Enter the page whose revision visibility you want to change. The next step lists its revisions.")),
		labeled(t.T("revdel_field_page", "Page"), s.pageEntry),
	)
	return container.NewVScroll(form)
}

// proceedFromSelection loads the entered page's history (unless it is already loaded) and moves to Options.
func (s *revdelWorkflowScreen) proceedFromSelection() {
	title := normalizeManualTitle(s.pageEntry.Text)
	if title == "" {
		s.app.showMessage(s.title(), t.T("revdel_need_page", "Enter a page title to continue."))
		return
	}
	if title == s.loadedTitle && len(s.revisions) > 0 {
		s.showOptionsStep() // same page: keep the loaded history and every choice made on it
		return
	}
	s.startLoadRevisions(title)
}

// startLoadRevisions fetches the page's history off the UI goroutine behind a cancellable progress dialog, then
// installs it as the loaded page and opens the Options step.
func (s *revdelWorkflowScreen) startLoadRevisions(title string) {
	if s.app.client == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.loadCancel = cancel
	progressBar := widget.NewProgressBarInfinite()
	progressBar.Start()
	status := widget.NewLabel(t.T("revdel_loading", "Loading the page history…"))
	cancelButton := widget.NewButton(t.T("common_cancel", "Cancel"), func() {
		cancel()
	})
	progress := dialog.NewCustomWithoutButtons(
		s.title(),
		container.NewVBox(status, progressBar, container.NewHBox(cancelButton)),
		s.app.window,
	)
	progress.SetOnClosed(func() {
		cancel()
	})
	progress.Show()

	provider := &revdelProvider{client: s.app.client, apiURL: s.app.apiURL}
	go func() {
		// The progress callback runs on this worker goroutine; hop to the UI goroutine to touch the label.
		revisions, err := provider.pageRevisions(ctx, title, func(loaded int) {
			fyne.Do(func() {
				status.SetText(t.Tp("revdel_loading_count",
					"{{.Count}} revision loaded…", "{{.Count}} revisions loaded…", loaded))
			})
		})
		fyne.Do(func() {
			// Capture the cancellation state before Hide(): dialog.Hide() fires SetOnClosed synchronously, which
			// cancels ctx — so reading ctx.Err() after Hide() would misread every outcome as a user cancel.
			canceled := ctx.Err() != nil || errors.Is(err, context.Canceled)
			progress.Hide()
			progressBar.Stop()
			s.loadCancel = nil
			if canceled {
				return
			}
			if err != nil {
				s.showLoadError(title, err)
				return
			}
			s.loadedTitle = title
			s.revisions = revisions
			s.selected = map[int64]struct{}{}
			s.optionsContent = nil // a new page means a fresh Options step (defaults come from its history)
			s.showOptionsStep()
		})
	}()
}

// showLoadError turns the history-load sentinel into localized guidance and reports any other failure plainly.
func (s *revdelWorkflowScreen) showLoadError(title string, err error) {
	if errors.Is(err, errPageMissing) {
		s.app.showMessage(s.title(), t.Td("revdel_err_missing",
			`The page "{{.Title}}" does not exist.`, map[string]any{"Title": title}))
		return
	}
	s.app.showError(s.title(), err)
}

// ---- options ----

func (s *revdelWorkflowScreen) showOptionsStep() {
	s.step = workflowStepOptions
	s.wf.SetStep(s.step)
	s.wf.SetButtons(workflowButtonState{
		BackEnabled:    true,
		HomeEnabled:    true,
		ProceedEnabled: true,
		ProceedLabel:   t.T("common_proceed", "Proceed"),
	})
	// Build the Options widgets once per loaded page and retain them: navigating to Verification and Back must
	// preserve every choice (selection, criteria, visibility, reason). Loading another page resets the cache.
	if s.optionsContent == nil {
		s.loadReasons()
		s.optionsContent = s.buildOptionsContent()
	}
	s.wf.SetContent(s.optionsContent)
	s.refreshSelectionCount()
}

func (s *revdelWorkflowScreen) buildOptionsContent() fyne.CanvasObject {
	// Border (not VBox) so the selection tabs — and the manual entry inside them — take all the vertical room
	// the visibility panel leaves.
	left := container.NewBorder(
		nil,
		container.NewVBox(widget.NewSeparator(), s.buildVisibilityPanel()),
		nil, nil,
		s.buildSelectionTabs(),
	)
	return container.NewHSplit(container.NewVScroll(left), s.buildRevisionPanel())
}

// buildSelectionTabs builds the mass-selection tabs: timestamp/author criteria, and manual revision-ID entry.
func (s *revdelWorkflowScreen) buildSelectionTabs() fyne.CanvasObject {
	s.fromEntry = widget.NewEntry()
	s.toEntry = widget.NewEntry()
	if len(s.revisions) > 0 {
		// Newest first: the last element is the page's first revision. Bounds default to the whole history.
		s.fromEntry.SetText(s.revisions[len(s.revisions)-1].Timestamp.UTC().Format(revdelTimeLayout))
		s.toEntry.SetText(s.revisions[0].Timestamp.UTC().Format(revdelTimeLayout))
	}
	s.authorEntry = widget.NewEntry()
	s.authorEntry.SetPlaceHolder(t.T("revdel_placeholder_author", "(any)"))
	matchBtn := widget.NewButton(t.T("revdel_select_matching", "Select matching revisions"), s.selectMatching)
	criteria := container.NewVBox(
		labeled(t.T("revdel_field_from", "From (UTC)"), s.fromEntry),
		labeled(t.T("revdel_field_to", "To (UTC)"), s.toEntry),
		labeled(t.T("revdel_field_author", "Author"), s.authorEntry),
		container.NewHBox(matchBtn),
	)

	s.manualIDs = widget.NewMultiLineEntry()
	s.manualIDs.SetPlaceHolder(t.T("revdel_placeholder_ids", "One revision ID per line"))
	manualBtn := widget.NewButton(t.T("revdel_select_listed", "Select listed revisions"), s.selectManualIDs)
	// Border (not VBox) so the multi-line entry fills the tab's full height, with the button pinned at the
	// bottom — same layout as the deletion workflow's manual tab.
	manualTab := container.NewBorder(nil, container.NewHBox(manualBtn), nil, nil, s.manualIDs)

	return container.NewAppTabs(
		container.NewTabItem(t.T("revdel_tab_criteria", "Criteria"), criteria),
		container.NewTabItem(t.T("revdel_tab_manual", "Manual entry"), manualTab),
	)
}

// buildVisibilityPanel builds the visibility options applied to all selected revisions, plus reason and dry-run.
// The Suppressed checkbox is the operation's level, not a separate action: hiding a field hides it from admins too
// while checked, so checking it relabels the dropdowns' hidden state from "deleted" to "suppressed" (and back).
func (s *revdelWorkflowScreen) buildVisibilityPanel() fyne.CanvasObject {
	s.suppressCheck = widget.NewCheck(
		t.T("revdel_suppress", "Suppressed (hidden from administrators too)"),
		func(bool) { s.applySuppressRule() },
	)
	fields := container.NewVBox()
	for _, field := range revdelFields {
		// Re-render the rows on every target change: which revisions are selectable depends on the targets
		// for a session that cannot touch suppressed fields (see selectionBlocked).
		sel := widget.NewSelect(s.targetOptions(), func(string) { s.refreshRevisionList() })
		sel.SetSelectedIndex(0)
		s.fieldSelects[field] = sel
		fields.Add(labeled(revdelFieldLabel(field), sel))
	}
	switch {
	case !s.canSuppress():
		s.suppressCheck.Disable() // shown but inert without the suppression right
	case !s.canRevDel():
		// A suppressor without deleterevision can only act at the suppression level.
		s.suppressCheck.SetChecked(true)
		s.suppressCheck.Disable()
	}
	s.applySuppressRule()

	s.reasonSelect = widget.NewSelect(s.reasonSelectOptions(), nil)
	s.reasonSelect.SetSelectedIndex(0)
	s.reasonEntry = widget.NewEntry()
	s.reasonEntry.SetPlaceHolder(t.T("revdel_reason_placeholder", "Additional reason"))
	s.dryRunCheck = widget.NewCheck(t.T("revdel_dry_run", "Dry-run"), nil)
	s.dryRunCheck.SetChecked(s.dryRun)

	return container.NewVBox(
		fieldLabel(t.T("revdel_options_heading", "Visibility of the selected revisions")),
		s.suppressCheck,
		fields,
		widget.NewSeparator(),
		labeled(t.T("revdel_field_reason", "Reason"), s.reasonSelect),
		labeled(t.T("revdel_field_additional_reason", "Additional reason text"), s.reasonEntry),
		s.dryRunCheck,
	)
}

// buildRevisionPanel builds the right-hand revision list: selection count, clear button, and the checkbox list with
// a horizontal scrollbar (rows carry comments and can be wide).
func (s *revdelWorkflowScreen) buildRevisionPanel() fyne.CanvasObject {
	s.selCountLabel = widget.NewLabel("")
	clearBtn := widget.NewButton(t.T("revdel_clear_selection", "Clear selection"), func() {
		clear(s.selected)
		s.refreshRevisionList()
	})
	s.revList = widget.NewList(
		func() int { return len(s.revisions) },
		func() fyne.CanvasObject {
			return container.NewHBox(widget.NewCheck("", nil), newRevdelGlyphArea(), widget.NewLabel(""))
		},
		func(id widget.ListItemID, o fyne.CanvasObject) { s.updateRevisionRow(id, o) },
	)
	holder := container.New(&minWidthLayout{width: s.revisionRowWidth()}, s.revList)
	header := container.NewVBox(
		fieldLabel(t.T("revdel_revision_list", "Revisions")),
		container.NewHBox(s.selCountLabel, layout.NewSpacer(), clearBtn),
	)
	s.refreshSelectionCount()
	return container.NewBorder(header, nil, nil, nil, container.NewHScroll(holder))
}

func (s *revdelWorkflowScreen) updateRevisionRow(id widget.ListItemID, o fyne.CanvasObject) {
	if id < 0 || id >= len(s.revisions) {
		return
	}
	rev := s.revisions[id]
	row, ok := o.(*fyne.Container)
	if !ok || len(row.Objects) < 3 {
		return
	}
	check, _ := row.Objects[0].(*widget.Check)
	glyphArea, _ := row.Objects[1].(*fyne.Container)
	label, _ := row.Objects[2].(*widget.Label)
	if check == nil || glyphArea == nil || label == nil {
		return
	}
	check.OnChanged = nil // never fire the recycled row's previous handler while re-binding
	_, isSelected := s.selected[rev.ID]
	blocked := rev.Current || s.selectionBlocked(rev)
	check.SetChecked(isSelected && !blocked)
	if blocked {
		check.Disable() // the current revision — or a suppressed-field conflict — can never be selected
	} else {
		check.Enable()
		revID := rev.ID
		check.OnChanged = func(on bool) { s.setRevisionSelected(revID, on) }
	}
	updateRevdelGlyphArea(glyphArea, rev)
	label.SetText(revdelRowText(rev))
}

// glyphSlotLayout lays out one glyph slot: the glyph fills the slot; the two strike lines span only the middle
// half of the slot's diagonals — a full corner-to-corner strike overshoots the glyph and reads as clutter. The
// second line runs along the anti-diagonal, forming an X with the first when both are shown.
type glyphSlotLayout struct{}

func (glyphSlotLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	size := fyne.Size{}
	for _, o := range objects {
		size = size.Max(o.MinSize())
	}
	return size
}

func (glyphSlotLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 3 {
		return
	}
	objects[0].Move(fyne.Position{})
	objects[0].Resize(size)
	quarterW, quarterH := size.Width/4, size.Height/4
	if strike, ok := objects[1].(*canvas.Line); ok {
		strike.Position1 = fyne.NewPos(quarterW, quarterH)
		strike.Position2 = fyne.NewPos(size.Width-quarterW, size.Height-quarterH)
	}
	if cross, ok := objects[2].(*canvas.Line); ok {
		cross.Position1 = fyne.NewPos(quarterW, size.Height-quarterH)
		cross.Position2 = fyne.NewPos(size.Width-quarterW, quarterH)
	}
}

// newRevdelGlyphArea builds the dedicated per-row area sitting between the checkbox and the revision text: one
// slot per visibility field (📄 content, 💬 comment, 🧍 username), always all three so rows stay aligned. A
// field the wiki already hides gets a diagonal line drawn through its glyph by updateRevdelGlyphArea — both
// diagonals (an X) when it is hidden at the suppression level. A strikethrough font style alone would be too
// easy to miss.
func newRevdelGlyphArea() *fyne.Container {
	slots := make([]fyne.CanvasObject, 0, len(revdelFields))
	for _, field := range revdelFields {
		glyph := canvas.NewText(revdelFieldGlyph(field), theme.Color(theme.ColorNameForeground))
		glyph.TextSize = theme.TextSize()
		strike := canvas.NewLine(theme.Color(theme.ColorNameError))
		strike.StrokeWidth = 2.5
		strike.Hide()
		cross := canvas.NewLine(theme.Color(theme.ColorNameError))
		cross.StrokeWidth = 2.5
		cross.Hide()
		slots = append(slots, container.New(glyphSlotLayout{}, glyph, strike, cross))
	}
	return container.NewHBox(slots...)
}

// updateRevdelGlyphArea strikes the glyphs of the fields rev already hides — one diagonal for plain deletion,
// an X when the revision's hidden fields are suppressed — and unstrikes the others.
func updateRevdelGlyphArea(area *fyne.Container, rev revdel.Revision) {
	for i, field := range revdelFields {
		if i >= len(area.Objects) {
			return
		}
		slot, ok := area.Objects[i].(*fyne.Container)
		if !ok || len(slot.Objects) < 3 {
			continue
		}
		strike, strikeOK := slot.Objects[1].(*canvas.Line)
		cross, crossOK := slot.Objects[2].(*canvas.Line)
		if !strikeOK || !crossOK {
			continue
		}
		hidden := revdelFieldHidden(rev, field)
		showIf(strike, hidden)
		showIf(cross, hidden && rev.Suppressed)
	}
	area.Refresh()
}

// showIf shows or hides a canvas object to match on.
func showIf(o fyne.CanvasObject, on bool) {
	if on {
		o.Show()
	} else {
		o.Hide()
	}
}

func (s *revdelWorkflowScreen) setRevisionSelected(id int64, on bool) {
	if on {
		s.selected[id] = struct{}{}
	} else {
		delete(s.selected, id)
	}
	s.applyFieldLocks()
	s.refreshSelectionCount()
}

func (s *revdelWorkflowScreen) refreshRevisionList() {
	s.applyFieldLocks()
	if s.revList != nil {
		s.revList.Refresh()
	}
	s.refreshSelectionCount()
}

// revdelFieldSuppressed reports whether rev's field is hidden at the suppression level. MediaWiki refuses to
// let a session without suppressrevision modify such an item in any way (revdelete-modify-no-access), so the
// UI must not let one be targeted (see selectionBlocked and applyFieldLocks — the two directions of the lock).
func revdelFieldSuppressed(rev revdel.Revision, field string) bool {
	return rev.Suppressed && revdelFieldHidden(rev, field)
}

// selectionBlocked reports whether rev cannot be selected: the session cannot suppress, and a field currently
// targeted for change (dropdown not at "(no change)") is suppressed on rev.
func (s *revdelWorkflowScreen) selectionBlocked(rev revdel.Revision) bool {
	if s.canSuppress() {
		return false
	}
	for _, field := range revdelFields {
		sel := s.fieldSelects[field]
		if sel != nil && sel.SelectedIndex() > 0 && revdelFieldSuppressed(rev, field) {
			return true
		}
	}
	return false
}

// applyFieldLocks is the converse lock of selectionBlocked: while a selected revision has a field suppressed,
// a session that cannot suppress has that field's dropdown stuck at "(no change)".
func (s *revdelWorkflowScreen) applyFieldLocks() {
	if s.canSuppress() {
		return
	}
	for _, field := range revdelFields {
		sel := s.fieldSelects[field]
		if sel == nil {
			continue
		}
		locked := false
		for _, rev := range s.revisions {
			if _, isSelected := s.selected[rev.ID]; isSelected && revdelFieldSuppressed(rev, field) {
				locked = true
				break
			}
		}
		switch {
		case locked && sel.SelectedIndex() != 0:
			sel.SetSelectedIndex(0)
			sel.Disable()
		case locked:
			sel.Disable()
		default:
			sel.Enable()
		}
	}
}

func (s *revdelWorkflowScreen) refreshSelectionCount() {
	if s.selCountLabel == nil {
		return
	}
	s.selCountLabel.SetText(t.Td("revdel_selected_count", "{{.Count}} of {{.Total}} selected",
		map[string]any{"Count": len(s.selected), "Total": len(s.revisions)}))
}

// revisionRowWidth is the widest row's rendered width, plus room for the checkbox, the glyph area, and
// paddings — the minimum width the list needs before the horizontal scrollbar takes over.
func (s *revdelWorkflowScreen) revisionRowWidth() float32 {
	const extra = 80 // checkbox + theme paddings
	longest := float32(0)
	for _, rev := range s.revisions {
		size := fyne.MeasureText(revdelRowText(rev), theme.TextSize(), fyne.TextStyle{})
		longest = max(longest, size.Width)
	}
	return longest + newRevdelGlyphArea().MinSize().Width + extra
}

// minWidthLayout gives its objects the container's full size, but never narrower than width. Wrapped in an HScroll it
// yields a horizontal scrollbar for content wider than the viewport, while the content keeps filling the viewport
// when it is narrower.
type minWidthLayout struct {
	width float32
}

func (l *minWidthLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	height := float32(0)
	for _, o := range objects {
		height = max(height, o.MinSize().Height)
	}
	return fyne.NewSize(l.width, height)
}

func (l *minWidthLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objects {
		o.Move(fyne.Position{})
		o.Resize(fyne.NewSize(max(l.width, size.Width), size.Height))
	}
}

// revdelRowText renders one revision row: ID, timestamp, author, comment, with the current-revision marker
// first. Visibility state is not spelled out here — the row's glyph area shows hidden fields as
// struck-through glyphs and suppression as ❌.
func revdelRowText(rev revdel.Revision) string {
	parts := []string{strconv.FormatInt(rev.ID, 10)}
	if !rev.Timestamp.IsZero() {
		parts = append(parts, rev.Timestamp.UTC().Format(revdelTimeLayout))
	}
	parts = append(parts, revdelUserText(rev), revdelCommentText(rev))
	line := strings.Join(parts, " · ")
	if rev.Current {
		line = t.T("revdel_current_marker", "(current)") + " " + line
	}
	return line
}

func revdelUserText(rev revdel.Revision) string {
	if rev.UserHidden && strings.TrimSpace(rev.User) == "" {
		return t.T("revdel_hidden_field", "(hidden)")
	}
	return rev.User
}

func revdelCommentText(rev revdel.Revision) string {
	if rev.CommentHidden && strings.TrimSpace(rev.Comment) == "" {
		return t.T("revdel_hidden_field", "(hidden)")
	}
	return rev.Comment
}

func revdelFieldHidden(rev revdel.Revision, field string) bool {
	switch field {
	case revdel.FieldContent:
		return rev.ContentHidden
	case revdel.FieldComment:
		return rev.CommentHidden
	default:
		return rev.UserHidden
	}
}

// revdelFieldLabel names a visibility field in the user's language.
func revdelFieldLabel(field string) string {
	switch field {
	case revdel.FieldContent:
		return t.T("revdel_field_content", "content")
	case revdel.FieldComment:
		return t.T("revdel_field_comment", "comment")
	default:
		return t.T("revdel_field_user", "username")
	}
}

// targetOptions are the dropdown choices for one field. The hidden state is named after the operation's level:
// "suppressed" while the Suppressed checkbox is checked, "deleted" otherwise.
func (s *revdelWorkflowScreen) targetOptions() []string {
	hidden := t.T("revdel_target_deleted", "deleted")
	if s.suppressCheck != nil && s.suppressCheck.Checked {
		hidden = t.T("revdel_state_suppressed", "suppressed")
	}
	return []string{
		t.T("revdel_target_no_change", "(no change)"),
		t.T("revdel_target_visible", "visible"),
		hidden,
	}
}

// applySuppressRule renames the dropdowns' hidden state to match the checkbox's level ("deleted" ↔
// "suppressed"), keeping each dropdown's selection: the checkbox changes what hiding means, not which fields
// are targeted.
func (s *revdelWorkflowScreen) applySuppressRule() {
	options := s.targetOptions()
	for _, field := range revdelFields {
		sel := s.fieldSelects[field]
		if sel == nil {
			continue
		}
		selected := sel.SelectedIndex()
		sel.SetOptions(options)
		sel.SetSelectedIndex(max(selected, 0))
	}
}

// selectMatching runs the criteria selection and reports a bad datetime or an empty match — the two outcomes that
// leave the selection visibly unchanged and would otherwise read as a dead button.
func (s *revdelWorkflowScreen) selectMatching() {
	matched, err := s.applyCriteria()
	s.refreshRevisionList()
	switch {
	case err != nil:
		s.app.showMessage(s.title(), err.Error())
	case matched == 0:
		s.app.showMessage(s.title(), t.T("revdel_no_match", "No revision matches the criteria."))
	}
}

// applyCriteria adds every revision matching the criteria (timestamp bounds inclusive, author compared exactly —
// MediaWiki usernames are case-sensitive) to the selection and returns how many matched; the current revision never
// matches.
func (s *revdelWorkflowScreen) applyCriteria() (int, error) {
	from, err := parseRevdelTime(s.fromEntry.Text)
	if err != nil {
		return 0, err
	}
	to, err := parseRevdelTime(s.toEntry.Text)
	if err != nil {
		return 0, err
	}
	author := strings.TrimSpace(s.authorEntry.Text)
	matched := 0
	for _, rev := range s.revisions {
		if rev.Current || s.selectionBlocked(rev) {
			continue
		}
		if !from.IsZero() && rev.Timestamp.Before(from) {
			continue
		}
		if !to.IsZero() && rev.Timestamp.After(to) {
			continue
		}
		if author != "" && rev.User != author {
			continue
		}
		s.selected[rev.ID] = struct{}{}
		matched++
	}
	return matched, nil
}

// parseRevdelTime parses a from/to criterion: second-precision UTC, empty meaning "unbounded".
func parseRevdelTime(text string) (time.Time, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return time.Time{}, nil
	}
	parsed, err := time.ParseInLocation(revdelTimeLayout, text, time.UTC)
	if err != nil {
		return time.Time{}, errors.New(t.T("revdel_err_bad_datetime",
			`Enter datetimes as "YYYY-MM-DD HH:MM:SS" (UTC), or leave them empty.`))
	}
	return parsed, nil
}

// selectManualIDs runs the manual-ID selection and reports how many entries were skipped rather than dropping them
// silently.
func (s *revdelWorkflowScreen) selectManualIDs() {
	skipped := s.ingestManualIDs()
	s.refreshRevisionList()
	if skipped > 0 {
		s.app.showMessage(s.title(), t.Tp("revdel_manual_skipped",
			"{{.Count}} entry was skipped: not a revision ID of this page, the current revision, or a "+
				"suppressed revision this session cannot change.",
			"{{.Count}} entries were skipped: not revision IDs of this page, the current revision, or "+
				"suppressed revisions this session cannot change.",
			skipped))
	}
}

// ingestManualIDs adds the listed revision IDs (one per line; commas and wiki bullets tolerated) to the selection,
// clears the entry, and returns how many entries it skipped: tokens that are not revision IDs of this page, plus the
// current revision, which can never be selected.
func (s *revdelWorkflowScreen) ingestManualIDs() int {
	byID := make(map[int64]revdel.Revision, len(s.revisions))
	for _, rev := range s.revisions {
		byID[rev.ID] = rev
	}
	skipped := 0
	tokens := strings.FieldsFunc(s.manualIDs.Text, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ' ' || r == '\t' || r == '*' || r == '#'
	})
	for _, token := range tokens {
		id, err := strconv.ParseInt(token, 10, 64)
		if err != nil {
			skipped++
			continue
		}
		rev, ok := byID[id]
		if !ok || rev.Current || s.selectionBlocked(rev) {
			skipped++
			continue
		}
		s.selected[id] = struct{}{}
	}
	s.manualIDs.SetText("")
	return skipped
}

// reasonSelectOptions lists "(none)" plus the wiki's predefined revision-deletion reasons.
func (s *revdelWorkflowScreen) reasonSelectOptions() []string {
	none := t.T("revdel_reason_none", "(none)")
	options := []string{none}
	for _, r := range s.reasons {
		if r = strings.TrimSpace(r); r != "" && r != none {
			options = append(options, r)
		}
	}
	return options
}

// combinedReason joins the selected predefined reason and the free-text addition, mirroring the other workflows.
func (s *revdelWorkflowScreen) combinedReason() string {
	reason := strings.TrimSpace(s.reasonSelect.Selected)
	if reason == t.T("revdel_reason_none", "(none)") {
		reason = ""
	}
	extra := strings.TrimSpace(s.reasonEntry.Text)
	switch {
	case reason == "":
		return extra
	case extra == "":
		return reason
	default:
		return reason + ": " + extra
	}
}

// loadReasons fills the reason list from the reasons cached when the session connected, so reaching the options step
// costs no request and cannot fail here.
func (s *revdelWorkflowScreen) loadReasons() {
	if len(s.reasons) > 0 {
		return
	}
	s.reasons = s.app.reasonsForAction(api.ReasonActionRevDelete)
}

// captureOptions reads the Options widgets into revdel.Settings, returning a validation message (or "" when valid).
func (s *revdelWorkflowScreen) captureOptions() string {
	if len(s.selected) == 0 {
		return t.T("revdel_need_selection", "Select at least one revision to continue.")
	}
	settings := revdel.Settings{
		Suppress: s.suppressLevel(),
		Content:  s.fieldTarget(revdel.FieldContent),
		Comment:  s.fieldTarget(revdel.FieldComment),
		User:     s.fieldTarget(revdel.FieldUser),
		Reason:   s.combinedReason(),
	}
	if settings.ChangesNothing() {
		return t.T("revdel_need_change", "Choose a visibility change: check Suppressed or set at least one field.")
	}
	none := t.T("revdel_reason_none", "(none)")
	if strings.TrimSpace(s.reasonSelect.Selected) == none && strings.TrimSpace(s.reasonEntry.Text) == "" {
		return t.Td("revdel_err_reason_text_required",
			"Additional reason text is required when {{.None}} is selected.", map[string]any{"None": none})
	}
	s.settings = settings
	s.dryRun = s.dryRunCheck.Checked
	return ""
}

// suppressLevel maps the checkbox to the operation's suppression level: checked → suppress, unchecked → plain
// deletion (clearing the bit) when the session may suppress, and "no change" otherwise — MediaWiki rejects a
// suppress=yes/no whose bit flip the session has no right to perform, so rightless sessions must not send one.
func (s *revdelWorkflowScreen) suppressLevel() revdel.SuppressLevel {
	switch {
	case s.suppressCheck.Checked:
		return revdel.SuppressYes
	case s.canSuppress():
		return revdel.SuppressNo
	default:
		return revdel.SuppressNoChange
	}
}

// fieldTarget maps a dropdown's selection to its planner target; the dropdown order is (no change), visible, deleted.
func (s *revdelWorkflowScreen) fieldTarget(field string) revdel.FieldTarget {
	switch s.fieldSelects[field].SelectedIndex() {
	case 1:
		return revdel.FieldVisible
	case 2:
		return revdel.FieldDeleted
	default:
		return revdel.FieldNoChange
	}
}

// selectedRevisions returns the selected revisions in list (newest-first) order.
func (s *revdelWorkflowScreen) selectedRevisions() []revdel.Revision {
	out := make([]revdel.Revision, 0, len(s.selected))
	for _, rev := range s.revisions {
		if _, ok := s.selected[rev.ID]; ok {
			out = append(out, rev)
		}
	}
	return out
}

// ---- verification ----

// showVerificationStep computes and displays the plan. Unlike protection there is no read phase: the history — with
// each revision's current visibility — was already loaded for the Options list, so planning is synchronous.
func (s *revdelWorkflowScreen) showVerificationStep() {
	s.step = workflowStepVerification
	s.wf.SetStep(s.step)
	// The client's live cap for this action (wiki-discovered, shrunk by any observed "toomanyvalues") sizes
	// the batches; the executor still re-splits at execution time if the wiki lowers the cap after planning.
	batch := 50
	if s.app.client != nil {
		batch = s.app.client.MultiValueCap("revisiondelete")
	} else if s.app.currentCaps.HasHighLimits {
		batch = 500
	}
	s.plan = revdel.BuildPlan(s.selectedRevisions(), s.settings, batch)
	s.wf.SetContent(s.buildVerificationContent())
	s.wf.SetButtons(workflowButtonState{
		BackEnabled:    true,
		HomeEnabled:    true,
		ProceedEnabled: s.plan.Change > 0,
		ProceedLabel:   t.T("common_proceed", "Proceed"),
	})
}

func (s *revdelWorkflowScreen) buildVerificationContent() fyne.CanvasObject {
	info := widget.NewLabel(t.Td("revdel_preview_summary",
		"{{.Change}} will change · {{.Unchanged}} unchanged",
		map[string]any{"Change": s.plan.Change, "Unchanged": s.plan.Unchanged}))
	verifyList := widget.NewList(
		func() int { return len(s.plan.Items) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id >= 0 && id < len(s.plan.Items) {
				o.(*widget.Label).SetText(revdelPlanRowText(s.plan.Items[id]))
			}
		},
	)
	legend := widget.NewLabelWithStyle(
		glyphUnchanged+" "+t.T("revdel_legend_unchanged", "already as requested"),
		fyne.TextAlignLeading, fyne.TextStyle{Italic: true},
	)
	return container.NewBorder(
		container.NewVBox(info, widget.NewSeparator()),
		container.NewVBox(widget.NewSeparator(), legend), nil, nil,
		container.NewVScroll(verifyList),
	)
}

// revdelPlanRowText renders a verification row: the revision, and what happens to each changing field. A field
// going hidden is named after the operation's level — "suppressed" when the operation suppresses, "deleted"
// otherwise.
func revdelPlanRowText(item revdel.PlanItem) string {
	base := strconv.FormatInt(item.Revision.ID, 10) + " · " + revdelUserText(item.Revision)
	switch {
	case item.Skipped:
		return glyphWarning + " " + base + " — " + t.T("revdel_row_current", "current revision; cannot be changed")
	case !item.Changed:
		return glyphUnchanged + " " + base
	}
	hiddenState := t.T("revdel_state_deleted", "deleted")
	if item.Suppress {
		hiddenState = t.T("revdel_state_suppressed", "suppressed")
	}
	parts := make([]string, 0, len(item.Changes))
	for _, change := range item.Changes {
		state := t.T("revdel_state_visible", "visible")
		if change.ToHidden {
			state = hiddenState
		}
		parts = append(parts, revdelFieldLabel(change.Field)+": "+state)
	}
	return base + "  [" + strings.Join(parts, ", ") + "]"
}

// ---- execution ----

func (s *revdelWorkflowScreen) showExecutionStep() {
	s.step = workflowStepExecution
	s.wf.SetStep(s.step)
	s.executionRows = s.executionRows[:0]
	for _, op := range s.plan.Ops {
		s.executionRows = append(s.executionRows, executionRow{Title: revdelOpRowTitle(op), Status: "pending"})
	}
	s.wf.SetContent(s.buildExecutionContent())
	s.restoreExecutionButtons()
}

// revdelOpRowTitle names one batched operation in the execution list: its size and the first few revision IDs.
func revdelOpRowTitle(op ops.Operation) string {
	ids := strings.Split(op.Params["ids"], "|")
	preview := strings.Join(ids[:min(5, len(ids))], ", ")
	if len(ids) > 5 {
		preview += ", …"
	}
	return t.Tp("revdel_batch_revisions", "{{.Count}} revision", "{{.Count}} revisions", len(ids)) +
		" (" + preview + ")"
}

// revdelOpCount is how many revisions one batched operation covers, for the execution progress bar.
func revdelOpCount(op ops.Operation) int {
	ids := strings.TrimSpace(op.Params["ids"])
	if ids == "" {
		return 0
	}
	return len(strings.Split(ids, "|"))
}

func (s *revdelWorkflowScreen) buildExecutionContent() fyne.CanvasObject {
	label := t.T("revdel_press_proceed", "Press Proceed to apply the visibility changes.")
	if s.dryRun {
		label = t.T("revdel_press_proceed_dry", "Dry-run: press Proceed to simulate (no changes are written).")
	}
	s.executionInfo = widget.NewLabel(label)
	// The revision count to process is known up front, so the bar is determinate: revisions done / revisions to
	// change. "12 / 340" is numbers, not language, so the formatter needs no translation. Hidden until Proceed.
	s.executionProgress = widget.NewProgressBar()
	s.executionProgress.Max = float64(s.plan.Change)
	s.executionProgress.TextFormatter = func() string {
		return fmt.Sprintf("%.0f / %.0f", s.executionProgress.Value, s.executionProgress.Max)
	}
	s.executionProgress.Hide()
	s.executionList = widget.NewList(
		func() int { return len(s.executionRows) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id >= 0 && id < len(s.executionRows) {
				row := s.executionRows[id]
				line := statusSymbol(row.Status) + " " + row.Title
				if strings.TrimSpace(row.Detail) != "" {
					line += " — " + row.Detail
				}
				o.(*widget.Label).SetText(line)
			}
		},
	)
	s.downloadsBox = container.NewHBox(widget.NewButton(t.T("revdel_download_journal", "Download journal"), func() {
		s.saveJournal("revdel-journal.json")
	}))
	s.downloadsBox.Hide()
	return container.NewBorder(
		container.NewVBox(s.executionInfo, s.executionProgress, widget.NewSeparator()),
		container.NewVBox(widget.NewSeparator(), s.downloadsBox), nil, nil,
		container.NewVScroll(s.executionList),
	)
}

func (s *revdelWorkflowScreen) restoreExecutionButtons() {
	label := t.T("common_proceed", "Proceed")
	if s.executionDone {
		label = t.T("common_done", "Done")
	}
	s.wf.SetButtons(workflowButtonState{
		BackEnabled:    !s.executionDone,
		HomeEnabled:    !s.executionDone,
		ProceedEnabled: true,
		ProceedLabel:   label,
	})
}

func (s *revdelWorkflowScreen) startExecution() {
	if s.app.client == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelExecution = cancel
	s.executionRunning = true
	s.executionInfo.SetText(t.T("revdel_executing", "Applying visibility changes…"))
	s.executionProgress.SetValue(0)
	s.executionProgress.Show()
	s.wf.SetButtons(workflowButtonState{CancelEnabled: true, ProceedLabel: t.T("common_proceed", "Proceed")})

	operations := slices.Clone(s.plan.Ops)
	dryRun := s.dryRun

	go func() {
		executor, err := api.NewHttpExecutor(s.app.client, s.app.apiURL)
		if err != nil {
			fyne.Do(func() {
				s.executionRunning = false
				s.cancelExecution = nil
				s.restoreExecutionButtons()
				s.app.showError(s.title(), err)
			})
			return
		}
		canceled := false
		for i, op := range operations {
			select {
			case <-ctx.Done():
				canceled = true
			default:
			}
			if canceled {
				break
			}
			result, detail, code := s.applyOne(ctx, executor, op, dryRun)
			if result == "canceled" {
				canceled = true
				break
			}
			entry := ops.JournalEntry{
				Timestamp: time.Now().UTC(), Module: "revdel", Operation: op, Result: result,
			}
			if result == "error" {
				entry.ErrorCode = code
				entry.ErrorDetail = detail
			}
			idx, opCount := i, revdelOpCount(op)
			fyne.Do(func() {
				s.executionRows[idx].Status = result
				s.executionRows[idx].Detail = detail
				s.executionList.Refresh()
				s.executionList.ScrollTo(idx)
				s.executionProgress.SetValue(s.executionProgress.Value + float64(opCount))
				s.recordEntry(entry)
			})
		}
		fyne.Do(func() {
			s.executionRunning = false
			s.cancelExecution = nil
			if canceled {
				s.executionInfo.SetText(t.T("revdel_exec_canceled", "Execution canceled."))
				s.restoreExecutionButtons()
				return
			}
			s.executionDone = true
			s.executionInfo.SetText(t.T("revdel_exec_complete", "Execution complete."))
			s.downloadsBox.Show()
			s.restoreExecutionButtons()
		})
	}()
}

// applyOne translates and runs one visibility op; on dry-run it records success without writing. It returns
// ("success"|"error"|"canceled", detail, errorCode).
func (s *revdelWorkflowScreen) applyOne(
	ctx context.Context, executor api.Executor, op ops.Operation, dryRun bool,
) (result, detail, code string) {
	calls, trErr := api.RevDelTranslator{}.Translate(op, s.app.currentCaps)
	if trErr != nil {
		return "error", trErr.Error(), "translate"
	}
	if dryRun {
		return "success", t.T("revdel_dry_run_ok", "(dry-run)"), ""
	}
	results, execErr := executor.Execute(ctx, calls)
	if execErr != nil {
		if ctx.Err() != nil {
			return "canceled", "", ""
		}
		return "error", execErr.Error(), "execute"
	}
	if len(results) == 0 || !results[0].Success {
		if len(results) > 0 && results[0].Error != nil {
			return "error", api.FriendlyRevDelErrorMessage(results[0].Error), results[0].Error.Code
		}
		return "error", t.T("revdel_unknown_failure", "unknown failure"), ""
	}
	return "success", "", ""
}

func (s *revdelWorkflowScreen) recordEntry(entry ops.JournalEntry) {
	s.journalEntries = append(s.journalEntries, entry)
	if s.app != nil {
		s.app.recordJournalEntry(entry)
	}
}

func (s *revdelWorkflowScreen) saveJournal(filename string) {
	wrapper := struct {
		Wiki    string             `json:"wiki"`
		Actions []ops.JournalEntry `json:"actions"`
	}{Wiki: s.app.canonicalURL(), Actions: s.journalEntries}
	payload, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		s.app.showError(t.T("revdel_download_journal", "Download journal"), err)
		return
	}
	// Report save/write failures (mirroring the other workflows): a journal the user asked for that silently never
	// lands on disk is worse than an error dialog. A nil writer just means the user canceled the dialog.
	d := dialog.NewFileSave(func(w fyne.URIWriteCloser, saveErr error) {
		if saveErr != nil {
			s.app.showError(t.T("revdel_download_journal", "Download journal"), saveErr)
			return
		}
		if w == nil {
			return
		}
		defer func() { _ = w.Close() }()
		if _, writeErr := w.Write(payload); writeErr != nil {
			s.app.showError(t.T("revdel_download_journal", "Download journal"), writeErr)
		}
	}, s.app.window)
	d.SetFileName(filename)
	d.Show()
}
