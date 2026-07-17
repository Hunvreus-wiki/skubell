package api

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/ops"
)

func TestRevDelTranslatorBuildsRevisionDelete(t *testing.T) {
	t.Parallel()

	op := ops.Operation{
		Type: ops.OpRevisionDelete,
		Params: map[string]string{
			"ids":    "12345|12346",
			"hide":   "user|content", // deliberately out of order and duplicated below
			"show":   "comment",
			"reason": "Vandalism",
		},
	}

	calls, err := RevDelTranslator{}.Translate(op, WikiCapabilities{})
	require.NoError(t, err)
	require.Len(t, calls, 1)
	require.Equal(t, "revisiondelete", calls[0].Action)
	require.Equal(t, "POST", calls[0].Method)
	require.Equal(t, "revision", calls[0].Params["type"])
	require.Equal(t, "12345|12346", calls[0].Params["ids"])
	// Fields re-emitted in canonical order content|comment|user regardless of the input order.
	require.Equal(t, "content|user", calls[0].Params["hide"])
	require.Equal(t, "comment", calls[0].Params["show"])
	// Granular changes state the API default explicitly: the suppression bit is left alone.
	require.Equal(t, "nochange", calls[0].Params["suppress"])
	require.Equal(t, "Vandalism", calls[0].Params["reason"])
}

// Suppression is a level on the fields being hidden, passed through as the API's suppress parameter.
func TestRevDelTranslatorPassesSuppressLevel(t *testing.T) {
	t.Parallel()

	op := ops.Operation{
		Type: ops.OpRevisionDelete,
		Params: map[string]string{
			"ids":      "99",
			"hide":     "content|user",
			"suppress": "yes",
			"reason":   "Oversight request",
		},
	}

	calls, err := RevDelTranslator{}.Translate(op, WikiCapabilities{})
	require.NoError(t, err)
	require.Len(t, calls, 1)
	require.Equal(t, "revisiondelete", calls[0].Action)
	require.Equal(t, "content|user", calls[0].Params["hide"])
	require.Equal(t, "yes", calls[0].Params["suppress"])
	require.Equal(t, "Oversight request", calls[0].Params["reason"])
	_, hasShow := calls[0].Params["show"]
	require.False(t, hasShow)

	op.Params["suppress"] = "no"
	calls, err = RevDelTranslator{}.Translate(op, WikiCapabilities{})
	require.NoError(t, err)
	require.Equal(t, "no", calls[0].Params["suppress"])
}

func TestRevDelTranslatorOmitsEmptyReason(t *testing.T) {
	t.Parallel()

	op := ops.Operation{
		Type:   ops.OpRevisionDelete,
		Params: map[string]string{"ids": "7", "hide": "content", "reason": "   "},
	}

	calls, err := RevDelTranslator{}.Translate(op, WikiCapabilities{})
	require.NoError(t, err)
	_, hasReason := calls[0].Params["reason"]
	require.False(t, hasReason)
}

func TestRevDelTranslatorRejectsInvalidOperations(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		op   ops.Operation
	}{
		{"missing ids", ops.Operation{Type: ops.OpRevisionDelete, Params: map[string]string{"hide": "content"}}},
		{"no changes", ops.Operation{Type: ops.OpRevisionDelete, Params: map[string]string{"ids": "1"}}},
		{"unknown field", ops.Operation{
			Type: ops.OpRevisionDelete, Params: map[string]string{"ids": "1", "hide": "sha1"},
		}},
		{"hide and show overlap", ops.Operation{
			Type: ops.OpRevisionDelete, Params: map[string]string{"ids": "1", "hide": "user", "show": "user"},
		}},
		{"invalid suppress level", ops.Operation{
			Type: ops.OpRevisionDelete, Params: map[string]string{"ids": "1", "hide": "user", "suppress": "maybe"},
		}},
		{"legacy suppress op type", ops.Operation{Type: ops.OpSuppress, Params: map[string]string{"ids": "1"}}},
		{"wrong type", ops.Operation{Type: ops.OpDeletePage, Params: map[string]string{"ids": "1"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := RevDelTranslator{}.Translate(tc.op, WikiCapabilities{})
			require.Error(t, err)
		})
	}
}

// MediaWiki hides per-item failures under an overall (and even per-item) "Success"; the validator digs the
// "errors" list out. Payload shapes taken from a live 1.46 response (errorformat=plaintext).
func TestValidateRevisionDeleteResponse(t *testing.T) {
	t.Parallel()

	require.Nil(t, ValidateRevisionDeleteResponse(map[string]any{
		"revisiondelete": map[string]any{"status": "Success", "items": []any{
			map[string]any{"status": "Success", "id": float64(210)},
		}},
	}))

	apiErr := ValidateRevisionDeleteResponse(map[string]any{
		"revisiondelete": map[string]any{"status": "Success", "items": []any{
			map[string]any{"status": "Success", "id": float64(209)},
			map[string]any{
				"status": "Success", "id": float64(210),
				"errors": []any{map[string]any{
					"code": "revdelete-modify-no-access",
					"*":    `This item has been marked "restricted". You do not have access to it.`,
				}},
			},
		}},
	})
	require.NotNil(t, apiErr)
	require.Equal(t, "revdelete-modify-no-access", apiErr.Code)
	require.Contains(t, apiErr.Info, "restricted")

	// A warnings list (e.g. revdelete-no-change) is not a failure.
	require.Nil(t, ValidateRevisionDeleteResponse(map[string]any{
		"revisiondelete": map[string]any{"status": "Success", "items": []any{
			map[string]any{"status": "Success", "warnings": []any{map[string]any{"code": "revdelete-no-change"}}},
		}},
	}))
}

func TestFriendlyRevDelErrorMessage(t *testing.T) {
	t.Parallel()

	require.Empty(t, FriendlyRevDelErrorMessage(nil))
	require.Equal(t, "You cannot change the visibility of the current revision.",
		FriendlyRevDelErrorMessage(&APIError{
			Code: "revdelete-only-current",
			Info: "You cannot change the visibility of the current revision.",
		}))
	require.Equal(t, "somecode", FriendlyRevDelErrorMessage(&APIError{Code: "somecode"}))
}
