//go:build !android && linux

package security

import (
	"fmt"

	"github.com/99designs/keyring"
)

const secretServiceUnavailableMessage = "Skubell requires a Secret Service provider (GNOME Keyring, KWallet...) " +
	"to store credentials securely. Please install and enable one, or start Skubell within a desktop " +
	"session that provides one."

var openSecretServiceKeyring = func(config keyring.Config) (keyring.Keyring, error) {
	return keyring.Open(config)
}

var probeSecretService = func() error {
	_, err := openSecretServiceKeyring(keyring.Config{
		ServiceName:     serviceName,
		AllowedBackends: []keyring.BackendType{keyring.SecretServiceBackend},
	})
	if err != nil {
		return fmt.Errorf("%s: %w", secretServiceUnavailableMessage, err)
	}
	return nil
}

// EnsureStartupCredentialStoreAvailability enforces Linux Secret Service requirements.
func EnsureStartupCredentialStoreAvailability() error {
	if err := probeSecretService(); err != nil {
		return err
	}
	return nil
}
