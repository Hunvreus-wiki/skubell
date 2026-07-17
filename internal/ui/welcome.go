package ui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/Hunvreus-wiki/skubell/internal/api"
	t "github.com/Hunvreus-wiki/skubell/internal/i18n"
	"github.com/Hunvreus-wiki/skubell/internal/ops"
)

// WelcomeScreen renders the post-connection landing page.
type WelcomeScreen struct {
	app  *App
	root fyne.CanvasObject
}

// NewWelcomeScreen builds the welcome screen.
func NewWelcomeScreen(app *App) *WelcomeScreen {
	screen := &WelcomeScreen{app: app}
	screen.root = screen.build()
	return screen
}

// Canvas returns the root canvas object.
func (w *WelcomeScreen) Canvas() fyne.CanvasObject {
	return w.root
}

func (w *WelcomeScreen) build() fyne.CanvasObject {
	picker := NewWikiPicker(w.app, func(wikiName string) {
		if wiki, ok := w.app.findWiki(wikiName); ok {
			w.app.connectAndOpenWelcome(wiki)
		}
	})

	home := widget.NewButtonWithIcon(t.T("common_home", "Home"), theme.HomeIcon(), func() {
		w.app.returnToStartup()
	})
	preferences := widget.NewButtonWithIcon(t.T("common_preferences", "Preferences"), theme.SettingsIcon(), func() {
		w.app.preferencesReturn = ReturnToWelcome
		w.app.openPreferences()
	})
	topBar := container.NewHBox(picker.Canvas(), layout.NewSpacer(), home, preferences)

	// Only implemented workflows are shown; the rest are hidden rather than displayed as dead buttons. As each remaining
	// workflow is built (Restoration, Blocking, HistoryMerge, UserRights, AugeasCore — see api.Workflow* constants), add
	// its button here.
	buttons := []fyne.CanvasObject{
		w.workflowButton(t.T("workflow_delete_pages", "Delete pages"), api.WorkflowDeletion, func() {
			w.app.openDeletionWorkflow()
		}),
		w.workflowButton(t.T("workflow_protection", "Change page protection"), api.WorkflowProtection, func() {
			w.app.openProtectionWorkflow()
		}),
		w.workflowButton(t.T("workflow_revdel", "Change the visibility of versions"), api.WorkflowRevisionDelete,
			func() {
				w.app.openRevDelWorkflow()
			}),
	}
	grid := container.NewGridWithColumns(2, buttons...)

	content := container.NewVBox(
		widget.NewLabelWithStyle(t.T("welcome_actions", "Actions"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		grid,
	)

	return container.NewBorder(topBar, w.buildFooter(), nil, nil, content)
}

// sessionJournalFooterCount is how many recent session actions the footer shows.
const sessionJournalFooterCount = 8

// sessionLogMinHeight caps the session-log area so a full log scrolls instead of forcing the window taller than a
// small screen can show.
const sessionLogMinHeight = 104

// buildFooter renders the welcome-screen footer: the current session's recent actions plus basic wiki statistics.
func (w *WelcomeScreen) buildFooter() fyne.CanvasObject {
	logBox := container.NewVBox()
	entries := w.app.sessionJournalTail(sessionJournalFooterCount)
	if len(entries) == 0 {
		logBox.Add(widget.NewLabel(t.T("welcome_no_actions", "No actions performed yet this session.")))
	} else {
		for _, entry := range entries {
			logBox.Add(widget.NewLabel(formatJournalLine(entry)))
		}
	}
	logScroll := container.NewVScroll(logBox)
	logScroll.SetMinSize(fyne.NewSize(0, sessionLogMinHeight))

	footer := container.NewVBox(
		widget.NewSeparator(),
		widget.NewLabelWithStyle(
			t.T("welcome_session_activity", "Session activity"),
			fyne.TextAlignLeading,
			fyne.TextStyle{Bold: true},
		),
		logScroll,
	)

	if stats := w.statsSummary(); stats != "" {
		footer.Add(widget.NewSeparator())
		footer.Add(widget.NewLabel(stats))
	}
	return footer
}

// statsSummary renders the wiki's basic statistics for the footer, or "" when none are available.
func (w *WelcomeScreen) statsSummary() string {
	caps := w.app.currentCaps
	parts := []string{}
	if caps.PageCount > 0 {
		parts = append(parts, t.Td("welcome_stat_pages", "Pages: {{.Count}}", map[string]any{"Count": caps.PageCount}))
	}
	if caps.ActiveUsers > 0 {
		parts = append(parts, t.Td("welcome_stat_active_users", "Active users: {{.Count}}",
			map[string]any{"Count": caps.ActiveUsers}))
	}
	return strings.Join(parts, "  ·  ")
}

// formatJournalLine renders one session-journal entry for the footer: local time, result glyph, and action summary.
func formatJournalLine(entry ops.JournalEntry) string {
	return fmt.Sprintf(
		"%s  %s  %s",
		entry.Timestamp.Local().Format("15:04:05"),
		journalResultGlyph(entry.Result),
		journalActionSummary(entry),
	)
}

// journalActionSummary describes an entry's operation in the active language. Known operation types are formatted from
// their structured params, so the line follows the current language and stays correct for already-persisted entries;
// anything else falls back to the operation's stored description, then the module name.
func journalActionSummary(entry ops.JournalEntry) string {
	op := entry.Operation
	if op.Type == ops.OpDeletePage {
		if title := strings.TrimSpace(op.Params["title"]); title != "" {
			if op.Params["delete_talk"] == "true" {
				return t.Td("journal_delete_page_talk", `Delete page "{{.Title}}" and its talk page`,
					map[string]any{"Title": title})
			}
			return t.Td("journal_delete_page", `Delete page "{{.Title}}"`, map[string]any{"Title": title})
		}
	}
	if op.Type == ops.OpRevisionDelete || op.Type == ops.OpSuppress {
		if ids := strings.TrimSpace(op.Params["ids"]); ids != "" {
			count := len(strings.Split(ids, "|"))
			// OpSuppress appears only in journals persisted before suppression became a level on OpRevisionDelete.
			if op.Type == ops.OpSuppress || op.Params["suppress"] == "yes" {
				return t.Tp("journal_suppress_revisions",
					"Suppress {{.Count}} revision", "Suppress {{.Count}} revisions", count)
			}
			return t.Tp("journal_revdel_revisions",
				"Change the visibility of {{.Count}} revision", "Change the visibility of {{.Count}} revisions", count)
		}
	}
	if d := strings.TrimSpace(op.Description); d != "" {
		return d
	}
	if m := strings.TrimSpace(entry.Module); m != "" {
		return m
	}
	return t.T("journal_action_fallback", "(action)")
}

// journalResultGlyph maps a JournalEntry result to a compact status glyph.
func journalResultGlyph(result string) string {
	switch result {
	case "success":
		return "✓"
	case "skipped":
		return "⊘"
	default:
		return "✗"
	}
}

func (w *WelcomeScreen) workflowButton(label, workflow string, onTap func()) fyne.CanvasObject {
	button := widget.NewButton(label, onTap)
	available := true
	missing := []string{}
	if w.app.workflowAvailability != nil {
		if status, ok := w.app.workflowAvailability[workflow]; ok {
			available = status.Available
			missing = status.MissingRights
		}
	}
	if w.app.currentCaps.SitewideBlock {
		available = false
		missing = []string{t.T("workflow_missing_sitewide_block", "sitewide block")}
	}
	if !available {
		button.Disable()
		if len(missing) > 0 {
			button.SetText(t.Td("workflow_requires_rights", "{{.Label}} (requires {{.Rights}})",
				map[string]any{"Label": label, "Rights": joinRights(missing)}))
		}
	}
	return bordered(button)
}

func joinRights(rights []string) string {
	if len(rights) == 0 {
		return ""
	}
	if len(rights) == 1 {
		return rights[0]
	}
	return strings.Join(rights, ", ")
}
