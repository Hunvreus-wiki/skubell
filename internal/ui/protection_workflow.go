package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// protectionTabTypes are the restriction types the Options step exposes, in tab order. edit is first; create and move
// mirror it by default. upload is files-only and deferred.
var protectionTabTypes = []string{"edit", "create", "move"}

// noChangeLevel is the sentinel level menu item meaning "leave this type's level as it currently is".
const noChangeLevel = "(no change)"

// removeLevel is the menu label for removing protection (API level "all").
const removeLevel = "(none)"

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
// method's controls are enabled; value() returns the API-ready expiry string or a validation error.
type expiryInput struct {
	method     *widget.RadioGroup
	predefined *widget.Select
	number     *widget.Entry
	unit       *widget.Select
	date       *widget.DateEntry
	root       fyne.CanvasObject

	optPreset, optCustom, optDate string
}

func newExpiryInput(presets []string) *expiryInput {
	e := &expiryInput{
		optPreset: t.T("protect_expiry_preset", "Preset duration"),
		optCustom: t.T("protect_expiry_custom", "Custom duration"),
		optDate:   t.T("protect_expiry_date", "Until a date"),
	}
	e.predefined = widget.NewSelect(presets, nil)
	if len(presets) > 0 {
		e.predefined.SetSelectedIndex(0)
	}
	e.number = widget.NewEntry()
	e.number.SetPlaceHolder("1")
	e.unit = widget.NewSelect(expiryUnitLabels(), nil)
	e.unit.SetSelectedIndex(1) // days
	e.date = widget.NewDateEntry()

	e.method = widget.NewRadioGroup([]string{e.optPreset, e.optCustom, e.optDate}, func(string) { e.apply() })
	e.method.Horizontal = true
	e.method.SetSelected(e.optPreset)

	e.root = container.NewVBox(
		e.method,
		e.predefined,
		container.NewBorder(nil, nil, e.number, nil, e.unit),
		e.date,
	)
	e.apply()
	return e
}

// apply enables only the selected method's controls.
func (e *expiryInput) apply() {
	e.predefined.Disable()
	e.number.Disable()
	e.unit.Disable()
	e.date.Disable()
	switch e.method.Selected {
	case e.optCustom:
		e.number.Enable()
		e.unit.Enable()
	case e.optDate:
		e.date.Enable()
	default:
		e.predefined.Enable()
	}
}

// setEnabled disables the whole control (used when a tab mirrors Edit) or re-applies per-method enabling.
func (e *expiryInput) setEnabled(on bool) {
	if !on {
		e.method.Disable()
		e.predefined.Disable()
		e.number.Disable()
		e.unit.Disable()
		e.date.Disable()
		return
	}
	e.method.Enable()
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
		if !e.date.Date.After(time.Now()) {
			return "", errors.New(t.T("protect_err_past_date", "The expiry date must be in the future."))
		}
		return e.date.Date.UTC().Format(time.RFC3339), nil
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
	searchNamespace *widget.Select
	searchPrefix    *widget.Entry
	searchLevel     *widget.Select
	searchExpiry    *widget.Select
	searchCascade   *widget.Select
	searchMinLinks  *widget.Entry
	manualEntry     *widget.Entry
	resultList      *widget.List
	finalList       *widget.List
	finalLabel      *widget.Label
	searchResults   []string

	// Options widgets, per type.
	levelSelects   map[string]*widget.Select
	expiryInputs   map[string]*expiryInput
	sameAsEdit     map[string]*widget.Check
	cascadeCheck   *widget.Check
	reasonSelect   *widget.Select
	reasonEntry    *widget.Entry
	dryRunCheck    *widget.Check
	expiryOptions  []string
	reasons        []string
	reasonsLoading bool

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
		app:            app,
		selected:       map[string]struct{}{},
		levelSelects:   map[string]*widget.Select{},
		expiryInputs:   map[string]*expiryInput{},
		sameAsEdit:     map[string]*widget.Check{},
		dryRun:         app.config.Preferences.DryRunByDefault,
		journalEntries: []ops.JournalEntry{},
	}
	s.wf = newWorkflowController(app, s.onBack, s.onHome, s.onCancel, s.onProceed)
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
		labeled(t.T("protect_field_min_links", "Min. incoming links/transclusions"), s.searchMinLinks),
		container.NewHBox(searchBtn, addResultsBtn),
	)

	s.manualEntry = widget.NewMultiLineEntry()
	s.manualEntry.SetPlaceHolder(t.T("protect_placeholder_manual", "One title per line"))
	manualAddBtn := widget.NewButton(t.T("protect_add_results", "Add results to list"), func() {
		s.ingestManualEntry()
		s.refreshLists()
	})
	manualTab := container.NewVBox(s.manualEntry, container.NewHBox(manualAddBtn))

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
	s.finalList = widget.NewList(
		func() int { return len(s.finalTitles()) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			titles := s.finalTitles()
			if id >= 0 && id < len(titles) {
				o.(*widget.Label).SetText(titles[id])
			}
		},
	)
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

func (s *protectionWorkflowScreen) namespaceOptions() []string {
	labels := []string{t.T("protect_ns_any", "(any)")}
	ids := make([]int, 0, len(s.app.currentCaps.Namespaces))
	for id := range s.app.currentCaps.Namespaces {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		name := strings.TrimSpace(s.app.currentCaps.Namespaces[id])
		if name == "" {
			name = t.T("protect_ns_main", "(Main)")
		}
		labels = append(labels, fmt.Sprintf("%d: %s", id, name))
	}
	return labels
}

// levelMenu lists the wiki's protection levels as menu labels. withNoChange prepends the "(no change)" sentinel.
func (s *protectionWorkflowScreen) levelMenu(withNoChange bool) []string {
	var out []string
	if withNoChange {
		out = append(out, noChangeLevel)
	}
	for _, lvl := range s.app.currentCaps.RestrictionLevels {
		if strings.TrimSpace(lvl) == "" {
			out = append(out, removeLevel)
		} else {
			out = append(out, lvl)
		}
	}
	if len(out) == 0 { // defensive fallback if siteinfo restrictions were unavailable
		out = append(out, removeLevel, "autoconfirmed", "sysop")
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

// runSearch queries list=allpages with the structural + current-protection filters, applies the min-links threshold,
// and shows the matching titles. It runs off the UI goroutine.
func (s *protectionWorkflowScreen) runSearch() {
	if s.app.client == nil {
		return
	}
	params := map[string]string{"action": "query", "list": "allpages", "aplimit": "500", "formatversion": "2"}
	if id, ok := s.selectedNamespaceID(); ok {
		params["apnamespace"] = strconv.Itoa(id)
	}
	if prefix := strings.TrimSpace(s.searchPrefix.Text); prefix != "" {
		params["apprefix"] = prefix
	}
	protectionFiltered := false
	if lvl := s.searchLevelValue(); lvl != "" {
		params["apprlevel"] = lvl
		protectionFiltered = true
	}
	switch s.searchExpiry.SelectedIndex() {
	case 1:
		params["apprexpiry"] = "definite"
		protectionFiltered = true
	case 2:
		params["apprexpiry"] = "indefinite"
		protectionFiltered = true
	}
	switch s.searchCascade.SelectedIndex() {
	case 1:
		params["apprfiltercascade"] = "cascading"
		protectionFiltered = true
	case 2:
		params["apprfiltercascade"] = "noncascading"
		protectionFiltered = true
	}
	if protectionFiltered {
		// The protection filters only apply when a protection type is named; edit|move covers page protection.
		params["apprtype"] = "edit|move"
	}
	minLinks := 0
	if n, err := strconv.Atoi(strings.TrimSpace(s.searchMinLinks.Text)); err == nil && n > 0 {
		minLinks = n
	}

	go func() {
		ctx := context.Background()
		payload, err := s.app.client.GetContext(ctx, s.app.apiURL, params)
		if err != nil {
			fyne.Do(func() { s.app.showError(t.T("protect_search", "Search"), err) })
			return
		}
		titles := parseAllPagesTitles(payload)
		if minLinks > 0 {
			titles = s.filterByLinkThreshold(ctx, titles, minLinks)
		}
		sort.Strings(titles)
		fyne.Do(func() {
			s.searchResults = titles
			s.refreshLists()
		})
	}()
}

func (s *protectionWorkflowScreen) searchLevelValue() string {
	if s.searchLevel.SelectedIndex() <= 0 { // 0 == "(any)"
		return ""
	}
	sel := strings.TrimSpace(s.searchLevel.Selected)
	if sel == removeLevel { // "(none)" can't be an apprlevel filter (lists protected pages only)
		return ""
	}
	return sel
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

// filterByLinkThreshold keeps titles transcluded on at least minLinks pages. It checks up to a bounded number of
// candidates to keep the query count sane; querypage-cached counts are not used here (per-page live check).
func (s *protectionWorkflowScreen) filterByLinkThreshold(ctx context.Context, titles []string, minLinks int) []string {
	const maxCandidates = 300
	kept := []string{}
	for i, title := range titles {
		if i >= maxCandidates {
			break
		}
		payload, err := s.app.client.GetContext(ctx, s.app.apiURL, map[string]string{
			"action": "query", "prop": "transcludedin", "titles": title,
			"tilimit": strconv.Itoa(minLinks), "formatversion": "2",
		})
		if err != nil {
			continue
		}
		if transclusionCountAtLeast(payload, minLinks) {
			kept = append(kept, title)
		}
	}
	return kept
}

// transclusionCountAtLeast reports whether the page is transcluded on at least minLinks pages, using the returned batch
// size plus the presence of a continuation token (more exist beyond the fetched tilimit).
func transclusionCountAtLeast(payload map[string]any, minLinks int) bool {
	query, _ := payload["query"].(map[string]any)
	pages, _ := query["pages"].([]any)
	count := 0
	for _, raw := range pages {
		page, _ := raw.(map[string]any)
		if ti, ok := page["transcludedin"].([]any); ok {
			count += len(ti)
		}
	}
	if count >= minLinks {
		return true
	}
	_, hasMore := payload["continue"]
	return hasMore
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
	s.wf.SetContent(s.buildOptionsContent())
	s.loadExpiryOptions()
	s.loadProtectReasonsIfNeeded()
}

// reasonSelectOptions lists "(none)" plus the wiki's predefined protection reasons, showing "(loading…)" while they load.
func (s *protectionWorkflowScreen) reasonSelectOptions() []string {
	none := t.T("protect_reason_none", "(none)")
	options := []string{none}
	for _, r := range s.reasons {
		if r = strings.TrimSpace(r); r != "" && r != none {
			options = append(options, r)
		}
	}
	if len(s.reasons) == 0 && s.reasonsLoading {
		options = append(options, t.T("protect_reason_loading", "(loading...)"))
	}
	return options
}

// combinedReason joins the selected predefined reason and the free-text addition, mirroring the deletion workflow.
func (s *protectionWorkflowScreen) combinedReason() string {
	reason := strings.TrimSpace(s.reasonSelect.Selected)
	if reason == t.T("protect_reason_none", "(none)") || reason == t.T("protect_reason_loading", "(loading...)") {
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

// loadProtectReasonsIfNeeded lazily fetches the wiki's Protect-dropdown reasons and repopulates the reason Select.
func (s *protectionWorkflowScreen) loadProtectReasonsIfNeeded() {
	if len(s.reasons) > 0 || s.reasonsLoading || s.app.client == nil {
		return
	}
	s.reasonsLoading = true
	if s.reasonSelect != nil {
		s.reasonSelect.Options = s.reasonSelectOptions()
		s.reasonSelect.Refresh()
	}
	go func() {
		dropdowns, err := api.FetchReasonDropdownsContext(
			context.Background(), s.app.client, s.app.apiURL, s.app.currentWiki.AdminLanguage)
		reasons := []string{}
		if err == nil {
			if protectReasons, ok := dropdowns["protect"]; ok {
				for _, category := range protectReasons.Categories {
					for _, reason := range category.Reasons {
						if strings.TrimSpace(reason) != "" {
							reasons = append(reasons, reason)
						}
					}
				}
			}
		}
		sort.Strings(reasons)
		fyne.Do(func() {
			s.reasonsLoading = false
			s.reasons = reasons
			if s.reasonSelect != nil {
				previous := s.reasonSelect.Selected
				s.reasonSelect.Options = s.reasonSelectOptions()
				s.reasonSelect.Refresh()
				if previous != "" && previous != t.T("protect_reason_loading", "(loading...)") {
					s.reasonSelect.SetSelected(previous)
				} else {
					s.reasonSelect.SetSelectedIndex(0)
				}
			}
		})
	}()
}

func (s *protectionWorkflowScreen) buildOptionsContent() fyne.CanvasObject {
	tabs := container.NewAppTabs()
	for _, typ := range protectionTabTypes {
		tabs.Append(container.NewTabItem(s.typeTabLabel(typ), s.buildTypeTab(typ)))
	}

	s.cascadeCheck = widget.NewCheck(t.T("protect_cascade", "Cascade (protect transcluded pages)"), nil)
	s.cascadeCheck.SetChecked(true)
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

	// The applicability note, per the spec: edit/move apply to existing pages, create to nonexistent ones.
	note := s.applicabilityNote(typ)

	body := container.NewVBox(
		widget.NewLabel(note),
		labeled(t.T("protect_field_level", "Level"), level),
		labeled(t.T("protect_field_expiry", "Expiry"), expiry.root),
	)

	if typ == "edit" {
		return body
	}
	// create and move mirror edit by default.
	same := widget.NewCheck(t.T("protect_same_as_edit", "Same as Edit"), func(bool) { s.applyTypeEnabled(typ) })
	same.SetChecked(true)
	s.sameAsEdit[typ] = same
	s.applyTypeEnabled(typ)
	return container.NewVBox(same, body)
}

func (s *protectionWorkflowScreen) applicabilityNote(typ string) string {
	if typ == "create" {
		return t.T("protect_note_create", "Applies only to pages that don't exist yet.")
	}
	return t.T("protect_note_existing", "Applies only to pages that already exist.")
}

// applyTypeEnabled disables a mirrored tab's own controls while its "same as edit" box is checked.
func (s *protectionWorkflowScreen) applyTypeEnabled(typ string) {
	same, ok := s.sameAsEdit[typ]
	if !ok {
		return
	}
	if same.Checked {
		s.levelSelects[typ].Disable()
		s.expiryInputs[typ].setEnabled(false)
	} else {
		s.levelSelects[typ].Enable()
		s.expiryInputs[typ].setEnabled(true)
	}
}

// expiryMenu offers the wiki's predefined durations (loaded lazily) plus a permanent option.
func (s *protectionWorkflowScreen) expiryMenu() []string {
	if len(s.expiryOptions) > 0 {
		return s.expiryOptions
	}
	return []string{"infinite", "1 week", "1 month", "3 months", "6 months", "1 year"}
}

// loadExpiryOptions fetches MediaWiki:Protect-expiry-options (the predefined-durations dropdown) once.
func (s *protectionWorkflowScreen) loadExpiryOptions() {
	if len(s.expiryOptions) > 0 || s.app.client == nil {
		return
	}
	go func() {
		payload, err := s.app.client.GetContext(context.Background(), s.app.apiURL, map[string]string{
			"action": "query", "meta": "allmessages", "ammessages": "Protect-expiry-options", "formatversion": "2",
		})
		if err != nil {
			return
		}
		options := parseExpiryOptions(payload)
		if len(options) == 0 {
			return
		}
		fyne.Do(func() {
			s.expiryOptions = options
			for _, in := range s.expiryInputs {
				in.predefined.Options = options
				in.predefined.SetSelectedIndex(0)
				in.predefined.Refresh()
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
	for _, typ := range protectionTabTypes {
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

// typeSetting builds the target for one restriction type, returning a validation message when its expiry is invalid.
func (s *protectionWorkflowScreen) typeSetting(typ string) (protect.TypeSetting, string) {
	levelSel := strings.TrimSpace(s.levelSelects[typ].Selected)
	if levelSel == noChangeLevel || levelSel == "" {
		return protect.TypeSetting{KeepLevel: true, KeepExpiry: true}, "" // leave this type unchanged
	}
	if levelSel == removeLevel {
		return protect.TypeSetting{Level: ""}, "" // remove protection; expiry is irrelevant
	}
	expiry, err := s.expiryInputs[typ].value()
	if err != nil {
		return protect.TypeSetting{}, err.Error()
	}
	return protect.TypeSetting{Level: levelSel, Expiry: expiry}, ""
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
		t.T("protect_legend", "✎ level  ·  ⏱ expiry  ·  ⊘ unchanged  ·  ⚠ can't apply"),
		fyne.TextAlignLeading, fyne.TextStyle{Italic: true},
	)
	return container.NewBorder(
		container.NewVBox(s.previewInfo, widget.NewSeparator()),
		container.NewVBox(widget.NewSeparator(), legend), nil, nil,
		container.NewVScroll(s.verifyList),
	)
}

func protectionRowText(item protect.PlanItem) string {
	switch {
	case item.Invalid:
		return "⚠ " + item.Title + " — " + item.Note
	case !item.Changed:
		return "⊘ " + item.Title
	}
	parts := []string{}
	for _, c := range item.Changes {
		from := protectionStateText(c.FromLevel, c.FromExpiry)
		to := protectionStateText(c.ToLevel, c.ToExpiry)
		if from != to {
			parts = append(parts, fmt.Sprintf("%s: %s → %s", c.Type, from, to))
		}
	}
	return item.Title + "  [" + strings.Join(parts, ", ") + "]"
}

func protectionStateText(level, expiry string) string {
	if strings.TrimSpace(level) == "" {
		return "none"
	}
	if expiry == "" || expiry == "infinity" {
		return level
	}
	return level + " until " + expiry
}

func (s *protectionWorkflowScreen) computePreview() {
	titles := s.finalTitles()
	ctx, cancel := context.WithCancel(context.Background())
	s.previewCancel = cancel
	s.previewComputing = true
	reader := &protectionProvider{client: s.app.client, apiURL: s.app.apiURL}
	cascading := s.app.currentCaps.CascadingLevels

	go func() {
		plan, err := protect.BuildPlan(ctx, reader, titles, s.settings, cascading)
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
			return "error", api.FriendlyErrorMessage(results[0].Error), results[0].Error.Code
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
	d := dialog.NewFileSave(func(w fyne.URIWriteCloser, saveErr error) {
		if saveErr != nil || w == nil {
			return
		}
		defer func() { _ = w.Close() }()
		_, _ = w.Write(payload)
	}, s.app.window)
	d.SetFileName(filename)
	d.Show()
}
