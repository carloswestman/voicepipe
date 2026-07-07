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
	// names. Empty (the default) follows the macOS default input — switch mics in
	// the Mac sound controls and voicepipe uses whatever's selected. Note: using a
	// Bluetooth headset's mic forces macOS into low-quality HFP; set a specific
	// substring (e.g. "MacBook") to pin the built-in mic and keep full quality.
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

	// Voice is the macOS `say` voice used by `talk` for spoken replies
	// (empty = system default; see `say -v '?'` for options).
	Voice string `json:"voice"`

	// SpeechRate is the words-per-minute for spoken replies (0 = system default).
	SpeechRate int `json:"speech_rate"`

	// CommandWord is the spoken prefix that marks a voicepipe command in `talk`
	// (e.g. "computer connect backend"). Everything not starting with it is sent
	// to the connected agent. Use a single, distinct word.
	CommandWord string `json:"command_word"`

	// Agents maps a friendly name (used by voice: "<word> connect to <name>") to a
	// tmux target (pane id like "%3", or "session:window.pane"). voicepipe is
	// agnostic about what runs there — anything that takes natural language. When
	// empty, `connect` falls back to matching the spoken name to a tmux window.
	Agents map[string]string `json:"agents"`

	// DefaultAgent is the agent name `talk` auto-connects to on start (when no
	// --pane is given). Empty means start unconnected.
	DefaultAgent string `json:"default_agent"`

	// FocusOnConnect brings an agent's tmux window/pane into view when you connect
	// to it by voice, so you see who you're talking to. Note: it pulls focus off
	// the voicepipe pane, so Esc-to-stop won't reach voicepipe until you click
	// back. Default true.
	FocusOnConnect bool `json:"focus_on_connect"`

	// PushToTalk starts `talk` with the mic closed; press space to toggle it open
	// (and closed again). Useful in noisy rooms so ambient sound doesn't keep
	// firing the recognizer. Default false (listen continuously). The `--ptt` flag
	// turns it on for a single run.
	PushToTalk bool `json:"push_to_talk"`

	// TTS selects the text-to-speech backend for `talk`: "say" (default) uses the
	// built-in macOS `say` — zero setup, the friction-free path; "openai" speaks
	// via an OpenAI-compatible /audio/speech server — a local Kokoro server for
	// nicer offline voices, or a hosted provider. With "openai", the Voice field
	// holds the backend's voice id (e.g. "af_heart").
	TTS string `json:"tts"`

	// TTSBaseURL is the OpenAI-compatible API base for the "openai" backend
	// (e.g. "http://127.0.0.1:8880/v1" for a local Kokoro server).
	TTSBaseURL string `json:"tts_base_url"`

	// TTSApiKey authenticates a hosted TTS provider. Leave empty for a local server.
	TTSApiKey string `json:"tts_api_key"`

	// TTSModel is the model name sent to the TTS server (e.g. "kokoro", "tts-1").
	TTSModel string `json:"tts_model"`

	// TTSFormat is the audio format requested and played with afplay (mp3, wav, aac).
	TTSFormat string `json:"tts_format"`

	// TTSServerCmd, if set, is the command voicepipe runs to launch a local TTS
	// server for the session (e.g. a speech-server binary path). voicepipe starts
	// it only when the endpoint isn't already up and stops it on exit. Empty means
	// you run the server yourself.
	TTSServerCmd string `json:"tts_server_cmd"`
}

// Default returns the recommended out-of-the-box configuration.
func Default() Config {
	return Config{
		InputDevice:    "", // follow the macOS default input device
		ModelPath:      filepath.Join(dataDir(), "models", "ggml-large-v3-turbo.bin"),
		WhisperBin:     "whisper-cli",
		SendEnter:      false,
		SilenceMs:      1200,
		MaxSeconds:     30,
		Language:       "en",
		Prompt:         "Talking to Claude Code in tmux with neovim. Terms: Claude Code, tmux, neovim, voicepipe, Anthropic, Opus, Sonnet.",
		Sink:           "keystroke", // works everywhere; tmux is opt-in via --target
		CommandWord:    "computer",
		Agents:         map[string]string{},
		FocusOnConnect: true,
		TTS:            "say", // built-in macOS speech; "openai" opts into Kokoro/hosted
		TTSBaseURL:     "http://127.0.0.1:8880/v1",
		TTSModel:       "kokoro",
		TTSFormat:      "mp3",
	}
}

// Path is the on-disk location of the config file.
func Path() string {
	return filepath.Join(configDir(), "config.json")
}

// LogPath is where `talk` writes its per-session diagnostic log (overwritten
// each run) — used to troubleshoot the reply reader after the fact.
func LogPath() string {
	return filepath.Join(configDir(), "talk.log")
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
