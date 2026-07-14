package ui

import (
	"testing"
	"time"

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
