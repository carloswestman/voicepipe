// Package config loads and persists voicepipe settings from
// ~/.config/voicepipe/config.json. Keeping config in one small struct makes
// the "easy setup" story simple: `voicepipe init` writes sane defaults and the
// user rarely needs to touch it.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds everything voicepipe needs at runtime. Fields are deliberately
// few — every extra knob is friction for new users.
type Config struct {
	// InputDevice is a case-insensitive substring matched against capture device
	// names. Empty means "system default". On Bluetooth headsets the default mic
	// forces macOS into low-quality HFP, so we default to the built-in mic.
	InputDevice string `json:"input_device"`

	// ModelPath points at a whisper.cpp GGML model (e.g. large-v3-turbo).
	ModelPath string `json:"model_path"`

	// WhisperBin is the whisper.cpp CLI to shell out to (whisper-cli).
	WhisperBin string `json:"whisper_bin"`

	// SendEnter submits the prompt after injecting (presses Enter in the pane).
	SendEnter bool `json:"send_enter"`

	// SilenceMs is how long a trailing silence ends an utterance (VAD auto-stop).
	SilenceMs int `json:"silence_ms"`

	// MaxSeconds caps a single capture so a stuck VAD can't record forever.
	MaxSeconds int `json:"max_seconds"`

	// Language hint passed to whisper ("en" keeps the English models fastest).
	Language string `json:"language"`

	// Prompt is whisper's initial prompt: it biases recognition toward your
	// vocabulary so proper nouns are spelled right (e.g. "Claude Code" instead of
	// "Cloud Agent"). Add your own jargon here.
	Prompt string `json:"prompt"`

	// Sink selects how text is delivered by default: "keystroke" types into the
	// focused window (works in any terminal/app, needs macOS Accessibility once),
	// "tmux" routes via send-keys (no permission, can target any pane — the
	// power-mode that enables talking to specific agents), or "clipboard" copies
	// the text (and pastes if ClipboardPaste). A --target on the capture command
	// forces tmux regardless of this default.
	Sink string `json:"sink"`

	// ClipboardPaste makes the clipboard sink auto-paste after copying (otherwise
	// it copies and you paste yourself — the zero-permission fallback).
	ClipboardPaste bool `json:"clipboard_paste"`
}

// Default returns the recommended out-of-the-box configuration.
func Default() Config {
	return Config{
		InputDevice: "MacBook", // prefer built-in mic over HFP Bluetooth
		ModelPath:   filepath.Join(dataDir(), "models", "ggml-large-v3-turbo.bin"),
		WhisperBin:  "whisper-cli",
		SendEnter:   false,
		SilenceMs:   1200,
		MaxSeconds:  30,
		Language:    "en",
		Prompt:      "Talking to Claude Code in tmux with neovim. Terms: Claude Code, tmux, neovim, voicepipe, Anthropic, Opus, Sonnet.",
		Sink:        "keystroke", // works everywhere; tmux is opt-in via --target
	}
}

// Path is the on-disk location of the config file.
func Path() string {
	return filepath.Join(configDir(), "config.json")
}

// Load reads the config file, falling back to defaults for any missing fields.
func Load() (Config, error) {
	cfg := Default()
	b, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing %s: %w", Path(), err)
	}
	return cfg, nil
}

// Save writes the config file, creating the directory if needed.
func (c Config) Save() error {
	if err := os.MkdirAll(configDir(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(Path(), b, 0o644)
}

func configDir() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "voicepipe")
	}
	return filepath.Join(os.Getenv("HOME"), ".config", "voicepipe")
}

func dataDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "voicepipe")
	}
	return filepath.Join(os.Getenv("HOME"), ".local", "share", "voicepipe")
}

// ModelDir is the directory where models are downloaded.
func ModelDir() string {
	return filepath.Join(dataDir(), "models")
}
