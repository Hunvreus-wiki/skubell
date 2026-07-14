package ui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/config"
)

// fakeStore is an in-memory CredentialStore whose Store can be made to fail (simulating a canceled keyring dialog).
type fakeStore struct {
	stored   map[string][]byte
	storeErr error
	deleted  []string
}

func (f *fakeStore) Store(name string, credential []byte) error {
	if f.storeErr != nil {
		return f.storeErr
	}
	if f.stored == nil {
		f.stored = map[string][]byte{}
	}
	f.stored[name] = credential
	return nil
}

func (f *fakeStore) Retrieve(name string) ([]byte, error) { return f.stored[name], nil }

func (f *fakeStore) Delete(name string) error {
	delete(f.stored, name)
	f.deleted = append(f.deleted, name)
	return nil
}

func (f *fakeStore) IsAvailable() bool { return true }

func newWikiEntry() config.WikiEntry {
	return config.WikiEntry{
		Name:       "My Wiki",
		Username:   "Admin@Bot",
		APIURL:     "https://example.org/api.php",
		Credential: keyringCredentialMarker,
	}
}

// The reported bug: canceling the keyring dialog left the configuration created but the keyring empty. persistWiki
// writes the keyring first, so a canceled/failed keyring write must leave nothing created — no in-memory wiki and no
// config file on disk — letting the user try again.
func TestPersistWikiKeyringCancelCreatesNothing(t *testing.T) {
	t.Parallel()
	store := &fakeStore{storeErr: errors.New("keyring canceled")}
	app := &App{store: store, configPath: filepath.Join(t.TempDir(), "config.json")}

	err := app.persistWiki(newWikiEntry(), "secret", WikiSettingsModeCreate, "")

	require.Error(t, err)
	require.Empty(t, app.config.Wikis, "no wiki should be added when the keyring write is canceled")
	require.NoFileExists(t, app.configPath, "no config file should be written when the keyring write is canceled")
	require.Empty(t, store.stored)
}

func TestPersistWikiSuccessStoresThenSaves(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	app := &App{store: store, configPath: filepath.Join(t.TempDir(), "config.json")}

	require.NoError(t, app.persistWiki(newWikiEntry(), "secret", WikiSettingsModeCreate, ""))

	require.Len(t, app.config.Wikis, 1)
	require.Equal(t, "My Wiki", app.config.Wikis[0].Name)
	require.Equal(t, []byte("secret"), store.stored["My Wiki"])
	require.FileExists(t, app.configPath)
}

// If the keyring write succeeds but the config write fails, the just-created keyring entry must be rolled back so no
// orphaned credential is left behind.
func TestPersistWikiRollsBackKeyringWhenConfigSaveFails(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	// A file where the config's parent directory should be makes config.Save's MkdirAll fail.
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))
	app := &App{store: store, configPath: filepath.Join(blocker, "config.json")}

	err := app.persistWiki(newWikiEntry(), "secret", WikiSettingsModeCreate, "")

	require.Error(t, err)
	require.Empty(t, app.config.Wikis)
	require.Empty(t, store.stored, "the orphaned keyring entry should be rolled back")
	require.Contains(t, store.deleted, "My Wiki")
}
