package security

// CredentialStore abstracts secure credential storage.
type CredentialStore interface {
	Store(wikiName string, credential []byte) error
	Retrieve(wikiName string) ([]byte, error)
	Delete(wikiName string) error
	IsAvailable() bool
}

const (
	serviceName         = "skubell"
	credentialKeyPrefix = "skubell:"
)
