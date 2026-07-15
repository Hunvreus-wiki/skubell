package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	defaultWriteThrottleMS           = 1000
	minimumWriteThrottleMS           = 200
	defaultMaxRetries                = 3
	defaultUIScale                   = 1.0
	defaultBatchSizeWarningThreshold = 50
)

var (
	runtimeGOOS   = runtime.GOOS
	userConfigDir = os.UserConfigDir
	userHomeDir   = os.UserHomeDir
)

// Config contains all persisted application settings.
type Config struct {
	Wikis       []WikiEntry `json:"wikis"`
	Preferences Preferences `json:"preferences"`
}

// WikiEntry is a configured wiki and its per-wiki settings.
type WikiEntry struct {
	Name            string `json:"name"`
	Farm            string `json:"farm"`
	Family          string `json:"family,omitempty"`
	Language        string `json:"language,omitempty"`
	WikiID          string `json:"wiki_id,omitempty"`
	APIURL          string `json:"api_url,omitempty"`
	Username        string `json:"username"`
	Credential      string `json:"credential"`
	WriteThrottleMS int    `json:"write_throttle_ms,omitempty"`
	MaxRetries      int    `json:"max_retries,omitempty"`
	AdminLanguage   string `json:"admin_language,omitempty"`
}

// Preferences stores global UI and behavior preferences.
type Preferences struct {
	Language                  string  `json:"language,omitempty"`
	Theme                     string  `json:"theme"`
	UIScale                   float64 `json:"ui_scale"`
	DryRunByDefault           bool    `json:"dry_run_by_default"`
	ConfirmBeforeBatch        bool    `json:"confirm_before_batch"`
	BatchSizeWarningThreshold int     `json:"batch_size_warning_threshold"`
	JournalDirectory          string  `json:"journal_directory,omitempty"`
	ShowAPICallsInLog         bool    `json:"show_api_calls_in_log"`
	LastConnectedWiki         string  `json:"last_connected_wiki,omitempty"`
	// Window size as the user last left it. Position is deliberately absent: Fyne exposes no way to place a window, and
	// under Wayland a client may not position itself at all.
	WindowWidth  float32 `json:"window_width,omitempty"`
	WindowHeight float32 `json:"window_height,omitempty"`
}

// DefaultConfig returns an initialized configuration with default values.
func DefaultConfig() Config {
	return Config{
		Wikis: []WikiEntry{},
		Preferences: Preferences{
			Theme:                     "system",
			UIScale:                   defaultUIScale,
			DryRunByDefault:           false,
			ConfirmBeforeBatch:        true,
			BatchSizeWarningThreshold: defaultBatchSizeWarningThreshold,
			ShowAPICallsInLog:         false,
		},
	}
}

// DefaultPath returns the platform-specific config file path.
func DefaultPath() (string, error) {
	configRoot, err := userConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}

	switch runtimeGOOS {
	case "linux":
		return filepath.Join(configRoot, "skubell", "config.json"), nil
	default:
		return filepath.Join(configRoot, "Skubell", "config.json"), nil
	}
}

// EffectiveJournalDir returns the journal root directory: the preference when set, else the platform default.
func EffectiveJournalDir(preference string) (string, error) {
	if dir := strings.TrimSpace(preference); dir != "" {
		return dir, nil
	}
	return DefaultJournalDir()
}

// DefaultJournalDir returns the platform-standard data directory for journal files. On Linux this is the XDG data
// directory ($XDG_DATA_HOME or ~/.local/share); on macOS and Windows it is the same base as the config directory.
func DefaultJournalDir() (string, error) {
	if runtimeGOOS == "linux" {
		dataRoot := os.Getenv("XDG_DATA_HOME")
		if dataRoot == "" {
			home, err := userHomeDir()
			if err != nil {
				return "", fmt.Errorf("resolve user home directory: %w", err)
			}
			dataRoot = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(dataRoot, "skubell", "journal"), nil
	}

	configRoot, err := userConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(configRoot, "Skubell", "journal"), nil
}

// Load reads a configuration file and returns defaults if the file is missing or invalid.
func Load(path string) Config {
	file, err := os.Open(path)
	if err != nil {
		return DefaultConfig()
	}
	defer func() {
		_ = file.Close()
	}()

	cfg := DefaultConfig()
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		return DefaultConfig()
	}

	if err := ensureNoTrailingJSON(decoder); err != nil {
		return DefaultConfig()
	}

	applyDefaults(&cfg)
	return cfg
}

// Save persists the configuration to disk as JSON.
func Save(path string, cfg Config) error {
	applyDefaults(&cfg)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create config file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	return nil
}

func applyDefaults(cfg *Config) {
	defaults := DefaultConfig()

	if cfg.Wikis == nil {
		cfg.Wikis = []WikiEntry{}
	}

	if cfg.Preferences.Theme == "" {
		cfg.Preferences.Theme = defaults.Preferences.Theme
	}
	if cfg.Preferences.UIScale <= 0 {
		cfg.Preferences.UIScale = defaults.Preferences.UIScale
	}
	if cfg.Preferences.BatchSizeWarningThreshold <= 0 {
		cfg.Preferences.BatchSizeWarningThreshold = defaults.Preferences.BatchSizeWarningThreshold
	}

	for idx := range cfg.Wikis {
		if cfg.Wikis[idx].WriteThrottleMS <= 0 {
			cfg.Wikis[idx].WriteThrottleMS = defaultWriteThrottleMS
		} else if cfg.Wikis[idx].WriteThrottleMS < minimumWriteThrottleMS {
			cfg.Wikis[idx].WriteThrottleMS = minimumWriteThrottleMS
		}
		if cfg.Wikis[idx].MaxRetries <= 0 {
			cfg.Wikis[idx].MaxRetries = defaultMaxRetries
		}
	}
}

func ensureNoTrailingJSON(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if err == nil {
		return errors.New("unexpected trailing data")
	}
	if err == io.EOF {
		return nil
	}
	return fmt.Errorf("decode trailing data: %w", err)
}
