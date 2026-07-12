package main

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed icon.png
var iconData []byte

// appIcon is the application/window icon (a vacuum cleaner — "skubell" is Breton for broom/sweeper).
var appIcon = fyne.NewStaticResource("icon.png", iconData)
