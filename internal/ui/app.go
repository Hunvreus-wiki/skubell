package ui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"

	"github.com/Hunvreus-wiki/skubell/internal/api"
	"github.com/Hunvreus-wiki/skubell/internal/config"
	t "github.com/Hunvreus-wiki/skubell/internal/i18n"
	"github.com/Hunvreus-wiki/skubell/internal/ops"
	"github.com/Hunvreus-wiki/skubell/internal/registry"
	"github.com/Hunvreus-wiki/skubell/internal/security"
	"github.com/Hunvreus-wiki/skubell/locales"
)

const (
	keyringCredentialMarker = "@keyring"
)

// App owns UI state and navigation.
type App struct {
	app        fyne.App
	window     fyne.Window
	configPath string
	config     config.Config

	store security.CredentialStore

	client *api.Client

	startupScreen *StartupScreen

	currentWiki          config.WikiEntry
	currentUser          string
	currentCaps          api.WikiCapabilities
	workflowAvailability map[string]api.WorkflowAvailability

	// sessionJournal accumulates the actions performed since the current connection began; the welcome-screen footer
	// renders its tail, and sessionJournalWriter (when set) persists each entry to disk. Reset when a new session
	// starts and guarded by the mutex because workflows may record entries from a background goroutine.
	sessionJournalMu     sync.Mutex
	sessionJournal       []ops.JournalEntry
	sessionJournalWriter *ops.SessionJournal

	preferencesReturn PreferencesReturnTarget

	apiURL string
}

// recordJournalEntry appends entry to the current session's activity journal and, when persistence is enabled, writes
// it to the on-disk journal. A disk-write failure is logged but never blocks the workflow.
func (a *App) recordJournalEntry(entry ops.JournalEntry) {
	a.sessionJournalMu.Lock()
	a.sessionJournal = append(a.sessionJournal, entry)
	writer := a.sessionJournalWriter
	a.sessionJournalMu.Unlock()

	if writer != nil {
		if err := writer.Append(entry); err != nil {
			logrus.WithError(err).Warn("skubell: failed to persist journal entry")
		}
	}
}

// sessionJournalTail returns a copy of up to the most recent n journal entries, oldest first.
func (a *App) sessionJournalTail(n int) []ops.JournalEntry {
	a.sessionJournalMu.Lock()
	defer a.sessionJournalMu.Unlock()
	if n <= 0 || len(a.sessionJournal) == 0 {
		return nil
	}
	return slices.Clone(a.sessionJournal[max(0, len(a.sessionJournal)-n):])
}

// resetSessionJournal clears the activity journal and stops persistence; called when a session starts or ends.
func (a *App) resetSessionJournal() {
	a.sessionJournalMu.Lock()
	defer a.sessionJournalMu.Unlock()
	a.sessionJournal = nil
	a.sessionJournalWriter = nil
}

// startSessionJournal enables on-disk persistence for the new session. Failures are non-fatal: the in-app session
// journal and footer still work; only disk persistence is skipped.
func (a *App) startSessionJournal() {
	root, err := config.EffectiveJournalDir(a.config.Preferences.JournalDirectory)
	if err != nil {
		logrus.WithError(err).Warn("skubell: cannot resolve journal directory; persistent journal disabled")
		return
	}
	writer := ops.NewSessionJournal(root, ops.WikiIdentity{
		URL:      a.canonicalURL(),
		Farm:     normalizedFarm(a.currentWiki.Farm),
		Family:   a.currentWiki.Family,
		Language: a.currentWiki.Language,
		Name:     a.currentWiki.Name,
		Username: a.currentWiki.Username, // the configured login name, never the credential
	}, nil)

	a.sessionJournalMu.Lock()
	a.sessionJournalWriter = writer
	a.sessionJournalMu.Unlock()
}

// canonicalURL returns the canonical base URL of the currently connected wiki.
func (a *App) canonicalURL() string {
	return registry.WikiEntry{
		Farm:         normalizedFarm(a.currentWiki.Farm),
		Family:       a.currentWiki.Family,
		Language:     a.currentWiki.Language,
		WikiID:       a.currentWiki.WikiID,
		CustomAPIURL: a.currentWiki.APIURL,
	}.CanonicalURL()
}

// normalizedFarm lower-cases a farm identifier, defaulting an empty value to "custom".
func normalizedFarm(farm string) string {
	if f := strings.TrimSpace(strings.ToLower(farm)); f != "" {
		return f
	}
	return "custom"
}

type forcedThemeVariant struct {
	fyne.Theme
	variant fyne.ThemeVariant
}

func (t *forcedThemeVariant) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	return t.Theme.Color(name, t.variant)
}

// NewApp initializes the UI state.
func NewApp(app fyne.App, window fyne.Window) (*App, error) {
	if app == nil {
		return nil, errors.New("fyne app is nil")
	}
	if window == nil {
		return nil, errors.New("fyne window is nil")
	}

	cfgPath, err := config.DefaultPath()
	if err != nil {
		return nil, err
	}
	cfg := config.Load(cfgPath)
	initI18n(cfg, cfgPath)

	store, err := security.NewDesktopCredentialStore()
	if err != nil {
		return nil, err
	}

	return &App{
		app:        app,
		window:     window,
		configPath: cfgPath,
		config:     cfg,
		store:      store,
	}, nil
}

// initI18n loads translations from the shipped (embedded) locales and the per-user locale directory, then activates the
// configured language (falling back to the system locale, then English). Users may drop active.<lang>.json files in a
// "locales" directory next to their config file to add or override translations.
func initI18n(cfg config.Config, configPath string) {
	userLocalesDir := filepath.Join(filepath.Dir(configPath), "locales")
	t.Init(locales.FS, userLocalesDir, resolveLanguage(cfg.Preferences.Language))
}

// resolveLanguage picks the active language: the preference, else the system locale ($LANG), else English.
func resolveLanguage(preference string) string {
	if pref := strings.TrimSpace(preference); pref != "" {
		return pref
	}
	if lang := os.Getenv("LANG"); lang != "" {
		if i := strings.IndexAny(lang, "_.@"); i > 0 {
			return lang[:i] // "fr_FR.UTF-8" -> "fr"
		}
		return lang
	}
	return "en"
}

// Run starts the UI.
func (a *App) Run() {
	a.applyThemeFromConfig()
	a.window.SetCloseIntercept(func() {
		a.logout()
		a.window.Close()
	})
	a.startupScreen = NewStartupScreen(a)
	a.window.SetContent(a.startupScreen.Canvas())
	a.window.ShowAndRun()
}

func (a *App) reloadConfig() {
	a.config = config.Load(a.configPath)
}

func (a *App) saveConfig(cfg config.Config) error {
	if err := config.Save(a.configPath, cfg); err != nil {
		return err
	}
	a.config = cfg
	return nil
}

func (a *App) findWiki(name string) (config.WikiEntry, bool) {
	for _, entry := range a.config.Wikis {
		if entry.Name == name {
			return entry, true
		}
	}
	return config.WikiEntry{}, false
}

func (a *App) startup() {
	a.reloadConfig()
	if a.startupScreen == nil {
		a.startupScreen = NewStartupScreen(a)
	}
	a.startupScreen.RefreshList()
	a.window.SetContent(a.startupScreen.Canvas())
}

func (a *App) showError(title string, err error) {
	if err == nil {
		return
	}
	dialog.ShowError(fmt.Errorf("%s: %w", title, err), a.window)
}

func (a *App) showMessage(title, message string) {
	dialog.ShowInformation(title, message, a.window)
}

func (a *App) openWelcome() {
	screen := NewWelcomeScreen(a)
	a.window.SetContent(screen.Canvas())
}

func (a *App) openDeletionWorkflow() {
	screen := NewDeletionWorkflowScreen(a)
	a.window.SetContent(screen.Canvas())
}

func (a *App) openPreferences() {
	screen := NewPreferencesScreen(a, a.preferencesReturn)
	a.window.SetContent(screen.Canvas())
}

func (a *App) disconnect() {
	a.logout()
	a.currentWiki = config.WikiEntry{}
	a.currentUser = ""
	a.currentCaps = api.WikiCapabilities{}
	a.workflowAvailability = nil
	a.resetSessionJournal()
}

func (a *App) returnToStartup() {
	a.disconnect()
	a.startup()
}

func (a *App) connectAndOpenWelcome(wiki config.WikiEntry) {
	client, err := api.NewClient(wiki.WriteThrottleMS, wiki.MaxRetries, nil)
	if err != nil {
		a.showError("Connect", err)
		return
	}
	a.client = client

	credential := wiki.Credential
	if credential == keyringCredentialMarker {
		value, err := a.store.Retrieve(wiki.Name)
		if err != nil {
			a.showError("Connect", fmt.Errorf("retrieve credential: %w", err))
			return
		}
		credential = string(value)
	}

	working := wiki
	working.Credential = credential

	ctx, cancel := context.WithCancel(context.Background())
	progressBar := widget.NewProgressBarInfinite()
	progressBar.Start()
	cancelButton := widget.NewButton("Cancel", func() {
		cancel()
	})
	progress := dialog.NewCustomWithoutButtons(
		"Connecting",
		container.NewVBox(widget.NewLabel("Contacting the wiki..."), progressBar, container.NewHBox(cancelButton)),
		a.window,
	)
	progress.SetOnClosed(func() {
		cancel()
	})
	progress.Show()

	go func() {
		result, connectErr := api.ConnectContext(ctx, client, working)
		fyne.Do(func() {
			// Capture the context's cancellation state BEFORE Hide(): progress.Hide() fires the dialog's OnClosed
			// callback, which calls cancel() and would otherwise make ctx.Err() non-nil and misclassify a genuine
			// failure as a user cancellation, silently swallowing the error.
			ctxErr := ctx.Err()
			progress.Hide()
			progressBar.Stop()
			switch classifyConnectResult(connectErr, ctxErr) {
			case connectCanceled:
				return
			case connectFailed:
				a.showError("Connect", connectErr)
				return
			}
			a.currentWiki = wiki
			a.currentUser = result.Username
			a.currentCaps = result.Capabilities
			a.workflowAvailability = api.EvaluateWorkflowAvailability(result.Capabilities.UserRights)
			a.apiURL = working.APIURL
			a.resetSessionJournal()
			a.startSessionJournal()
			a.openWelcome()
		})
	}()
}

// connectOutcome classifies what the UI should do with a connection attempt's result.
type connectOutcome int

const (
	// connectProceed means the connection succeeded; continue to the welcome screen.
	connectProceed connectOutcome = iota
	// connectCanceled means the user aborted; stay silent (no error dialog).
	connectCanceled
	// connectFailed means a genuine failure; surface the error to the user.
	connectFailed
)

// classifyConnectResult decides how to react to a connection attempt, keeping the "never fail silently" rule in one
// pure, testable place. ctxErr must be captured BEFORE hiding the progress dialog (hiding it cancels the context, so a
// later read would spuriously report cancellation): a non-nil connectErr is a cancellation only when ctxErr is set or
// the error wraps context.Canceled; every other non-nil error is a failure that must be shown.
func classifyConnectResult(connectErr, ctxErr error) connectOutcome {
	if connectErr == nil {
		return connectProceed
	}
	if ctxErr != nil || errors.Is(connectErr, context.Canceled) {
		return connectCanceled
	}
	return connectFailed
}

func (a *App) logout() {
	if a.client == nil || strings.TrimSpace(a.apiURL) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := api.LogoutContext(ctx, a.client, a.apiURL); err != nil {
		a.showError("Logout", err)
	}
	a.client = nil
	a.apiURL = ""
}

func (a *App) applyThemeFromConfig() {
	switch strings.ToLower(strings.TrimSpace(a.config.Preferences.Theme)) {
	case "light":
		a.app.Settings().SetTheme(&forcedThemeVariant{
			Theme:   theme.DefaultTheme(),
			variant: theme.VariantLight,
		})
	case "dark":
		a.app.Settings().SetTheme(&forcedThemeVariant{
			Theme:   theme.DefaultTheme(),
			variant: theme.VariantDark,
		})
	default:
		a.app.Settings().SetTheme(nil)
	}
}
