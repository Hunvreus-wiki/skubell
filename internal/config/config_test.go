package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")

	cfg := Load(path)

	require.Equal(t, DefaultConfig(), cfg)
}

func TestLoadValidFileParsesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{
  "wikis": [
    {
      "name": "Local Wiki",
      "farm": "custom",
      "api_url": "http://localhost:8080/w/",
      "username": "Admin@Bot",
      "credential": "@keyring",
      "write_throttle_ms": 250,
      "max_retries": 2
    }
  ],
  "preferences": {
    "theme": "dark",
    "ui_scale": 1.2,
    "confirm_before_batch": false,
    "batch_size_warning_threshold": 20
  }
}`

	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	cfg := Load(path)

	require.Len(t, cfg.Wikis, 1)
	require.Equal(t, "Local Wiki", cfg.Wikis[0].Name)
	require.Equal(t, 250, cfg.Wikis[0].WriteThrottleMS)
	require.Equal(t, 2, cfg.Wikis[0].MaxRetries)
	require.Equal(t, "dark", cfg.Preferences.Theme)
	require.InDelta(t, 1.2, cfg.Preferences.UIScale, 0.000001)
	require.False(t, cfg.Preferences.ConfirmBeforeBatch)
	require.Equal(t, 20, cfg.Preferences.BatchSizeWarningThreshold)
}

func TestLoadCorruptFileReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(path, []byte("{not-json"), 0o644))

	cfg := Load(path)

	require.Equal(t, DefaultConfig(), cfg)
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")

	expected := DefaultConfig()
	expected.Wikis = []WikiEntry{
		{
			Name:            "Test Wiki",
			Farm:            "custom",
			APIURL:          "http://localhost:8080/w/",
			Username:        "Admin@Bot",
			Credential:      "@keyring",
			WriteThrottleMS: 500,
			MaxRetries:      5,
		},
	}
	expected.Preferences.Language = "fr"
	expected.Preferences.Theme = "light"
	expected.Preferences.UIScale = 1.25
	expected.Preferences.JournalDirectory = "/tmp/journal"
	expected.Preferences.LastConnectedWiki = "Test Wiki"

	require.NoError(t, Save(path, expected))

	actual := Load(path)
	require.Equal(t, expected, actual)
}

func TestLoadAppliesWikiDefaultsAndMinimums(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{
  "wikis": [
    {
      "name": "Wiki A",
      "farm": "custom",
      "api_url": "http://localhost:8080/w/",
      "username": "Admin@Bot",
      "credential": "@keyring",
      "write_throttle_ms": 0,
      "max_retries": 0
    },
    {
      "name": "Wiki B",
      "farm": "custom",
      "api_url": "http://localhost:8081/w/",
      "username": "Admin@Bot",
      "credential": "@keyring",
      "write_throttle_ms": 150,
      "max_retries": 1
    }
  ],
  "preferences": {
    "theme": "system",
    "ui_scale": 1.0,
    "batch_size_warning_threshold": 50
  }
}`

	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	cfg := Load(path)
	require.Len(t, cfg.Wikis, 2)

	require.Equal(t, 1000, cfg.Wikis[0].WriteThrottleMS)
	require.Equal(t, 3, cfg.Wikis[0].MaxRetries)
	require.Equal(t, 200, cfg.Wikis[1].WriteThrottleMS)
	require.Equal(t, 1, cfg.Wikis[1].MaxRetries)
}

func TestDefaultPathByOS(t *testing.T) {
	originalOS := runtimeGOOS
	originalUserConfigDir := userConfigDir
	t.Cleanup(func() {
		runtimeGOOS = originalOS
		userConfigDir = originalUserConfigDir
	})

	runtimeGOOS = "linux"
	userConfigDir = func() (string, error) {
		return "/home/test/.config", nil
	}
	linuxPath, err := DefaultPath()
	require.NoError(t, err)
	require.Equal(t, filepath.Join("/home/test/.config", "skubell", "config.json"), linuxPath)

	runtimeGOOS = "darwin"
	userConfigDir = func() (string, error) {
		return "/Users/test/Library/Application Support", nil
	}
	macosPath, err := DefaultPath()
	require.NoError(t, err)
	require.Equal(t, filepath.Join("/Users/test/Library/Application Support", "Skubell", "config.json"), macosPath)

	runtimeGOOS = "windows"
	userConfigDir = func() (string, error) {
		return "/Users/test/AppData/Roaming", nil
	}
	windowsPath, err := DefaultPath()
	require.NoError(t, err)
	require.Equal(t, filepath.Join("/Users/test/AppData/Roaming", "Skubell", "config.json"), windowsPath)
}

func TestDefaultJournalDirByOS(t *testing.T) {
	originalOS := runtimeGOOS
	originalUserConfigDir := userConfigDir
	originalUserHomeDir := userHomeDir
	t.Cleanup(func() {
		runtimeGOOS = originalOS
		userConfigDir = originalUserConfigDir
		userHomeDir = originalUserHomeDir
	})

	// Linux honors XDG_DATA_HOME.
	runtimeGOOS = "linux"
	t.Setenv("XDG_DATA_HOME", "/xdg/data")
	dir, err := DefaultJournalDir()
	require.NoError(t, err)
	require.Equal(t, filepath.Join("/xdg/data", "skubell", "journal"), dir)

	// Linux falls back to ~/.local/share when XDG_DATA_HOME is unset.
	t.Setenv("XDG_DATA_HOME", "")
	userHomeDir = func() (string, error) { return "/home/test", nil }
	dir, err = DefaultJournalDir()
	require.NoError(t, err)
	require.Equal(t, filepath.Join("/home/test", ".local", "share", "skubell", "journal"), dir)

	// macOS and Windows use the same base as the config directory.
	runtimeGOOS = "darwin"
	userConfigDir = func() (string, error) { return "/Users/test/Library/Application Support", nil }
	dir, err = DefaultJournalDir()
	require.NoError(t, err)
	require.Equal(t, filepath.Join("/Users/test/Library/Application Support", "Skubell", "journal"), dir)
}

func TestEffectiveJournalDirPrefersPreference(t *testing.T) {
	dir, err := EffectiveJournalDir("  /custom/journal  ")
	require.NoError(t, err)
	require.Equal(t, "/custom/journal", dir)
}

func TestDefaultPathReturnsErrorWhenUserConfigDirFails(t *testing.T) {
	originalUserConfigDir := userConfigDir
	t.Cleanup(func() {
		userConfigDir = originalUserConfigDir
	})

	userConfigDir = func() (string, error) {
		return "", errors.New("unavailable")
	}

	_, err := DefaultPath()
	require.Error(t, err)
	require.Contains(t, err.Error(), "resolve user config directory")
}
