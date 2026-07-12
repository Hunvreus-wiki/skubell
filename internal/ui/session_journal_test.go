package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/config"
	"github.com/Hunvreus-wiki/skubell/internal/ops"
)

func TestSessionJournalTailReturnsMostRecentOldestFirst(t *testing.T) {
	t.Parallel()

	a := &App{}
	for i := range 10 {
		a.recordJournalEntry(
			ops.JournalEntry{Module: "deletion", Operation: ops.Operation{Description: fmt.Sprintf("op %d", i)}},
		)
	}

	tail := a.sessionJournalTail(3)
	require.Len(t, tail, 3)
	require.Equal(t, "op 7", tail[0].Operation.Description)
	require.Equal(t, "op 9", tail[2].Operation.Description)

	// The tail is a copy: mutating it does not affect the stored journal.
	tail[0].Module = "mutated"
	require.Equal(t, "deletion", a.sessionJournalTail(3)[0].Module)
}

func TestSessionJournalTailBounds(t *testing.T) {
	t.Parallel()

	a := &App{}
	require.Nil(t, a.sessionJournalTail(5))

	a.recordJournalEntry(ops.JournalEntry{Module: "deletion"})
	a.recordJournalEntry(ops.JournalEntry{Module: "deletion"})
	require.Len(t, a.sessionJournalTail(10), 2) // n larger than count returns all
	require.Nil(t, a.sessionJournalTail(0))     // non-positive n returns nothing

	a.resetSessionJournal()
	require.Nil(t, a.sessionJournalTail(5))
}

func TestFormatJournalLine(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 7, 7, 15, 30, 5, 0, time.Local)
	require.Equal(t, `15:30:05  ✓  Delete page "Apple"`, formatJournalLine(ops.JournalEntry{
		Timestamp: ts,
		Result:    "success",
		Operation: ops.Operation{Description: `Delete page "Apple"`},
	}))
	// Falls back to the module name when the operation has no description.
	require.Equal(t, "15:30:05  ✗  deletion", formatJournalLine(ops.JournalEntry{
		Timestamp: ts,
		Result:    "error",
		Module:    "deletion",
	}))
}

func TestJournalResultGlyph(t *testing.T) {
	t.Parallel()

	require.Equal(t, "✓", journalResultGlyph("success"))
	require.Equal(t, "⊘", journalResultGlyph("skipped"))
	require.Equal(t, "✗", journalResultGlyph("error"))
	require.Equal(t, "✗", journalResultGlyph("unrecognized"))
}

// TestAppPersistsSessionJournalToDisk exercises the full connect → record path (minus Fyne): startSessionJournal wires
// a writer under the configured directory, and the first recorded entry lazily creates the per-wiki subdirectory, the
// identity file, and the session file.
func TestAppPersistsSessionJournalToDisk(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	a := &App{}
	a.config.Preferences.JournalDirectory = root
	a.currentWiki = config.WikiEntry{
		Farm:     "custom",
		APIURL:   "http://localhost:8081/api.php",
		Name:     "Test Wiki",
		Username: "TestAdmin@SkubellTest",
	}
	a.startSessionJournal()

	// Nothing is written until the first action.
	before, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Empty(t, before)

	a.recordJournalEntry(ops.JournalEntry{
		Timestamp: time.Now().UTC(),
		Module:    "deletion",
		Result:    "success",
		Operation: ops.Operation{Description: "Delete X"},
	})

	subdirs, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, subdirs, 1, "one per-wiki subdirectory")
	dir := filepath.Join(root, subdirs[0].Name())

	identity, err := os.ReadFile(filepath.Join(dir, "wiki_identity.json"))
	require.NoError(t, err)
	require.Contains(t, string(identity), "TestAdmin@SkubellTest")
	require.NotContains(t, string(identity), "password")

	sessionFiles, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	require.NoError(t, err)
	require.Len(t, sessionFiles, 1)
	content, err := os.ReadFile(sessionFiles[0])
	require.NoError(t, err)
	require.Contains(t, string(content), "Delete X")

	// A new session (disconnect) stops persistence.
	a.resetSessionJournal()
	require.Nil(t, a.sessionJournalWriter)
}
