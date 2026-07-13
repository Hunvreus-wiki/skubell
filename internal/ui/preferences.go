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

	titleLabel      *widget.Label
	themeFieldLabel *widget.Label
	langFieldLabel  *widget.Label
	themeSelect     *widget.Select
	langSelect      *widget.Select
	saveButton      *widget.Button
	cancelButton    *widget.Button
	entryLang       string // active language when the screen opened; restored if the user cancels the live preview
	returnTarget    PreferencesReturnTarget
}

// PreferencesReturnTarget indicates where to return after closing preferences.
type PreferencesReturnTarget int

const (
	ReturnToWelcome PreferencesReturnTarget = iota
	ReturnToStartup
)

// themeChoices lists the theme options in display order, pairing each stored config value with the message key and
// English default for its localized menu label. The value ("system"/"light"/"dark") is what persists to config.
var themeChoices = []struct {
	value, key, english string
}{
	{"system", "prefs_theme_system", "System"},
	{"light", "prefs_theme_light", "Light"},
	{"dark", "prefs_theme_dark", "Dark"},
}

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
	p.entryLang = t.CurrentLanguage()

	p.titleLabel = widget.NewLabelWithStyle(
		t.T("prefs_title", "Preferences"),
		fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true},
	)

	p.themeSelect = widget.NewSelect(themeLabels(), nil)
	p.themeSelect.SetSelected(themeLabelFor(p.currentThemeValue()))

	// Set the initial selection before wiring OnChanged, so building the screen doesn't re-trigger a language switch.
	p.langSelect = widget.NewSelect(t.AvailableLanguages(), nil)
	p.langSelect.SetSelected(p.currentLanguageValue())
	p.langSelect.OnChanged = func(lang string) { p.applyLanguage(lang) }

	p.themeFieldLabel = fieldLabel(t.T("prefs_theme", "Theme"))
	p.langFieldLabel = fieldLabel(t.T("prefs_language", "Language"))
	form := container.NewVBox(
		container.NewVBox(p.themeFieldLabel, p.themeSelect),
		container.NewVBox(p.langFieldLabel, p.langSelect),
	)

	p.saveButton = widget.NewButton(t.T("common_save", "Save"), p.handleSave)
	p.cancelButton = widget.NewButton(t.T("common_cancel", "Cancel"), p.handleCancel)

	footer := container.NewHBox(layout.NewSpacer(), p.cancelButton, p.saveButton)
	return container.NewBorder(container.NewVBox(p.titleLabel, widget.NewSeparator()), footer, nil, nil, form)
}

// applyLanguage activates lang and re-labels the visible screen in place, preserving the in-progress theme selection
// so an unsaved choice survives the switch. It runs live as the language dropdown changes, before anything is saved.
func (p *PreferencesScreen) applyLanguage(lang string) {
	themeValue := themeValueFor(p.themeSelect.Selected) // resolve under the still-active previous language
	t.SetLanguage(lang)

	p.titleLabel.SetText(t.T("prefs_title", "Preferences"))
	p.themeFieldLabel.SetText(t.T("prefs_theme", "Theme"))
	p.langFieldLabel.SetText(t.T("prefs_language", "Language"))
	p.saveButton.SetText(t.T("common_save", "Save"))
	p.cancelButton.SetText(t.T("common_cancel", "Cancel"))

	p.themeSelect.Options = themeLabels()
	p.themeSelect.SetSelected(themeLabelFor(themeValue))
	p.themeSelect.Refresh()
}

func (p *PreferencesScreen) handleSave() {
	cfg := p.app.config
	cfg.Preferences.Theme = themeValueFor(p.themeSelect.Selected)
	cfg.Preferences.Language = strings.TrimSpace(p.langSelect.Selected)

	if err := p.app.saveConfig(cfg); err != nil {
		p.app.showError(t.T("prefs_title", "Preferences"), err)
		return
	}
	p.app.applyThemeFromConfig()
	t.SetLanguage(cfg.Preferences.Language)
	p.returnFromPreferences()
}

// handleCancel discards the live language preview, restoring the language active when the screen opened.
func (p *PreferencesScreen) handleCancel() {
	t.SetLanguage(p.entryLang)
	p.returnFromPreferences()
}

// currentThemeValue is the stored theme value, defaulting to "system" when unset.
func (p *PreferencesScreen) currentThemeValue() string {
	if v := strings.TrimSpace(p.app.config.Preferences.Theme); v != "" {
		return v
	}
	return themeChoices[0].value
}

// currentLanguageValue is the configured language, or the active language when none is pinned in config.
func (p *PreferencesScreen) currentLanguageValue() string {
	if v := strings.TrimSpace(p.app.config.Preferences.Language); v != "" {
		return v
	}
	return t.CurrentLanguage()
}

func (p *PreferencesScreen) returnFromPreferences() {
	switch p.returnTarget {
	case ReturnToStartup:
		p.app.startup()
	default:
		p.app.openWelcome()
	}
}

func fieldLabel(label string) *widget.Label {
	return widget.NewLabelWithStyle(label, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
}

// themeLabels returns the localized theme menu labels in display order.
func themeLabels() []string {
	labels := make([]string, len(themeChoices))
	for i, c := range themeChoices {
		labels[i] = t.T(c.key, c.english)
	}
	return labels
}

// themeLabelFor maps a stored theme value to its localized menu label, falling back to the first choice.
func themeLabelFor(value string) string {
	for _, c := range themeChoices {
		if c.value == value {
			return t.T(c.key, c.english)
		}
	}
	return t.T(themeChoices[0].key, themeChoices[0].english)
}

// themeValueFor maps a localized menu label back to its stored theme value, falling back to the first choice.
func themeValueFor(label string) string {
	for _, c := range themeChoices {
		if t.T(c.key, c.english) == label {
			return c.value
		}
	}
	return themeChoices[0].value
}
