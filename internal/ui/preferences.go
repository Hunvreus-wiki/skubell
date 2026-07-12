package ui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	t "github.com/Hunvreus-wiki/skubell/internal/i18n"
)

// PreferencesScreen renders the preferences window.
type PreferencesScreen struct {
	app  *App
	root fyne.CanvasObject

	themeSelect  *widget.Select
	langSelect   *widget.Select
	returnTarget PreferencesReturnTarget
}

// PreferencesReturnTarget indicates where to return after closing preferences.
type PreferencesReturnTarget int

const (
	ReturnToWelcome PreferencesReturnTarget = iota
	ReturnToStartup
)

// NewPreferencesScreen creates the preferences screen.
func NewPreferencesScreen(app *App, target PreferencesReturnTarget) *PreferencesScreen {
	screen := &PreferencesScreen{app: app, returnTarget: target}
	screen.root = screen.build()
	return screen
}

// Canvas returns the root canvas object.
func (p *PreferencesScreen) Canvas() fyne.CanvasObject {
	return p.root
}

func (p *PreferencesScreen) build() fyne.CanvasObject {
	title := widget.NewLabelWithStyle(
		t.T("prefs_title", "Preferences"),
		fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true},
	)

	p.themeSelect = widget.NewSelect([]string{"system", "light", "dark"}, nil)
	p.themeSelect.SetSelected(p.app.config.Preferences.Theme)
	if p.themeSelect.Selected == "" {
		p.themeSelect.SetSelected("system")
	}

	languages := t.AvailableLanguages()
	p.langSelect = widget.NewSelect(languages, nil)
	if p.app.config.Preferences.Language != "" {
		p.langSelect.SetSelected(p.app.config.Preferences.Language)
	} else if len(languages) > 0 {
		p.langSelect.SetSelected(languages[0])
	}

	form := container.NewVBox(
		labeledField(t.T("prefs_theme", "Theme"), p.themeSelect),
		labeledField(t.T("prefs_language", "Language"), p.langSelect),
	)

	save := widget.NewButton(t.T("common_save", "Save"), func() {
		p.handleSave()
	})
	cancel := widget.NewButton(t.T("common_cancel", "Cancel"), func() {
		p.returnFromPreferences()
	})

	footer := container.NewHBox(layout.NewSpacer(), cancel, save)
	return container.NewBorder(container.NewVBox(title, widget.NewSeparator()), footer, nil, nil, form)
}

func (p *PreferencesScreen) handleSave() {
	themeValue := strings.TrimSpace(p.themeSelect.Selected)
	langValue := strings.TrimSpace(p.langSelect.Selected)

	cfg := p.app.config
	cfg.Preferences.Theme = themeValue
	cfg.Preferences.Language = langValue

	if err := p.app.saveConfig(cfg); err != nil {
		p.app.showError(t.T("prefs_title", "Preferences"), err)
		return
	}
	p.app.applyThemeFromConfig()
	// Apply the language immediately: returnFromPreferences rebuilds the destination screen, which then reads the
	// newly-active translations.
	t.SetLanguage(langValue)
	p.returnFromPreferences()
}

func labeledField(label string, obj fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVBox(widget.NewLabelWithStyle(label, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), obj)
}

func (p *PreferencesScreen) returnFromPreferences() {
	switch p.returnTarget {
	case ReturnToStartup:
		p.app.startup()
	default:
		p.app.openWelcome()
	}
}
