package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/dialog"

	"github.com/Hunvreus-wiki/skubell/internal/security"
	"github.com/Hunvreus-wiki/skubell/internal/ui"
)

func main() {
	a := app.New()
	a.SetIcon(appIcon)
	w := a.NewWindow("Skubell")
	w.SetIcon(appIcon)
	w.Resize(fyne.NewSize(800, 520))

	if err := security.EnsureStartupCredentialStoreAvailability(); err != nil {
		errorDialog := dialog.NewError(err, w)
		errorDialog.SetOnClosed(func() {
			a.Quit()
		})
		errorDialog.Show()
		w.ShowAndRun()
		return
	}

	appUI, err := ui.NewApp(a, w)
	if err != nil {
		errorDialog := dialog.NewError(err, w)
		errorDialog.SetOnClosed(func() {
			a.Quit()
		})
		errorDialog.Show()
		w.ShowAndRun()
		return
	}

	appUI.Run()
}
