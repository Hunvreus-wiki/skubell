package ui

import (
	"context"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/deletion"
)

func TestProgressLabels(t *testing.T) {
	// Not parallel: go-i18n's Localizer (used by t.Td) is not safe for concurrent use.
	redirects := redirectStepLabel(3, 60, 71)
	require.Contains(t, redirects, "3")
	require.Contains(t, redirects, "60")
	require.Contains(t, redirects, "71")
	require.NotContains(t, redirects, "%") // each row's bar carries its own fraction

	require.Contains(t, talkStepLabel(50, 60), "50")
	require.Contains(t, categoryCheckLabel(1, 2), "1")
}

// TestVerificationStepShowsOnlyRealWork pins the point of the three rows: a step with nothing to do stays out of the
// way, so a slow run shows which step is slow instead of hiding it behind one number.
func TestVerificationStepShowsOnlyRealWork(t *testing.T) {
	// Not parallel: building the row measures its label, and Fyne's text shaping is not safe for concurrent use.
	step := newVerificationStep()
	require.False(t, step.row.Visible()) // nothing has happened yet

	step.set(3, 60, "Finding redirects: 3/60")
	require.True(t, step.row.Visible())
	require.InDelta(t, 60.0, step.bar.Max, 0.001)
	require.InDelta(t, 3.0, step.bar.Value, 0.001)

	// found can outrun the reference list, but a bar must not overshoot its own total.
	step.set(99, 60, "x")
	require.InDelta(t, 60.0, step.bar.Value, 0.001)

	// A step can finish inside one throttle window; completing it fills the bar rather than leaving it mid-way.
	step.set(1, 60, "x")
	step.complete()
	require.InDelta(t, 60.0, step.bar.Value, 0.001)

	// No work: no row. This is the redirects-off and no-categories case.
	step.set(0, 0, "x")
	require.False(t, step.row.Visible())
}

// TestVerificationProgressFlushesTheHeldUpdate pins the fix for the frozen label: the final callback of a pass lands
// inside the throttle window more often than not, and dropping it left "84/89 pages processed" on a finished search.
// The throttle must hold the newest dropped update per step and replay it at completion — and only the newest, and
// only for the step being flushed.
func TestVerificationProgressFlushesTheHeldUpdate(t *testing.T) {
	t.Parallel()

	var applied []string
	progress := newVerificationProgress()
	progress.do = func(update func()) { update() } // main-goroutine dispatch, inline for the test
	record := func(text string) func() {
		return func() { applied = append(applied, text) }
	}

	// The first update opens the throttle window; everything after it inside the window is held, newest per step.
	progress.push("redirects", record("redirects 3/89"))
	progress.push("redirects", record("redirects 84/89"))
	progress.push("redirects", record("redirects 89/89"))
	progress.push("talk", record("talk 2/2"))
	require.Equal(t, []string{"redirects 3/89"}, applied)

	progress.flush("redirects")
	require.Equal(t, []string{"redirects 3/89", "redirects 89/89"}, applied, "the final counts, not the last survivor")

	progress.flush("redirects")
	require.Len(t, applied, 2, "a held update is applied once")

	progress.flush("talk")
	require.Equal(t, "talk 2/2", applied[len(applied)-1], "each step holds its own update")

	// An update that got through leaves nothing behind to replay.
	progress.lastPush = progress.lastPush.Add(-2 * progressThrottle)
	progress.push("redirects", record("redirects done"))
	progress.flush("redirects")
	require.Equal(t, "redirects done", applied[len(applied)-1])
	require.Len(t, applied, 4)
}

// TestBuildPreviewRowsCountsCategoriesNotPages pins what the category row reports. Only a category costs a request
// there, so a batch of ordinary pages must not announce a pass over all of them — nor show a row at all.
func TestBuildPreviewRowsCountsCategoriesNotPages(t *testing.T) {
	t.Parallel()

	screen := &deleteWorkflowScreen{app: &App{}}
	plan := deletion.Plan{Items: []deletion.PlanItem{
		{Title: "Apple"}, {Title: "Banana"}, {Title: "Cherry"}, {Title: "Date"},
	}}

	var ticks [][2]int
	rows, err := screen.buildPreviewRows(context.Background(), plan, func(done, total int) {
		ticks = append(ticks, [2]int{done, total})
	})
	require.NoError(t, err)
	require.Len(t, rows, 4) // every page still gets a row
	require.Empty(t, ticks) // but none of them is work, so the row never claims otherwise
}

// TestVerificationStepRowIsActuallyLaidOut guards the difference between a row being visible and a row being seen.
// Fyne sizes a container from its visible children, and showing a child refreshes only the child — so the first cut of
// this shipped rows that reported Visible() at zero height, which is to say no progress bar at all.
func TestVerificationStepRowIsActuallyLaidOut(t *testing.T) {
	// Not parallel: builds widgets, and Fyne's text shaping is not safe for concurrent use.
	fynetest.NewTempApp(t)

	step := newVerificationStep()
	steps := container.NewVBox(step.row)
	header := container.NewVBox(widget.NewLabel("Computing pages to delete…"), steps)
	step.header = header
	window := fynetest.NewTempWindow(t, container.NewBorder(header, nil, nil, nil, widget.NewLabel("list")))
	window.Resize(fyne.NewSize(700, 300))

	require.Zero(t, steps.Size().Height) // nothing to report yet

	step.set(3, 60, "Checking talk pages: 3/60")
	require.Positive(t, step.row.Size().Height, "a shown row must have height, not merely Visible() == true")
	require.Positive(t, steps.Size().Height, "the container must make room for it")

	step.hide()
	require.Zero(t, steps.Size().Height, "and give the room back")
}

// TestPendingStepsFollowTheChosenOptions pins which rows appear and when. They are put up before any of them has a
// count, so the reader sees the whole plan at once rather than watching rows arrive; which ones is settled by the
// options already chosen, and by whether there is a category to check at all.
func TestPendingStepsFollowTheChosenOptions(t *testing.T) {
	// Not parallel: builds widgets, and Fyne's text shaping is not safe for concurrent use.
	fynetest.NewTempApp(t)

	newScreen := func(talk, redirects bool) *deleteWorkflowScreen {
		return &deleteWorkflowScreen{
			app:            &App{},
			stepRedirects:  newVerificationStep(),
			stepTalkPages:  newVerificationStep(),
			stepCategories: newVerificationStep(),

			optionIncludeTalk:  talk,
			optionIncludeRedir: redirects,
		}
	}

	// Talk pages only, no category in the selection: one row, up before it has anything to say.
	screen := newScreen(true, false)
	screen.showPendingVerificationSteps([]string{"Apple", "Banana"})
	require.False(t, screen.stepRedirects.row.Visible(), "redirects were not asked for")
	require.True(t, screen.stepTalkPages.row.Visible())
	require.False(t, screen.stepCategories.row.Visible(), "nothing in the selection to check")
	require.Equal(t, "Checking talk pages", screen.stepTalkPages.label.Text) // named, not yet counted

	// Everything on, with a category selected: all three, from the start.
	screen = newScreen(true, true)
	screen.showPendingVerificationSteps([]string{"Apple", "Category:Fruit"})
	require.True(t, screen.stepRedirects.row.Visible())
	require.True(t, screen.stepTalkPages.row.Visible())
	require.True(t, screen.stepCategories.row.Visible())

	// Neither option: nothing to announce.
	screen = newScreen(false, false)
	screen.showPendingVerificationSteps([]string{"Apple"})
	require.False(t, screen.stepRedirects.row.Visible())
	require.False(t, screen.stepTalkPages.row.Visible())
}

// TestCompletedStepsStayUp pins that the rows survive the answer. A short list finishes each pass faster than it can be
// watched, so clearing the rows when the final list appeared turned the whole search into a flicker — the rows are the
// record that the search the user asked for actually ran, and a filled bar says it succeeded.
func TestCompletedStepsStayUp(t *testing.T) {
	// Not parallel: builds widgets, and Fyne's text shaping is not safe for concurrent use.
	fynetest.NewTempApp(t)

	screen := &deleteWorkflowScreen{
		app:            &App{},
		stepRedirects:  newVerificationStep(),
		stepTalkPages:  newVerificationStep(),
		stepCategories: newVerificationStep(),

		optionIncludeTalk: true,
	}
	// Rendered for real: the rows have to be laid out, not merely flagged visible, for the assertion below to mean
	// anything.
	steps := container.NewVBox(screen.stepRedirects.row, screen.stepTalkPages.row, screen.stepCategories.row)
	header := container.NewVBox(widget.NewLabel("Computing pages to delete…"), steps)
	for _, step := range []*verificationStep{screen.stepRedirects, screen.stepTalkPages, screen.stepCategories} {
		step.header = header
	}
	window := fynetest.NewTempWindow(t, container.NewBorder(header, nil, nil, nil, widget.NewLabel("list")))
	window.Resize(fyne.NewSize(700, 300))

	screen.showPendingVerificationSteps([]string{"Apple", "Banana"})
	screen.stepTalkPages.set(2, 2, talkStepLabel(2, 2))

	screen.completeVerificationSteps()

	require.True(t, screen.stepTalkPages.row.Visible(), "the row must outlive the search it reports")
	require.Positive(t, screen.stepTalkPages.row.Size().Height, "and still be laid out, not merely visible")
	require.InDelta(t, screen.stepTalkPages.bar.Max, screen.stepTalkPages.bar.Value, 0.001, "showing it finished")

	// A pass with nothing to do is a different matter: it never ran, and says so by not being there.
	require.False(t, screen.stepRedirects.row.Visible())
}
