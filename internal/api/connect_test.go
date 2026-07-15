package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/config"
)

func TestConnectFullSequence(t *testing.T) {
	t.Parallel()

	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !assert.NoError(t, request.ParseForm()) {
			return
		}
		action := request.Form.Get("action")
		switch action {
		case "query":
			switch request.Form.Get("meta") {
			case "tokens":
				if request.Form.Get("type") == "login" {
					_, _ = writer.Write([]byte(`{"query":{"tokens":{"logintoken":"login-token"}}}`))
					return
				}
				if request.Form.Get("type") == "csrf" {
					_, _ = writer.Write([]byte(`{"query":{"tokens":{"csrftoken":"csrf-token"}}}`))
					return
				}
			case "siteinfo":
				_, _ = writer.Write(
					[]byte(
						`{"query":{"general":{"generator":"MediaWiki 1.45.1"},"namespaces":[{"id":0,"name":""},{"id":1,"name":"Talk"}],"extensions":[{"name":"Abuse Filter"}]}}`,
					),
				)
				return
			case "userinfo":
				_, _ = writer.Write(
					[]byte(
						`{"query":{"userinfo":{"rights":["read","delete","apihighlimits"],"groups":["*","sysop"]}}}`,
					),
				)
				return
			case "allmessages":
				_, _ = writer.Write(
					[]byte(
						`{"query":{"allmessages":[{"name":"deletereason-dropdown","content":"* Common\n** Cleanup"},{"name":"ipbreason-dropdown","content":"* Block\n** Vandalism"},{"name":"protect-dropdown","content":"* Protect\n** Edit war"},{"name":"revdelete-reason-dropdown","content":"* Revdel\n** Attack name"}]}}`,
					),
				)
				return
			}
		case "login":
			_, _ = writer.Write([]byte(`{"login":{"result":"Success","lgusername":"TestAdmin"}}`))
			return
		}
		http.Error(writer, "unexpected request", http.StatusBadRequest)
	}))
	defer testServer.Close()

	client, err := NewClient(200, 1, nil)
	require.NoError(t, err)

	result, err := Connect(client, config.WikiEntry{
		Name:       "Local",
		Farm:       "custom",
		APIURL:     testServer.URL,
		Username:   "TestAdmin@SkubellTest",
		Credential: "secret",
	})
	require.NoError(t, err)
	require.Equal(t, "TestAdmin", result.Username)
	require.Equal(t, "1.45.1", result.Capabilities.Version)
	require.True(t, result.Capabilities.HasHighLimits)
	require.Contains(t, result.Capabilities.UserRights, "delete")
	require.NotEmpty(t, result.ReasonDropdown[ReasonActionDelete].Categories)
}

func TestConnectWrongCustomURLReturnsGuidance(t *testing.T) {
	t.Parallel()

	client, err := NewClient(200, 0, nil)
	require.NoError(t, err)

	_, err = Connect(client, config.WikiEntry{
		Name:       "Broken",
		Farm:       "custom",
		APIURL:     "http://127.0.0.1:9/api.php",
		Username:   "TestAdmin@SkubellTest",
		Credential: "secret",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "Could not reach the MediaWiki API.")
	require.Contains(t, err.Error(), "api.php")
}
