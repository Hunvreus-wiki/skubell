package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/config"
)

// defaultWindow is the size cmd/skubell opens at before anything is restored.
var defaultWindow = fyne.NewSize(800, 520)

// newSizedApp builds an App around a real window: the size is only answerable by one that exists.
func newSizedApp(t *testing.T, prefs config.Preferences) (*App, fyne.Window) {
	t.Helper()
	fynetest.NewTempApp(t)
	window := fynetest.NewTempWindow(t, widget.NewLabel("content"))
	window.Resize(defaultWindow)
	return &App{
		window:     window,
		config:     config.Config{Preferences: prefs},
		configPath: t.TempDir() + "/config.json",
	}, window
}

// TestWindowSizeIsRestored is the point of the feature: the window comes back the size it was left.
func TestWindowSizeIsRestored(t *testing.T) {
	// Not parallel: builds widgets, and Fyne's text shaping is not safe for concurrent use.
	app, window := newSizedApp(t, config.Preferences{WindowWidth: 1024, WindowHeight: 700})

	app.applyWindowSizeFromConfig()

	require.Equal(t, fyne.NewSize(1024, 700), window.Canvas().Size())
}

// TestWindowSizeIgnoresNothingSaved leaves a first run alone: with no size remembered, the default stands.
func TestWindowSizeIgnoresNothingSaved(t *testing.T) {
	app, window := newSizedApp(t, config.Preferences{})

	app.applyWindowSizeFromConfig()

	require.Equal(t, defaultWindow, window.Canvas().Size())
}

// TestWindowSizeIgnoresUnusableSaved guards against obeying a size the app cannot be used from. A window can be dragged
// down to almost nothing, and reopening into that sliver is worse than forgetting it.
func TestWindowSizeIgnoresUnusableSaved(t *testing.T) {
	app, window := newSizedApp(t, config.Preferences{WindowWidth: 40, WindowHeight: 12})

	app.applyWindowSizeFromConfig()

	require.Equal(t, defaultWindow, window.Canvas().Size())
}

// TestRememberWindowSizeWritesOnlyOnChange keeps quitting cheap: the config file is not rewritten when the window was
// never resized.
func TestRememberWindowSizeWritesOnlyOnChange(t *testing.T) {
	app, window := newSizedApp(t, config.Preferences{WindowWidth: 800, WindowHeight: 520})

	app.rememberWindowSize() // unchanged from what is stored
	require.NoFileExists(t, app.configPath)

	window.Resize(fyne.NewSize(1024, 700))
	app.rememberWindowSize()
	require.FileExists(t, app.configPath)

	saved := config.Load(app.configPath)
	require.InDelta(t, 1024, saved.Preferences.WindowWidth, 0.001)
	require.InDelta(t, 700, saved.Preferences.WindowHeight, 0.001)
}

// TestRememberWindowSizeIgnoresUnusable is the other half of the guard: a sliver is not worth remembering either, or
// the next run would restore it.
func TestRememberWindowSizeIgnoresUnusable(t *testing.T) {
	app, window := newSizedApp(t, config.Preferences{WindowWidth: 1024, WindowHeight: 700})

	window.Resize(fyne.NewSize(40, 12))
	app.rememberWindowSize()

	require.NoFileExists(t, app.configPath)
}
