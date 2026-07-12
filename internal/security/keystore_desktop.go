//go:build !android

package security

import (
	"errors"
	"fmt"

	"github.com/99designs/keyring"
)

type keyringBackend interface {
	Set(item keyring.Item) error
	Get(key string) (keyring.Item, error)
	Remove(key string) error
}

var openDesktopKeyring = func() (keyringBackend, error) {
	ring, err := keyring.Open(keyring.Config{ServiceName: serviceName})
	if err != nil {
		return nil, fmt.Errorf("open desktop credential store: %w", err)
	}
	return ring, nil
}

// DesktopCredentialStore stores credentials in the native OS keyring.
type DesktopCredentialStore struct {
	ring keyringBackend
}

func NewDesktopCredentialStore() (*DesktopCredentialStore, error) {
	ring, err := openDesktopKeyring()
	if err != nil {
		return nil, err
	}
	return &DesktopCredentialStore{ring: ring}, nil
}

func (s *DesktopCredentialStore) Store(wikiName string, credential []byte) error {
	if s.ring == nil {
		return errors.New("credential store is not available")
	}

	return s.ring.Set(keyring.Item{
		Key:  credentialKeyPrefix + wikiName,
		Data: credential,
	})
}

func (s *DesktopCredentialStore) Retrieve(wikiName string) ([]byte, error) {
	if s.ring == nil {
		return nil, errors.New("credential store is not available")
	}

	item, err := s.ring.Get(credentialKeyPrefix + wikiName)
	if err != nil {
		return nil, err
	}

	return item.Data, nil
}

func (s *DesktopCredentialStore) Delete(wikiName string) error {
	if s.ring == nil {
		return errors.New("credential store is not available")
	}

	return s.ring.Remove(credentialKeyPrefix + wikiName)
}

func (s *DesktopCredentialStore) IsAvailable() bool {
	if s == nil {
		return false
	}
	return s.ring != nil
}
