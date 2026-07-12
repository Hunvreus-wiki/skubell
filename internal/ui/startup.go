package ui

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/Hunvreus-wiki/skubell/internal/config"
	t "github.com/Hunvreus-wiki/skubell/internal/i18n"
)

// StartupScreen renders the initial wiki list screen.
type StartupScreen struct {
	app  *App
	root fyne.CanvasObject
	list *fyne.Container
}

// NewStartupScreen builds the startup screen.
func NewStartupScreen(app *App) *StartupScreen {
	screen := &StartupScreen{app: app}
	screen.root = screen.build()
	return screen
}

// Canvas returns the root canvas for this screen.
func (s *StartupScreen) Canvas() fyne.CanvasObject {
	return s.root
}

// RefreshList rebuilds the wiki list rows.
func (s *StartupScreen) RefreshList() {
	if s.list == nil {
		return
	}
	s.list.Objects = s.list.Objects[:0]
	for _, wiki := range s.app.config.Wikis {
		s.list.Add(s.buildRow(wiki))
	}
	s.list.Refresh()
}

func (s *StartupScreen) build() fyne.CanvasObject {
	title := widget.NewLabelWithStyle(
		t.T("startup_title", "Select a wiki"),
		fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true},
	)
	s.list = container.NewVBox()
	s.RefreshList()
	scrollContentPad := canvas.NewRectangle(color.Transparent)
	scrollContentPad.SetMinSize(fyne.NewSize(theme.ScrollBarSize()+theme.InnerPadding(), 0))
	scrollContent := container.NewBorder(nil, nil, nil, scrollContentPad, s.list)

	scroll := container.NewVScroll(scrollContent)
	scroll.SetMinSize(fyne.NewSize(0, 300))

	outerPadWidth := theme.Padding() * 0.5
	leftPad := canvas.NewRectangle(color.Transparent)
	leftPad.SetMinSize(fyne.NewSize(outerPadWidth, 0))
	rightPad := canvas.NewRectangle(color.Transparent)
	rightPad.SetMinSize(fyne.NewSize(outerPadWidth, 0))
	scrollArea := container.NewBorder(nil, nil, leftPad, rightPad, scroll)

	addButton := widget.NewButton(t.T("startup_add_wiki", "+ Add a new wiki"), func() {
		screen := NewWikiSettingsScreen(s.app, WikiSettingsModeCreate, config.WikiEntry{})
		s.app.window.SetContent(screen.Canvas())
	})

	settingsButton := widget.NewButtonWithIcon(t.T("common_preferences", "Preferences"), theme.SettingsIcon(), func() {
		s.app.preferencesReturn = ReturnToStartup
		s.app.openPreferences()
	})

	return container.NewBorder(
		container.NewBorder(nil, nil, title, settingsButton, widget.NewSeparator()),
		container.NewVBox(widget.NewSeparator(), addButton),
		nil,
		nil,
		scrollArea,
	)
}

func (s *StartupScreen) buildRow(wiki config.WikiEntry) fyne.CanvasObject {
	security := securityIndicator(wiki.APIURL)
	name := widget.NewLabelWithStyle(wiki.Name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})

	connect := widget.NewButton(t.T("common_connect", "Connect"), func() {
		s.app.connectAndOpenWelcome(wiki)
	})
	deleteBtn := widget.NewButton(t.T("common_delete", "Delete"), func() {
		s.deleteWiki(wiki)
	})
	edit := widget.NewButton(t.T("common_edit", "Edit"), func() {
		screen := NewWikiSettingsScreen(s.app, WikiSettingsModeEdit, wiki)
		s.app.window.SetContent(screen.Canvas())
	})

	row := container.NewBorder(nil, nil, container.NewHBox(security, name, layout.NewSpacer()), nil,
		container.NewHBox(layout.NewSpacer(), connect, edit, deleteBtn),
	)

	card := widget.NewCard("", "", row)
	card.SetSubTitle(wiki.APIURL)
	return container.NewVBox(bordered(card), verticalGap())
}

func (s *StartupScreen) deleteWiki(wiki config.WikiEntry) {
	confirm := dialog.NewConfirm(
		t.T("startup_delete_title", "Delete Wiki"),
		t.Td("startup_delete_confirm", "Remove \"{{.Name}}\" from the wiki list?", map[string]any{"Name": wiki.Name}),
		func(ok bool) {
			if !ok {
				return
			}

			cfg := s.app.config
			filtered := make([]config.WikiEntry, 0, len(cfg.Wikis))
			for _, entry := range cfg.Wikis {
				if entry.Name != wiki.Name {
					filtered = append(filtered, entry)
				}
			}
			cfg.Wikis = filtered

			if err := s.app.saveConfig(cfg); err != nil {
				s.app.showError("Delete", err)
				return
			}
			if err := s.app.store.Delete(wiki.Name); err != nil {
				s.app.showError("Delete", fmt.Errorf("remove credential: %w", err))
				return
			}

			s.RefreshList()
		},
		s.app.window,
	)
	confirm.Show()
}

func isSecureURL(apiURL string) bool {
	return strings.HasPrefix(strings.ToLower(apiURL), "https://")
}

func securityLabel(apiURL string) string {
	switch {
	case apiURL == "":
		return t.T("security_unknown", "Unknown")
	case isSecureURL(apiURL):
		return t.T("security_secure", "Secure")
	default:
		return t.T("security_insecure", "Insecure")
	}
}

func securityIndicator(apiURL string) fyne.CanvasObject {
	icon := unlockIcon()
	if isSecureURL(apiURL) {
		icon = lockIcon()
	}
	label := widget.NewLabel(securityLabel(apiURL))
	indicator := container.NewHBox(widget.NewIcon(icon), label)

	maxLabelSize := fyne.MeasureText(t.T("security_insecure", "Insecure"), theme.TextSize(), fyne.TextStyle{})
	width := theme.IconInlineSize() + theme.InnerPadding() + maxLabelSize.Width
	height := float32(math.Max(float64(theme.IconInlineSize()), float64(maxLabelSize.Height)))

	return container.NewGridWrap(fyne.NewSize(width, height), indicator)
}
