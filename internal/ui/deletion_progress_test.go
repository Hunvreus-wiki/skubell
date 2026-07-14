package ui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeletionPercent(t *testing.T) {
	t.Parallel()
	require.Equal(t, 0, deletionPercent(0, 0)) // unknown total
	require.Equal(t, 0, deletionPercent(5, 0)) // guards divide-by-zero
	require.Equal(t, 50, deletionPercent(1, 2))
	require.Equal(t, 63, deletionPercent(63, 100))
	require.Equal(t, 100, deletionPercent(10, 10))
	require.Equal(t, 100, deletionPercent(11, 10)) // clamped: found can briefly lag processed
}

func TestProgressLabels(t *testing.T) {
	// Not parallel: go-i18n's Localizer (used by t.Td) is not safe for concurrent use.
	calc := calcProgressLabel(600, 1000)
	require.Contains(t, calc, "60%")
	require.Contains(t, calc, "1000")
	require.Contains(t, finalizeProgressLabel(1, 4), "25%")
}
