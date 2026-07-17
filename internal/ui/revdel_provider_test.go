package ui

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseRevision(t *testing.T) {
	t.Parallel()

	// A plain visible revision; the page's lastrevid marks it as the current one.
	current := parseRevision(map[string]any{
		"revid":     float64(104),
		"timestamp": "2026-07-04T12:00:00Z",
		"user":      "Alice",
		"comment":   "fix typo",
	}, 104)
	require.Equal(t, int64(104), current.ID)
	require.True(t, current.Current)
	require.Equal(t, "Alice", current.User)
	require.Equal(t, "fix typo", current.Comment)
	require.Equal(t, "2026-07-04T12:00:00Z", current.Timestamp.Format("2006-01-02T15:04:05Z"))
	require.False(t, current.ContentHidden)

	// fv2 emits the *hidden keys (as true) only when hidden; sha1hidden stands in for the content's visibility.
	hidden := parseRevision(map[string]any{
		"revid":         float64(103),
		"timestamp":     "2026-07-03T12:00:00Z",
		"userhidden":    true,
		"commenthidden": true,
		"sha1hidden":    true,
		"suppressed":    true,
	}, 104)
	require.False(t, hidden.Current)
	require.True(t, hidden.UserHidden)
	require.True(t, hidden.CommentHidden)
	require.True(t, hidden.ContentHidden)
	require.True(t, hidden.Suppressed)
	require.Empty(t, hidden.User)
}

func TestFirstPageRejectsMissingAndInvalid(t *testing.T) {
	t.Parallel()

	page, err := firstPage(map[string]any{"query": map[string]any{"pages": []any{
		map[string]any{"title": "Apple", "lastrevid": float64(7)},
	}}})
	require.NoError(t, err)
	require.Equal(t, "Apple", page["title"])

	_, err = firstPage(map[string]any{"query": map[string]any{"pages": []any{
		map[string]any{"title": "Nope", "missing": true},
	}}})
	require.ErrorIs(t, err, errPageMissing)

	_, err = firstPage(map[string]any{"query": map[string]any{"pages": []any{
		map[string]any{"title": "|bad|", "invalid": true},
	}}})
	require.ErrorIs(t, err, errPageMissing)

	_, err = firstPage(map[string]any{})
	require.ErrorIs(t, err, errPageMissing)
}

// Continuation offsets and IDs may decode as float64, json.Number, or (defensively) strings; anything else is 0.
func TestJSONInt64(t *testing.T) {
	t.Parallel()

	require.Equal(t, int64(42), jsonInt64(float64(42)))
	require.Equal(t, int64(42), jsonInt64(json.Number("42")))
	require.Equal(t, int64(42), jsonInt64(" 42 "))
	require.Equal(t, int64(0), jsonInt64(nil))
	require.Equal(t, int64(0), jsonInt64(true))
}
