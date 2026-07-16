package ui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWorkflowControllerTitle pins the chrome title to the constructor argument: the controller is shared by every
// workflow, so a hardcoded title shows the wrong workflow name (protection once read "Delete pages").
func TestWorkflowControllerTitle(t *testing.T) {
	// Not parallel: it builds Fyne widgets, whose shared render caches are not safe for concurrent access.
	w := newWorkflowController(&App{}, "Change page protection", nil, nil, nil, nil)
	require.Equal(t, "Change page protection", w.titleLabel.Text)
}
