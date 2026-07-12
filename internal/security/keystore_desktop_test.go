//go:build !android

package security

import (
	"errors"
	"testing"

	"github.com/99designs/keyring"
	"github.com/stretchr/testify/require"
)

type mockKeyring struct {
	entries           map[string][]byte
	lastSetKey        string
	lastGetKey        string
	lastRemoveKey     string
	lastSetData       []byte
	removeCallCount   int
	retrieveCallCount int
}

func (m *mockKeyring) Set(item keyring.Item) error {
	if m.entries == nil {
		m.entries = map[string][]byte{}
	}
	m.lastSetKey = item.Key
	m.lastSetData = append([]byte{}, item.Data...)
	m.entries[item.Key] = append([]byte{}, item.Data...)
	return nil
}

func (m *mockKeyring) Get(key string) (keyring.Item, error) {
	m.lastGetKey = key
	m.retrieveCallCount++
	value, ok := m.entries[key]
	if !ok {
		return keyring.Item{}, errors.New("missing key")
	}
	return keyring.Item{Key: key, Data: append([]byte{}, value...)}, nil
}

func (m *mockKeyring) Remove(key string) error {
	m.lastRemoveKey = key
	m.removeCallCount++
	delete(m.entries, key)
	return nil
}

func TestDesktopCredentialStoreLifecycle(t *testing.T) {
	mockRing := &mockKeyring{entries: map[string][]byte{}}
	store := &DesktopCredentialStore{ring: mockRing}

	require.True(t, store.IsAvailable())
	require.NoError(t, store.Store("TestWiki", []byte("secret")))
	require.Equal(t, credentialKeyPrefix+"TestWiki", mockRing.lastSetKey)
	require.Equal(t, []byte("secret"), mockRing.lastSetData)

	credential, err := store.Retrieve("TestWiki")
	require.NoError(t, err)
	require.Equal(t, []byte("secret"), credential)
	require.Equal(t, credentialKeyPrefix+"TestWiki", mockRing.lastGetKey)

	require.NoError(t, store.Delete("TestWiki"))
	require.Equal(t, credentialKeyPrefix+"TestWiki", mockRing.lastRemoveKey)
	_, err = store.Retrieve("TestWiki")
	require.Error(t, err)
}

func TestNewDesktopCredentialStoreUnavailable(t *testing.T) {
	original := openDesktopKeyring
	t.Cleanup(func() {
		openDesktopKeyring = original
	})

	openDesktopKeyring = func() (keyringBackend, error) {
		return nil, errors.New("backend unavailable")
	}

	store, err := NewDesktopCredentialStore()
	require.Error(t, err)
	require.Nil(t, store)
}

func TestDesktopCredentialStoreIsAvailableFalseCases(t *testing.T) {
	var nilStore *DesktopCredentialStore
	require.False(t, nilStore.IsAvailable())

	store := &DesktopCredentialStore{}
	require.False(t, store.IsAvailable())
}
