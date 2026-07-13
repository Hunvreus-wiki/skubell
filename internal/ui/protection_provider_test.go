package ui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParsePageProtection(t *testing.T) {
	t.Parallel()

	// Existing page with edit (permanent) and move (dated, cascading) protection.
	existing := parsePageProtection("Apple", map[string]any{
		"title": "Apple",
		"protection": []any{
			map[string]any{"type": "edit", "level": "sysop", "expiry": "infinity"},
			map[string]any{"type": "move", "level": "autoconfirmed", "expiry": "2026-12-31T00:00:00Z", "cascade": true},
		},
	})
	require.True(t, existing.Exists)
	require.Equal(t, "sysop", existing.Protections["edit"].Level)
	require.Equal(t, "infinity", existing.Protections["edit"].Expiry)
	require.False(t, existing.Protections["edit"].Cascade)
	require.Equal(t, "autoconfirmed", existing.Protections["move"].Level)
	require.True(t, existing.Protections["move"].Cascade)

	// Missing page: "missing": true, no protection.
	missing := parsePageProtection("Nope", map[string]any{"title": "Nope", "missing": true})
	require.False(t, missing.Exists)
	require.Empty(t, missing.Protections)

	// Existing but unprotected page: empty protection array.
	unprotected := parsePageProtection("Banana", map[string]any{"title": "Banana", "protection": []any{}})
	require.True(t, unprotected.Exists)
	require.Empty(t, unprotected.Protections)
}
