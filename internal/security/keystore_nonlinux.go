//go:build !android && !linux

package security

// EnsureStartupCredentialStoreAvailability is a no-op on non-Linux desktop platforms.
func EnsureStartupCredentialStoreAvailability() error {
	return nil
}
