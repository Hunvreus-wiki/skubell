package api

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/ops"
)

func TestDeleteTranslator(t *testing.T) {
	t.Parallel()

	op := ops.Operation{
		Type: ops.OpDeletePage,
		Params: map[string]string{
			"title":  "Foo",
			"reason": "Cleanup",
		},
	}

	translator := DeleteTranslator{}
	calls, err := translator.Translate(op, WikiCapabilities{})
	require.NoError(t, err)
	require.Len(t, calls, 1)
	require.Equal(t, "delete", calls[0].Action)
	require.Equal(t, "POST", calls[0].Method)
	require.Equal(t, "Foo", calls[0].Params["title"])
	require.Equal(t, "Cleanup", calls[0].Params["reason"])
}

func TestDeleteTranslatorIncludesDeleteTalk(t *testing.T) {
	t.Parallel()

	op := ops.Operation{
		Type: ops.OpDeletePage,
		Params: map[string]string{
			"title":       "Foo",
			"delete_talk": "true",
		},
	}

	translator := DeleteTranslator{}
	calls, err := translator.Translate(op, WikiCapabilities{})
	require.NoError(t, err)
	require.Len(t, calls, 1)
	require.Equal(t, "1", calls[0].Params["deletetalk"])
}
