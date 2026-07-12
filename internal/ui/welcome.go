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
	// workflow is built (Restoration, Blocking, HistoryMerge, Protection, RevisionDelete, UserRights, AugeasCore — see
	// api.Workflow* constants), add its button here.
	buttons := []fyne.CanvasObject{
		w.workflowButton(t.T("workflow_delete_pages", "Delete pages"), api.WorkflowDeletion, func() {
			w.app.openDeletionWorkflow()
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

// buildFooter renders the welcome-screen footer: the current session's recent actions plus basic wiki statistics.
func (w *WelcomeScreen) buildFooter() fyne.CanvasObject {
	footer := container.NewVBox(
		widget.NewSeparator(),
		widget.NewLabelWithStyle(
			t.T("welcome_session_activity", "Session activity"),
			fyne.TextAlignLeading,
			fyne.TextStyle{Bold: true},
		),
	)

	entries := w.app.sessionJournalTail(sessionJournalFooterCount)
	if len(entries) == 0 {
		footer.Add(widget.NewLabel(t.T("welcome_no_actions", "No actions performed yet this session.")))
	} else {
		for _, entry := range entries {
			footer.Add(widget.NewLabel(formatJournalLine(entry)))
		}
	}

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
	summary := strings.TrimSpace(entry.Operation.Description)
	if summary == "" {
		summary = strings.TrimSpace(entry.Module)
	}
	if summary == "" {
		summary = "(action)"
	}
	return fmt.Sprintf(
		"%s  %s  %s",
		entry.Timestamp.Local().Format("15:04:05"),
		journalResultGlyph(entry.Result),
		summary,
	)
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
		missing = []string{"sitewide block"}
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
