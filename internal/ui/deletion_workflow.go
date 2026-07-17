package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/Hunvreus-wiki/skubell/internal/api"
	"github.com/Hunvreus-wiki/skubell/internal/deletion"
	t "github.com/Hunvreus-wiki/skubell/internal/i18n"
	"github.com/Hunvreus-wiki/skubell/internal/ops"
)

type searchResult struct {
	Title    string
	Size     int64
	Redirect bool
}

type executionRow struct {
	Title  string
	Status string
	Detail string
}

var errSearchCriteriaTooBroad = errors.New("search criteria are too broad")

const searchCriteriaTooBroadMessage = "" +
	"Your search criteria are too broad. Please provide stricter criteria, " +
	"such as a category, creator, linked-from page, template, or broken-redirects filter."

type deletableList struct {
	widget.List
	onDelete func()
}

func newDeletableList(
	length func() int,
	createItem func() fyne.CanvasObject,
	updateItem func(widget.ListItemID, fyne.CanvasObject),
	onDelete func(),
) *deletableList {
	list := &deletableList{onDelete: onDelete}
	list.Length = length
	list.CreateItem = createItem
	list.UpdateItem = updateItem
	list.ExtendBaseWidget(list)
	return list
}

func (l *deletableList) TypedKey(event *fyne.KeyEvent) {
	if event != nil && (event.Name == fyne.KeyDelete || event.Name == fyne.KeyBackspace) {
		if l.onDelete != nil {
			l.onDelete()
			return
		}
	}
	l.List.TypedKey(event)
}

type deleteWorkflowScreen struct {
	app  *App
	wf   *workflowController
	root fyne.CanvasObject

	step int

	selectedTitles  map[string]struct{}
	categoryParents map[string]map[string]struct{}
	searchResults   []searchResult
	journalEntries  []ops.JournalEntry
	reasons         []string

	optionReasonChoice   string
	optionReasonFreeText string
	optionIncludeTalk    bool
	optionIncludeRedir   bool
	optionDryRun         bool

	selectionResultLabel *widget.Label
	selectionResultList  *deletableList
	selectionFinalLabel  *widget.Label
	selectionFinalList   *deletableList
	selectionProceedBtn  *widget.Button
	selectedSearchIndex  int
	selectedFinalIndex   int

	searchPrefixEntry      *widget.Entry
	searchNamespaceSelect  *widget.Select
	searchCategoryEntry    *widget.Entry
	searchCategoryRecurChk *widget.Check
	searchCategoryInclChk  *widget.Check
	searchCreatorEntry     *widget.Entry
	searchLinkedFromEntry  *widget.Entry
	searchTemplateEntry    *widget.Entry
	searchMinSizeEntry     *widget.Entry
	searchMaxSizeEntry     *widget.Entry
	searchRedirectsCheck   *widget.Check
	searchBrokenRedirCheck *widget.Check

	manualEntry *widget.Entry

	optionReasonSelect      *widget.Select
	optionReasonTextEntry   *widget.Entry
	optionIncludeTalkCheck  *widget.Check
	optionIncludeRedirCheck *widget.Check
	optionDryRunCheck       *widget.Check

	previewPlan          deletion.Plan
	previewRows          []previewRow
	previewComputing     bool
	previewCancel        context.CancelFunc
	verificationInfo     *widget.Label
	stepRedirects        *verificationStep
	stepTalkPages        *verificationStep
	stepCategories       *verificationStep
	verificationList     *deletableList
	selectedPreviewIndex int

	executionRows     []executionRow
	executionList     *widget.List
	executionInfo     *widget.Label
	executionDone     bool
	executionRunning  bool
	executionCanceled bool
	cancelExecution   context.CancelFunc

	downloadsBox *fyne.Container
}

// previewRow is one verification-list row: a plan item plus UI-only annotations
// computed during the read phase (e.g. a category that will not be empty).
type previewRow struct {
	item             deletion.PlanItem
	categoryNotEmpty bool
	remainingMembers int
}

// Glyphs marking preview rows. They live here rather than in the messages: a symbol is not language, and the legend
// below the list has to explain the very glyphs the rows carry.
const (
	glyphDerived  = "↳" // a row pulled in by a selected page, not chosen directly
	glyphTalkPage = "💬" // a subject page whose talk page goes with it
	glyphWarning  = "⚠"
)

// previewRowText renders a preview row: derived rows are indented with glyphDerived, a subject page whose talk page is
// also deleted is marked with glyphTalkPage, and a category that will still have members (so the delete will fail) is
// flagged.
func previewRowText(row previewRow) string {
	line := row.item.Title
	if row.item.Derived {
		line = " " + glyphDerived + " " + line
	}
	if row.item.HasTalkPage {
		line += " " + glyphTalkPage
	}
	if row.categoryNotEmpty {
		line += "  " + glyphWarning + " " + t.Tp(
			"del_category_not_empty",
			"category not empty ({{.Count}} member remaining) — deletion will fail",
			"category not empty ({{.Count}} members remaining) — deletion will fail",
			row.remainingMembers,
		)
	}
	return line
}

// NewDeletionWorkflowScreen creates the Delete pages workflow screen.
func NewDeletionWorkflowScreen(app *App) *deleteWorkflowScreen {
	s := &deleteWorkflowScreen{
		app:                  app,
		selectedTitles:       map[string]struct{}{},
		categoryParents:      map[string]map[string]struct{}{},
		searchResults:        []searchResult{},
		journalEntries:       []ops.JournalEntry{},
		optionDryRun:         app.config.Preferences.DryRunByDefault,
		selectedSearchIndex:  -1,
		selectedFinalIndex:   -1,
		selectedPreviewIndex: -1,
	}
	s.wf = newWorkflowController(app, t.T("workflow_delete_pages", "Delete pages"), s.onBack, s.onHome, s.onCancel,
		s.onProceed)
	s.root = s.wf.Canvas()
	s.showSelectionStep()
	return s
}

// Canvas returns the root canvas object.
func (s *deleteWorkflowScreen) Canvas() fyne.CanvasObject {
	return s.root
}

func (s *deleteWorkflowScreen) onBack() {
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

func (s *deleteWorkflowScreen) onHome() {
	if s.executionRunning {
		return
	}
	s.app.openWelcome()
}

func (s *deleteWorkflowScreen) onCancel() {
	if s.previewComputing && s.previewCancel != nil {
		s.previewCancel()
		return
	}
	if s.executionRunning && s.cancelExecution != nil {
		s.cancelExecution()
	}
}

func (s *deleteWorkflowScreen) onProceed() {
	switch s.step {
	case workflowStepSelection:
		if len(s.selectedTitles) == 0 {
			s.app.showMessage(
				t.T("workflow_delete_pages", "Delete pages"),
				t.T("del_need_one_page", "Add at least one page to continue."),
			)
			return
		}
		s.showOptionsStep()
	case workflowStepOptions:
		s.captureOptions()
		if err := s.validateOptions(); err != nil {
			s.app.showMessage(t.T("del_options_heading", "Deletion options"), err.Error())
			return
		}
		if msg := s.validateMediaWikiNamespaceDeleteAccess(s.finalTitles()); msg != "" {
			s.app.showMessage(t.T("del_options_heading", "Deletion options"), msg)
			return
		}
		s.showVerificationStep()
	case workflowStepVerification:
		if msg := s.validateMediaWikiNamespaceDeleteAccess(s.finalTitles()); msg != "" {
			s.app.showMessage(t.T("workflow_delete_pages", "Delete pages"), msg)
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

func (s *deleteWorkflowScreen) showSelectionStep() {
	s.step = workflowStepSelection
	s.wf.SetStep(s.step)
	s.wf.SetButtons(workflowButtonState{
		BackEnabled:    false,
		HomeEnabled:    true,
		CancelEnabled:  false,
		ProceedEnabled: len(s.selectedTitles) > 0,
		ProceedLabel:   t.T("common_proceed", "Proceed"),
	})
	s.wf.SetContent(s.buildSelectionContent())
}

func (s *deleteWorkflowScreen) showOptionsStep() {
	s.step = workflowStepOptions
	s.wf.SetStep(s.step)
	s.wf.SetButtons(workflowButtonState{
		BackEnabled:    true,
		HomeEnabled:    true,
		CancelEnabled:  false,
		ProceedEnabled: true,
		ProceedLabel:   t.T("common_proceed", "Proceed"),
	})
	// Load before building: the reasons come from the session cache, so the Select can be built already populated.
	s.loadReasons()
	s.wf.SetContent(s.buildOptionsContent())
}

func (s *deleteWorkflowScreen) showVerificationStep() {
	s.step = workflowStepVerification
	s.wf.SetStep(s.step)
	s.wf.SetContent(s.buildVerificationContent())
	// The read phase computes the full expanded list (redirects, transitively,
	// plus talk pages); until it finishes only Cancel is active.
	s.wf.SetButtons(workflowButtonState{
		BackEnabled:    false,
		HomeEnabled:    false,
		CancelEnabled:  true,
		ProceedEnabled: false,
		ProceedLabel:   t.T("common_proceed", "Proceed"),
	})
	s.computePreview()
}

// progressThrottle bounds how often the verification screen repaints while the list is being calculated, so a long
// (thousands of pages) read phase does not flood the main goroutine with updates.
const progressThrottle = 120 * time.Millisecond

// verificationProgress rate-limits the read phase's UI updates while remembering the newest one it drops for each
// step. The last callback of a pass carries the final counts and usually lands inside the throttle window; discarding
// it froze the label at the last survivor ("84/89 pages processed" on a finished search) while complete() filled the
// bar. flush replays the held update when the pass ends, so the words agree with the bar.
type verificationProgress struct {
	mu       sync.Mutex
	lastPush time.Time
	held     map[string]func() // newest dropped update per step, keyed by the step's name
	do       func(func())      // fyne.Do, injectable for tests
}

func newVerificationProgress() *verificationProgress {
	return &verificationProgress{held: map[string]func(){}, do: fyne.Do}
}

// push runs update on the main goroutine, or — inside the throttle window — holds it under key, replacing any update
// already held there: only the newest counts matter.
func (p *verificationProgress) push(key string, update func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Since(p.lastPush) < progressThrottle {
		p.held[key] = update
		return
	}
	p.lastPush = time.Now()
	delete(p.held, key)
	p.do(update)
}

// flush runs the update held for key, if any. Call it on the main goroutine, after the pass that pushes under key has
// finished — it applies the final counts the throttle held back, unthrottled, since the pass will send no more.
func (p *verificationProgress) flush(key string) {
	p.mu.Lock()
	update := p.held[key]
	delete(p.held, key)
	p.mu.Unlock()
	if update != nil {
		update()
	}
}

// computePreview runs the deletion read phase off the main goroutine and streams progress to the verification screen.
// Phase 1 (BuildPlan) expands the selection (redirects, talk pages) and reports "Calculating… N% (M found)"; once the
// list exists it is shown immediately, then phase 2 annotates categories (emptiness checks) with a percentage over the
// known total. Canceling (workflow Cancel button) cancels the context and returns to Options.
func (s *deleteWorkflowScreen) computePreview() {
	if s.app.client == nil {
		s.verificationInfo.SetText(t.T("del_not_connected", "Not connected to a wiki."))
		s.restoreExecutionButtons()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.previewCancel = cancel
	s.previewComputing = true

	titles := s.finalTitles()
	planOptions := deletion.PlanOptions{
		Reason:          s.combinedReason(),
		IncludeTalk:     s.optionIncludeTalk,
		IncludeRedirect: s.optionIncludeRedir,
	}
	provider := &deletionDataProvider{
		client: s.app.client,
		apiURL: s.app.apiURL,
		caps:   s.app.currentCaps,
		ctxFn: func() context.Context {
			return ctx
		},
	}

	// Announce the passes that will run before any of them has anything to report, so the reader sees the whole plan
	// rather than rows arriving one at a time. Which ones run is already settled: the two options just chosen, and
	// whether the selection holds a category for the last one to have anything to check.
	s.showPendingVerificationSteps(titles)

	go func() {
		defer cancel()

		progress := newVerificationProgress()

		// Discovery, measured against the reference pages — the only total known this early.
		if planOptions.IncludeRedirect {
			planOptions.OnProgress = func(processed, total, found int) {
				progress.push("redirects", func() {
					s.stepRedirects.set(processed, total, redirectStepLabel(processed, total, found))
				})
			}
		}
		planOptions.OnTalkCheck = func(done, total int) {
			progress.push("talk", func() { s.stepTalkPages.set(done, total, talkStepLabel(done, total)) })
		}
		plan, err := deletion.BuildPlan(provider, titles, planOptions)
		if err != nil {
			fyne.Do(func() { s.finishPreviewWithError(ctx, err) })
			return
		}

		// The list is now available: show every row immediately (final order), then annotate categories.
		base := make([]previewRow, len(plan.Items))
		for i, item := range plan.Items {
			base[i] = previewRow{item: item}
		}
		fyne.Do(func() {
			s.previewRows = base
			s.verificationList.Refresh()
			// Both passes are over: apply the final counts the throttle held back, then fill the bars rather than
			// leave them wherever the throttle last let them land. Either can finish inside one throttle window.
			progress.flush("redirects")
			progress.flush("talk")
			s.stepRedirects.complete()
			s.stepTalkPages.complete()
		})

		// The category-emptiness checks: a request per category, and nothing at all for any other page.
		rows, err := s.buildPreviewRows(ctx, plan, func(done, total int) {
			progress.push("categories", func() { s.stepCategories.set(done, total, categoryCheckLabel(done, total)) })
		})
		fyne.Do(func() {
			if err != nil {
				s.finishPreviewWithError(ctx, err)
				return
			}
			s.previewComputing = false
			s.previewCancel = nil
			s.previewPlan = plan
			s.previewRows = rows
			progress.flush("categories")
			s.completeVerificationSteps()
			s.verificationInfo.SetText(t.Td(
				"del_summary",
				"{{.Operations}} operations · {{.Pages}} pages will be deleted.",
				map[string]any{"Operations": plan.OperationCount(), "Pages": plan.PageCount},
			))
			s.verificationList.Refresh()
			s.restoreExecutionButtons()
		})
	}()
}

// finishPreviewWithError returns to Options after a failed/canceled read phase, surfacing an error dialog only when the
// failure is not a cancellation.
func (s *deleteWorkflowScreen) finishPreviewWithError(ctx context.Context, err error) {
	s.previewComputing = false
	s.previewCancel = nil
	s.showOptionsStep()
	if ctx.Err() == nil && !errors.Is(err, context.Canceled) {
		s.app.showError(t.T("del_err_verify", "Verify"), err)
	}
}

// verificationProgressWidth keeps each bar a fixed strip at the left of its row: a ProgressBar sizes itself to the text
// it draws, and these draw none.
const verificationProgressWidth = 120

// verificationStep is one of the passes on the way to the final list. Each says what it is doing and counts its own
// work, so a slow run shows which step is slow rather than hiding it behind one number: finding redirects is a request
// per level of the redirect chain, the talk-page check is one batched query, and the category check is a request per
// category.
//
// The rows that will run are all shown from the moment the step opens, before any of them has anything to report, so
// the reader can see the whole plan rather than watch rows appear and vanish. Which rows those are is decided by the
// options already chosen: no redirects wanted, no redirects row.
type verificationStep struct {
	bar   *widget.ProgressBar
	label *widget.Label
	row   fyne.CanvasObject
	// header is the container the row is laid out in. Showing a hidden child does not tell its ancestors that their
	// height changed, so without refreshing it a shown row is Visible() at zero height — present, and invisible.
	header *fyne.Container
}

func newVerificationStep() *verificationStep {
	bar := widget.NewProgressBar()
	bar.TextFormatter = func() string { return "" } // the label beside it carries the words
	label := widget.NewLabel("")
	step := &verificationStep{bar: bar, label: label}
	step.row = container.NewBorder(
		nil, nil,
		container.NewGridWrap(fyne.NewSize(verificationProgressWidth, label.MinSize().Height), bar),
		nil,
		label,
	)
	step.row.Hide()
	return step
}

// pending shows the row before its work starts. Two of the three cannot know their total until discovery has finished —
// how many talk pages there are to check is one of the things being discovered — so the row names itself and leaves the
// bar empty rather than inventing a number or waiting to appear.
func (v *verificationStep) pending(text string) {
	wasHidden := !v.row.Visible()
	v.bar.Max = 1
	v.bar.SetValue(0)
	v.label.SetText(text)
	v.row.Show()
	if wasHidden {
		v.refreshHeader()
	}
}

// set moves the step to done/total. A total of zero means there turned out to be nothing to do, and the row goes: a
// batch with no categories in it should not be told about the category check.
func (v *verificationStep) set(done, total int, text string) {
	if total <= 0 {
		v.hide()
		return
	}
	wasHidden := !v.row.Visible()
	v.bar.Max = float64(total)
	v.bar.SetValue(float64(min(done, total)))
	v.label.SetText(text)
	v.row.Show()
	if wasHidden {
		v.refreshHeader()
	}
}

// refreshHeader re-lays out the rows after one appears or goes. Fyne sizes a container from its visible children, but
// showing a child only refreshes the child: the row would sit at zero height, visible to code and to no one else.
func (v *verificationStep) refreshHeader() {
	if v.header != nil {
		v.header.Refresh()
	}
}

// complete fills the bar. A step can finish inside a single throttle window — discovery without redirects needs no
// request at all — which would otherwise leave the bar showing the first tick it happened to catch.
func (v *verificationStep) complete() {
	if v.row.Visible() {
		v.bar.SetValue(v.bar.Max)
	}
}

func (v *verificationStep) hide() {
	if !v.row.Visible() {
		return
	}
	v.row.Hide()
	v.refreshHeader()
}

// showPendingVerificationSteps puts up the rows for the passes that will run, before any of them has a count. The two
// options settle their own rows. The category row needs a category in the selection to have anything to check — a
// discovered redirect can be one too, so the row can still arrive later, but the common case is decided here rather
// than left to appear mid-run.
func (s *deleteWorkflowScreen) showPendingVerificationSteps(titles []string) {
	if s.optionIncludeRedir {
		s.stepRedirects.pending(t.T("del_step_redirects_pending", "Finding redirects"))
	}
	if s.optionIncludeTalk {
		s.stepTalkPages.pending(t.T("del_step_talk_pending", "Checking talk pages"))
	}
	if slices.ContainsFunc(titles, s.isCategoryTitle) {
		s.stepCategories.pending(t.T("del_step_categories_pending", "Checking categories"))
	}
}

// completeVerificationSteps fills the rows that ran and leaves them standing. They are the record of the search the
// user asked for: a short list finishes each pass faster than it can be watched, and clearing the rows at the end
// turned that into a flicker — the answer arrives having apparently done nothing.
func (s *deleteWorkflowScreen) completeVerificationSteps() {
	for _, step := range []*verificationStep{s.stepRedirects, s.stepTalkPages, s.stepCategories} {
		if step != nil {
			step.complete()
		}
	}
}

// redirectStepLabel names the discovery pass by the thing that costs time in it: one request per page asking the wiki
// what redirects there. It counts the reference pages, not everything discovered, and reports found beside them.
func redirectStepLabel(processed, total, found int) string {
	return t.Td(
		"del_step_redirects",
		"Finding redirects: {{.Processed}}/{{.Total}} pages processed, {{.Found}} pages found",
		map[string]any{"Processed": processed, "Total": total, "Found": found},
	)
}

// talkStepLabel names the associated-talk-page existence check. The wiki answers in batches, so it moves in jumps.
func talkStepLabel(done, total int) string {
	return t.Td(
		"del_step_talk_pages",
		"Checking talk pages: {{.Done}}/{{.Total}}",
		map[string]any{"Done": done, "Total": total},
	)
}

// categoryCheckLabel is the status line for the category-emptiness checks, which are the only work left once the list
// is known. It names them rather than saying "finalizing", because it counts categories and not pages, and a batch with
// no categories never shows it at all. The bar stays where discovery left it — full — this being a different pass over
// a different set.
func categoryCheckLabel(done, total int) string {
	return t.Td(
		"del_checking_categories",
		"Checking categories: {{.Done}}/{{.Total}}",
		map[string]any{"Done": done, "Total": total},
	)
}

// buildPreviewRows wraps plan items with UI annotations. For each category it checks how many members would remain
// (not in the deletion set), since MediaWiki refuses to delete a non-empty category. onProgress, when set, is called
// after each item with the count done and the total, for the finalize-phase percentage.
func (s *deleteWorkflowScreen) buildPreviewRows(
	ctx context.Context, plan deletion.Plan, onProgress func(done, total int),
) ([]previewRow, error) {
	deleted := make(map[string]struct{}, len(plan.Items))
	for _, item := range plan.Items {
		deleted[item.Title] = struct{}{}
	}

	// A category is the only row that costs a request here, so it is the only one worth counting. Ticking once per page
	// announced work for pages nothing was ever done to, and a batch with no categories at all claimed a full pass.
	categories := 0
	for _, item := range plan.Items {
		if s.isCategoryTitle(item.Title) {
			categories++
		}
	}
	if onProgress != nil && categories > 0 {
		onProgress(0, categories)
	}

	rows := make([]previewRow, 0, len(plan.Items))
	checked := 0
	for _, item := range plan.Items {
		row := previewRow{item: item}
		if s.isCategoryTitle(item.Title) {
			remaining, err := s.categoryMembersOutsideSet(ctx, item.Title, deleted)
			if err != nil {
				return nil, err
			}
			if remaining > 0 {
				row.categoryNotEmpty = true
				row.remainingMembers = remaining
			}
			checked++
			if onProgress != nil {
				onProgress(checked, categories)
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// categoryMembersOutsideSet counts a category's members that are not themselves
// being deleted — i.e. those that would keep the category from being empty.
func (s *deleteWorkflowScreen) categoryMembersOutsideSet(
	ctx context.Context,
	category string,
	deleted map[string]struct{},
) (int, error) {
	params := map[string]string{
		"action":        "query",
		"list":          "categorymembers",
		"cmtitle":       category,
		"cmlimit":       "max",
		"formatversion": "2",
	}
	remaining := 0
	for {
		payload, err := s.app.client.GetContext(ctx, s.app.apiURL, params)
		if err != nil {
			return 0, fmt.Errorf("query members of %q: %w", category, err)
		}
		query, _ := payload["query"].(map[string]any)
		members, _ := query["categorymembers"].([]any)
		for _, raw := range members {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			title, _ := entry["title"].(string)
			if strings.TrimSpace(title) == "" {
				continue
			}
			if _, ok := deleted[title]; !ok {
				remaining++
			}
		}
		continueMap, _ := payload["continue"].(map[string]any)
		next, _ := continueMap["cmcontinue"].(string)
		if next == "" {
			break
		}
		params["cmcontinue"] = next
	}
	return remaining, nil
}

func (s *deleteWorkflowScreen) showExecutionStep() {
	s.step = workflowStepExecution
	s.wf.SetStep(s.step)
	s.executionDone = false
	s.executionCanceled = false
	s.wf.SetButtons(workflowButtonState{
		BackEnabled:    true,
		HomeEnabled:    true,
		CancelEnabled:  false,
		ProceedEnabled: true,
		ProceedLabel:   t.T("common_proceed", "Proceed"),
	})
	s.wf.SetContent(s.buildExecutionContent())
}

func (s *deleteWorkflowScreen) buildSelectionContent() fyne.CanvasObject {
	s.selectionResultLabel = widget.NewLabel(t.Tp("del_results_count", "{{.Count}} result", "{{.Count}} results", 0))
	s.selectionResultList = newDeletableList(
		func() int { return len(s.searchResults) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < 0 || id >= len(s.searchResults) {
				obj.(*widget.Label).SetText("")
				return
			}
			item := s.searchResults[id]
			if item.Size > 0 {
				obj.(*widget.Label).SetText(
					t.Tpd(
						"del_result_bytes",
						"{{.Title}} ({{.Count}} byte)",
						"{{.Title}} ({{.Count}} bytes)",
						int(item.Size),
						map[string]any{"Title": item.Title},
					),
				)
				return
			}
			obj.(*widget.Label).SetText(item.Title)
		},
		func() {
			s.deleteSelectedSearchResult()
		},
	)
	s.selectionResultList.OnSelected = func(id widget.ListItemID) {
		s.selectedSearchIndex = id
		s.selectedFinalIndex = -1
	}
	s.selectionResultList.OnUnselected = func(widget.ListItemID) {
		s.selectedSearchIndex = -1
	}

	s.searchPrefixEntry = widget.NewEntry()
	s.searchNamespaceSelect = widget.NewSelect(s.namespaceOptions(), nil)
	s.searchNamespaceSelect.SetSelected(t.T("del_namespace_any", "(any)"))
	s.searchCategoryEntry = widget.NewEntry()
	s.searchCategoryRecurChk = widget.NewCheck(t.T("del_check_recursive", "Recursive search"), nil)
	s.searchCategoryInclChk = widget.NewCheck(t.T("del_check_include_categories", "Include categories"), nil)
	s.searchCreatorEntry = widget.NewEntry()
	s.searchLinkedFromEntry = widget.NewEntry()
	s.searchTemplateEntry = widget.NewEntry()
	s.searchMinSizeEntry = widget.NewEntry()
	s.searchMaxSizeEntry = widget.NewEntry()
	s.searchRedirectsCheck = widget.NewCheck(t.T("del_check_redirects_only", "Redirects only"), nil)
	s.searchBrokenRedirCheck = widget.NewCheck(t.T("del_check_broken_redirects", "Broken redirects"), nil)

	searchBtn := widget.NewButton(t.T("del_search", "Search"), func() { s.performSearch() })
	addSearchBtn := widget.NewButton(t.T("del_add_to_list", "Add to list"), func() {
		for _, result := range s.searchResults {
			s.addToFinalList(result.Title)
		}
		s.searchResults = []searchResult{}
		s.selectedSearchIndex = -1
		s.refreshSelectionPanel()
	})
	clearResultsBtn := widget.NewButton(t.T("del_clear_results", "Clear results"), func() {
		s.searchResults = []searchResult{}
		s.refreshSelectionPanel()
	})

	searchForm := container.NewVBox(
		widget.NewLabelWithStyle(t.T("del_search", "Search"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		s.labeledField(t.T("del_field_title_prefix", "Title prefix"), s.searchPrefixEntry),
		s.labeledField(t.T("del_field_namespace", "Namespace"), s.searchNamespaceSelect),
		s.labeledField(t.T("del_field_category", "Category"), s.searchCategoryEntry),
		container.NewHBox(s.searchCategoryRecurChk, s.searchCategoryInclChk),
		s.labeledField(t.T("del_field_creator", "Creator"), s.searchCreatorEntry),
		s.labeledField(t.T("del_field_linked_from", "Pages linked from"), s.searchLinkedFromEntry),
		s.labeledField(t.T("del_field_template", "Pages transcluding template"), s.searchTemplateEntry),
		s.labeledField(t.T("del_field_min_size", "Min size (bytes)"), s.searchMinSizeEntry),
		s.labeledField(t.T("del_field_max_size", "Max size (bytes)"), s.searchMaxSizeEntry),
		container.NewHBox(s.searchRedirectsCheck, s.searchBrokenRedirCheck),
		searchBtn,
	)

	s.manualEntry = widget.NewMultiLineEntry()
	s.manualEntry.SetPlaceHolder(t.T("del_placeholder_manual", "One title per line"))
	manualAddBtn := widget.NewButton(t.T("del_add_to_list", "Add to list"), func() {
		for line := range strings.SplitSeq(s.manualEntry.Text, "\n") {
			title := normalizeManualTitle(line)
			if title == "" {
				continue
			}
			s.addToFinalList(title)
		}
		s.refreshSelectionPanel()
	})
	importBtn := widget.NewButton(t.T("del_import_file", "Import file..."), func() {
		d := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				s.app.showError(t.T("del_import", "Import"), err)
				return
			}
			if reader == nil {
				return
			}
			defer func() {
				_ = reader.Close()
			}()
			bytes, readErr := io.ReadAll(reader)
			if readErr != nil {
				s.app.showError(t.T("del_import", "Import"), readErr)
				return
			}
			for line := range strings.SplitSeq(string(bytes), "\n") {
				title := normalizeManualTitle(line)
				if title == "" {
					continue
				}
				s.addToFinalList(title)
			}
			s.refreshSelectionPanel()
		}, s.app.window)
		d.Show()
	})

	// Border (not VBox) so the multi-line entry fills the tab's full height, with the buttons pinned at the bottom —
	// there is nothing below it, so the whole vertical space is used.
	manualTab := container.NewBorder(
		nil,
		container.NewHBox(manualAddBtn, importBtn),
		nil,
		nil,
		s.manualEntry,
	)

	// Scroll the (tall) search form so the Selection step doesn't force the window taller than a small screen.
	left := container.NewAppTabs(
		container.NewTabItem(t.T("del_search", "Search"), container.NewVScroll(searchForm)),
		container.NewTabItem(t.T("del_tab_manual", "Manual entry"), manualTab),
	)

	s.selectionFinalLabel = widget.NewLabel("")
	s.selectionFinalList = newDeletableList(
		func() int { return len(s.finalTitles()) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			titles := s.finalTitles()
			if id < 0 || id >= len(titles) {
				obj.(*widget.Label).SetText("")
				return
			}
			obj.(*widget.Label).SetText(titles[id])
		},
		func() {
			s.deleteSelectedFinalItem()
		},
	)
	s.selectionFinalList.OnSelected = func(id widget.ListItemID) {
		s.selectedFinalIndex = id
		s.selectedSearchIndex = -1
	}
	s.selectionFinalList.OnUnselected = func(widget.ListItemID) {
		s.selectedFinalIndex = -1
	}

	clearListBtn := widget.NewButton(t.T("del_clear_list", "Clear list"), func() {
		s.selectedTitles = map[string]struct{}{}
		s.categoryParents = map[string]map[string]struct{}{}
		s.refreshSelectionPanel()
	})
	s.selectionProceedBtn = widget.NewButton(t.T("del_proceed_options", "Proceed to options")+" →", func() {
		s.onProceed()
	})

	searchResultsPanel := container.NewBorder(
		container.NewVBox(
			widget.NewLabelWithStyle(
				t.T("del_search_results", "Search results"),
				fyne.TextAlignLeading,
				fyne.TextStyle{Bold: true},
			),
			s.selectionResultLabel,
		),
		container.NewVBox(widget.NewSeparator(), container.NewHBox(addSearchBtn, clearResultsBtn)),
		nil,
		nil,
		container.NewVScroll(s.selectionResultList),
	)

	finalListPanel := container.NewBorder(
		container.NewVBox(
			widget.NewLabelWithStyle(
				t.T("del_final_list", "Final list"),
				fyne.TextAlignLeading,
				fyne.TextStyle{Bold: true},
			),
			s.selectionFinalLabel,
		),
		container.NewVBox(widget.NewSeparator(), container.NewHBox(clearListBtn, s.selectionProceedBtn)),
		nil,
		nil,
		container.NewVScroll(s.selectionFinalList),
	)

	right := container.NewGridWithRows(
		2,
		searchResultsPanel,
		finalListPanel,
	)

	s.refreshSelectionPanel()
	return container.NewHSplit(left, right)
}

func (s *deleteWorkflowScreen) buildOptionsContent() fyne.CanvasObject {
	s.optionReasonSelect = widget.NewSelect(s.reasonSelectOptions(), func(value string) {
		s.optionReasonChoice = value
	})
	if s.optionReasonChoice != "" {
		s.optionReasonSelect.SetSelected(s.optionReasonChoice)
	} else if len(s.optionReasonSelect.Options) > 0 {
		s.optionReasonSelect.SetSelected(s.optionReasonSelect.Options[0])
	}

	s.optionReasonTextEntry = widget.NewEntry()
	s.optionReasonTextEntry.SetText(s.optionReasonFreeText)

	s.optionIncludeTalkCheck = widget.NewCheck(
		t.T("del_check_delete_talk", "Also delete talk pages"),
		func(value bool) {
			s.optionIncludeTalk = value
		},
	)
	s.optionIncludeTalkCheck.SetChecked(s.optionIncludeTalk)

	s.optionIncludeRedirCheck = widget.NewCheck(
		t.T("del_check_delete_redirects", "Also delete redirects"),
		func(value bool) {
			s.optionIncludeRedir = value
		},
	)
	s.optionIncludeRedirCheck.SetChecked(s.optionIncludeRedir)

	s.optionDryRunCheck = widget.NewCheck(t.T("del_check_dry_run", "Dry-run"), func(value bool) {
		s.optionDryRun = value
	})
	s.optionDryRunCheck.SetChecked(s.optionDryRun)

	return container.NewVBox(
		widget.NewLabelWithStyle(
			t.T("del_options_heading", "Deletion options"),
			fyne.TextAlignLeading,
			fyne.TextStyle{Bold: true},
		),
		s.labeledField(t.T("del_field_reason", "Reason"), s.optionReasonSelect),
		s.labeledField(t.T("del_field_additional_reason", "Additional reason text"), s.optionReasonTextEntry),
		widget.NewSeparator(),
		s.optionIncludeTalkCheck,
		s.optionIncludeRedirCheck,
		s.optionDryRunCheck,
	)
}

func (s *deleteWorkflowScreen) buildVerificationContent() fyne.CanvasObject {
	s.verificationInfo = widget.NewLabel(t.T("del_computing", "Computing pages to delete…"))
	s.stepRedirects = newVerificationStep()
	s.stepTalkPages = newVerificationStep()
	s.stepCategories = newVerificationStep()
	detail := widget.NewLabel(s.optionSummary())
	s.previewRows = nil

	s.verificationList = newDeletableList(
		func() int { return len(s.previewRows) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < 0 || id >= len(s.previewRows) {
				obj.(*widget.Label).SetText("")
				return
			}
			obj.(*widget.Label).SetText(previewRowText(s.previewRows[id]))
		},
		func() { s.deleteSelectedPreviewItem() },
	)
	s.verificationList.OnSelected = func(id widget.ListItemID) {
		s.selectedPreviewIndex = id
	}
	s.verificationList.OnUnselected = func(widget.ListItemID) {
		s.selectedPreviewIndex = -1
	}

	legend := widget.NewLabelWithStyle(
		glyphTalkPage+" "+t.T("del_legend_talk", "talk page also deleted")+
			"   ·   "+glyphDerived+" "+t.T("del_legend_redirect", "redirect to a selected page"),
		fyne.TextAlignLeading,
		fyne.TextStyle{Italic: true},
	)
	// One row per pass that can cost time, each hidden until it has work, so what is taking the time says so itself.
	steps := container.NewVBox(s.stepRedirects.row, s.stepTalkPages.row, s.stepCategories.row)
	header := container.NewVBox(s.verificationInfo, steps, detail, widget.NewSeparator())
	for _, step := range []*verificationStep{s.stepRedirects, s.stepTalkPages, s.stepCategories} {
		step.header = header
	}

	return container.NewBorder(
		header,
		container.NewVBox(widget.NewSeparator(), legend),
		nil,
		nil,
		container.NewVScroll(s.verificationList),
	)
}

// deleteSelectedPreviewItem drops the highlighted page (and its dependents) from the verification list via the
// Delete/Backspace key — the same affordance as the Selection step's lists — since keeping some pages after all is part
// of verifying. Removing a selected page also removes its talk page and redirects; removing a redirect removes only the
// redirects that point at it. A row is re-selected afterwards so repeated Delete keeps pruning.
func (s *deleteWorkflowScreen) deleteSelectedPreviewItem() {
	if s.previewComputing || s.selectedPreviewIndex < 0 || s.selectedPreviewIndex >= len(s.previewRows) {
		return
	}
	s.previewPlan = s.previewPlan.RemoveWithDependents(s.previewRows[s.selectedPreviewIndex].item.Title)

	remaining := make(map[string]struct{}, len(s.previewPlan.Items))
	for _, item := range s.previewPlan.Items {
		remaining[item.Title] = struct{}{}
	}
	kept := s.previewRows[:0]
	for _, row := range s.previewRows {
		if _, ok := remaining[row.item.Title]; ok {
			kept = append(kept, row)
		}
	}
	s.previewRows = kept

	next := s.selectedPreviewIndex
	if next >= len(s.previewRows) {
		next = len(s.previewRows) - 1
	}
	s.selectedPreviewIndex = next
	s.verificationList.Refresh()
	s.updateVerificationSummary()
	s.verificationList.UnselectAll()
	if next >= 0 {
		s.verificationList.Select(next)
	}
}

// updateVerificationSummary refreshes the count line and the Proceed button after a removal; an emptied list cannot
// proceed.
func (s *deleteWorkflowScreen) updateVerificationSummary() {
	if len(s.previewPlan.Items) == 0 {
		s.verificationInfo.SetText(t.T("del_verify_empty", "No pages left — go Back to change the selection."))
		s.wf.SetButtons(workflowButtonState{
			BackEnabled:    true,
			HomeEnabled:    true,
			CancelEnabled:  false,
			ProceedEnabled: false,
			ProceedLabel:   t.T("common_proceed", "Proceed"),
		})
		return
	}
	s.verificationInfo.SetText(t.Td(
		"del_summary",
		"{{.Operations}} operations · {{.Pages}} pages will be deleted.",
		map[string]any{"Operations": s.previewPlan.OperationCount(), "Pages": s.previewPlan.PageCount},
	))
	s.restoreExecutionButtons()
}

func (s *deleteWorkflowScreen) buildExecutionContent() fyne.CanvasObject {
	s.executionInfo = widget.NewLabel(t.T("del_press_proceed", "Press Proceed to execute the deletion workflow."))
	s.executionRows = []executionRow{}
	s.executionList = widget.NewList(
		func() int { return len(s.executionRows) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < 0 || id >= len(s.executionRows) {
				obj.(*widget.Label).SetText("")
				return
			}
			row := s.executionRows[id]
			line := fmt.Sprintf("%s %s", statusSymbol(row.Status), row.Title)
			if strings.TrimSpace(row.Detail) != "" {
				line += " - " + row.Detail
			}
			obj.(*widget.Label).SetText(line)
		},
	)

	s.downloadsBox = container.NewHBox(
		widget.NewButton(t.T("del_download_journal", "Download journal"), func() {
			s.saveJournalFile("journal.json", s.journalEntries)
		}),
		widget.NewButton(t.T("del_download_successes", "Download successes"), func() {
			s.saveJournalFile("journal_successes.json", s.filterJournalEntries("success"))
		}),
		widget.NewButton(t.T("del_download_failures", "Download failures"), func() {
			s.saveJournalFile("journal_failures.json", s.filterJournalEntries("error"))
		}),
	)
	s.downloadsBox.Hide()

	return container.NewBorder(
		container.NewVBox(
			widget.NewLabelWithStyle(
				t.T("del_execution_heading", "Execution"),
				fyne.TextAlignLeading,
				fyne.TextStyle{Bold: true},
			),
			s.executionInfo,
			widget.NewSeparator(),
		),
		container.NewVBox(widget.NewSeparator(), s.downloadsBox),
		nil,
		nil,
		container.NewVScroll(s.executionList),
	)
}

func (s *deleteWorkflowScreen) captureOptions() {
	if s.optionReasonSelect != nil {
		s.optionReasonChoice = strings.TrimSpace(s.optionReasonSelect.Selected)
	}
	if s.optionReasonTextEntry != nil {
		s.optionReasonFreeText = strings.TrimSpace(s.optionReasonTextEntry.Text)
	}
}

func (s *deleteWorkflowScreen) optionSummary() string {
	parts := []string{}
	if reason := s.combinedReason(); reason != "" {
		parts = append(parts, t.Td("del_summary_reason", "Reason: {{.Reason}}", map[string]any{"Reason": reason}))
	}
	if s.optionIncludeTalk {
		parts = append(parts, t.T("del_summary_includes_talk", "Includes talk pages"))
	}
	if s.optionIncludeRedir {
		parts = append(parts, t.T("del_summary_includes_redirects", "Includes redirects"))
	}
	if s.optionDryRun {
		parts = append(parts, t.T("del_summary_dry_run", "Dry-run mode"))
	}
	if len(parts) == 0 {
		return t.T("del_summary_none", "No additional options.")
	}
	return strings.Join(parts, " | ")
}

func (s *deleteWorkflowScreen) refreshSelectionPanel() {
	if s.selectionResultLabel != nil {
		s.selectionResultLabel.SetText(
			t.Tp("del_results_count", "{{.Count}} result", "{{.Count}} results", len(s.searchResults)),
		)
	}
	if s.selectionResultList != nil {
		s.selectionResultList.Refresh()
	}
	if s.selectionFinalLabel != nil {
		s.selectionFinalLabel.SetText(
			t.Tp("del_pages_in_list", "{{.Count}} page in list", "{{.Count}} pages in list", len(s.selectedTitles)),
		)
	}
	if s.selectionFinalList != nil {
		s.selectionFinalList.Refresh()
	}
	if s.selectionProceedBtn != nil {
		if len(s.selectedTitles) > 0 {
			s.selectionProceedBtn.Enable()
		} else {
			s.selectionProceedBtn.Disable()
		}
	}
	if s.step == workflowStepSelection {
		s.wf.SetButtons(workflowButtonState{
			BackEnabled:    false,
			HomeEnabled:    true,
			CancelEnabled:  false,
			ProceedEnabled: len(s.selectedTitles) > 0,
			ProceedLabel:   t.T("common_proceed", "Proceed"),
		})
	}
}

func (s *deleteWorkflowScreen) deleteSelectedSearchResult() {
	if s.selectedSearchIndex < 0 || s.selectedSearchIndex >= len(s.searchResults) {
		return
	}
	s.searchResults = append(s.searchResults[:s.selectedSearchIndex], s.searchResults[s.selectedSearchIndex+1:]...)
	next := s.selectedSearchIndex
	if next >= len(s.searchResults) {
		next = len(s.searchResults) - 1
	}
	s.selectedSearchIndex = next
	s.refreshSelectionPanel()
	if s.selectionResultList != nil {
		s.selectionResultList.UnselectAll()
	}
	if next >= 0 && s.selectionResultList != nil {
		s.selectionResultList.Select(next)
	}
}

func (s *deleteWorkflowScreen) deleteSelectedFinalItem() {
	titles := s.finalTitles()
	if s.selectedFinalIndex < 0 || s.selectedFinalIndex >= len(titles) {
		return
	}
	delete(s.selectedTitles, titles[s.selectedFinalIndex])
	next := s.selectedFinalIndex
	if next >= len(s.selectedTitles) {
		next = len(s.selectedTitles) - 1
	}
	s.selectedFinalIndex = next
	s.refreshSelectionPanel()
	if s.selectionFinalList != nil {
		s.selectionFinalList.UnselectAll()
	}
	if next >= 0 && s.selectionFinalList != nil {
		s.selectionFinalList.Select(next)
	}
}

func (s *deleteWorkflowScreen) addToFinalList(title string) {
	title = strings.TrimSpace(title)
	if title == "" {
		return
	}
	s.selectedTitles[title] = struct{}{}
}

func (s *deleteWorkflowScreen) finalTitles() []string {
	nonCategory := []string{}
	categories := []string{}
	for title := range s.selectedTitles {
		if s.isCategoryTitle(title) {
			categories = append(categories, title)
		} else {
			nonCategory = append(nonCategory, title)
		}
	}
	sort.Strings(nonCategory)
	return append(nonCategory, sortCategoriesTopologically(categories, s.categoryParents)...)
}

func (s *deleteWorkflowScreen) namespaceOptions() []string {
	labels := []string{t.T("del_namespace_any", "(any)")}
	if len(s.app.currentCaps.Namespaces) == 0 {
		return labels
	}
	keys := make([]int, 0, len(s.app.currentCaps.Namespaces))
	for id := range s.app.currentCaps.Namespaces {
		keys = append(keys, id)
	}
	sort.Ints(keys)
	for _, id := range keys {
		name := strings.TrimSpace(s.app.currentCaps.Namespaces[id])
		if name == "" {
			name = t.T("del_namespace_main", "(Main)")
		}
		labels = append(labels, fmt.Sprintf("%d: %s", id, name))
	}
	return labels
}

func (s *deleteWorkflowScreen) parseNamespaceSelection() (int, bool) {
	if s.searchNamespaceSelect == nil {
		return 0, false
	}
	selected := strings.TrimSpace(s.searchNamespaceSelect.Selected)
	if selected == "" || selected == t.T("del_namespace_any", "(any)") {
		return 0, false
	}
	parts := strings.SplitN(selected, ":", 2)
	id, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, false
	}
	return id, true
}

// searchCriteria is a snapshot of the search form. It is captured on the main
// goroutine (see collectSearchCriteria) so the background search never reads Fyne
// widgets off-thread.
type searchCriteria struct {
	prefix            string
	category          string
	categoryRecursive bool
	categoryInclude   bool
	creator           string
	linkedFrom        string
	template          string
	redirectsOnly     bool
	brokenRedirects   bool
	namespaceID       int
	namespaceSet      bool
	minSize           int64
	minSet            bool
	minSizeText       string
	minSizeErr        error
	maxSize           int64
	maxSet            bool
	maxSizeText       string
	maxSizeErr        error
}

// collectSearchCriteria reads the search-form widgets into a snapshot. Call it on
// the main goroutine only.
func (s *deleteWorkflowScreen) collectSearchCriteria() searchCriteria {
	namespaceID, namespaceSet := s.parseNamespaceSelection()
	minSize, minSet, minSizeErr := parseSizeFilter(s.searchMinSizeEntry.Text)
	maxSize, maxSet, maxSizeErr := parseSizeFilter(s.searchMaxSizeEntry.Text)
	return searchCriteria{
		prefix:            strings.TrimSpace(s.searchPrefixEntry.Text),
		category:          strings.TrimSpace(s.searchCategoryEntry.Text),
		categoryRecursive: s.searchCategoryRecurChk != nil && s.searchCategoryRecurChk.Checked,
		categoryInclude:   s.searchCategoryInclChk != nil && s.searchCategoryInclChk.Checked,
		creator:           strings.TrimSpace(s.searchCreatorEntry.Text),
		linkedFrom:        strings.TrimSpace(s.searchLinkedFromEntry.Text),
		template:          strings.TrimSpace(s.searchTemplateEntry.Text),
		redirectsOnly:     s.searchRedirectsCheck.Checked,
		brokenRedirects:   s.searchBrokenRedirCheck.Checked,
		namespaceID:       namespaceID,
		namespaceSet:      namespaceSet,
		minSize:           minSize,
		minSet:            minSet,
		minSizeText:       strings.TrimSpace(s.searchMinSizeEntry.Text),
		minSizeErr:        minSizeErr,
		maxSize:           maxSize,
		maxSet:            maxSet,
		maxSizeText:       strings.TrimSpace(s.searchMaxSizeEntry.Text),
		maxSizeErr:        maxSizeErr,
	}
}

// validate reports why a search cannot run, or nil when the criteria are usable.
func (c searchCriteria) validate() error {
	if c.minSizeErr != nil {
		return fmt.Errorf("invalid min size: %w", c.minSizeErr)
	}
	if c.maxSizeErr != nil {
		return fmt.Errorf("invalid max size: %w", c.maxSizeErr)
	}
	if c.prefix == "" && c.category == "" && c.creator == "" && c.linkedFrom == "" && c.template == "" &&
		!c.redirectsOnly && !c.brokenRedirects && !c.namespaceSet &&
		c.minSizeText == "" && c.maxSizeText == "" {
		return errors.New(t.T("del_err_need_criterion", "enter at least one search criterion"))
	}
	if searchNeedsAllPages(c.category, c.creator, c.linkedFrom, c.template, c.brokenRedirects) {
		return errSearchCriteriaTooBroad
	}
	return nil
}

func (s *deleteWorkflowScreen) performSearch() {
	if s.app.client == nil {
		s.app.showMessage(t.T("del_search", "Search"), t.T("del_not_connected", "Not connected to a wiki."))
		return
	}
	criteria := s.collectSearchCriteria()
	if err := criteria.validate(); err != nil {
		if errors.Is(err, errSearchCriteriaTooBroad) {
			s.app.showMessage(t.T("del_search", "Search"), searchCriteriaTooBroadMessage)
			return
		}
		s.app.showMessage(t.T("del_search", "Search"), err.Error())
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	progressBar := widget.NewProgressBarInfinite()
	progressBar.Start()
	status := widget.NewLabel(t.T("del_searching", "Searching pages..."))
	cancelButton := widget.NewButton(t.T("common_cancel", "Cancel"), func() {
		cancel()
	})
	progress := dialog.NewCustomWithoutButtons(
		"Search",
		container.NewVBox(status, progressBar, container.NewHBox(cancelButton)),
		s.app.window,
	)
	progress.SetOnClosed(func() {
		cancel()
	})
	progress.Show()

	go func() {
		results, categoryParents, err := s.searchPages(ctx, criteria)
		fyne.Do(func() {
			progress.Hide()
			progressBar.Stop()
			if err != nil {
				if ctx.Err() != nil || errors.Is(err, context.Canceled) {
					return
				}
				s.app.showError(t.T("del_search", "Search"), err)
				return
			}
			// Apply category parent relations on the UI goroutine: s.categoryParents
			// is read by finalTitles() during list rendering, so it must not be
			// mutated from this background search goroutine.
			s.rememberCategoryParents(categoryParents)
			s.searchResults = results
			s.refreshSelectionPanel()
		})
	}()
}

func (s *deleteWorkflowScreen) searchPages(
	ctx context.Context,
	c searchCriteria,
) ([]searchResult, map[string]map[string]struct{}, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := c.validate(); err != nil {
		return nil, nil, err
	}

	var results []searchResult
	var categoryParents map[string]map[string]struct{}
	resultsSeeded := false

	if c.brokenRedirects {
		brokenSet, fetchErr := s.fetchBrokenRedirects(ctx)
		if fetchErr != nil {
			return nil, nil, fetchErr
		}
		results = resultsFromTitleSet(brokenSet)
		resultsSeeded = true
	}

	if c.category != "" {
		members, categories, parents, fetchErr := s.fetchCategoryMembers(
			ctx,
			c.category,
			c.categoryRecursive,
			c.namespaceID,
			c.namespaceSet,
		)
		if fetchErr != nil {
			return nil, nil, fetchErr
		}
		categoryParents = parents
		if resultsSeeded {
			results = filterBySet(results, members)
		} else {
			results = resultsFromTitleSet(members)
			resultsSeeded = true
		}
		if c.categoryInclude {
			results = append(results, resultsFromTitleSet(categories)...)
		}
	}

	if c.linkedFrom != "" {
		links, fetchErr := s.fetchLinksFrom(ctx, c.linkedFrom, c.namespaceID, c.namespaceSet)
		if fetchErr != nil {
			return nil, nil, fetchErr
		}
		if resultsSeeded {
			results = filterBySet(results, links)
		} else {
			results = resultsFromTitleSet(links)
			resultsSeeded = true
		}
	}

	if c.template != "" {
		embedded, fetchErr := s.fetchEmbeddedIn(ctx, c.template, c.namespaceID, c.namespaceSet)
		if fetchErr != nil {
			return nil, nil, fetchErr
		}
		if resultsSeeded {
			results = filterBySet(results, embedded)
		} else {
			results = resultsFromTitleSet(embedded)
			resultsSeeded = true
		}
	}

	if c.creator != "" {
		created, fetchErr := s.fetchPagesCreatedBy(ctx, c.creator, c.namespaceID, c.namespaceSet)
		if fetchErr != nil {
			return nil, nil, fetchErr
		}
		if resultsSeeded {
			results = filterBySet(results, created)
		} else {
			results = resultsFromTitleSet(created)
			resultsSeeded = true
		}
	}

	if c.prefix != "" && resultsSeeded {
		results = filterByPrefix(results, c.prefix)
	}

	needMetadata := c.minSet || c.maxSet
	if c.redirectsOnly && resultsSeeded {
		needMetadata = true
	}
	if needMetadata {
		if fillErr := s.populateResultMetadata(ctx, results); fillErr != nil {
			return nil, nil, fillErr
		}
	}

	if c.redirectsOnly && resultsSeeded {
		results = filterRedirectsOnly(results)
	}

	if c.minSet || c.maxSet {
		filtered := make([]searchResult, 0, len(results))
		for _, r := range results {
			if c.minSet && r.Size < c.minSize {
				continue
			}
			if c.maxSet && r.Size > c.maxSize {
				continue
			}
			filtered = append(filtered, r)
		}
		results = filtered
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Title < results[j].Title
	})
	return results, categoryParents, nil
}

func (s *deleteWorkflowScreen) populateResultMetadata(ctx context.Context, results []searchResult) error {
	if len(results) == 0 {
		return nil
	}
	titles := make([]string, 0, len(results))
	for _, item := range results {
		if strings.TrimSpace(item.Title) == "" {
			continue
		}
		titles = append(titles, item.Title)
	}
	if len(titles) == 0 {
		return nil
	}

	metadataByTitle, err := s.fetchPageMetadata(ctx, titles)
	if err != nil {
		return err
	}
	for i := range results {
		if metadata, ok := metadataByTitle[results[i].Title]; ok {
			results[i].Size = metadata.Size
			results[i].Redirect = metadata.Redirect
		}
	}
	return nil
}

type pageMetadata struct {
	Size     int64
	Redirect bool
}

func (s *deleteWorkflowScreen) fetchPageMetadata(
	ctx context.Context,
	titles []string,
) (map[string]pageMetadata, error) {
	const chunkSize = 50
	out := map[string]pageMetadata{}
	for start := 0; start < len(titles); start += chunkSize {
		end := min(start+chunkSize, len(titles))
		chunk := titles[start:end]
		params := map[string]string{
			"action":        "query",
			"prop":          "info",
			"titles":        strings.Join(chunk, "|"),
			"formatversion": "2",
		}
		payload, err := s.app.client.GetContext(ctx, s.app.apiURL, params)
		if err != nil {
			return nil, fmt.Errorf("query page sizes: %w", err)
		}
		query, _ := payload["query"].(map[string]any)
		pages, _ := query["pages"].([]any)
		for _, raw := range pages {
			page, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			title, _ := page["title"].(string)
			if strings.TrimSpace(title) == "" {
				continue
			}
			metadata := pageMetadata{}
			switch v := page["length"].(type) {
			case float64:
				metadata.Size = int64(v)
			case int64:
				metadata.Size = v
			case int:
				metadata.Size = int64(v)
			}
			if _, ok := page["redirect"]; ok {
				metadata.Redirect = true
			}
			out[title] = metadata
		}
	}
	return out, nil
}

func (s *deleteWorkflowScreen) fetchCategoryMembers(
	ctx context.Context,
	category string,
	recursive bool,
	namespaceID int,
	namespaceSet bool,
) (map[string]struct{}, map[string]struct{}, map[string]map[string]struct{}, error) {
	category = s.normalizeCategoryTitle(category)
	if category == "" {
		return map[string]struct{}{}, map[string]struct{}{}, map[string]map[string]struct{}{}, nil
	}
	out := map[string]struct{}{}
	categories := map[string]struct{}{
		category: {},
	}
	categoryParents := map[string]map[string]struct{}{}
	toVisit := []string{category}
	visited := map[string]struct{}{}
	for len(toVisit) > 0 {
		current := toVisit[0]
		toVisit = toVisit[1:]
		if _, seen := visited[current]; seen {
			continue
		}
		visited[current] = struct{}{}

		params := map[string]string{
			"action":        "query",
			"list":          "categorymembers",
			"cmtitle":       current,
			"cmlimit":       "max",
			"formatversion": "2",
		}
		if namespaceSet {
			params["cmnamespace"] = strconv.Itoa(namespaceID)
		}
		pageCount := 0
		for {
			payload, err := s.app.client.GetContext(ctx, s.app.apiURL, params)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("query category members: %w", err)
			}
			query, _ := payload["query"].(map[string]any)
			members, _ := query["categorymembers"].([]any)
			pageCount++
			for _, raw := range members {
				entry, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				title, _ := entry["title"].(string)
				if title != "" {
					ns := extractNamespaceID(entry["ns"])
					if ns == 14 {
						categories[title] = struct{}{}
						recordCategoryParent(categoryParents, title, current)
					} else {
						out[title] = struct{}{}
					}
				}
				if !recursive {
					continue
				}
				if extractNamespaceID(entry["ns"]) == 14 && title != "" {
					if _, seen := visited[title]; !seen {
						toVisit = append(toVisit, title)
					}
				}
			}
			continueMap, _ := payload["continue"].(map[string]any)
			next, _ := continueMap["cmcontinue"].(string)
			if next == "" {
				break
			}
			params["cmcontinue"] = next
		}
	}
	return out, categories, categoryParents, nil
}

func (s *deleteWorkflowScreen) fetchLinksFrom(
	ctx context.Context,
	title string,
	namespaceID int,
	namespaceSet bool,
) (map[string]struct{}, error) {
	params := map[string]string{
		"action":        "query",
		"prop":          "links",
		"titles":        strings.TrimSpace(title),
		"pllimit":       "max",
		"formatversion": "2",
	}
	if namespaceSet {
		params["plnamespace"] = strconv.Itoa(namespaceID)
	}
	out := map[string]struct{}{}
	for {
		payload, err := s.app.client.GetContext(ctx, s.app.apiURL, params)
		if err != nil {
			return nil, fmt.Errorf("query links: %w", err)
		}
		query, _ := payload["query"].(map[string]any)
		pages, _ := query["pages"].([]any)
		for _, raw := range pages {
			page, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			links, _ := page["links"].([]any)
			for _, linkRaw := range links {
				entry, ok := linkRaw.(map[string]any)
				if !ok {
					continue
				}
				t, _ := entry["title"].(string)
				if t != "" {
					out[t] = struct{}{}
				}
			}
		}
		continueMap, _ := payload["continue"].(map[string]any)
		next, _ := continueMap["plcontinue"].(string)
		if next == "" {
			break
		}
		params["plcontinue"] = next
	}
	return out, nil
}

func (s *deleteWorkflowScreen) fetchEmbeddedIn(
	ctx context.Context,
	template string,
	namespaceID int,
	namespaceSet bool,
) (map[string]struct{}, error) {
	template = normalizeTitleForNamespace(s.app.currentCaps, template, 10, "Template")
	params := map[string]string{
		"action":        "query",
		"list":          "embeddedin",
		"eititle":       template,
		"eilimit":       "max",
		"formatversion": "2",
	}
	if namespaceSet {
		params["einamespace"] = strconv.Itoa(namespaceID)
	}
	out := map[string]struct{}{}
	for {
		payload, err := s.app.client.GetContext(ctx, s.app.apiURL, params)
		if err != nil {
			return nil, fmt.Errorf("query embeddedin: %w", err)
		}
		query, _ := payload["query"].(map[string]any)
		embedded, _ := query["embeddedin"].([]any)
		for _, raw := range embedded {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			title, _ := entry["title"].(string)
			if title != "" {
				out[title] = struct{}{}
			}
		}
		continueMap, _ := payload["continue"].(map[string]any)
		next, _ := continueMap["eicontinue"].(string)
		if next == "" {
			break
		}
		params["eicontinue"] = next
	}
	return out, nil
}

func (s *deleteWorkflowScreen) fetchPagesCreatedBy(
	ctx context.Context,
	creator string,
	namespaceID int,
	namespaceSet bool,
) (map[string]struct{}, error) {
	params := map[string]string{
		"action":        "query",
		"list":          "usercontribs",
		"ucuser":        strings.TrimSpace(creator),
		"ucshow":        "new",
		"uclimit":       "max",
		"formatversion": "2",
	}
	if namespaceSet {
		params["ucnamespace"] = strconv.Itoa(namespaceID)
	}

	out := map[string]struct{}{}
	for {
		payload, err := s.app.client.GetContext(ctx, s.app.apiURL, params)
		if err != nil {
			return nil, fmt.Errorf("query pages created by %q: %w", creator, err)
		}
		query, _ := payload["query"].(map[string]any)
		contribs, _ := query["usercontribs"].([]any)
		for _, raw := range contribs {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			title, _ := entry["title"].(string)
			if title != "" {
				out[title] = struct{}{}
			}
		}
		continueMap, _ := payload["continue"].(map[string]any)
		next, _ := continueMap["uccontinue"].(string)
		if next == "" {
			break
		}
		params["uccontinue"] = next
	}
	return out, nil
}

func (s *deleteWorkflowScreen) fetchBrokenRedirects(ctx context.Context) (map[string]struct{}, error) {
	params := map[string]string{
		"action":        "query",
		"list":          "querypage",
		"qppage":        "BrokenRedirects",
		"qplimit":       "max",
		"formatversion": "2",
	}
	out := map[string]struct{}{}
	for {
		payload, err := s.app.client.GetContext(ctx, s.app.apiURL, params)
		if err != nil {
			return nil, fmt.Errorf("query broken redirects: %w", err)
		}
		for _, title := range parseQueryPageTitles(payload, "querypage") {
			out[title] = struct{}{}
		}
		continueMap, _ := payload["continue"].(map[string]any)
		next, _ := continueMap["qpoffset"].(string)
		if next == "" {
			break
		}
		params["qpoffset"] = next
	}
	return out, nil
}

func parseQueryPageTitles(payload map[string]any, field string) []string {
	query, _ := payload["query"].(map[string]any)
	rawQueryPage, ok := query[field]
	if !ok {
		return []string{}
	}

	results := []any{}
	if queryPageMap, ok := rawQueryPage.(map[string]any); ok {
		results, _ = queryPageMap["results"].([]any)
	}
	if queryPageList, ok := rawQueryPage.([]any); ok {
		for _, item := range queryPageList {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			list, _ := entry["results"].([]any)
			results = append(results, list...)
		}
	}

	titles := make([]string, 0, len(results))
	for _, raw := range results {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		title, _ := entry["title"].(string)
		if strings.TrimSpace(title) == "" {
			continue
		}
		titles = append(titles, title)
	}
	return titles
}

func resultsFromTitleSet(items map[string]struct{}) []searchResult {
	results := make([]searchResult, 0, len(items))
	for title := range items {
		results = append(results, searchResult{Title: title})
	}
	return results
}

// loadReasons fills the reason list from the reasons cached when the session connected, so reaching the options step
// costs no request and cannot fail here.
func (s *deleteWorkflowScreen) loadReasons() {
	if len(s.reasons) > 0 {
		return
	}
	s.reasons = s.app.reasonsForAction(api.ReasonActionDelete)
}

func (s *deleteWorkflowScreen) reasonSelectOptions() []string {
	none := t.T("del_reason_none", "(none)")
	options := []string{none}
	for _, reason := range s.reasons {
		reason = strings.TrimSpace(reason)
		if reason == "" || reason == none {
			continue
		}
		options = append(options, reason)
	}
	return options
}

func (s *deleteWorkflowScreen) validateOptions() error {
	none := t.T("del_reason_none", "(none)")
	if strings.TrimSpace(s.optionReasonChoice) == none && strings.TrimSpace(s.optionReasonFreeText) == "" {
		return errors.New(t.Td("del_err_reason_text_required",
			"additional reason text is required when {{.None}} is selected", map[string]any{"None": none}))
	}
	return nil
}

// validateMediaWikiNamespaceDeleteAccess returns a user-facing message explaining why the deletion cannot proceed, or
// "" when it is allowed. It returns a message rather than an error: the value is a complete, punctuated sentence shown
// verbatim to the user, not a composable error string.
func (s *deleteWorkflowScreen) validateMediaWikiNamespaceDeleteAccess(titles []string) string {
	if s == nil || s.app == nil || s.optionDryRun {
		return ""
	}
	for _, title := range titles {
		if msg := api.DeleteAccessMessage(s.app.currentCaps, title); msg != "" {
			return msg
		}
	}
	return ""
}

// restoreExecutionButtons sets the navigation buttons back to their idle state
// (usable Back/Home/Proceed, no Cancel). Must be called on the main goroutine.
func (s *deleteWorkflowScreen) restoreExecutionButtons() {
	s.wf.SetButtons(workflowButtonState{
		BackEnabled:    true,
		HomeEnabled:    true,
		CancelEnabled:  false,
		ProceedEnabled: true,
		ProceedLabel:   t.T("common_proceed", "Proceed"),
	})
}

// recordEntry appends entry to this workflow's journal (used for the download files) and to the app-wide session
// journal shown in the welcome-screen footer. Must be called on the main goroutine.
func (s *deleteWorkflowScreen) recordEntry(entry ops.JournalEntry) {
	s.journalEntries = append(s.journalEntries, entry)
	if s.app != nil {
		s.app.recordJournalEntry(entry)
	}
}

func (s *deleteWorkflowScreen) startExecution() {
	if s.executionRunning {
		return
	}
	s.executionDone = false
	s.executionCanceled = false
	s.journalEntries = []ops.JournalEntry{}

	if s.app.client == nil {
		s.app.showMessage(t.T("del_execute", "Execute"), t.T("del_not_connected", "Not connected to a wiki."))
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancelExecution = cancel
	s.executionRunning = true
	s.wf.SetButtons(workflowButtonState{
		BackEnabled:    false,
		HomeEnabled:    false,
		CancelEnabled:  true,
		ProceedEnabled: false,
		ProceedLabel:   t.T("common_proceed", "Proceed"),
	})
	s.executionInfo.SetText(t.T("del_preparing", "Preparing operations..."))

	// The verification read phase already computed the full plan; execution just
	// runs it. Projecting and ordering (category topological order) are local, so
	// they stay on the main goroutine.
	dryRun := s.optionDryRun
	plan := s.previewPlan.ExecutionPlan()
	plan.Operations = s.orderDeletionOperations(plan.Operations, s.categoryParents)

	// defer cancel() releases the context on every exit path (including dry-run
	// and normal completion).
	go func() {
		defer cancel()

		if !dryRun {
			plannedTitles := make([]string, 0, len(plan.Operations))
			for _, op := range plan.Operations {
				plannedTitles = append(plannedTitles, op.Params["title"])
			}
			if msg := s.validateMediaWikiNamespaceDeleteAccess(plannedTitles); msg != "" {
				fyne.Do(func() {
					s.executionRunning = false
					s.cancelExecution = nil
					s.restoreExecutionButtons()
					s.executionInfo.SetText(t.T("del_exec_not_started", "Execution not started."))
					s.app.showMessage(t.T("del_execute", "Execute"), msg)
				})
				return
			}
		}

		fyne.Do(func() {
			s.executionRows = make([]executionRow, 0, len(plan.Operations))
			for _, op := range plan.Operations {
				s.executionRows = append(s.executionRows, executionRow{Title: op.Params["title"], Status: "pending"})
			}
			s.executionList.Refresh()
			s.executionInfo.SetText(
				t.Tp(
					"del_executing",
					"Executing {{.Count}} operation…",
					"Executing {{.Count}} operations…",
					len(plan.Operations),
				),
			)
			s.downloadsBox.Hide()
		})

		if dryRun {
			fyne.Do(func() {
				for i := range s.executionRows {
					s.executionRows[i].Status = "success"
					s.executionRows[i].Detail = "dry-run"
					s.recordEntry(ops.JournalEntry{
						Timestamp: time.Now().UTC(),
						Module:    "deletion",
						Operation: plan.Operations[i],
						Result:    "success",
					})
				}
				s.executionList.Refresh()
				s.executionRunning = false
				s.cancelExecution = nil
				s.executionDone = true
				s.executionInfo.SetText(t.T("del_dryrun_complete", "Dry-run complete."))
				s.downloadsBox.Show()
				s.wf.SetButtons(workflowButtonState{
					BackEnabled:    false,
					HomeEnabled:    false,
					CancelEnabled:  false,
					ProceedEnabled: true,
					ProceedLabel:   t.T("common_done", "Done"),
				})
			})
			return
		}

		executor, err := api.NewHttpExecutor(s.app.client, s.app.apiURL)
		if err != nil {
			fyne.Do(func() {
				s.executionRunning = false
				s.cancelExecution = nil
				s.restoreExecutionButtons()
				s.app.showError(t.T("del_execute", "Execute"), err)
			})
			return
		}
		translator := api.DeleteTranslator{}

		canceled := false
		for i, op := range plan.Operations {
			select {
			case <-ctx.Done():
				canceled = true
			default:
			}
			if canceled {
				break
			}

			if title := strings.TrimSpace(op.Params["title"]); s.isCategoryTitle(title) {
				hasMembers, checkErr := s.categoryHasMembers(ctx, title)
				if checkErr != nil {
					if errors.Is(checkErr, context.Canceled) {
						canceled = true
						break
					}
					entry := ops.JournalEntry{
						Timestamp:   time.Now().UTC(),
						Module:      "deletion",
						Operation:   op,
						Result:      "error",
						ErrorCode:   "category_check",
						ErrorDetail: checkErr.Error(),
					}
					fyne.Do(func() {
						s.executionRows[i].Status = "error"
						s.executionRows[i].Detail = checkErr.Error()
						s.executionList.Refresh()
						s.executionList.ScrollTo(i)
						s.recordEntry(entry)
					})
					continue
				}
				if hasMembers {
					// A category can become non-empty mid-workflow (e.g. a page added
					// after the preview, or a member that failed to delete). Report it
					// and move on — it is not a fatal error.
					detail := t.T("del_detail_category_not_empty", "Category is not empty; not deleted.")
					entry := ops.JournalEntry{
						Timestamp:   time.Now().UTC(),
						Module:      "deletion",
						Operation:   op,
						Result:      "error",
						ErrorCode:   "category_not_empty",
						ErrorDetail: detail,
					}
					fyne.Do(func() {
						s.executionRows[i].Status = "error"
						s.executionRows[i].Detail = detail
						s.executionList.Refresh()
						s.executionList.ScrollTo(i)
						s.recordEntry(entry)
					})
					continue
				}
			}

			calls, trErr := translator.Translate(op, s.app.currentCaps)
			if trErr != nil {
				entry := ops.JournalEntry{
					Timestamp:   time.Now().UTC(),
					Module:      "deletion",
					Operation:   op,
					Result:      "error",
					ErrorCode:   "translate",
					ErrorDetail: trErr.Error(),
				}
				fyne.Do(func() {
					s.executionRows[i].Status = "error"
					s.executionRows[i].Detail = trErr.Error()
					s.executionList.Refresh()
					s.executionList.ScrollTo(i)
					s.recordEntry(entry)
				})
				continue
			}

			results, execErr := executor.Execute(ctx, calls)
			if execErr != nil {
				if errors.Is(execErr, context.Canceled) {
					canceled = true
					break
				}
				entry := ops.JournalEntry{
					Timestamp:   time.Now().UTC(),
					Module:      "deletion",
					Operation:   op,
					Result:      "error",
					ErrorCode:   "execute",
					ErrorDetail: execErr.Error(),
				}
				fyne.Do(func() {
					s.executionRows[i].Status = "error"
					s.executionRows[i].Detail = execErr.Error()
					s.executionList.Refresh()
					s.executionList.ScrollTo(i)
					s.recordEntry(entry)
				})
				continue
			}

			result := "success"
			detail := ""
			errorCode := ""
			if len(results) == 0 || !results[0].Success {
				result = "error"
				detail = t.T("del_detail_unknown_failure", "unknown failure")
				if len(results) > 0 && results[0].Error != nil {
					detail = api.FriendlyErrorMessage(results[0].Error)
					errorCode = results[0].Error.Code
				}
			}

			entry := ops.JournalEntry{
				Timestamp: time.Now().UTC(),
				Module:    "deletion",
				Operation: op,
				Result:    result,
			}
			if result == "error" {
				entry.ErrorCode = errorCode
				entry.ErrorDetail = detail
			}

			fyne.Do(func() {
				s.executionRows[i].Status = result
				s.executionRows[i].Detail = detail
				s.executionList.Refresh()
				s.executionList.ScrollTo(i)
				s.recordEntry(entry)
			})
		}

		fyne.Do(func() {
			s.executionRunning = false
			s.cancelExecution = nil
			if canceled {
				s.executionCanceled = true
				s.executionInfo.SetText(t.T("del_exec_canceled", "Execution canceled."))
				s.restoreExecutionButtons()
				return
			}
			s.executionDone = true
			s.executionInfo.SetText(t.T("del_exec_complete", "Execution complete."))
			s.downloadsBox.Show()
			s.wf.SetButtons(workflowButtonState{
				BackEnabled:    false,
				HomeEnabled:    false,
				CancelEnabled:  false,
				ProceedEnabled: true,
				ProceedLabel:   t.T("common_done", "Done"),
			})
		})
	}()
}

func (s *deleteWorkflowScreen) categoryHasMembers(ctx context.Context, title string) (bool, error) {
	title = strings.TrimSpace(title)
	if !s.isCategoryTitle(title) {
		return false, nil
	}
	payload, err := s.app.client.GetContext(ctx, s.app.apiURL, map[string]string{
		"action":        "query",
		"list":          "categorymembers",
		"cmtitle":       title,
		"cmlimit":       "1",
		"formatversion": "2",
	})
	if err != nil {
		return false, fmt.Errorf("query category members for %q: %w", title, err)
	}
	query, _ := payload["query"].(map[string]any)
	members, _ := query["categorymembers"].([]any)
	return len(members) > 0, nil
}

func (s *deleteWorkflowScreen) combinedReason() string {
	reason := strings.TrimSpace(s.optionReasonChoice)
	if reason == t.T("del_reason_none", "(none)") {
		reason = ""
	}
	extra := strings.TrimSpace(s.optionReasonFreeText)
	if reason == "" {
		return extra
	}
	if extra == "" {
		return reason
	}
	return reason + ": " + extra
}

func (s *deleteWorkflowScreen) saveJournalFile(filename string, entries []ops.JournalEntry) {
	wrapper := struct {
		Wiki    string             `json:"wiki"`
		Actions []ops.JournalEntry `json:"actions"`
	}{
		Wiki:    s.currentCanonicalURL(),
		Actions: entries,
	}

	payload, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		s.app.showError(t.T("del_download", "Download"), err)
		return
	}

	d := dialog.NewFileSave(func(writer fyne.URIWriteCloser, saveErr error) {
		if saveErr != nil {
			s.app.showError(t.T("del_download", "Download"), saveErr)
			return
		}
		if writer == nil {
			return
		}
		defer func() {
			_ = writer.Close()
		}()
		if _, writeErr := writer.Write(payload); writeErr != nil {
			s.app.showError(t.T("del_download", "Download"), writeErr)
			return
		}
	}, s.app.window)
	d.SetFileName(filename)
	d.Show()
}

func (s *deleteWorkflowScreen) currentCanonicalURL() string {
	return s.app.canonicalURL()
}

func (s *deleteWorkflowScreen) filterJournalEntries(result string) []ops.JournalEntry {
	out := make([]ops.JournalEntry, 0)
	for _, entry := range s.journalEntries {
		if entry.Result == result {
			out = append(out, entry)
		}
	}
	return out
}

func (s *deleteWorkflowScreen) labeledField(label string, obj fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVBox(widget.NewLabelWithStyle(label, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), obj)
}

func statusSymbol(status string) string {
	switch status {
	case "success":
		return "[OK]"
	case "error":
		return "[ERR]"
	default:
		return "[...]"
	}
}

func parseSizeFilter(raw string) (int64, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false, err
	}
	if value < 0 {
		value = 0
	}
	return value, true, nil
}

func searchNeedsAllPages(category, creator, linkedFrom, template string, brokenRedirects bool) bool {
	return !brokenRedirects &&
		strings.TrimSpace(category) == "" &&
		strings.TrimSpace(creator) == "" &&
		strings.TrimSpace(linkedFrom) == "" &&
		strings.TrimSpace(template) == ""
}

func filterBySet(results []searchResult, allowed map[string]struct{}) []searchResult {
	filtered := make([]searchResult, 0, len(results))
	for _, item := range results {
		if _, ok := allowed[item.Title]; ok {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func filterByPrefix(results []searchResult, prefix string) []searchResult {
	filtered := make([]searchResult, 0, len(results))
	for _, item := range results {
		if strings.HasPrefix(item.Title, prefix) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func filterRedirectsOnly(results []searchResult) []searchResult {
	filtered := make([]searchResult, 0, len(results))
	for _, item := range results {
		if item.Redirect {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func extractNamespaceID(raw any) int {
	switch v := raw.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}

func (s *deleteWorkflowScreen) isCategoryTitle(title string) bool {
	caps := api.WikiCapabilities{}
	if s != nil && s.app != nil {
		caps = s.app.currentCaps
	}
	return titleHasNamespace(caps, title, 14, "Category")
}

func (s *deleteWorkflowScreen) normalizeCategoryTitle(title string) string {
	caps := api.WikiCapabilities{}
	if s != nil && s.app != nil {
		caps = s.app.currentCaps
	}
	return normalizeTitleForNamespace(caps, title, 14, "Category")
}

func hasNamespacePrefix(title, prefix string) bool {
	title = strings.TrimSpace(title)
	prefix = strings.TrimSpace(prefix)
	if title == "" || prefix == "" {
		return false
	}
	return strings.HasPrefix(strings.ToLower(title), strings.ToLower(prefix)+":")
}

func namespacePrefixes(caps api.WikiCapabilities, namespaceID int, fallback string) []string {
	prefixes := []string{}
	if name := strings.TrimSpace(caps.Namespaces[namespaceID]); name != "" {
		prefixes = append(prefixes, name)
	}
	for _, alias := range caps.NamespaceAliases[namespaceID] {
		if !containsFold(prefixes, alias) {
			prefixes = append(prefixes, alias)
		}
	}
	if strings.TrimSpace(fallback) != "" && !containsFold(prefixes, fallback) {
		prefixes = append(prefixes, fallback)
	}
	return prefixes
}

func preferredNamespacePrefix(caps api.WikiCapabilities, namespaceID int, fallback string) string {
	prefixes := namespacePrefixes(caps, namespaceID, fallback)
	if len(prefixes) == 0 {
		return fallback
	}
	return prefixes[0]
}

func titleHasNamespace(caps api.WikiCapabilities, title string, namespaceID int, fallback string) bool {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return false
	}
	for _, prefix := range namespacePrefixes(caps, namespaceID, fallback) {
		if hasNamespacePrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

func normalizeTitleForNamespace(caps api.WikiCapabilities, title string, namespaceID int, fallback string) string {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return ""
	}
	for _, prefix := range namespacePrefixes(caps, namespaceID, fallback) {
		if hasNamespacePrefix(trimmed, prefix) {
			return trimmed
		}
	}
	return preferredNamespacePrefix(caps, namespaceID, fallback) + ":" + trimmed
}

func splitKnownNamespace(caps api.WikiCapabilities, title string) (int, string, bool) {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return 0, "", false
	}
	parts := strings.SplitN(trimmed, ":", 2)
	if len(parts) != 2 {
		return 0, trimmed, false
	}
	prefix := strings.TrimSpace(parts[0])
	remainder := strings.TrimSpace(parts[1])
	for namespaceID := range caps.Namespaces {
		for _, candidate := range namespacePrefixes(caps, namespaceID, "") {
			if strings.EqualFold(prefix, strings.TrimSpace(candidate)) {
				return namespaceID, remainder, true
			}
		}
	}
	return 0, trimmed, false
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func (s *deleteWorkflowScreen) rememberCategoryParents(relations map[string]map[string]struct{}) {
	if s.categoryParents == nil {
		s.categoryParents = map[string]map[string]struct{}{}
	}
	for child, parents := range relations {
		if _, ok := s.categoryParents[child]; !ok {
			s.categoryParents[child] = map[string]struct{}{}
		}
		for parent := range parents {
			s.categoryParents[child][parent] = struct{}{}
		}
	}
}

func recordCategoryParent(relations map[string]map[string]struct{}, child, parent string) {
	if child == parent {
		return
	}
	if _, ok := relations[child]; !ok {
		relations[child] = map[string]struct{}{}
	}
	relations[child][parent] = struct{}{}
}

func sortCategoriesTopologically(categories []string, parentsByChild map[string]map[string]struct{}) []string {
	if len(categories) == 0 {
		return []string{}
	}

	nodeSet := map[string]struct{}{}
	for _, category := range categories {
		nodeSet[category] = struct{}{}
	}

	indegree := map[string]int{}
	outEdges := map[string][]string{}
	for category := range nodeSet {
		indegree[category] = 0
	}

	for child, parents := range parentsByChild {
		if _, ok := nodeSet[child]; !ok {
			continue
		}
		for parent := range parents {
			if _, ok := nodeSet[parent]; !ok {
				continue
			}
			outEdges[child] = append(outEdges[child], parent)
			indegree[parent]++
		}
	}

	available := make([]string, 0, len(nodeSet))
	for category, degree := range indegree {
		if degree == 0 {
			available = append(available, category)
		}
	}
	sort.Strings(available)

	ordered := make([]string, 0, len(nodeSet))
	seen := map[string]struct{}{}
	for len(available) > 0 {
		current := available[0]
		available = available[1:]
		if _, ok := seen[current]; ok {
			continue
		}
		seen[current] = struct{}{}
		ordered = append(ordered, current)

		parents := append([]string{}, outEdges[current]...)
		sort.Strings(parents)
		for _, parent := range parents {
			indegree[parent]--
			if indegree[parent] == 0 {
				available = append(available, parent)
			}
		}
		sort.Strings(available)
	}

	if len(ordered) < len(nodeSet) {
		remaining := make([]string, 0, len(nodeSet)-len(ordered))
		for category := range nodeSet {
			if _, ok := seen[category]; !ok {
				remaining = append(remaining, category)
			}
		}
		sort.Strings(remaining)
		ordered = append(ordered, remaining...)
	}

	return ordered
}

// orderDeletionOperations sorts operations so category members are deleted
// before their parent categories. parents is the caller-supplied snapshot of the
// child->parents relation (see s.categoryParents); it is passed in rather than
// read from the receiver so this can run off the main goroutine safely.
func (s *deleteWorkflowScreen) orderDeletionOperations(
	operations []ops.Operation,
	parents map[string]map[string]struct{},
) []ops.Operation {
	if len(operations) == 0 {
		return nil
	}

	nonCategory := make([]ops.Operation, 0, len(operations))
	danglingCategoryRedirects := make([]ops.Operation, 0, len(operations))
	categoryByTitle := map[string][]ops.Operation{}
	categoryRedirectsByTarget := map[string][]ops.Operation{}

	for _, op := range operations {
		title := strings.TrimSpace(op.Params["title"])
		if !s.isCategoryTitle(title) {
			nonCategory = append(nonCategory, op)
			continue
		}
		target := strings.TrimSpace(op.Params["redirect_target"])
		if target != "" && s.isCategoryTitle(target) {
			categoryRedirectsByTarget[target] = append(categoryRedirectsByTarget[target], op)
			continue
		}
		if target != "" {
			danglingCategoryRedirects = append(danglingCategoryRedirects, op)
			continue
		}
		categoryByTitle[title] = append(categoryByTitle[title], op)
	}

	if len(categoryByTitle) == 0 && len(categoryRedirectsByTarget) == 0 && len(danglingCategoryRedirects) == 0 {
		return append([]ops.Operation{}, operations...)
	}

	categoryTitleSet := map[string]struct{}{}
	for title := range categoryByTitle {
		categoryTitleSet[title] = struct{}{}
	}
	for target := range categoryRedirectsByTarget {
		categoryTitleSet[target] = struct{}{}
	}
	for target := range categoryRedirectsByTarget {
		sortOperationsByTitle(categoryRedirectsByTarget[target])
	}
	sortOperationsByTitle(danglingCategoryRedirects)
	categoryTitles := make([]string, 0, len(categoryTitleSet))
	for title := range categoryTitleSet {
		categoryTitles = append(categoryTitles, title)
	}
	orderedTitles := sortCategoriesTopologically(categoryTitles, parents)

	ordered := make([]ops.Operation, 0, len(operations))
	ordered = append(ordered, nonCategory...)
	for _, title := range orderedTitles {
		ordered = append(ordered, categoryRedirectsByTarget[title]...)
		ordered = append(ordered, categoryByTitle[title]...)
	}
	ordered = append(ordered, danglingCategoryRedirects...)
	return ordered
}

func sortOperationsByTitle(operations []ops.Operation) {
	sort.Slice(operations, func(i, j int) bool {
		return strings.TrimSpace(operations[i].Params["title"]) < strings.TrimSpace(operations[j].Params["title"])
	})
}

func normalizeManualTitle(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}
	for strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "#") {
		trimmed = strings.TrimSpace(trimmed[1:])
	}
	trimmed = strings.TrimPrefix(trimmed, "[[")
	trimmed = strings.TrimSuffix(trimmed, "]]")
	if parts := strings.SplitN(trimmed, "|", 2); len(parts) > 0 {
		trimmed = parts[0]
	}
	return strings.TrimSpace(trimmed)
}

// deletionDataProvider adapts the wiki API to planner data lookups.
type deletionDataProvider struct {
	client *api.Client
	apiURL string
	caps   api.WikiCapabilities
	ctxFn  func() context.Context
}

func (p *deletionDataProvider) GetRevisions(title string) ([]ops.Revision, error) {
	return nil, errors.New("not implemented")
}

func (p *deletionDataProvider) GetPageInfo(title string) (*ops.PageInfo, error) {
	return nil, errors.New("not implemented")
}

func (p *deletionDataProvider) GetDeletedRevisions(title string) ([]ops.Revision, error) {
	return nil, errors.New("not implemented")
}

func (p *deletionDataProvider) GetTalkPageTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", nil
	}
	caps := p.caps

	namespaceID, remainder, ok := splitKnownNamespace(caps, title)
	if ok && namespaceID%2 == 1 {
		return "", nil
	}

	if !ok {
		return preferredNamespacePrefix(caps, 1, "Talk") + ":" + title, nil
	}
	if namespaceID < 0 {
		return "", nil
	}
	talkNamespaceID := namespaceID + 1
	prefix := preferredNamespacePrefix(caps, talkNamespaceID, "")
	if prefix == "" {
		return "", nil
	}
	return prefix + ":" + remainder, nil
}

// GetSubjectPageTitle returns the subject page of a talk page, or an empty
// string when title is not a talk page. It is the namespace inverse of
// GetTalkPageTitle (talk namespaces are odd; the subject namespace is one less).
func (p *deletionDataProvider) GetSubjectPageTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", nil
	}
	caps := p.caps

	namespaceID, remainder, ok := splitKnownNamespace(caps, title)
	if !ok || namespaceID%2 == 0 {
		return "", nil // main namespace or an even (subject) namespace: not a talk page
	}
	subjectNamespaceID := namespaceID - 1
	if subjectNamespaceID == 0 {
		return remainder, nil // Talk namespace maps back to the prefixless main namespace
	}
	prefix := preferredNamespacePrefix(caps, subjectNamespaceID, "")
	if prefix == "" {
		return "", nil
	}
	return prefix + ":" + remainder, nil
}

// PagesExist reports whether each requested title exists, in batches sized to the session's live multivalue
// cap (see api.Client.ForEachChunk). Titles absent from the returned map are treated as non-existent.
func (p *deletionDataProvider) PagesExist(titles []string) (map[string]bool, error) {
	result := make(map[string]bool, len(titles))
	err := p.client.ForEachChunk("query", titles, func(batch []string) error {
		params := map[string]string{
			"action":        "query",
			"prop":          "info",
			"titles":        strings.Join(batch, "|"),
			"formatversion": "2",
		}
		payload, err := p.client.GetContext(p.context(), p.apiURL, params)
		if err != nil {
			return err
		}
		query, _ := payload["query"].(map[string]any)

		// MediaWiki may normalize requested titles; map results back to what we asked for.
		requestedFor := map[string]string{}
		if normalized, ok := query["normalized"].([]any); ok {
			for _, raw := range normalized {
				entry, _ := raw.(map[string]any)
				from, _ := entry["from"].(string)
				to, _ := entry["to"].(string)
				if from != "" && to != "" {
					requestedFor[to] = from
				}
			}
		}
		pages, _ := query["pages"].([]any)
		for _, raw := range pages {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			pageTitle, _ := entry["title"].(string)
			key := pageTitle
			if original, ok := requestedFor[pageTitle]; ok {
				key = original
			}
			_, missing := entry["missing"]
			result[key] = !missing
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("query page info: %w", err)
	}
	return result, nil
}

// GetRedirects returns, for each requested title, the pages that redirect to it.
//
// It asks prop=redirects, which answers exactly that question, for a batch of titles at a time, which is how the API
// wants to be asked. The obvious-looking alternative — list=backlinks with blfilterredir=redirects — answers a
// different question, "pages that link to title and are themselves redirects", and takes one title per request. A
// redirect pointing somewhere else entirely satisfies it, as long as it happens to link here: deleting "Cat" would have
// taken "Kucing" with it, which redirects to Kuching, a city in Malaysia.
func (p *deletionDataProvider) GetRedirects(titles []string) (map[string][]string, error) {
	result := make(map[string][]string, len(titles))
	err := p.client.ForEachChunk("query", titles, func(batch []string) error {
		params := map[string]string{
			"action":        "query",
			"prop":          "redirects",
			"titles":        strings.Join(batch, "|"),
			"rdlimit":       "max",
			"formatversion": "2",
		}
		for {
			payload, err := p.client.GetContext(p.context(), p.apiURL, params)
			if err != nil {
				return err
			}
			query, _ := payload["query"].(map[string]any)

			// MediaWiki may normalize requested titles; map results back to what we asked for, or the caller will not
			// recognize its own pages.
			requestedFor := map[string]string{}
			if normalized, ok := query["normalized"].([]any); ok {
				for _, raw := range normalized {
					entry, _ := raw.(map[string]any)
					from, _ := entry["from"].(string)
					to, _ := entry["to"].(string)
					if from != "" && to != "" {
						requestedFor[to] = from
					}
				}
			}

			pages, _ := query["pages"].([]any)
			for _, rawPage := range pages {
				page, ok := rawPage.(map[string]any)
				if !ok {
					continue
				}
				pageTitle, _ := page["title"].(string)
				key := pageTitle
				if original, ok := requestedFor[pageTitle]; ok {
					key = original
				}
				items, _ := page["redirects"].([]any)
				for _, raw := range items {
					entry, ok := raw.(map[string]any)
					if !ok {
						continue
					}
					name, _ := entry["title"].(string)
					if strings.TrimSpace(name) != "" {
						result[key] = append(result[key], name)
					}
				}
			}

			continueMap, _ := payload["continue"].(map[string]any)
			next, _ := continueMap["rdcontinue"].(string)
			if next == "" {
				return nil
			}
			params["rdcontinue"] = next
		}
	})
	if err != nil {
		return nil, fmt.Errorf("query redirects: %w", err)
	}
	return result, nil
}

func (p *deletionDataProvider) context() context.Context {
	if p == nil || p.ctxFn == nil {
		return context.Background()
	}
	ctx := p.ctxFn()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (p *deletionDataProvider) GetBlockInfo(user string) (*ops.BlockInfo, error) {
	return nil, errors.New("not implemented")
}

func (p *deletionDataProvider) GetUserInfo() (*ops.UserInfo, error) {
	return nil, errors.New("not implemented")
}

func (p *deletionDataProvider) GetSiteInfo() (*ops.SiteInfo, error) {
	return nil, errors.New("not implemented")
}
