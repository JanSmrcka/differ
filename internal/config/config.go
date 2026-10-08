package config

import (
	"encoding/json"
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
	// EditorStrategy picks how `e` opens a file:
	//   "" / "auto" — reuse an editor already open in this tmux session or
	//                 herdr workspace, else a new tmux window or herdr pane,
	//                 else take over differ's terminal
	//   "reuse"     — only reuse; report a problem when there is nothing to
	//                 reuse
	//   "window"    — always a new tmux window, or a new pane in herdr
	//   "inline"    — always take over differ's terminal and resume after
	//   "detach"    — run the editor in the background; for GUI editors that
	//                 need no terminal and reuse their own window
	EditorStrategy string `json:"editor_strategy"`
	// EditorPanes are the pane commands reuse treats as an editor. Empty
	// means nvim, vim, vi and view.
	EditorPanes []string `json:"editor_panes"`
	// EditorTarget scopes reuse: "" or "session" is differ's own tmux
	// session or herdr workspace, "any" is everywhere, anything else names a session or
	// workspace (by label or id).
	EditorTarget string `json:"editor_target"`
	// EditorLineArgs replaces how the file and line reach the editor, as a
	// whitespace-separated template over {file} and {line} — for an editor
	// differ does not know, such as "{file}:{line}". Empty means the
	// built-in table.
	EditorLineArgs string `json:"editor_line_args"`
	// EditorTimeoutMS bounds a tmux or herdr command or an editor open. 0 means 5000.
	EditorTimeoutMS int `json:"editor_timeout_ms"`
	// EditorProbeTimeoutMS bounds asking a running nvim a question. 0 means
	// 1000. Raise it over a slow SSH hop.
	EditorProbeTimeoutMS int `json:"editor_probe_timeout_ms"`

	// FeedbackTarget selects where review feedback is delivered:
	// "clipboard" (default), "stdout", "tmux", "herdr" or "zellij".
	FeedbackTarget string `json:"feedback_target"`
	// TmuxTarget is the tmux pane feedback is sent to when FeedbackTarget is
	// "tmux". Empty means the last active pane in the current window.
	TmuxTarget string `json:"tmux_target"`
	// HerdrTarget is the herdr agent's session id feedback is sent to when
	// FeedbackTarget is "herdr" — stable across pane moves, unlike a pane id.
	HerdrTarget string `json:"herdr_target"`
	// HerdrPane is the pane that agent was in when chosen, used when herdr
	// no longer lists its session.
	HerdrPane string `json:"herdr_pane"`
	// ZellijTarget is the zellij pane ("terminal_N") feedback is sent to when
	// FeedbackTarget is "zellij".
	ZellijTarget string `json:"zellij_target"`
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
func Load() Config {
	path, err := configPath()
	if err != nil {
		return Default()
	}
	return LoadFrom(path)
}

// LoadFrom reads config from the given path. Returns defaults on error.
func LoadFrom(path string) Config {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	return cfg
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
