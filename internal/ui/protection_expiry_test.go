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

	// Default method ("(no change)") keeps the current expiry, so none of the value rows show.
	require.True(t, e.isNoChange())
	require.False(t, e.predefined.Visible())
	require.False(t, e.customRow.Visible())
	require.False(t, e.dateRow.Visible())

	// Selecting the preset method reveals only the preset dropdown.
	e.method.SetSelected(e.optPreset)
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

// typeSetting reads the level and expiry controls independently: each has a "(no change)" state, so keeping one while
// changing the other must be expressible (a bulk temporary→permanent change keeps the level and sets the expiry).
func TestTypeSettingIndependentLevelAndExpiry(t *testing.T) {
	newScreen := func() (*protectionWorkflowScreen, *widget.Select, *expiryInput) {
		level := widget.NewSelect([]string{noChangeLevel(), removeLevel(), "sysop"}, nil)
		level.SetSelectedIndex(0)
		expiry := newExpiryInput([]string{"infinite", "1 month"})
		s := &protectionWorkflowScreen{
			levelSelects: map[string]*widget.Select{"edit": level},
			expiryInputs: map[string]*expiryInput{"edit": expiry},
		}
		return s, level, expiry
	}

	// Both "(no change)": leave the type entirely alone.
	s, _, _ := newScreen()
	set, msg := s.typeSetting("edit")
	require.Empty(t, msg)
	require.True(t, set.KeepLevel)
	require.True(t, set.KeepExpiry)

	// Keep the level, set an expiry: the make-permanent case.
	s, _, expiry := newScreen()
	expiry.method.SetSelected(expiry.optPreset)
	expiry.predefined.SetSelected("infinite")
	set, msg = s.typeSetting("edit")
	require.Empty(t, msg)
	require.True(t, set.KeepLevel)
	require.False(t, set.KeepExpiry)
	require.Equal(t, "infinite", set.Expiry)

	// Change the level, keep the expiry.
	s, level, _ := newScreen()
	level.SetSelected("sysop")
	set, msg = s.typeSetting("edit")
	require.Empty(t, msg)
	require.False(t, set.KeepLevel)
	require.Equal(t, "sysop", set.Level)
	require.True(t, set.KeepExpiry)

	// Remove protection: expiry is irrelevant.
	s, level, _ = newScreen()
	level.SetSelected(removeLevel())
	set, msg = s.typeSetting("edit")
	require.Empty(t, msg)
	require.Empty(t, set.Level)
	require.False(t, set.KeepLevel)
}

// setPredefinedOptions must keep the operator's chosen duration when the loaded list omits it, appending it rather than
// resetting to the first option — otherwise the async load silently changes a choice made against the fallback list.
func TestSetPredefinedOptionsKeepsChoice(t *testing.T) {
	// The operator picked "1 year" from the fallback list; the wiki's loaded list omits it.
	e := newExpiryInput([]string{"infinite", "1 week", "1 year"})
	e.predefined.SetSelected("1 year")
	e.setPredefinedOptions([]string{"2 hours", "1 day", "1 month"})
	require.Equal(t, "1 year", e.predefined.Selected, "the operator's choice is preserved, not reset to index 0")
	require.Contains(t, e.predefined.Options, "1 year")

	// A choice the loaded list contains is simply kept, no duplicate appended.
	e2 := newExpiryInput([]string{"infinite"})
	e2.predefined.SetSelected("infinite")
	e2.setPredefinedOptions([]string{"1 month", "infinite"})
	require.Equal(t, "infinite", e2.predefined.Selected)
	require.Equal(t, []string{"1 month", "infinite"}, e2.predefined.Options)
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
