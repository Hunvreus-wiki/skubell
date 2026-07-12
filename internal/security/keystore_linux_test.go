//go:build linux && !android

package security

import (
	"errors"
	"testing"

	"github.com/99designs/keyring"
	"github.com/stretchr/testify/require"
)

type stubKeyring struct{}

func (stubKeyring) Get(string) (keyring.Item, error) {
	return keyring.Item{}, nil
}

func (stubKeyring) GetMetadata(string) (keyring.Metadata, error) {
	return keyring.Metadata{}, nil
}

func (stubKeyring) Set(keyring.Item) error {
	return nil
}

func (stubKeyring) Remove(string) error {
	return nil
}

func (stubKeyring) Keys() ([]string, error) {
	return nil, nil
}

func TestEnsureStartupCredentialStoreAvailabilitySuccess(t *testing.T) {
	original := openSecretServiceKeyring
	t.Cleanup(func() {
		openSecretServiceKeyring = original
	})

	called := false
	openSecretServiceKeyring = func(config keyring.Config) (keyring.Keyring, error) {
		called = true
		require.Equal(t, serviceName, config.ServiceName)
		require.Equal(t, []keyring.BackendType{keyring.SecretServiceBackend}, config.AllowedBackends)
		return stubKeyring{}, nil
	}

	require.NoError(t, EnsureStartupCredentialStoreAvailability())
	require.True(t, called)
}

func TestEnsureStartupCredentialStoreAvailabilityFailure(t *testing.T) {
	original := openSecretServiceKeyring
	t.Cleanup(func() {
		openSecretServiceKeyring = original
	})

	openSecretServiceKeyring = func(config keyring.Config) (keyring.Keyring, error) {
		require.Equal(t, serviceName, config.ServiceName)
		return nil, errors.New("dbus unavailable")
	}

	err := EnsureStartupCredentialStoreAvailability()
	require.Error(t, err)
	require.Contains(t, err.Error(), "Secret Service provider")
	require.Contains(t, err.Error(), "dbus unavailable")
}
