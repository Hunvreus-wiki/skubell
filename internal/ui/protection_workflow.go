package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/Hunvreus-wiki/skubell/internal/api"
	t "github.com/Hunvreus-wiki/skubell/internal/i18n"
	"github.com/Hunvreus-wiki/skubell/internal/ops"
	"github.com/Hunvreus-wiki/skubell/internal/protect"
)

// protectionTabTypes are the restriction types the Options step exposes, in tab order. edit is first; move and create
// mirror it by default. create is the rarer case (it applies only to pages that don't exist yet), so it goes last.
// upload is files-only and deferred.
var protectionTabTypes = []string{"edit", "move", "create"}

// noChangeLevel is the sentinel level menu item meaning "leave this type's level as it currently is".
func noChangeLevel() string { return t.T("protect_level_no_change", "(no change)") }

// removeLevel is the menu label for removing protection (API level "all").
func removeLevel() string { return t.T("protect_level_none", "(no protection)") }

// expiryUnitValues are the strtotime units MediaWiki accepts, in the display order of the unit dropdown. The value sent
// to the API is always the English word; only the dropdown label is translated (index-mapped, see expiryInput.value).
var expiryUnitValues = []string{"hours", "days", "weeks", "months", "years"}

func expiryUnitLabels() []string {
	return []string{
		t.T("protect_unit_hours", "hours"), t.T("protect_unit_days", "days"), t.T("protect_unit_weeks", "weeks"),
		t.T("protect_unit_months", "months"), t.T("protect_unit_years", "years"),
	}
}

// expiryInput lets the user set an expiry three ways, chosen by a radio (default: preset duration): a predefined
// duration, a custom number+unit relative duration, or a specific future date (widget.DateEntry). Only the selected
// method's row of controls is shown; value() returns the API-ready expiry string or a validation error.
type expiryInput struct {
	method     *widget.RadioGroup
	predefined *widget.Select
	number     *widget.Entry
	unit       *widget.Select
	date       *widget.DateEntry
	hour       *widget.Select    // 00–23 (UTC)
	minute     *widget.Select    // 00–59 (UTC)
	customRow  fyne.CanvasObject // number+unit row, shown for the custom-duration method
	dateRow    fyne.CanvasObject // date+time row, shown for the until-a-date method
	root       fyne.CanvasObject

	optNoChange, optPreset, optCustom, optDate string
}

// twoDigitRange returns "00".."<n-1>" zero-padded, for the hour/minute dropdowns (index == value).
func twoDigitRange(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%02d", i)
	}
	return out
}

func newExpiryInput(presets []string) *expiryInput {
	e := &expiryInput{
		// "(no change)" mirrors the level control's sentinel: it keeps the page's current expiry, so level and expiry
		// can be changed independently (e.g. keep the level but set a new expiry, or change the level but keep expiry).
		optNoChange: t.T("protect_level_no_change", "(no change)"),
		optPreset:   t.T("protect_expiry_preset", "Preset duration"),
		optCustom:   t.T("protect_expiry_custom", "Custom duration"),
		optDate:     t.T("protect_expiry_date", "Until a date"),
	}
	e.predefined = widget.NewSelect(presets, nil)
	if len(presets) > 0 {
		e.predefined.SetSelectedIndex(0)
	}
	e.number = widget.NewEntry()
	e.number.SetPlaceHolder("1")
	e.unit = widget.NewSelect(expiryUnitLabels(), nil)
	e.unit.SetSelectedIndex(1) // days

	// Default the date/time to 24h out (in UTC, since the controls are interpreted as UTC): a definite expiry must be
	// in the future, so defaulting to "now" would be immediately invalid.
	tomorrow := time.Now().UTC().Add(24 * time.Hour)
	e.date = widget.NewDateEntry()
	e.date.SetDate(&tomorrow)
	e.hour = widget.NewSelect(twoDigitRange(24), nil)
	e.hour.SetSelectedIndex(tomorrow.Hour())
	e.minute = widget.NewSelect(twoDigitRange(60), nil)
	e.minute.SetSelectedIndex(tomorrow.Minute())

	// Build the method rows before wiring the radio: SetSelected fires the OnChanged callback (apply), which
	// shows/hides customRow and dateRow — so they must already exist or apply() dereferences a nil row.
	// The time is interpreted as UTC (MediaWiki stores timestamps in UTC), shown next to the hour:minute dropdowns.
	utcLabel := widget.NewLabel(t.T("protect_utc", "UTC"))
	e.customRow = container.NewBorder(nil, nil, e.number, nil, e.unit)
	// The date entry needs the row's width; an HBox pins it to a too-narrow MinSize that clips to the month. Give it
	// the expanding center of a Border and keep the time controls at their natural width on the right.
	timeControls := container.NewHBox(e.hour, widget.NewLabel(":"), e.minute, utcLabel)
	e.dateRow = container.NewBorder(nil, nil, nil, timeControls, e.date)

	e.method = widget.NewRadioGroup(
		[]string{e.optNoChange, e.optPreset, e.optCustom, e.optDate}, func(string) { e.apply() },
	)
	e.method.Horizontal = true
	e.method.SetSelected(e.optNoChange) // default: keep the current expiry

	e.root = container.NewVBox(
		e.method,
		e.predefined,
		e.customRow,
		e.dateRow,
	)
	e.apply()
	return e
}

// apply shows only the selected method's row of controls; the others are hidden (not just disabled), so unused
// controls don't clutter the form. The "(no change)" method needs no control — it keeps the current expiry.
func (e *expiryInput) apply() {
	e.predefined.Hide()
	e.customRow.Hide()
	e.dateRow.Hide()
	switch e.method.Selected {
	case e.optPreset:
		e.predefined.Show()
	case e.optCustom:
		e.customRow.Show()
	case e.optDate:
		e.dateRow.Show()
	}
}

// isNoChange reports whether the "(no change)" method is selected, i.e. the current expiry should be kept.
func (e *expiryInput) isNoChange() bool { return e.method.Selected == e.optNoChange }

// setPredefinedOptions swaps in the loaded presets while keeping the current selection — appending it when the loaded
// list omits it — so a duration the operator chose against the fallback list (e.g. "1 year") is never silently reset
// to the first option just because the wiki's Protect-expiry-options doesn't include it.
func (e *expiryInput) setPredefinedOptions(options []string) {
	prev := e.predefined.Selected
	if prev != "" && !slices.Contains(options, prev) {
		options = append(slices.Clone(options), prev)
	}
	e.predefined.Options = options
	if prev == "" {
		e.predefined.SetSelectedIndex(0)
	} else {
		e.predefined.SetSelected(prev)
	}
	e.predefined.Refresh()
}

func (e *expiryInput) disableAll() {
	e.predefined.Disable()
	e.number.Disable()
	e.unit.Disable()
	e.date.Disable()
	e.hour.Disable()
	e.minute.Disable()
}

func (e *expiryInput) enableAll() {
	e.predefined.Enable()
	e.number.Enable()
	e.unit.Enable()
	e.date.Enable()
	e.hour.Enable()
	e.minute.Enable()
}

// setEnabled disables the whole control (used when a tab mirrors Edit) or re-enables it and shows the active row.
func (e *expiryInput) setEnabled(on bool) {
	if !on {
		e.method.Disable()
		e.disableAll()
		return
	}
	e.method.Enable()
	e.enableAll()
	e.apply()
}

// value returns the API-ready expiry string for the selected method, or a validation error.
func (e *expiryInput) value() (string, error) {
	switch e.method.Selected {
	case e.optCustom:
		num, err := strconv.Atoi(strings.TrimSpace(e.number.Text))
		if err != nil || num <= 0 {
			return "", errors.New(t.T("protect_err_bad_duration",
				"Enter a positive whole number for the custom duration."))
		}
		unit := expiryUnitValues[max(0, e.unit.SelectedIndex())]
		return fmt.Sprintf("%d %s", num, unit), nil
	case e.optDate:
		if e.date.Date == nil {
			return "", errors.New(t.T("protect_err_no_date", "Pick a date for the expiry."))
		}
		// Combine the picked calendar day with the chosen hour:minute, interpreted as UTC.
		year, month, day := e.date.Date.Date()
		hh, mm := max(0, e.hour.SelectedIndex()), max(0, e.minute.SelectedIndex())
		ts := time.Date(year, month, day, hh, mm, 0, 0, time.UTC)
		if !ts.After(time.Now()) {
			return "", errors.New(t.T("protect_err_past_date", "The expiry date must be in the future."))
		}
		return ts.Format(time.RFC3339), nil
	default:
		return e.predefined.Selected, nil
	}
}

// protectionWorkflowScreen renders the bulk "Change page protection" workflow.
type protectionWorkflowScreen struct {
	app  *App
	wf   *workflowController
	root fyne.CanvasObject
	step int

	selected map[string]struct{}

	// Selection widgets.
	searchNamespace    *widget.Select
	searchPrefix       *widget.Entry
	searchLevel        *widget.Select
	searchExpiry       *widget.Select
	searchCascade      *widget.Select
	searchMetric       *widget.Select // transclusion count vs inbound-link count
	searchMinLinks     *widget.Entry
	manualEntry        *widget.Entry
	resultList         *widget.List
	finalList          *deletableList
	finalLabel         *widget.Label
	searchResults      []string
	selectedFinalIndex int // highlighted row in finalList, for Delete-key removal (-1 = none)

	// Options widgets, per type.
	levelSelects   map[string]*widget.Select
	expiryInputs   map[string]*expiryInput
	sameAsEdit     map[string]*widget.Check
	cascadeCheck   *widget.Check
	reasonSelect   *widget.Select
	reasonEntry    *widget.Entry
	dryRunCheck    *widget.Check
	optionsContent fyne.CanvasObject // built once and retained, so Back preserves the operator's choices
	expiryOptions  []string
	expiryLoading  bool // an async Protect-expiry-options fetch is in flight; guards duplicate loads
	reasons        []string

	// Captured target + plan.
	settings protect.Settings
	dryRun   bool
	plan     protect.Plan

	// Verification/execution state.
	previewInfo      *widget.Label
	verifyList       *widget.List
	previewComputing bool
	previewCancel    context.CancelFunc

	executionInfo    *widget.Label
	executionList    *widget.List
	executionRows    []executionRow
	executionRunning bool
	executionDone    bool
	cancelExecution  context.CancelFunc
	downloadsBox     *fyne.Container
	journalEntries   []ops.JournalEntry
}

// NewProtectionWorkflowScreen creates the Change-page-protection workflow screen.
func NewProtectionWorkflowScreen(app *App) *protectionWorkflowScreen {
	s := &protectionWorkflowScreen{
		app:                app,
		selected:           map[string]struct{}{},
		selectedFinalIndex: -1,
		levelSelects:       map[string]*widget.Select{},
		expiryInputs:       map[string]*expiryInput{},
		sameAsEdit:         map[string]*widget.Check{},
		dryRun:             app.config.Preferences.DryRunByDefault,
		journalEntries:     []ops.JournalEntry{},
	}
	s.wf = newWorkflowController(app, s.title(), s.onBack, s.onHome, s.onCancel, s.onProceed)
	s.root = s.wf.Canvas()
	s.showSelectionStep()
	return s
}

// Canvas returns the root canvas object.
func (s *protectionWorkflowScreen) Canvas() fyne.CanvasObject { return s.root }

func (s *protectionWorkflowScreen) title() string {
	return t.T("protect_title", "Change page protection")
}

// ---- navigation ----

func (s *protectionWorkflowScreen) onBack() {
	switch s.step {
	case workflowStepOptions:
		s.showSelectionStep()
	case workflowStepVerification:
		if s.previewComputing {
			return
		}
		s.showOptionsStep()
	case workflowStepExecution:
		if s.executionRunning {
			return
		}
		s.showVerificationStep()
	}
}

func (s *protectionWorkflowScreen) onHome() {
	if s.executionRunning || s.previewComputing {
		return
	}
	s.app.openWelcome()
}

func (s *protectionWorkflowScreen) onCancel() {
	if s.previewComputing && s.previewCancel != nil {
		s.previewCancel()
		return
	}
	if s.executionRunning && s.cancelExecution != nil {
		s.cancelExecution()
	}
}

func (s *protectionWorkflowScreen) onProceed() {
	switch s.step {
	case workflowStepSelection:
		s.ingestManualEntry()
		if len(s.selected) == 0 {
			s.app.showMessage(s.title(), t.T("protect_need_one_page", "Add at least one page to continue."))
			return
		}
		s.showOptionsStep()
	case workflowStepOptions:
		if msg := s.captureOptions(); msg != "" {
			s.app.showMessage(s.title(), msg)
			return
		}
		if msg := s.validateNamespaceProtectAccess(); msg != "" {
			s.app.showMessage(s.title(), msg)
			return
		}
		s.showVerificationStep()
	case workflowStepVerification:
		if s.previewComputing {
			return
		}
		if msg := s.validateNamespaceProtectAccess(); msg != "" {
			s.app.showMessage(s.title(), msg)
			return
		}
		if s.plan.Change == 0 {
			msg := t.T("protect_nothing_to_change", "No pages would change; adjust the settings or selection.")
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

func (s *protectionWorkflowScreen) showSelectionStep() {
	s.step = workflowStepSelection
	s.wf.SetStep(s.step)
	s.wf.SetButtons(workflowButtonState{
		HomeEnabled:    true,
		ProceedEnabled: true,
		ProceedLabel:   t.T("common_proceed", "Proceed"),
	})
	s.wf.SetContent(s.buildSelectionContent())
}

func (s *protectionWorkflowScreen) buildSelectionContent() fyne.CanvasObject {
	s.searchNamespace = widget.NewSelect(s.namespaceOptions(), nil)
	s.searchNamespace.SetSelected(t.T("protect_ns_any", "(any)"))
	s.searchPrefix = widget.NewEntry()
	s.searchLevel = widget.NewSelect(append([]string{t.T("protect_any", "(any)")}, s.levelMenu(false)...), nil)
	s.searchLevel.SetSelectedIndex(0)
	s.searchExpiry = widget.NewSelect([]string{
		t.T("protect_any", "(any)"),
		t.T("protect_expiry_temporary", "temporary"),
		t.T("protect_expiry_permanent", "permanent"),
	}, nil)
	s.searchExpiry.SetSelectedIndex(0)
	s.searchCascade = widget.NewSelect([]string{
		t.T("protect_any", "(any)"),
		t.T("protect_cascade_only", "cascading"),
		t.T("protect_noncascade", "non-cascading"),
	}, nil)
	s.searchCascade.SetSelectedIndex(0)
	// Wired after all three selects exist: the rule touches expiry and cascade whenever the level changes.
	s.searchLevel.OnChanged = func(string) { s.applySearchLevelRule() }
	s.applySearchLevelRule()
	s.searchMetric = widget.NewSelect([]string{
		t.T("protect_metric_transclusions", "Transclusions"),
		t.T("protect_metric_links", "Incoming links"),
	}, nil)
	s.searchMetric.SetSelectedIndex(0)
	s.searchMinLinks = widget.NewEntry()
	s.searchMinLinks.SetPlaceHolder("0")

	searchBtn := widget.NewButton(t.T("protect_search", "Search"), s.runSearch)
	addResultsBtn := widget.NewButton(t.T("protect_add_results", "Add results to list"), func() {
		for _, r := range s.searchResults {
			s.selected[r] = struct{}{}
		}
		s.searchResults = nil
		s.refreshLists()
	})

	searchForm := container.NewVBox(
		labeled(t.T("protect_field_namespace", "Namespace"), s.searchNamespace),
		labeled(t.T("protect_field_prefix", "Title prefix"), s.searchPrefix),
		labeled(t.T("protect_field_cur_level", "Current level"), s.searchLevel),
		labeled(t.T("protect_field_cur_expiry", "Current expiry"), s.searchExpiry),
		labeled(t.T("protect_field_cur_cascade", "Cascade"), s.searchCascade),
		labeled(t.T("protect_field_metric", "Count metric"), s.searchMetric),
		labeled(t.T("protect_field_min_count", "Minimum count"), s.searchMinLinks),
		container.NewHBox(searchBtn, addResultsBtn),
	)

	s.manualEntry = widget.NewMultiLineEntry()
	s.manualEntry.SetPlaceHolder(t.T("protect_placeholder_manual", "One title per line"))
	// Not protect_add_results: this button adds the typed titles, not search results.
	manualAddBtn := widget.NewButton(t.T("protect_add_to_list", "Add to list"), func() {
		s.ingestManualEntry()
		s.refreshLists()
	})
	// Border (not VBox) so the multi-line entry fills the tab's full height, with the button pinned at the
	// bottom — same layout as the deletion workflow's manual tab.
	manualTab := container.NewBorder(nil, container.NewHBox(manualAddBtn), nil, nil, s.manualEntry)

	tabs := container.NewAppTabs(
		container.NewTabItem(t.T("protect_search", "Search"), container.NewVScroll(searchForm)),
		container.NewTabItem(t.T("protect_tab_manual", "Manual entry"), manualTab),
	)

	s.resultList = widget.NewList(
		func() int { return len(s.searchResults) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id >= 0 && id < len(s.searchResults) {
				o.(*widget.Label).SetText(s.searchResults[id])
			}
		},
	)
	s.finalLabel = widget.NewLabel("")
	s.finalList = newDeletableList(
		func() int { return len(s.finalTitles()) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			titles := s.finalTitles()
			if id >= 0 && id < len(titles) {
				o.(*widget.Label).SetText(titles[id])
			}
		},
		func() { s.deleteSelectedFinalItem() },
	)
	s.finalList.OnSelected = func(id widget.ListItemID) {
		s.selectedFinalIndex = id
	}
	s.finalList.OnUnselected = func(widget.ListItemID) {
		s.selectedFinalIndex = -1
	}
	clearBtn := widget.NewButton(t.T("protect_clear_list", "Clear list"), func() {
		s.selected = map[string]struct{}{}
		s.refreshLists()
	})

	resultsPanel := container.NewBorder(
		fieldLabel(t.T("protect_search_results", "Search results")),
		nil, nil, nil, container.NewVScroll(s.resultList),
	)
	finalPanel := container.NewBorder(
		container.NewVBox(
			fieldLabel(t.T("protect_final_list", "Selected pages")),
			s.finalLabel,
		),
		container.NewHBox(clearBtn), nil, nil, container.NewVScroll(s.finalList),
	)
	right := container.NewGridWithRows(2, resultsPanel, finalPanel)
	s.refreshLists()
	return container.NewHSplit(tabs, right)
}

func (s *protectionWorkflowScreen) refreshLists() {
	if s.resultList != nil {
		s.resultList.Refresh()
	}
	if s.finalList != nil {
		s.finalList.Refresh()
	}
	if s.finalLabel != nil {
		count := map[string]any{"Count": len(s.selected)}
		s.finalLabel.SetText(t.Td("protect_selected_count", "{{.Count}} selected", count))
	}
}

func (s *protectionWorkflowScreen) finalTitles() []string {
	titles := make([]string, 0, len(s.selected))
	for title := range s.selected {
		titles = append(titles, title)
	}
	sort.Strings(titles)
	return titles
}

// deleteSelectedFinalItem removes the highlighted page from the selected list via the Delete/Backspace key — the same
// affordance as the deletion workflow — re-selecting the next row so repeated Delete keeps pruning.
func (s *protectionWorkflowScreen) deleteSelectedFinalItem() {
	titles := s.finalTitles()
	if s.selectedFinalIndex < 0 || s.selectedFinalIndex >= len(titles) {
		return
	}
	delete(s.selected, titles[s.selectedFinalIndex])
	next := s.selectedFinalIndex
	if next >= len(s.selected) {
		next = len(s.selected) - 1
	}
	s.selectedFinalIndex = next
	s.refreshLists()
	if s.finalList != nil {
		s.finalList.UnselectAll()
	}
	if next >= 0 && s.finalList != nil {
		s.finalList.Select(next)
	}
}

func (s *protectionWorkflowScreen) ingestManualEntry() {
	if s.manualEntry == nil {
		return
	}
	for line := range strings.SplitSeq(s.manualEntry.Text, "\n") {
		if title := normalizeManualTitle(line); title != "" {
			s.selected[title] = struct{}{}
		}
	}
	s.manualEntry.SetText("")
}

// namespaceOptions lists the "(any)" sentinel plus each listable namespace. It uses namespaceIDs so the Special (-1)
// and Media (-2) pseudo-namespaces are excluded — list=allpages can't enumerate them, so offering them would only
// produce an API error or an empty result.
func (s *protectionWorkflowScreen) namespaceOptions() []string {
	labels := []string{t.T("protect_ns_any", "(any)")}
	for _, id := range s.namespaceIDs() {
		name := strings.TrimSpace(s.app.currentCaps.Namespaces[id])
		if name == "" {
			name = t.T("protect_ns_main", "(Main)")
		}
		labels = append(labels, fmt.Sprintf("%d: %s", id, name))
	}
	return labels
}

// exposedTypes are the Options tabs to show: the UI's candidate types narrowed to those the wiki actually offers
// (siteinfo restrictions.types), so a tab can never produce a type action=protect would reject. An unknown wiki (empty
// RestrictionTypes) shows them all. UI-hidden types such as upload are never exposed here; the planner preserves them.
func (s *protectionWorkflowScreen) exposedTypes() []string {
	supported := s.app.currentCaps.RestrictionTypes
	if len(supported) == 0 {
		return protectionTabTypes
	}
	out := make([]string, 0, len(protectionTabTypes))
	for _, typ := range protectionTabTypes {
		if slices.Contains(supported, typ) {
			out = append(out, typ)
		}
	}
	return out
}

// levelMenu lists the wiki's protection levels as menu labels. withNoChange prepends the "(no change)" sentinel.
func (s *protectionWorkflowScreen) levelMenu(withNoChange bool) []string {
	var out []string
	if withNoChange {
		out = append(out, noChangeLevel())
	}
	for _, lvl := range s.app.currentCaps.RestrictionLevels {
		if strings.TrimSpace(lvl) == "" {
			out = append(out, removeLevel())
		} else {
			out = append(out, lvl)
		}
	}
	if len(out) == 0 { // defensive fallback if siteinfo restrictions were unavailable
		out = append(out, removeLevel(), "autoconfirmed", "sysop")
	}
	return out
}

// labeled wraps a widget with a bold caption above it.
func labeled(caption string, w fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVBox(fieldLabel(caption), w)
}

func (s *protectionWorkflowScreen) selectedNamespaceID() (int, bool) {
	sel := strings.TrimSpace(s.searchNamespace.Selected)
	if sel == "" || sel == t.T("protect_ns_any", "(any)") {
		return 0, false
	}
	id, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(sel, ":", 2)[0]))
	if err != nil {
		return 0, false
	}
	return id, true
}

// searchNoProtectionSelected reports whether the current-level filter is "(no protection)": a request to find pages
// that carry no direct protection. Unlike a concrete level it has no apprlevel equivalent (allpages lists protected
// pages only), so it is filtered client-side by subtracting the protected set.
func (s *protectionWorkflowScreen) searchNoProtectionSelected() bool {
	return s.searchLevel != nil && strings.TrimSpace(s.searchLevel.Selected) == removeLevel()
}

// protectSearchCriteria is an immutable snapshot of the search form, captured on the UI goroutine so the background
// search never reads Fyne widgets off-thread (as the deletion workflow does with searchCriteria).
type protectSearchCriteria struct {
	namespaceID  int
	namespaceSet bool
	prefix       string
	apprlevel    string // concrete level filter; "" for "(any)" and "(no protection)"
	noProtection bool   // "(no protection)" selected: keep only pages with no direct edit/move protection
	expiryIndex  int    // searchExpiry selection: 1 = definite, 2 = indefinite
	cascadeIndex int    // searchCascade selection: 1 = cascading, 2 = non-cascading
	metric       string // querypage serving the count threshold
	minCount     int
}

// collectSearchCriteria reads the search-form widgets into a snapshot. Call it on the UI goroutine only.
func (s *protectionWorkflowScreen) collectSearchCriteria() protectSearchCriteria {
	nsID, nsSet := s.selectedNamespaceID()
	minCount := 0
	if n, err := strconv.Atoi(strings.TrimSpace(s.searchMinLinks.Text)); err == nil && n > 0 {
		minCount = n
	}
	return protectSearchCriteria{
		namespaceID:  nsID,
		namespaceSet: nsSet,
		prefix:       strings.TrimSpace(s.searchPrefix.Text),
		apprlevel:    s.searchLevelValue(),
		noProtection: s.searchNoProtectionSelected(),
		expiryIndex:  s.searchExpiry.SelectedIndex(),
		cascadeIndex: s.searchCascade.SelectedIndex(),
		metric:       s.selectedMetric(),
		minCount:     minCount,
	}
}

// protectionFiltersActive reports whether a current-protection filter with an apprlevel/apprexpiry/apprfiltercascade
// equivalent is set. The unprotected filter is deliberately excluded: it has no allpages equivalent and is handled by
// subtraction, so it must not add apprtype=edit|move (which would list protected pages — the opposite).
func (c protectSearchCriteria) protectionFiltersActive() bool {
	return c.apprlevel != "" || c.expiryIndex > 0 || c.cascadeIndex > 0
}

// needsAllPages reports whether a count-threshold search must also enumerate allpages: a protection constraint (a
// level/expiry/cascade filter, or the unprotected filter) has no cached equivalent, so the cache only settles the
// count while allpages applies the constraint.
func (c protectSearchCriteria) needsAllPages() bool {
	return c.protectionFiltersActive() || c.noProtection
}

// hasCriterion reports whether at least one criterion narrows the search; a criterion-less search (every namespace, no
// prefix, no protection constraint, no threshold) enumerates the whole wiki to no meaningful end.
func (c protectSearchCriteria) hasCriterion() bool {
	return c.namespaceSet || c.prefix != "" || c.protectionFiltersActive() || c.noProtection || c.minCount > 0
}

// sweepsAllNamespaces reports whether the search will enumerate allpages in every namespace — legitimate but slow, so
// worth a confirmation. A threshold without protection constraints is served from the count cache alone and never
// visits allpages, so "(any)" namespace costs nothing there; the unprotected filter always enumerates allpages.
func (c protectSearchCriteria) sweepsAllNamespaces() bool {
	if c.namespaceSet {
		return false
	}
	return c.minCount == 0 || c.protectionFiltersActive() || c.noProtection
}

// runSearch validates the criteria, confirms an all-namespace sweep, then runs the search behind a cancellable
// progress dialog. A minimum-count threshold (transclusions or inbound links, per the metric picker) is served from
// the wiki's matching cached querypage (§3.1) so it scales to large wikis; without a threshold it is a plain
// list=allpages search.
func (s *protectionWorkflowScreen) runSearch() {
	if s.app.client == nil {
		return
	}
	criteria := s.collectSearchCriteria()
	if !criteria.hasCriterion() {
		s.app.showMessage(t.T("protect_search", "Search"), t.T(
			"protect_err_need_criterion",
			"Enter at least one search criterion: a namespace, a title prefix, a protection filter, or a minimum count.",
		))
		return
	}
	if criteria.sweepsAllNamespaces() {
		dialog.NewConfirm(
			t.T("protect_search", "Search"),
			t.Td(
				"protect_confirm_all_namespaces",
				"No namespace is selected, so all {{.Count}} namespaces will be searched. This can be slow. Search anyway?",
				map[string]any{"Count": len(s.namespaceIDs())},
			),
			func(ok bool) {
				if ok {
					s.startSearch(criteria)
				}
			},
			s.app.window,
		).Show()
		return
	}
	s.startSearch(criteria)
}

// startSearch runs the search off the UI goroutine behind a cancellable progress dialog and shows the matches. It takes
// an immutable criteria snapshot so the worker never reads Fyne widgets off-thread.
func (s *protectionWorkflowScreen) startSearch(criteria protectSearchCriteria) {
	ctx, cancel := context.WithCancel(context.Background())
	progressBar := widget.NewProgressBarInfinite()
	progressBar.Start()
	status := widget.NewLabel(t.T("protect_searching", "Searching pages…"))
	cancelButton := widget.NewButton(t.T("common_cancel", "Cancel"), func() {
		cancel()
	})
	progress := dialog.NewCustomWithoutButtons(
		t.T("protect_search", "Search"),
		container.NewVBox(status, progressBar, container.NewHBox(cancelButton)),
		s.app.window,
	)
	progress.SetOnClosed(func() {
		cancel()
	})
	progress.Show()

	go func() {
		var (
			titles []string
			err    error
		)
		if criteria.minCount > 0 {
			titles, err = s.searchByMetric(ctx, criteria)
		} else {
			titles, err = s.searchByAllPages(ctx, criteria)
		}
		sort.Strings(titles)
		fyne.Do(func() {
			// Capture the cancellation state before Hide(): dialog.Hide() fires SetOnClosed synchronously, which
			// cancels ctx — so reading ctx.Err() after Hide() would misread every outcome as a user cancel.
			canceled := ctx.Err() != nil || errors.Is(err, context.Canceled)
			progress.Hide()
			progressBar.Stop()
			// Honor cancellation regardless of err: a request that completed successfully just before the user
			// canceled must not still publish results for a search they abandoned.
			if canceled {
				return
			}
			if err != nil {
				s.showSearchError(err)
				return
			}
			s.searchResults = titles
			s.refreshLists()
		})
	}()
}

// showSearchError turns the search-rejection sentinels into localized guidance — pointing at the manual entry as the
// escape hatch, since building the page list is then a job for a more specialised tool than Skubell — and reports any
// other failure as a plain error.
func (s *protectionWorkflowScreen) showSearchError(err error) {
	title := t.T("protect_search", "Search")
	var floorErr *countBelowCacheFloorError
	switch {
	case errors.Is(err, errSearchTooBroad):
		s.app.showMessage(title, t.Td("protect_err_too_broad",
			"The search matches too many pages (more than {{.Max}} in one namespace). Narrow it with a title prefix "+
				"or a protection filter, or build the page list with a more specialised tool and paste the titles "+
				"under Manual entry.",
			map[string]any{"Max": allPagesBatchSize * maxAllPagesBatches}))
	case errors.Is(err, errNoCountCache):
		s.app.showMessage(title, t.T("protect_err_no_count_cache",
			"This wiki has no cached count data to search, and counting live page by page is too costly. Build the "+
				"page list with a more specialised tool and paste the titles under Manual entry."))
	case errors.As(err, &floorErr):
		// Min is the smallest cached count; Floor (= Min+1) is the lowest threshold the cache answers reliably — the
		// cache may be truncated amid entries tied at Min, so a threshold at Min could return an incomplete set.
		s.app.showMessage(title, t.Td("protect_err_count_floor",
			"This wiki's cached counts only go down to {{.Min}}, so they can only reliably answer a minimum count "+
				"of {{.Floor}} or more. Raise the minimum count to {{.Floor}} or more, or build the page list with "+
				"a more specialised tool and paste the titles under Manual entry.",
			map[string]any{"Min": floorErr.MinCached, "Floor": floorErr.MinCached + 1}))
	default:
		s.app.showError(title, err)
	}
}

// searchByAllPages runs the structural + current-protection filters over list=allpages. allpages is per-namespace with
// no "all namespaces" option — so when no namespace is chosen it searches every namespace and combines. (Otherwise a
// protection-level search in "(any)" namespace silently covers only the main namespace and misses e.g. templates.)
func (s *protectionWorkflowScreen) searchByAllPages(ctx context.Context, c protectSearchCriteria) ([]string, error) {
	if c.namespaceSet {
		return s.allPagesInNamespace(ctx, c, c.namespaceID)
	}
	var all []string
	for _, id := range s.namespaceIDs() {
		titles, err := s.allPagesInNamespace(ctx, c, id)
		if err != nil {
			return nil, err
		}
		all = append(all, titles...)
	}
	return all, nil
}

// allPagesBatchSize is aplimit per request; maxAllPagesBatches bounds one namespace's enumeration. A namespace still
// unfinished past the cap fails the search as too broad rather than returning a silently truncated list — a partial
// result would leave matching pages unprotected with nothing telling the user.
const (
	allPagesBatchSize  = 500
	maxAllPagesBatches = 10
)

// errSearchTooBroad rejects a search whose allpages enumeration outgrew maxAllPagesBatches; showSearchError maps it to
// guidance (narrow the search, or paste an explicit list as manual entries).
var errSearchTooBroad = errors.New("the search matches too many pages")

// allPagesInNamespace queries a single namespace with the criteria's structural + current-protection filters. The
// "(no protection)" filter has no allpages equivalent, so it is served by subtraction (unprotectedPagesInNamespace).
func (s *protectionWorkflowScreen) allPagesInNamespace(
	ctx context.Context, c protectSearchCriteria, nsID int,
) ([]string, error) {
	if c.noProtection {
		return s.unprotectedPagesInNamespace(ctx, c, nsID)
	}
	extra := map[string]string{}
	if c.apprlevel != "" {
		extra["apprlevel"] = c.apprlevel
	}
	switch c.expiryIndex {
	case 1:
		extra["apprexpiry"] = "definite"
	case 2:
		extra["apprexpiry"] = "indefinite"
	}
	switch c.cascadeIndex {
	case 1:
		extra["apprfiltercascade"] = "cascading"
	case 2:
		extra["apprfiltercascade"] = "noncascading"
	}
	if c.protectionFiltersActive() {
		// The protection filters only apply when a protection type is named; edit|move covers page protection.
		extra["apprtype"] = "edit|move"
	}
	return s.enumerateAllPages(ctx, nsID, c.prefix, extra)
}

// unprotectedPagesInNamespace returns the namespace's titles that carry no protection at all. list=allpages has no
// "unprotected only" filter and its apprtype filter joins only the direct page_restrictions table, so a page protected
// through another page's cascade has no direct row and would slip through. It therefore enumerates every matching page
// and confirms each is unprotected with prop=info, which reports the inherited (sourced) cascade entry too.
func (s *protectionWorkflowScreen) unprotectedPagesInNamespace(
	ctx context.Context, c protectSearchCriteria, nsID int,
) ([]string, error) {
	all, err := s.enumerateAllPages(ctx, nsID, c.prefix, nil)
	if err != nil {
		return nil, err
	}
	return s.keepUnprotected(ctx, all)
}

// keepUnprotected returns the subset of titles whose prop=info&inprop=protection reports no protection — direct or
// inherited via another page's cascade — querying in batches sized to the session's live multivalue cap (see
// api.Client.ForEachChunk).
func (s *protectionWorkflowScreen) keepUnprotected(ctx context.Context, titles []string) ([]string, error) {
	kept := []string{}
	err := s.app.client.ForEachChunk("query", titles, func(batch []string) error {
		payload, err := s.app.client.GetContext(ctx, s.app.apiURL, map[string]string{
			"action": "query", "prop": "info", "inprop": "protection",
			"titles": strings.Join(batch, "|"), "formatversion": "2",
		})
		if err != nil {
			return err
		}
		kept = append(kept, unprotectedTitles(payload)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return kept, nil
}

// unprotectedTitles returns the titles in a prop=info&inprop=protection payload whose protection array is empty — no
// direct restriction and no cascade-sourced entry. A non-empty array (including a sourced/inherited one) means the page
// is effectively protected, so it is excluded.
func unprotectedTitles(payload map[string]any) []string {
	out := []string{}
	query, _ := payload["query"].(map[string]any)
	pages, _ := query["pages"].([]any)
	for _, raw := range pages {
		page, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if prot, _ := page["protection"].([]any); len(prot) > 0 {
			continue
		}
		if title, _ := page["title"].(string); strings.TrimSpace(title) != "" {
			out = append(out, title)
		}
	}
	return out
}

// enumerateAllPages lists a namespace's titles under the given prefix and extra allpages filters, following the
// continuation until the namespace is exhausted or the batch cap trips (errSearchTooBroad).
func (s *protectionWorkflowScreen) enumerateAllPages(
	ctx context.Context, nsID int, prefix string, extra map[string]string,
) ([]string, error) {
	params := map[string]string{
		"action": "query", "list": "allpages", "aplimit": strconv.Itoa(allPagesBatchSize), "formatversion": "2",
		"apnamespace": strconv.Itoa(nsID),
	}
	if prefix != "" {
		params["apprefix"] = prefix
	}
	maps.Copy(params, extra)
	titles := []string{}
	for range maxAllPagesBatches {
		payload, err := s.app.client.GetContext(ctx, s.app.apiURL, params)
		if err != nil {
			return nil, err
		}
		titles = append(titles, parseAllPagesTitles(payload)...)
		continueMap, _ := payload["continue"].(map[string]any)
		next, _ := continueMap["apcontinue"].(string)
		if next == "" {
			return titles, nil
		}
		params["apcontinue"] = next
	}
	return nil, errSearchTooBroad
}

// namespaceIDs returns the wiki's listable namespace IDs (>= 0, excluding the Special/Media pseudo-namespaces), sorted.
func (s *protectionWorkflowScreen) namespaceIDs() []int {
	ids := make([]int, 0, len(s.app.currentCaps.Namespaces))
	for id := range s.app.currentCaps.Namespaces {
		if id >= 0 {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return ids
}

// The cached querypage serving each count metric (list=querypage&qppage=): a whole-wiki title-to-count list, sorted
// by count. Transclusions come from Mostlinkedtemplates and inbound links from Mostlinked — the names MediaWiki
// accepts (Mosttranscludedpages / Mostlinkedpages are badvalue).
const (
	queryPageTransclusions = "Mostlinkedtemplates"
	queryPageInboundLinks  = "Mostlinked"
)

// selectedMetric maps the metric picker to the querypage serving it; transclusions is the default (index 0).
func (s *protectionWorkflowScreen) selectedMetric() string {
	if s.searchMetric != nil && s.searchMetric.SelectedIndex() == 1 {
		return queryPageInboundLinks
	}
	return queryPageTransclusions
}

// errNoCountCache rejects a count search on a wiki whose querypage cache is empty (a miser-mode wiki whose cron has
// not run): Skubell never counts live page by page — prohibitive in the general case — so a more specialised tool must
// produce the page list instead, pasted as manual entries. showSearchError maps it to that guidance.
var errNoCountCache = errors.New("the wiki has no cached count data")

// countBelowCacheFloorError rejects a threshold the cache can't settle: the querypage holds only the wiki's top pages
// and may be truncated amid entries tied at the smallest cached count, so a page with exactly that count can be absent.
type countBelowCacheFloorError struct {
	MinCached int // the smallest cached count; only thresholds strictly above it are answerable reliably
}

func (e *countBelowCacheFloorError) Error() string {
	return fmt.Sprintf("the minimum count must exceed the smallest cached count (%d)", e.MinCached)
}

// searchByMetric serves the minimum-count threshold from the wiki's matching cached querypage (whole-wiki, counts
// precomputed) so a page used/linked on tens of thousands of pages is found regardless of alphabetical position, in a
// couple of requests; namespace/prefix are then applied client-side. When current-protection filters are also set —
// they have no cached equivalent — MediaWiki applies them via allpages and the cache settles the threshold. A search
// the cache cannot settle is rejected (errNoCountCache / countBelowCacheFloorError) rather than counted live.
func (s *protectionWorkflowScreen) searchByMetric(ctx context.Context, c protectSearchCriteria) ([]string, error) {
	counts, minCached, err := s.fetchQueryPageCounts(ctx, c.metric)
	if err != nil {
		return nil, err
	}
	// The cache holds only the wiki's top pages, so it may be truncated amid entries tied at the smallest cached
	// count — a page with exactly that count can be missing. Only thresholds strictly above that floor are safe.
	if c.minCount <= minCached {
		return nil, &countBelowCacheFloorError{MinCached: minCached}
	}

	if c.needsAllPages() {
		titles, err := s.searchByAllPages(ctx, c)
		if err != nil {
			return nil, err
		}
		// minCount > minCached, so a title absent from the cache is genuinely below the threshold.
		kept := []string{}
		for _, title := range titles {
			if counts[title] >= c.minCount {
				kept = append(kept, title)
			}
		}
		return kept, nil
	}

	candidates := make([]string, 0, len(counts))
	for title, count := range counts {
		if count >= c.minCount {
			candidates = append(candidates, title)
		}
	}
	return s.filterByNamespaceAndPrefix(c, candidates), nil
}

// filterByNamespaceAndPrefix keeps cache candidates matching the criteria's namespace and title prefix, evaluated
// client-side (no extra requests). Prefix matching is case/underscore-insensitive on the page's main text — a search
// convenience; operations always use the exact cached title, so there is no case conflation (§3.3).
func (s *protectionWorkflowScreen) filterByNamespaceAndPrefix(c protectSearchCriteria, titles []string) []string {
	if !c.namespaceSet && c.prefix == "" {
		return titles
	}
	kept := []string{}
	for _, title := range titles {
		id, main := s.splitNamespace(title)
		if c.namespaceSet && id != c.namespaceID {
			continue
		}
		if c.prefix != "" && !hasPrefixFold(main, c.prefix) {
			continue
		}
		kept = append(kept, title)
	}
	return kept
}

// splitNamespace resolves a full title into its namespace id and main text using the wiki's namespace names; an
// unrecognized or absent prefix means the main namespace (0).
func (s *protectionWorkflowScreen) splitNamespace(title string) (int, string) {
	if idx := strings.IndexByte(title, ':'); idx > 0 {
		if id, ok := s.namespaceIDByName(title[:idx]); ok {
			return id, strings.TrimSpace(title[idx+1:])
		}
	}
	return 0, title
}

// namespaceIDByName looks up a namespace id from its name, matching case- and underscore-insensitively.
func (s *protectionWorkflowScreen) namespaceIDByName(name string) (int, bool) {
	want := normalizeNSName(name)
	for id, n := range s.app.currentCaps.Namespaces {
		if normalizeNSName(n) == want {
			return id, true
		}
	}
	return 0, false
}

func normalizeNSName(name string) string {
	return strings.ToLower(strings.TrimSpace(strings.ReplaceAll(name, "_", " ")))
}

// hasPrefixFold reports whether str begins with prefix, comparing case- and underscore-insensitively.
func hasPrefixFold(str, prefix string) bool {
	return strings.HasPrefix(normalizeNSName(str), normalizeNSName(prefix))
}

func (s *protectionWorkflowScreen) searchLevelValue() string {
	if s.searchLevel.SelectedIndex() <= 0 { // 0 == "(any)"
		return ""
	}
	sel := strings.TrimSpace(s.searchLevel.Selected)
	if sel == removeLevel() { // "(none)" can't be an apprlevel filter (lists protected pages only)
		return ""
	}
	return sel
}

// applySearchLevelRule keeps the current-expiry and cascade filters usable for any level and for "(any)" — they are
// independent protection filters (all temporary / all cascading pages), each implying apprtype=edit|move on its own.
// They are only disabled and reset for "(no protection)", where they are contradictory: an unprotected page has neither.
func (s *protectionWorkflowScreen) applySearchLevelRule() {
	if s.searchNoProtectionSelected() {
		s.searchExpiry.SetSelectedIndex(0)
		s.searchExpiry.Disable()
		s.searchCascade.SetSelectedIndex(0)
		s.searchCascade.Disable()
		return
	}
	s.searchExpiry.Enable()
	s.searchCascade.Enable()
}

func parseAllPagesTitles(payload map[string]any) []string {
	out := []string{}
	query, _ := payload["query"].(map[string]any)
	pages, _ := query["allpages"].([]any)
	for _, raw := range pages {
		if page, ok := raw.(map[string]any); ok {
			if title, _ := page["title"].(string); strings.TrimSpace(title) != "" {
				out = append(out, title)
			}
		}
	}
	return out
}

// fetchQueryPageCounts reads a cached count querypage (e.g. Mostlinkedtemplates / Mostlinked) into title -> count,
// plus the smallest count returned — the cache's authority floor. An empty cache (e.g. a freshly-installed wiki whose
// querypage cron has not run yet) is errNoCountCache; a failed request is reported as itself, not as a missing cache.
func (s *protectionWorkflowScreen) fetchQueryPageCounts(
	ctx context.Context, qpPage string,
) (map[string]int, int, error) {
	params := map[string]string{
		"action": "query", "list": "querypage", "qppage": qpPage,
		"qplimit": "max", "formatversion": "2",
	}
	counts := map[string]int{}
	minCached := 0
	for {
		payload, err := s.app.client.GetContext(ctx, s.app.apiURL, params)
		if err != nil {
			return nil, 0, err
		}
		for title, value := range parseQueryPageCounts(payload) {
			counts[title] = value
			if minCached == 0 || value < minCached {
				minCached = value
			}
		}
		continueMap, _ := payload["continue"].(map[string]any)
		next := continueToken(continueMap["qpoffset"])
		if next == "" {
			break
		}
		params["qpoffset"] = next
	}
	if len(counts) == 0 {
		return nil, 0, errNoCountCache
	}
	return counts, minCached, nil
}

// continueToken renders a continuation value, which MediaWiki may serialize as a string or (for qpoffset under
// formatversion=2) a JSON number, into its decimal string form. A plain string assertion drops the numeric form and
// stops the loop after the first batch, truncating the cache; other types yield "".
func continueToken(v any) string {
	switch n := v.(type) {
	case string:
		return strings.TrimSpace(n)
	case float64:
		return strconv.FormatInt(int64(n), 10)
	case json.Number:
		return n.String()
	default:
		return ""
	}
}

// parseQueryPageCounts extracts title -> value (the querypage metric, here transclusion count) from a querypage
// payload, tolerating both the formatversion=2 object form and the legacy batched-list form.
func parseQueryPageCounts(payload map[string]any) map[string]int {
	out := map[string]int{}
	query, _ := payload["query"].(map[string]any)
	raw, ok := query["querypage"]
	if !ok {
		return out
	}
	results := []any{}
	if m, ok := raw.(map[string]any); ok {
		results, _ = m["results"].([]any)
	}
	if list, ok := raw.([]any); ok {
		for _, item := range list {
			if entry, ok := item.(map[string]any); ok {
				sub, _ := entry["results"].([]any)
				results = append(results, sub...)
			}
		}
	}
	for _, item := range results {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		title, _ := entry["title"].(string)
		if strings.TrimSpace(title) == "" {
			continue
		}
		out[title] = queryPageValue(entry["value"])
	}
	return out
}

// queryPageValue reads a querypage `value`, which the API may serialize as a JSON number or a numeric string.
func queryPageValue(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	default:
		return 0
	}
}

// ---- options ----

func (s *protectionWorkflowScreen) showOptionsStep() {
	s.step = workflowStepOptions
	s.wf.SetStep(s.step)
	s.wf.SetButtons(workflowButtonState{
		BackEnabled:    true,
		HomeEnabled:    true,
		ProceedEnabled: true,
		ProceedLabel:   t.T("common_proceed", "Proceed"),
	})
	// Build the Options widgets once and retain them: navigating to Verification and Back must preserve every choice
	// (level, expiry, same-as-edit, cascade, reason), not reset to defaults. Reasons come from the session cache, so
	// the Select is built already populated.
	if s.optionsContent == nil {
		s.loadReasons()
		s.optionsContent = s.buildOptionsContent()
		s.loadExpiryOptions()
	}
	s.wf.SetContent(s.optionsContent)
}

// reasonSelectOptions lists "(none)" plus the wiki's predefined protection reasons.
func (s *protectionWorkflowScreen) reasonSelectOptions() []string {
	none := t.T("protect_reason_none", "(none)")
	options := []string{none}
	for _, r := range s.reasons {
		if r = strings.TrimSpace(r); r != "" && r != none {
			options = append(options, r)
		}
	}
	return options
}

// combinedReason joins the selected predefined reason and the free-text addition, mirroring the deletion workflow.
func (s *protectionWorkflowScreen) combinedReason() string {
	reason := strings.TrimSpace(s.reasonSelect.Selected)
	if reason == t.T("protect_reason_none", "(none)") {
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
func (s *protectionWorkflowScreen) loadReasons() {
	if len(s.reasons) > 0 {
		return
	}
	s.reasons = s.app.reasonsForAction(api.ReasonActionProtect)
}

func (s *protectionWorkflowScreen) buildOptionsContent() fyne.CanvasObject {
	tabs := container.NewAppTabs()
	for _, typ := range s.exposedTypes() {
		tabs.Append(container.NewTabItem(s.typeTabLabel(typ), s.buildTypeTab(typ)))
	}

	s.cascadeCheck = widget.NewCheck(t.T("protect_cascade", "Cascade (protect transcluded pages)"), nil)
	s.cascadeCheck.SetChecked(false) // cascade is rarely used; opt in rather than out
	s.applyCascadeEnabled()          // reflect the current Edit level (disabled when unprotecting)
	s.reasonSelect = widget.NewSelect(s.reasonSelectOptions(), nil)
	s.reasonSelect.SetSelectedIndex(0)
	s.reasonEntry = widget.NewEntry()
	s.reasonEntry.SetPlaceHolder(t.T("protect_reason_placeholder", "Additional reason"))
	s.dryRunCheck = widget.NewCheck(t.T("protect_dry_run", "Dry-run"), nil)
	s.dryRunCheck.SetChecked(s.dryRun)

	form := container.NewVBox(
		fieldLabel(t.T("protect_options_heading", "Protection settings")),
		tabs,
		widget.NewSeparator(),
		s.cascadeCheck,
		labeled(t.T("protect_field_reason", "Reason"), s.reasonSelect),
		labeled(t.T("protect_field_additional_reason", "Additional reason text"), s.reasonEntry),
		s.dryRunCheck,
	)
	return container.NewVScroll(form)
}

func (s *protectionWorkflowScreen) typeTabLabel(typ string) string {
	switch typ {
	case "edit":
		return t.T("protect_tab_edit", "Edit")
	case "create":
		return t.T("protect_tab_create", "Create")
	case "move":
		return t.T("protect_tab_move", "Move")
	}
	return typ
}

func (s *protectionWorkflowScreen) buildTypeTab(typ string) fyne.CanvasObject {
	level := widget.NewSelect(s.levelMenu(true), nil)
	level.SetSelectedIndex(0) // "(no change)"
	s.levelSelects[typ] = level

	expiry := newExpiryInput(s.expiryMenu())
	s.expiryInputs[typ] = expiry

	// Wire the level change only after expiry exists — SetSelectedIndex above would otherwise fire into a nil expiry.
	level.OnChanged = func(string) { s.applyTypeEnabled(typ) }

	// The applicability note, per the spec: edit/move apply to existing pages, create to nonexistent ones.
	note := s.applicabilityNote(typ)

	body := container.NewVBox(
		widget.NewLabel(note),
		labeled(t.T("protect_field_level", "Level"), level),
		labeled(t.T("protect_field_expiry", "Expiry"), expiry.root),
	)

	// The "Same as Edit" mirror only makes sense when an Edit tab exists. When the wiki omits edit protection (its
	// configured types have move/create but not edit), captureOptions would otherwise mirror to an absent edit tab and
	// dereference nil widgets, so mirroring is offered only when edit is exposed.
	if typ == "edit" || !s.editExposed() {
		s.applyTypeEnabled(typ) // no "same as edit" box, but still apply the level -> expiry rule
		return body
	}
	// create and move mirror edit by default.
	same := widget.NewCheck(t.T("protect_same_as_edit", "Same as Edit"), func(bool) { s.applyTypeEnabled(typ) })
	same.SetChecked(true)
	s.sameAsEdit[typ] = same
	s.applyTypeEnabled(typ)
	return container.NewVBox(same, body)
}

// editExposed reports whether the Edit tab is among the exposed tabs.
func (s *protectionWorkflowScreen) editExposed() bool {
	return slices.Contains(s.exposedTypes(), "edit")
}

func (s *protectionWorkflowScreen) applicabilityNote(typ string) string {
	if typ == "create" {
		return t.T("protect_note_create", "Applies only to pages that don't exist yet.")
	}
	return t.T("protect_note_existing", "Applies only to pages that already exist.")
}

// applyTypeEnabled disables a mirrored tab's own controls while its "same as edit" box is checked.
func (s *protectionWorkflowScreen) applyTypeEnabled(typ string) {
	// A type mirrors Edit while its "same as edit" box is checked (Edit itself has no box, so it never mirrors).
	mirrored := false
	if same, ok := s.sameAsEdit[typ]; ok && same.Checked {
		mirrored = true
	}
	if mirrored {
		s.levelSelects[typ].Disable()
	} else {
		s.levelSelects[typ].Enable()
	}
	// The expiry only applies when a concrete level is set: "(no protection)" unprotects the type, so its expiry is
	// meaningless. Disable it then (and while the type mirrors Edit).
	unprotect := s.levelSelects[typ].Selected == removeLevel()
	s.expiryInputs[typ].setEnabled(!mirrored && !unprotect)
	s.applyCascadeEnabled()
}

// applyCascadeEnabled disables the page-level Cascade control when Edit is being unprotected — cascade protects a
// page's transclusions, so it is meaningless with no edit protection. Safe to call before the checkbox exists.
func (s *protectionWorkflowScreen) applyCascadeEnabled() {
	if s.cascadeCheck == nil {
		return
	}
	edit, ok := s.levelSelects["edit"]
	if ok && edit.Selected == removeLevel() {
		s.cascadeCheck.Disable()
		return
	}
	s.cascadeCheck.Enable()
}

// expiryMenu offers the wiki's predefined durations (loaded lazily) plus a permanent option.
func (s *protectionWorkflowScreen) expiryMenu() []string {
	if len(s.expiryOptions) > 0 {
		return s.expiryOptions
	}
	return []string{"infinite", "1 week", "1 month", "3 months", "6 months", "1 year"}
}

// loadExpiryOptions fetches MediaWiki:Protect-expiry-options (the predefined-durations dropdown) once. When the reply
// arrives it preserves each control's current preset selection if it still exists, so a choice made against the
// fallback menu before the fetch returns is not silently reset. expiryLoading guards against a duplicate in-flight load.
func (s *protectionWorkflowScreen) loadExpiryOptions() {
	if len(s.expiryOptions) > 0 || s.expiryLoading || s.app.client == nil {
		return
	}
	s.expiryLoading = true
	go func() {
		payload, err := s.app.client.GetContext(context.Background(), s.app.apiURL, map[string]string{
			"action": "query", "meta": "allmessages", "ammessages": "Protect-expiry-options", "formatversion": "2",
		})
		var options []string
		if err == nil {
			options = parseExpiryOptions(payload)
		}
		fyne.Do(func() {
			s.expiryLoading = false
			if len(options) == 0 {
				return
			}
			s.expiryOptions = options
			for _, in := range s.expiryInputs {
				in.setPredefinedOptions(options)
			}
		})
	}()
}

// parseExpiryOptions turns the "label:value,label:value" Protect-expiry-options message into the value list.
func parseExpiryOptions(payload map[string]any) []string {
	query, _ := payload["query"].(map[string]any)
	messages, _ := query["allmessages"].([]any)
	if len(messages) == 0 {
		return nil
	}
	msg, _ := messages[0].(map[string]any)
	content, _ := msg["content"].(string)
	if content == "" {
		content, _ = msg["*"].(string)
	}
	out := []string{}
	for pair := range strings.SplitSeq(content, ",") {
		parts := strings.SplitN(strings.TrimSpace(pair), ":", 2)
		value := parts[0]
		if len(parts) == 2 {
			value = parts[1]
		}
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

// captureOptions reads the Options widgets into protect.Settings, returning a validation message (or "" when valid).
func (s *protectionWorkflowScreen) captureOptions() string {
	byType := map[string]protect.TypeSetting{}
	for _, typ := range s.exposedTypes() {
		src := typ
		if same, ok := s.sameAsEdit[typ]; ok && same.Checked {
			src = "edit" // mirror edit's level+expiry
		}
		setting, msg := s.typeSetting(src)
		if msg != "" {
			return msg
		}
		byType[typ] = setting
	}
	none := t.T("protect_reason_none", "(none)")
	if strings.TrimSpace(s.reasonSelect.Selected) == none && strings.TrimSpace(s.reasonEntry.Text) == "" {
		return t.Td("protect_err_reason_text_required",
			"Additional reason text is required when {{.None}} is selected.", map[string]any{"None": none})
	}
	s.settings = protect.Settings{
		ByType:  byType,
		Cascade: s.cascadeCheck.Checked,
		Reason:  s.combinedReason(),
	}
	s.dryRun = s.dryRunCheck.Checked
	return ""
}

// validateNamespaceProtectAccess returns a message if any selected page needs a MediaWiki-namespace/site-config right
// (editinterface, editsite*) the session lacks, or "" when all pages can be protected. Skipped in dry-run (no writes).
func (s *protectionWorkflowScreen) validateNamespaceProtectAccess() string {
	if s.dryRun {
		return ""
	}
	for _, title := range s.finalTitles() {
		if msg := api.ProtectAccessMessage(s.app.currentCaps, title); msg != "" {
			return msg
		}
	}
	return ""
}

// typeSetting builds the target for one restriction type from its independent level and expiry controls, returning a
// validation message when the expiry is invalid. Level and expiry each carry a "(no change)" state, so a page's level
// and expiry can be changed independently (e.g. keep the level but set a new expiry, or vice versa).
func (s *protectionWorkflowScreen) typeSetting(typ string) (protect.TypeSetting, string) {
	levelSel := strings.TrimSpace(s.levelSelects[typ].Selected)
	if levelSel == removeLevel() {
		return protect.TypeSetting{Level: ""}, "" // remove protection; expiry is irrelevant
	}
	setting := protect.TypeSetting{KeepLevel: levelSel == noChangeLevel() || levelSel == ""}
	if !setting.KeepLevel {
		setting.Level = levelSel
	}
	expiry := s.expiryInputs[typ]
	if expiry.isNoChange() {
		setting.KeepExpiry = true
		return setting, ""
	}
	value, err := expiry.value()
	if err != nil {
		return protect.TypeSetting{}, err.Error()
	}
	setting.Expiry = value
	return setting, ""
}

// ---- verification (read phase) ----

func (s *protectionWorkflowScreen) showVerificationStep() {
	s.step = workflowStepVerification
	s.wf.SetStep(s.step)
	s.wf.SetContent(s.buildVerificationContent())
	s.wf.SetButtons(workflowButtonState{CancelEnabled: true, ProceedLabel: t.T("common_proceed", "Proceed")})
	s.computePreview()
}

func (s *protectionWorkflowScreen) buildVerificationContent() fyne.CanvasObject {
	s.previewInfo = widget.NewLabel(t.T("protect_computing", "Computing changes…"))
	s.plan = protect.Plan{}
	s.verifyList = widget.NewList(
		func() int { return len(s.plan.Items) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id >= 0 && id < len(s.plan.Items) {
				o.(*widget.Label).SetText(protectionRowText(s.plan.Items[id]))
			}
		},
	)
	legend := widget.NewLabelWithStyle(
		glyphUnchanged+" "+t.T("protect_legend_unchanged", "unchanged")+
			"   ·   "+glyphWarning+" "+t.T("protect_legend_invalid", "can't apply"),
		fyne.TextAlignLeading, fyne.TextStyle{Italic: true},
	)
	return container.NewBorder(
		container.NewVBox(s.previewInfo, widget.NewSeparator()),
		container.NewVBox(widget.NewSeparator(), legend), nil, nil,
		container.NewVScroll(s.verifyList),
	)
}

// glyphUnchanged marks a page that already carries the protection asked for. It lives here rather than in the messages
// — a symbol is not language — and the legend takes it from here too, so the legend cannot describe a glyph the rows do
// not draw. The one it replaced did exactly that, promising a ✎ for level and a ⏱ for expiry that no row has ever
// carried. glyphWarning is deletion's, and means the same thing here: this will not work.
const glyphUnchanged = "⊘"

func protectionRowText(item protect.PlanItem) string {
	switch {
	case item.Invalid:
		// The planner reports the blocking level structurally (its only invalid case), so the reason is rendered here,
		// in the user's language — a raw English note from a non-UI package would leak through translated rows.
		reason := t.Td("protect_invalid_cascade_level",
			`cascade requires a cascading edit level; "{{.Level}}" is not one`,
			map[string]any{"Level": item.InvalidLevel})
		return glyphWarning + " " + item.Title + " — " + reason
	case !item.Changed:
		return glyphUnchanged + " " + item.Title
	}
	parts := []string{}
	for _, c := range item.Changes {
		from := protectionStateText(c.FromLevel, c.FromExpiry)
		to := protectionStateText(c.ToLevel, c.ToExpiry)
		if from != to {
			parts = append(parts, fmt.Sprintf("%s: %s → %s", c.Type, from, to))
		}
	}
	if item.FromCascade != item.ToCascade {
		parts = append(parts, fmt.Sprintf("%s: %s → %s", t.T("protect_field_cur_cascade", "Cascade"),
			cascadeStateWord(item.FromCascade), cascadeStateWord(item.ToCascade)))
	}
	return item.Title + "  [" + strings.Join(parts, ", ") + "]"
}

// cascadeStateWord names a cascade on/off state using the same words as the search cascade filter.
func cascadeStateWord(on bool) string {
	if on {
		return t.T("protect_cascade_only", "cascading")
	}
	return t.T("protect_noncascade", "non-cascading")
}

func protectionStateText(level, expiry string) string {
	if strings.TrimSpace(level) == "" {
		return t.T("protect_state_none", "none")
	}
	if expiry == "" || expiry == "infinity" {
		return level
	}
	return t.Td("protect_state_until", "{{.Level}} until {{.Expiry}}",
		map[string]any{"Level": level, "Expiry": expiry})
}

func (s *protectionWorkflowScreen) computePreview() {
	titles := s.finalTitles()
	ctx, cancel := context.WithCancel(context.Background())
	s.previewCancel = cancel
	s.previewComputing = true
	reader := &protectionProvider{client: s.app.client, apiURL: s.app.apiURL}
	cascading := s.app.currentCaps.CascadingLevels
	restrictionTypes := s.app.currentCaps.RestrictionTypes

	go func() {
		plan, err := protect.BuildPlan(ctx, reader, titles, s.settings, cascading, restrictionTypes)
		fyne.Do(func() {
			s.previewComputing = false
			s.previewCancel = nil
			if err != nil {
				if ctx.Err() == nil {
					s.app.showError(s.title(), err)
				}
				s.showOptionsStep()
				return
			}
			s.plan = plan
			s.verifyList.Refresh()
			s.previewInfo.SetText(t.Td("protect_preview_summary",
				"{{.Change}} will change · {{.Unchanged}} unchanged · {{.Invalid}} can't apply",
				map[string]any{"Change": plan.Change, "Unchanged": plan.Unchanged, "Invalid": plan.Invalid}))
			s.wf.SetButtons(workflowButtonState{
				BackEnabled:    true,
				HomeEnabled:    true,
				ProceedEnabled: plan.Change > 0,
				ProceedLabel:   t.T("common_proceed", "Proceed"),
			})
		})
	}()
}

// ---- execution ----

func (s *protectionWorkflowScreen) showExecutionStep() {
	s.step = workflowStepExecution
	s.wf.SetStep(s.step)
	s.executionRows = s.executionRows[:0]
	for _, item := range s.plan.Items {
		if item.Changed && !item.Invalid {
			s.executionRows = append(s.executionRows, executionRow{Title: item.Title, Status: "pending"})
		}
	}
	s.wf.SetContent(s.buildExecutionContent())
	s.restoreExecutionButtons()
}

func (s *protectionWorkflowScreen) buildExecutionContent() fyne.CanvasObject {
	label := t.T("protect_press_proceed", "Press Proceed to apply the protection changes.")
	if s.dryRun {
		label = t.T("protect_press_proceed_dry", "Dry-run: press Proceed to simulate (no changes are written).")
	}
	s.executionInfo = widget.NewLabel(label)
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
	s.downloadsBox = container.NewHBox(widget.NewButton(t.T("protect_download_journal", "Download journal"), func() {
		s.saveJournal("protection-journal.json")
	}))
	s.downloadsBox.Hide()
	return container.NewBorder(
		container.NewVBox(s.executionInfo, widget.NewSeparator()),
		container.NewVBox(widget.NewSeparator(), s.downloadsBox), nil, nil,
		container.NewVScroll(s.executionList),
	)
}

func (s *protectionWorkflowScreen) restoreExecutionButtons() {
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

func (s *protectionWorkflowScreen) startExecution() {
	if s.app.client == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelExecution = cancel
	s.executionRunning = true
	s.executionInfo.SetText(t.T("protect_executing", "Applying protection changes…"))
	s.wf.SetButtons(workflowButtonState{CancelEnabled: true, ProceedLabel: t.T("common_proceed", "Proceed")})

	changing := []protect.PlanItem{}
	for _, item := range s.plan.Items {
		if item.Changed && !item.Invalid {
			changing = append(changing, item)
		}
	}
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
		translator := api.ProtectTranslator{}
		canceled := false
		for i, item := range changing {
			select {
			case <-ctx.Done():
				canceled = true
			default:
			}
			if canceled {
				break
			}
			result, detail, code := s.applyOne(ctx, executor, translator, item.Op, dryRun)
			if result == "canceled" {
				canceled = true
				break
			}
			entry := ops.JournalEntry{
				Timestamp: time.Now().UTC(), Module: "protection", Operation: item.Op, Result: result,
			}
			if result == "error" {
				entry.ErrorCode = code
				entry.ErrorDetail = detail
			}
			idx := i
			fyne.Do(func() {
				s.executionRows[idx].Status = result
				s.executionRows[idx].Detail = detail
				s.executionList.Refresh()
				s.executionList.ScrollTo(idx)
				s.recordEntry(entry)
			})
		}
		fyne.Do(func() {
			s.executionRunning = false
			s.cancelExecution = nil
			if canceled {
				s.executionInfo.SetText(t.T("protect_exec_canceled", "Execution canceled."))
				s.restoreExecutionButtons()
				return
			}
			s.executionDone = true
			s.executionInfo.SetText(t.T("protect_exec_complete", "Execution complete."))
			s.downloadsBox.Show()
			s.restoreExecutionButtons()
		})
	}()
}

// applyOne translates and runs one protection op; on dry-run it records success without writing. It returns
// ("success"|"error"|"canceled", detail, errorCode).
func (s *protectionWorkflowScreen) applyOne(
	ctx context.Context, executor api.Executor, translator api.ProtectTranslator, op ops.Operation, dryRun bool,
) (result, detail, code string) {
	calls, trErr := translator.Translate(op, s.app.currentCaps)
	if trErr != nil {
		return "error", trErr.Error(), "translate"
	}
	if dryRun {
		return "success", t.T("protect_dry_run_ok", "(dry-run)"), ""
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
			return "error", api.FriendlyProtectErrorMessage(results[0].Error), results[0].Error.Code
		}
		return "error", t.T("protect_unknown_failure", "unknown failure"), ""
	}
	return "success", "", ""
}

func (s *protectionWorkflowScreen) recordEntry(entry ops.JournalEntry) {
	s.journalEntries = append(s.journalEntries, entry)
	if s.app != nil {
		s.app.recordJournalEntry(entry)
	}
}

func (s *protectionWorkflowScreen) saveJournal(filename string) {
	wrapper := struct {
		Wiki    string             `json:"wiki"`
		Actions []ops.JournalEntry `json:"actions"`
	}{Wiki: s.app.canonicalURL(), Actions: s.journalEntries}
	payload, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		s.app.showError(t.T("protect_download_journal", "Download journal"), err)
		return
	}
	// Report save/write failures (mirroring the deletion workflow): a journal the user asked for that silently never
	// lands on disk is worse than an error dialog. A nil writer just means the user canceled the dialog.
	d := dialog.NewFileSave(func(w fyne.URIWriteCloser, saveErr error) {
		if saveErr != nil {
			s.app.showError(t.T("protect_download_journal", "Download journal"), saveErr)
			return
		}
		if w == nil {
			return
		}
		defer func() { _ = w.Close() }()
		if _, writeErr := w.Write(payload); writeErr != nil {
			s.app.showError(t.T("protect_download_journal", "Download journal"), writeErr)
		}
	}, s.app.window)
	d.SetFileName(filename)
	d.Show()
}
