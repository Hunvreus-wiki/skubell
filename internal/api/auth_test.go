package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoginSuccess(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !assert.NoError(t, request.ParseForm()) {
			return
		}

		action := request.Form.Get("action")
		switch action {
		case "query":
			assert.Equal(t, "tokens", request.Form.Get("meta"))
			assert.Equal(t, "login", request.Form.Get("type"))
			_, _ = writer.Write([]byte(`{"query":{"tokens":{"logintoken":"login-token"}}}`))
		case "login":
			assert.Equal(t, "TestAdmin@SkubellTest", request.Form.Get("lgname"))
			assert.Equal(t, "secret", request.Form.Get("lgpassword"))
			assert.Equal(t, "login-token", request.Form.Get("lgtoken"))
			_, _ = writer.Write([]byte(`{"login":{"result":"Success","lgusername":"TestAdmin@SkubellTest"}}`))
		default:
			http.Error(writer, "unexpected action", http.StatusBadRequest)
		}
	}))
	defer testServer.Close()

	client, err := NewClient(1000, 3, nil)
	require.NoError(t, err)

	username, err := Login(client, testServer.URL, "TestAdmin@SkubellTest", "secret")
	require.NoError(t, err)
	require.Equal(t, "TestAdmin@SkubellTest", username)
}

func TestLoginWrongCredentials(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !assert.NoError(t, request.ParseForm()) {
			return
		}

		action := request.Form.Get("action")
		switch action {
		case "query":
			_, _ = writer.Write([]byte(`{"query":{"tokens":{"logintoken":"login-token"}}}`))
		case "login":
			_, _ = writer.Write(
				[]byte(`{"login":{"result":"Failed","reason":"Incorrect username or password entered."}}`),
			)
		default:
			http.Error(writer, "unexpected action", http.StatusBadRequest)
		}
	}))
	defer testServer.Close()

	client, err := NewClient(1000, 3, nil)
	require.NoError(t, err)

	_, err = Login(client, testServer.URL, "TestAdmin@SkubellTest", "wrong")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrAuthenticationFailed)
}

func TestLoginBotPasswordsDisabled(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !assert.NoError(t, request.ParseForm()) {
			return
		}

		action := request.Form.Get("action")
		switch action {
		case "query":
			_, _ = writer.Write([]byte(`{"query":{"tokens":{"logintoken":"login-token"}}}`))
		case "login":
			_, _ = writer.Write(
				[]byte(`{"login":{"result":"Failed","reason":"Bot passwords are disabled on this wiki."}}`),
			)
		default:
			http.Error(writer, "unexpected action", http.StatusBadRequest)
		}
	}))
	defer testServer.Close()

	client, err := NewClient(1000, 3, nil)
	require.NoError(t, err)

	_, err = Login(client, testServer.URL, "TestAdmin@SkubellTest", "secret")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrBotPasswordsDisabled)
}
