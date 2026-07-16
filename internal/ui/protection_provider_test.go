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

// A restriction inherited from another page's cascade carries a "source" field: it is not a direct protection on this
// page, so it must be excluded — otherwise a later move/edit change resends it and an unprotect preview claims to
// remove a restriction that stays inherited.
func TestParsePageProtectionExcludesInherited(t *testing.T) {
	t.Parallel()

	page := parsePageProtection("Cascaded", map[string]any{
		"title": "Cascaded",
		"protection": []any{
			map[string]any{"type": "edit", "level": "sysop", "expiry": "infinity", "source": "Template:Hub"},
			map[string]any{"type": "move", "level": "autoconfirmed", "expiry": "infinity"}, // page's own
		},
	})
	require.True(t, page.Exists)
	require.NotContains(t, page.Protections, "edit", "inherited (sourced) protection is not direct")
	require.Equal(t, "autoconfirmed", page.Protections["move"].Level)
}
