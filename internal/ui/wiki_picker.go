package ui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/Hunvreus-wiki/skubell/internal/config"
)

// WikiPicker renders the top-bar wiki selector.
type WikiPicker struct {
	app     *App
	onPick  func(name string)
	selectW *widget.Select
	root    fyne.CanvasObject
}

// NewWikiPicker creates a wiki picker component.
func NewWikiPicker(app *App, onPick func(name string)) *WikiPicker {
	picker := &WikiPicker{
		app:    app,
		onPick: onPick,
	}
	picker.root = picker.build()
	return picker
}

// Canvas returns the picker canvas object.
func (p *WikiPicker) Canvas() fyne.CanvasObject {
	return p.root
}

func (p *WikiPicker) build() fyne.CanvasObject {
	options := wikiNames(p.app.config.Wikis)
	p.selectW = widget.NewSelect(options, func(value string) {
		if value == "" || value == p.app.currentWiki.Name {
			return
		}
		if p.onPick != nil {
			p.onPick(value)
		}
	})
	p.selectW.SetSelected(p.app.currentWiki.Name)

	icon := widget.NewIcon(securityIconResource(p.app.currentWiki.APIURL))
	user := widget.NewLabel(p.connectedUserLabel())

	return container.NewHBox(icon, p.selectW, user)
}

func (p *WikiPicker) connectedUserLabel() string {
	if strings.TrimSpace(p.app.currentUser) == "" {
		return ""
	}
	return "User: " + p.app.currentUser
}

func wikiNames(entries []config.WikiEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.Name) == "" {
			continue
		}
		names = append(names, entry.Name)
	}
	return names
}

func securityIconResource(apiURL string) fyne.Resource {
	if strings.HasPrefix(strings.ToLower(apiURL), "https://") {
		return lockIcon()
	}
	return unlockIcon()
}
