package ops

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func fixedClock(ts time.Time) func() time.Time {
	return func() time.Time { return ts }
}

func wikiDir(t *testing.T, root, url string) string {
	t.Helper()
	sum := sha1.Sum([]byte(url))
	return filepath.Join(root, hex.EncodeToString(sum[:]))
}

func TestSessionJournalCreatesNothingBeforeFirstAppend(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	_ = NewSessionJournal(root, WikiIdentity{URL: "https://example.org/"}, fixedClock(time.Now()))

	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Empty(t, entries, "an idle session must leave the journal directory empty")
}

func TestSessionJournalWritesIdentityAndEntries(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ts := time.Date(2026, 2, 8, 15, 30, 0, 0, time.UTC)
	identity := WikiIdentity{
		URL: "https://fr.wikipedia.org/", Farm: "wikimedia", Family: "wikipedia",
		Language: "fr", Name: "French Wikipedia", Username: "MyAccount@Bot",
	}
	j := NewSessionJournal(root, identity, fixedClock(ts))

	require.NoError(
		t,
		j.Append(
			JournalEntry{
				Timestamp: ts,
				Module:    "deletion",
				Result:    "success",
				Operation: Operation{Description: "Delete A"},
			},
		),
	)
	require.NoError(
		t,
		j.Append(
			JournalEntry{
				Timestamp: ts,
				Module:    "deletion",
				Result:    "error",
				ErrorCode: "blocked",
				Operation: Operation{Description: "Delete B"},
			},
		),
	)

	dir := wikiDir(t, root, identity.URL)
	require.DirExists(t, dir)

	raw, err := os.ReadFile(filepath.Join(dir, "wiki_identity.json"))
	require.NoError(t, err)
	require.NotContains(t, strings.ToLower(string(raw)), "password")
	var gotIdentity WikiIdentity
	require.NoError(t, json.Unmarshal(raw, &gotIdentity))
	require.Equal(t, identity, gotIdentity)

	// Both entries land in one session file named by the first action's timestamp (colons → hyphens).
	content, err := os.ReadFile(filepath.Join(dir, "2026-02-08T15-30-00Z.jsonl"))
	require.NoError(t, err)
	lines := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
	require.Len(t, lines, 2)
	var first JournalEntry
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &first))
	require.Equal(t, "Delete A", first.Operation.Description)
	require.Equal(t, "success", first.Result)
}

func TestSessionJournalTimestampTiebreaker(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ts := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	identity := WikiIdentity{URL: "https://example.org/"}

	require.NoError(t, NewSessionJournal(root, identity, fixedClock(ts)).Append(JournalEntry{Timestamp: ts}))
	require.NoError(t, NewSessionJournal(root, identity, fixedClock(ts)).Append(JournalEntry{Timestamp: ts}))

	dir := wikiDir(t, root, identity.URL)
	require.FileExists(t, filepath.Join(dir, "2026-03-01T09-00-00Z.jsonl"))
	require.FileExists(t, filepath.Join(dir, "2026-03-01T09-00-00Z-2.jsonl"))
}

func TestSessionJournalNilReceiverIsNoOp(t *testing.T) {
	t.Parallel()

	var j *SessionJournal
	require.NoError(t, j.Append(JournalEntry{}))
}
