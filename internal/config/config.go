package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds user preferences.
type Config struct {
	Theme           string `json:"theme"`
	TabWidth        int    `json:"tab_width"`
	CommitMsgCmd    string `json:"commit_msg_cmd"`
	CommitMsgPrompt string `json:"commit_msg_prompt"`
	SplitDiff       bool   `json:"split_diff"`
	EditorCmd       string `json:"editor_cmd"`
}

// Default returns the default configuration.
func Default() Config {
	return Config{
		Theme:    "dark",
		TabWidth: 4,
	}
}

// Load reads config from ~/.config/differ/config.json.
// Returns defaults if file doesn't exist.
func Load() (Config, error) {
	path, err := configPath()
	if err != nil {
		return Default(), fmt.Errorf("could not resolve config path: %w", err)
	}
	return LoadFrom(path)
}

// LoadFrom reads config from the given path.
// Returns defaults and a nil error if the file doesn't exist; otherwise returns defaults plus an error on failures (e.g., read/parse errors).
func LoadFrom(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("could not read config file %q: %w", path, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("invalid config file %q: %w; fix the JSON or remove the file to use defaults", path, err)
	}
	return cfg, nil
}

// Save writes config to ~/.config/differ/config.json.
func Save(cfg Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	return SaveTo(cfg, path)
}

// SaveTo writes config to the given path, creating parent dirs as needed.
func SaveTo(cfg Config, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "differ", "config.json"), nil
}
