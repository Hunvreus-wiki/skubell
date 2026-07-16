package api

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/ops"
)

func TestProtectTranslatorBuildsProtections(t *testing.T) {
	t.Parallel()

	op := ops.Operation{
		Type: ops.OpProtectPage,
		Params: map[string]string{
			"title":        "Template:Infobox",
			"protect_edit": "sysop",
			"expiry_edit":  "infinite",
			"protect_move": "sysop",
			"expiry_move":  "2026-12-31T00:00:00Z",
			"cascade":      "true",
			"reason":       "High-traffic template",
		},
	}

	calls, err := ProtectTranslator{}.Translate(op, WikiCapabilities{})
	require.NoError(t, err)
	require.Len(t, calls, 1)
	require.Equal(t, "protect", calls[0].Action)
	require.Equal(t, "POST", calls[0].Method)
	require.Equal(t, "Template:Infobox", calls[0].Params["title"])
	// Types emitted in the canonical order edit|create|move|upload — here edit then move.
	require.Equal(t, "edit=sysop|move=sysop", calls[0].Params["protections"])
	require.Equal(t, "infinite|2026-12-31T00:00:00Z", calls[0].Params["expiry"])
	require.Equal(t, "1", calls[0].Params["cascade"])
	require.Equal(t, "High-traffic template", calls[0].Params["reason"])
}

func TestProtectTranslatorUnprotectAndDefaults(t *testing.T) {
	t.Parallel()

	// Empty level means "all" (remove protection); missing expiry defaults to infinite; no cascade key omits cascade.
	op := ops.Operation{
		Type: ops.OpProtectPage,
		Params: map[string]string{
			"title":        "Old page",
			"protect_edit": "",
			"protect_move": "all",
		},
	}

	calls, err := ProtectTranslator{}.Translate(op, WikiCapabilities{})
	require.NoError(t, err)
	require.Equal(t, "edit=all|move=all", calls[0].Params["protections"])
	require.Equal(t, "infinite|infinite", calls[0].Params["expiry"])
	_, hasCascade := calls[0].Params["cascade"]
	require.False(t, hasCascade)
	_, hasReason := calls[0].Params["reason"]
	require.False(t, hasReason)
}

// A wiki-specific custom restriction type the planner preserved (beyond edit/create/move/upload) must still be
// emitted, or action=protect's whole-set replacement would drop it — the very removal the preservation guards against.
func TestProtectTranslatorEmitsCustomTypes(t *testing.T) {
	t.Parallel()

	op := ops.Operation{
		Type: ops.OpProtectPage,
		Params: map[string]string{
			"title":        "Board:Discussion",
			"protect_edit": "sysop",
			"expiry_edit":  "infinite",
			"protect_flow": "sysop", // a custom $wgRestrictionTypes action
			"expiry_flow":  "infinite",
		},
	}

	calls, err := ProtectTranslator{}.Translate(op, WikiCapabilities{})
	require.NoError(t, err)
	// Canonical types come first (edit), custom types after in sorted order (flow).
	require.Equal(t, "edit=sysop|flow=sysop", calls[0].Params["protections"])
	require.Equal(t, "infinite|infinite", calls[0].Params["expiry"])
}

func TestProtectTranslatorRejectsBadInput(t *testing.T) {
	t.Parallel()

	_, err := ProtectTranslator{}.Translate(ops.Operation{Type: ops.OpDeletePage}, WikiCapabilities{})
	require.Error(t, err)

	noTitle := ops.Operation{Type: ops.OpProtectPage, Params: map[string]string{}}
	_, err = ProtectTranslator{}.Translate(noTitle, WikiCapabilities{})
	require.Error(t, err) // missing title

	_, err = ProtectTranslator{}.Translate(
		ops.Operation{Type: ops.OpProtectPage, Params: map[string]string{"title": "X"}}, WikiCapabilities{})
	require.Error(t, err) // no protections
}
