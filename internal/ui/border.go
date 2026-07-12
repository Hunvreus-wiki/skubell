package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

func bordered(content fyne.CanvasObject) fyne.CanvasObject {
	border := canvas.NewRectangle(color.Transparent)
	border.StrokeColor = theme.Color(theme.ColorNameInputBorder)
	border.StrokeWidth = theme.SeparatorThicknessSize() * 1.5
	return container.NewStack(border, container.NewPadded(content))
}

func verticalGap() fyne.CanvasObject {
	gap := canvas.NewRectangle(color.Transparent)
	gap.SetMinSize(fyne.NewSize(0, theme.InnerPadding()))
	return gap
}
