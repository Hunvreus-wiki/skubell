package api

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Hunvreus-wiki/skubell/internal/ops"
)

// DeleteTranslator translates deletion operations.
type DeleteTranslator struct{}

// Translate converts a delete operation into an API call.
func (t DeleteTranslator) Translate(op ops.Operation, caps WikiCapabilities) ([]APICall, error) {
	if op.Type != ops.OpDeletePage {
		return nil, fmt.Errorf("unsupported operation type: %s", op.Type)
	}

	title := strings.TrimSpace(op.Params["title"])
	if title == "" {
		return nil, errors.New("delete operation missing title")
	}

	params := map[string]string{
		"title": title,
	}
	if reason := strings.TrimSpace(op.Params["reason"]); reason != "" {
		params["reason"] = reason
	}
	if op.Params["delete_talk"] == "true" {
		params["deletetalk"] = "1"
	}

	return []APICall{
		{
			Action: "delete",
			Method: "POST",
			Params: params,
		},
	}, nil
}
