package ui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2/widget"
	"github.com/stretchr/testify/require"
)

// Regression: newExpiryInput must not panic. SetSelected fires the radio's OnChanged (apply), which shows/hides the
// customRow/dateRow containers — so they must be built before SetSelected, or apply() dereferences a nil row.
func TestNewExpiryInputInitialisesWithoutPanic(t *testing.T) {
	e := newExpiryInput([]string{"1 day", "infinite"})
	require.NotNil(t, e.root)

	// Default method (preset) shows only the preset dropdown.
	require.True(t, e.predefined.Visible())
	require.False(t, e.customRow.Visible())
	require.False(t, e.dateRow.Visible())

	// The date defaults to a future datetime (~now + 1 day), so it validates out of the box.
	require.NotNil(t, e.date.Date)
	require.True(t, e.date.Date.After(time.Now()))

	// Switching the method shows only that row.
	e.method.SetSelected(e.optDate)
	require.False(t, e.predefined.Visible())
	require.True(t, e.dateRow.Visible())
}

// applyTypeEnabled must disable the expiry when the level removes protection ("(no protection)") — the page is just
// unprotected, so an expiry is meaningless — and enable it for a concrete level.
func TestApplyTypeEnabledDisablesExpiryWhenUnprotecting(t *testing.T) {
	expiry := newExpiryInput([]string{"1 day"})
	level := widget.NewSelect([]string{noChangeLevel(), removeLevel(), "sysop"}, nil)
	s := &protectionWorkflowScreen{
		levelSelects: map[string]*widget.Select{"edit": level},
		expiryInputs: map[string]*expiryInput{"edit": expiry},
		sameAsEdit:   map[string]*widget.Check{},
	}

	level.SetSelected("sysop")
	s.applyTypeEnabled("edit")
	require.False(t, expiry.method.Disabled()) // concrete level -> expiry active

	level.SetSelected(removeLevel())
	s.applyTypeEnabled("edit")
	require.True(t, expiry.method.Disabled()) // "(no protection)" -> expiry disabled
}
