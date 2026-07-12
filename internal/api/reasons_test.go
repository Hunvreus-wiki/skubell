package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseReasonDropdownsFromFixture(t *testing.T) {
	t.Parallel()

	fixture, err := LoadFixture("allmessages_reasons_sample.json")
	require.NoError(t, err)

	dropdowns, err := parseReasonDropdownResponse(fixture)
	require.NoError(t, err)

	deleteDropdown := dropdowns[reasonDelete]
	require.Equal(t, reasonDelete, deleteDropdown.Action)
	require.Len(t, deleteDropdown.Categories, 2)
	require.Equal(t, "Common deletion reasons", deleteDropdown.Categories[0].Label)
	require.Equal(t, []string{
		"Off-topic: please read the admissibility criteria",
		"Test, please use the sandbox",
		"No meaningful content",
	}, deleteDropdown.Categories[0].Reasons)
	require.Equal(t, "Speedy deletion criteria", deleteDropdown.Categories[1].Label)
	require.Contains(t, deleteDropdown.Categories[1].Reasons, "CSD2: vandalism")
}

func TestFetchReasonDropdownsAdminLanguageModes(t *testing.T) {
	t.Parallel()

	var seenAMLang string
	var seenAMCustomised string

	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !assert.NoError(t, request.ParseForm()) {
			return
		}
		seenAMLang = request.Form.Get("amlang")
		seenAMCustomised = request.Form.Get("amcustomised")
		_, _ = writer.Write(
			[]byte(
				`{"query":{"allmessages":[{"name":"deletereason-dropdown","content":"* Common\n** Cleanup"},{"name":"ipbreason-dropdown","content":"* Block\n** Vandalism"},{"name":"protect-dropdown","content":"* Protect\n** Edit war"},{"name":"revdelete-reason-dropdown","content":"* Revdel\n** Attack name"}]}}`,
			),
		)
	}))
	defer testServer.Close()

	client, err := NewClient(200, 1, nil)
	require.NoError(t, err)

	_, err = FetchReasonDropdowns(client, testServer.URL, "")
	require.NoError(t, err)
	require.Empty(t, seenAMLang)

	_, err = FetchReasonDropdowns(client, testServer.URL, "en")
	require.NoError(t, err)
	require.Equal(t, "en", seenAMLang)
	require.Equal(t, "unmodified", seenAMCustomised)
}
