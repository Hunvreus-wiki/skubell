package ui

import (
	"fmt"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/Hunvreus-wiki/skubell/internal/config"
	t "github.com/Hunvreus-wiki/skubell/internal/i18n"
	"github.com/Hunvreus-wiki/skubell/internal/registry"
)

// WikiSettingsMode controls create vs edit behavior.
type WikiSettingsMode int

const (
	WikiSettingsModeCreate WikiSettingsMode = iota
	WikiSettingsModeEdit
)

const (
	farmCustom    = "custom"
	farmWikimedia = "wikimedia"
	farmFandom    = "fandom"
	farmMiraheze  = "miraheze"
	farmWikigg    = "wikigg"
)

type wikiFormState struct {
	mode            WikiSettingsMode
	selectedFarm    string
	projectFamily   string
	language        string
	wikiID          string
	customURL       string
	name            string
	originalName    string
	nameEdited      bool
	username        string
	credential      string
	credentialDirty bool
}

// WikiSettingsScreen renders the create/edit wiki screen.
type WikiSettingsScreen struct {
	app  *App
	root fyne.CanvasObject

	state          wikiFormState
	suggestingName bool

	nameEntry     *widget.Entry
	urlEntry      *focusEntry
	userEntry     *widget.Entry
	credentialEnt *widget.Entry

	modeRadio     *widget.RadioGroup
	farmSelect    *widget.Select
	projectSelect *widget.Select
	languageEntry *widget.Entry
	wikiIDEntry   *widget.Entry

	farmField     fyne.CanvasObject
	projectField  fyne.CanvasObject
	languageField fyne.CanvasObject
	wikiIDField   fyne.CanvasObject
}

// NewWikiSettingsScreen creates a new wiki settings form.
func NewWikiSettingsScreen(app *App, mode WikiSettingsMode, wiki config.WikiEntry) *WikiSettingsScreen {
	screen := &WikiSettingsScreen{
		app: app,
		state: wikiFormState{
			mode:         mode,
			selectedFarm: farmCustom,
		},
	}
	screen.initState(wiki)
	screen.root = screen.build()
	return screen
}

// Canvas returns the root canvas object.
func (s *WikiSettingsScreen) Canvas() fyne.CanvasObject {
	return s.root
}

func (s *WikiSettingsScreen) initState(wiki config.WikiEntry) {
	if wiki.Name != "" {
		s.state.name = wiki.Name
		s.state.originalName = wiki.Name
		s.state.username = wiki.Username
		s.state.selectedFarm = strings.ToLower(strings.TrimSpace(wiki.Farm))
		if s.state.selectedFarm == "" {
			s.state.selectedFarm = farmCustom
		}
		s.state.projectFamily = wiki.Family
		s.state.language = wiki.Language
		s.state.wikiID = wiki.WikiID
		s.state.customURL = wiki.APIURL
		if wiki.Credential != "" && wiki.Credential != keyringCredentialMarker {
			s.state.credential = wiki.Credential
		}
	}
}

func (s *WikiSettingsScreen) build() fyne.CanvasObject {
	title := widget.NewLabelWithStyle(
		t.T("wiki_settings_title", "Wiki Settings"),
		fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true},
	)

	s.nameEntry = widget.NewEntry()
	s.nameEntry.SetText(s.state.name)
	s.nameEntry.OnChanged = func(value string) {
		s.state.name = value
		if s.suggestingName {
			return
		}
		if strings.TrimSpace(value) == "" {
			s.state.nameEdited = false
			return
		}
		s.state.nameEdited = true
	}
	s.userEntry = widget.NewEntry()
	s.userEntry.SetText(s.state.username)
	s.userEntry.SetPlaceHolder("Admin@BotName")

	s.credentialEnt = widget.NewPasswordEntry()
	s.credentialEnt.SetText(s.state.credential)
	s.credentialEnt.OnChanged = func(value string) {
		s.state.credentialDirty = true
	}

	s.urlEntry = newFocusEntry()
	s.urlEntry.SetText(s.state.customURL)
	s.urlEntry.onFocusLost = func() {
		s.handleAutoDetect(strings.TrimSpace(s.urlEntry.Text))
	}
	s.urlEntry.OnChanged = func(value string) {
		s.state.customURL = value
	}

	s.modeRadio = widget.NewRadioGroup(
		[]string{t.T("wiki_mode_custom", "Custom URL"), t.T("wiki_mode_wellknown", "Well-known host")},
		func(value string) {
			if value == t.T("wiki_mode_wellknown", "Well-known host") {
				if s.state.selectedFarm == farmCustom {
					s.state.selectedFarm = farmWikimedia
				}
			} else {
				s.state.selectedFarm = farmCustom
			}
			s.updateFarmFields()
		})
	s.farmSelect = widget.NewSelect([]string{"Wikimedia", "Fandom", "Miraheze", "wiki.gg"}, func(value string) {
		switch value {
		case "Wikimedia":
			s.state.selectedFarm = farmWikimedia
		case "Fandom":
			s.state.selectedFarm = farmFandom
		case "Miraheze":
			s.state.selectedFarm = farmMiraheze
		case "wiki.gg":
			s.state.selectedFarm = farmWikigg
		}
		s.updateFarmFields()
		s.applyNameSuggestion()
	})
	s.projectSelect = widget.NewSelect(wikimediaFamilies(), func(value string) {
		s.state.projectFamily = value
		s.updateURLFromWellKnown()
		s.applyNameSuggestion()
	})
	s.languageEntry = widget.NewEntry()
	s.languageEntry.SetPlaceHolder("en")
	s.languageEntry.OnChanged = func(value string) {
		s.state.language = strings.TrimSpace(value)
		s.updateURLFromWellKnown()
		s.applyNameSuggestion()
	}
	s.wikiIDEntry = widget.NewEntry()
	s.wikiIDEntry.SetPlaceHolder("starwars")
	s.wikiIDEntry.OnChanged = func(value string) {
		s.state.wikiID = strings.TrimSpace(value)
		s.updateURLFromWellKnown()
		s.applyNameSuggestion()
	}

	s.applyStateToSelectors()
	if s.state.selectedFarm == farmCustom {
		s.modeRadio.Selected = t.T("wiki_mode_custom", "Custom URL")
	} else {
		s.modeRadio.Selected = t.T("wiki_mode_wellknown", "Well-known host")
	}
	s.modeRadio.Refresh()

	form := container.NewVBox(
		s.labeledField(t.T("wiki_field_name", "Name"), s.nameEntry),
		s.labeledField(t.T("wiki_field_username", "Username"), s.userEntry),
		s.labeledField(t.T("wiki_field_bot_password", "Bot Password"), s.credentialEnt),
		widget.NewSeparator(),
		s.labeledField(t.T("wiki_field_mode", "Mode"), s.modeRadio),
		s.labeledField(t.T("wiki_field_url", "Wiki URL"), s.urlEntry),
	)
	s.farmField = s.labeledField(t.T("wiki_field_farm", "Farm"), s.farmSelect)
	s.projectField = s.labeledField(t.T("wiki_field_project", "Project"), s.projectSelect)
	s.languageField = s.labeledField(t.T("wiki_field_language", "Language"), s.languageEntry)
	s.wikiIDField = s.labeledField(t.T("wiki_field_wiki_name", "Wiki name"), s.wikiIDEntry)
	form.Add(s.farmField)
	form.Add(s.projectField)
	form.Add(s.languageField)
	form.Add(s.wikiIDField)

	s.updateFarmFields()

	save := widget.NewButton(t.T("common_save", "Save"), func() {
		s.handleSave()
	})
	cancel := widget.NewButton(t.T("common_cancel", "Cancel"), func() {
		s.app.startup()
	})

	footer := container.NewHBox(layout.NewSpacer(), cancel, save)
	return container.NewBorder(container.NewVBox(title, widget.NewSeparator()), footer, nil, nil, form)
}

func (s *WikiSettingsScreen) labeledField(label string, obj fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVBox(widget.NewLabelWithStyle(label, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), obj)
}

func (s *WikiSettingsScreen) applyStateToSelectors() {
	switch s.state.selectedFarm {
	case farmWikimedia:
		s.farmSelect.SetSelected("Wikimedia")
	case farmFandom:
		s.farmSelect.SetSelected("Fandom")
	case farmMiraheze:
		s.farmSelect.SetSelected("Miraheze")
	case farmWikigg:
		s.farmSelect.SetSelected("wiki.gg")
	}

	if s.state.projectFamily != "" {
		s.projectSelect.SetSelected(s.state.projectFamily)
	}
	s.languageEntry.SetText(s.state.language)
	s.wikiIDEntry.SetText(s.state.wikiID)
}

func (s *WikiSettingsScreen) updateFarmFields() {
	customMode := s.state.selectedFarm == farmCustom

	if customMode {
		s.urlEntry.Enable()
	} else {
		s.urlEntry.Disable()
	}
	s.farmSelect.Disable()
	s.projectSelect.Disable()
	s.languageEntry.Disable()
	s.wikiIDEntry.Disable()

	if customMode {
		s.urlEntry.Show()
		s.hideField(s.farmField, s.farmSelect)
		s.hideField(s.projectField, s.projectSelect)
		s.hideField(s.languageField, s.languageEntry)
		s.hideField(s.wikiIDField, s.wikiIDEntry)
		s.urlEntry.Refresh()
		return
	}

	s.farmSelect.Enable()
	s.showField(s.farmField, s.farmSelect)

	if strings.TrimSpace(s.farmSelect.Selected) == "" {
		s.hideField(s.projectField, s.projectSelect)
		s.hideField(s.languageField, s.languageEntry)
		s.hideField(s.wikiIDField, s.wikiIDEntry)
		return
	}

	switch s.state.selectedFarm {
	case farmWikimedia:
		s.projectSelect.Enable()
		s.languageEntry.Enable()
		s.wikiIDEntry.Disable()
		s.showField(s.projectField, s.projectSelect)
		s.showField(s.languageField, s.languageEntry)
		s.hideField(s.wikiIDField, s.wikiIDEntry)
	case farmFandom:
		s.projectSelect.Disable()
		s.languageEntry.Enable()
		s.wikiIDEntry.Enable()
		s.hideField(s.projectField, s.projectSelect)
		s.showField(s.languageField, s.languageEntry)
		s.showField(s.wikiIDField, s.wikiIDEntry)
	case farmMiraheze, farmWikigg:
		s.projectSelect.Disable()
		s.languageEntry.Disable()
		s.wikiIDEntry.Enable()
		s.hideField(s.projectField, s.projectSelect)
		s.hideField(s.languageField, s.languageEntry)
		s.showField(s.wikiIDField, s.wikiIDEntry)
	}
	s.updateURLFromWellKnown()
	s.applyNameSuggestion()
}

func (s *WikiSettingsScreen) updateURLFromWellKnown() {
	if s.state.selectedFarm == farmCustom {
		return
	}
	entry := registry.WikiEntry{
		Farm:     s.state.selectedFarm,
		Family:   strings.ToLower(strings.TrimSpace(s.state.projectFamily)),
		Language: strings.ToLower(strings.TrimSpace(s.state.language)),
		WikiID:   strings.ToLower(strings.TrimSpace(s.state.wikiID)),
	}
	url := entry.APIURL()
	s.urlEntry.SetText(url)
}

func (s *WikiSettingsScreen) applyNameSuggestion() {
	if s.state.mode == WikiSettingsModeEdit {
		return
	}
	if s.state.selectedFarm == farmCustom {
		return
	}
	if s.state.nameEdited {
		return
	}

	wikiName := strings.TrimSpace(s.state.wikiID)
	if s.state.selectedFarm == farmWikimedia {
		wikiName = strings.TrimSpace(s.state.projectFamily)
	}
	if wikiName == "" {
		return
	}

	suggestion := titleFromSlug(wikiName)
	farmLabel := farmDisplayName(s.state.selectedFarm)
	if farmLabel != "" {
		suggestion = farmLabel + " " + suggestion
	}
	if lang := strings.TrimSpace(s.state.language); lang != "" {
		suggestion += " " + lang
	}
	s.setSuggestedName(suggestion)
}

func farmDisplayName(farm string) string {
	switch strings.ToLower(strings.TrimSpace(farm)) {
	case farmFandom:
		return "Fandom"
	case farmMiraheze:
		return "Miraheze"
	case farmWikigg:
		return "wiki.gg"
	default:
		return ""
	}
}

func (s *WikiSettingsScreen) hideField(field fyne.CanvasObject, input fyne.CanvasObject) {
	if input != nil {
		input.Hide()
	}
	if field != nil {
		field.Hide()
	}
}

func (s *WikiSettingsScreen) showField(field fyne.CanvasObject, input fyne.CanvasObject) {
	if input != nil {
		input.Show()
	}
	if field != nil {
		field.Show()
	}
}

func (s *WikiSettingsScreen) setSuggestedName(value string) {
	s.suggestingName = true
	s.nameEntry.SetText(value)
	s.state.name = value
	s.suggestingName = false
}

func titleFromSlug(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	parts := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		runes := []rune(strings.ToLower(part))
		if len(runes) == 0 {
			continue
		}
		runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		out = append(out, string(runes))
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, " ")
}

func (s *WikiSettingsScreen) handleAutoDetect(value string) {
	if s.state.selectedFarm != farmCustom {
		return
	}
	detected := registry.DetectFromURL(value)
	if detected.Farm == farmCustom {
		return
	}

	s.state.selectedFarm = detected.Farm
	s.state.projectFamily = detected.Family
	s.state.language = detected.Language
	s.state.wikiID = detected.WikiID
	s.modeRadio.SetSelected(t.T("wiki_mode_wellknown", "Well-known host"))
	s.applyStateToSelectors()
	s.updateFarmFields()
}

func (s *WikiSettingsScreen) handleSave() {
	name := strings.TrimSpace(s.nameEntry.Text)
	if name == "" {
		s.app.showMessage(t.T("common_save", "Save"), t.T("wiki_err_name_required", "Name is required."))
		return
	}
	if s.nameExists(name) {
		s.app.showMessage(t.T("common_save", "Save"), t.T("wiki_err_name_exists", "Name already exists."))
		return
	}

	username := strings.TrimSpace(s.userEntry.Text)
	if username == "" {
		s.app.showMessage(t.T("common_save", "Save"), t.T("wiki_err_username_required", "Username is required."))
		return
	}

	apiURL := strings.TrimSpace(s.urlEntry.Text)
	if apiURL == "" {
		s.app.showMessage(t.T("common_save", "Save"), t.T("wiki_err_url_required", "Wiki URL is required."))
		return
	}

	updated := config.WikiEntry{
		Name:       name,
		Farm:       s.state.selectedFarm,
		Family:     strings.ToLower(strings.TrimSpace(s.state.projectFamily)),
		Language:   strings.ToLower(strings.TrimSpace(s.state.language)),
		WikiID:     strings.ToLower(strings.TrimSpace(s.state.wikiID)),
		Username:   username,
		Credential: keyringCredentialMarker,
	}

	if s.state.selectedFarm != farmCustom {
		entry := registry.WikiEntry{
			Farm:     updated.Farm,
			Family:   updated.Family,
			Language: updated.Language,
			WikiID:   updated.WikiID,
		}
		updated.APIURL = entry.APIURL()
	} else {
		entry := registry.WikiEntry{
			Farm:         farmCustom,
			CustomAPIURL: apiURL,
		}
		normalized := entry.APIURL()
		if normalized == "" {
			s.app.showMessage(t.T("common_save", "Save"), t.T("wiki_err_url_invalid", "Wiki URL is invalid."))
			return
		}
		updated.APIURL = normalized
	}

	credential := strings.TrimSpace(s.credentialEnt.Text)
	if credential == "" {
		s.app.showMessage(
			t.T("common_save", "Save"),
			t.T("wiki_err_bot_password_required", "Bot Password is required."),
		)
		return
	}

	if err := s.app.persistWiki(updated, credential, s.state.mode, s.state.originalName); err != nil {
		s.app.showError(t.T("common_save", "Save"), err)
		return
	}

	s.app.startup()
}

// persistWiki writes the credential to the OS keyring FIRST, then saves the config — so a canceled or failed keyring
// write aborts the whole save and the configuration is not created, letting the user try again. If the config write
// then fails, a newly-created keyring entry is rolled back so no orphaned credential is left behind.
func (a *App) persistWiki(
	updated config.WikiEntry, credential string, mode WikiSettingsMode, originalName string,
) error {
	if err := a.store.Store(updated.Name, []byte(credential)); err != nil {
		return fmt.Errorf("store credential: %w", err)
	}

	cfg := a.config
	cfg.Wikis = slices.Clone(a.config.Wikis) // don't mutate a.config until the write succeeds
	if mode == WikiSettingsModeCreate {
		cfg.Wikis = append(cfg.Wikis, updated)
	} else {
		replaced := false
		for idx := range cfg.Wikis {
			if cfg.Wikis[idx].Name == originalName {
				cfg.Wikis[idx] = updated
				replaced = true
				break
			}
		}
		if !replaced {
			cfg.Wikis = append(cfg.Wikis, updated)
		}
	}

	if err := a.saveConfig(cfg); err != nil {
		if mode == WikiSettingsModeCreate {
			_ = a.store.Delete(updated.Name) // avoid leaving an orphaned credential behind
		}
		return err
	}
	return nil
}

func (s *WikiSettingsScreen) nameExists(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return false
	}
	for _, entry := range s.app.config.Wikis {
		if strings.ToLower(strings.TrimSpace(entry.Name)) != normalized {
			continue
		}
		if s.state.mode == WikiSettingsModeEdit &&
			strings.ToLower(strings.TrimSpace(s.state.originalName)) == normalized {
			continue
		}
		return true
	}
	return false
}

func wikimediaFamilies() []string {
	return []string{
		"wikipedia",
		"wiktionary",
		"wikibooks",
		"wikinews",
		"wikiquote",
		"wikisource",
		"wikiversity",
		"wikivoyage",
		"commons",
		"wikidata",
		"wikispecies",
		"meta",
		"mediawiki",
		"incubator",
		"wikifunctions",
	}
}

type focusEntry struct {
	widget.Entry
	onFocusLost func()
}

func newFocusEntry() *focusEntry {
	entry := &focusEntry{}
	entry.ExtendBaseWidget(entry)
	return entry
}

// FocusLost triggers the custom callback after losing focus.
func (f *focusEntry) FocusLost() {
	f.Entry.FocusLost()
	if f.onFocusLost != nil {
		f.onFocusLost()
	}
}
