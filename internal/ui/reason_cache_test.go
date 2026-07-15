package ui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/api"
)

func testReasonDropdowns() map[string]api.ReasonDropdown {
	return map[string]api.ReasonDropdown{
		api.ReasonActionDelete: {
			Action: api.ReasonActionDelete,
			Categories: []api.ReasonCategory{
				{Label: "Common", Reasons: []string{"Spam", "   "}},
				{Label: "Speedy", Reasons: []string{"Copyright violation"}},
			},
		},
		api.ReasonActionProtect: {
			Action:     api.ReasonActionProtect,
			Categories: []api.ReasonCategory{{Label: "Common", Reasons: []string{"Edit war"}}},
		},
	}
}

// TestReasonsForActionFlattensCachedDropdown checks the accessor every workflow shares: categories collapse into one
// sorted list, blank reasons drop out, and each action reads its own dropdown.
func TestReasonsForActionFlattensCachedDropdown(t *testing.T) {
	t.Parallel()

	app := &App{reasonDropdowns: testReasonDropdowns()}

	require.Equal(t, []string{"Copyright violation", "Spam"}, app.reasonsForAction(api.ReasonActionDelete))
	require.Equal(t, []string{"Edit war"}, app.reasonsForAction(api.ReasonActionProtect))
	// An action the wiki defines no reasons for is not an error: the workflow simply offers "(none)" plus free text.
	require.Empty(t, app.reasonsForAction(api.ReasonActionRevDelete))
}

// TestReasonsForActionWithoutConnection guards the disconnected state: the cache is cleared on disconnect, so the
// accessor must yield nothing rather than panic on a nil map.
func TestReasonsForActionWithoutConnection(t *testing.T) {
	t.Parallel()

	app := &App{}
	require.Empty(t, app.reasonsForAction(api.ReasonActionDelete))
}

// TestLoadReasonsUsesCacheWithoutClient is the point of the cache: reaching the options step spends no request, so the
// reasons appear even with no client to fetch them with.
func TestLoadReasonsUsesCacheWithoutClient(t *testing.T) {
	t.Parallel()

	screen := &deleteWorkflowScreen{app: &App{reasonDropdowns: testReasonDropdowns()}}
	require.Nil(t, screen.app.client)

	screen.loadReasons()

	require.Equal(t, []string{"Copyright violation", "Spam"}, screen.reasons)
	require.Equal(t, []string{"(none)", "Copyright violation", "Spam"}, screen.reasonSelectOptions())
}
