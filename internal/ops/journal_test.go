package ops

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAppendAndReadJournalRoundTrip(t *testing.T) {
	t.Parallel()

	journalPath := filepath.Join(t.TempDir(), "journal", "session.jsonl")
	first := JournalEntry{
		Timestamp: time.Date(2026, 2, 17, 16, 0, 0, 0, time.UTC),
		Module:    "deletion",
		Operation: Operation{
			Type:        OpDeletePage,
			Params:      map[string]string{"title": "Apple", "reason": "Cleanup"},
			Description: "Delete [[Apple]]",
		},
		Result: "success",
	}
	second := JournalEntry{
		Timestamp:   time.Date(2026, 2, 17, 16, 0, 1, 0, time.UTC),
		Module:      "deletion",
		Operation:   Operation{Type: OpDeletePage, Params: map[string]string{"title": "Banana"}},
		Result:      "error",
		ErrorCode:   "blocked",
		ErrorDetail: "You cannot delete this page.",
	}

	require.NoError(t, AppendToJournal(journalPath, first))
	require.NoError(t, AppendToJournal(journalPath, second))

	got, err := ReadJournal(journalPath)
	require.NoError(t, err)
	require.Equal(t, []JournalEntry{first, second}, got)
}

func TestReadJournalMissingFileReturnsEmpty(t *testing.T) {
	t.Parallel()

	got, err := ReadJournal(filepath.Join(t.TempDir(), "missing.jsonl"))
	require.NoError(t, err)
	require.Empty(t, got)
}
