# Changelog

All notable changes to voicepipe are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]
### Added
- `talk` — two-way voice: speak to one agent (a tmux pane, default the active
  one) and hear its reply read aloud via macOS `say`. Reads the reply by
  diffing the pane after output settles and filtering UI noise, and sanitizes
  symbols/markers/emoji so speech stays clean. Options: `--pane`/`--target`,
  `--voice`, `--rate`. Config: `voice`, `speech_rate`.
- `panes` — list all tmux panes (id, location, command) to find a `talk` target.

### Changed
- `talk` validates its target pane, accepts `--pane` (alias of `--target`) and a
  bare pane argument, errors on unknown flags, and warns when targeting its own
  pane.

## [0.1.0] - 2026-05-30
### Added
- `capture` — record one utterance and deliver it (one-shot dictation).
- `listen` — continuous mode: speak → pause → send, repeatedly; auto-submits each
  utterance (`--no-send` to disable).
- `type` — type given text via the keystroke sink (for testing).
- `init`, `doctor`, `devices`, `tmux-install` helper commands.
- Three delivery sinks: **keystroke** (types into the focused window, default),
  **tmux** (per-pane routing via `send-keys`), and **clipboard** (copy / auto-paste).
- Local transcription via whisper.cpp (large-v3-turbo, Metal) with vocabulary
  biasing (initial prompt) and non-speech token suppression.
- Noise/hallucination defense: energy VAD with sustained-voice gating, silence
  trimming before transcription, and output filtering of non-speech cues and
  repetition loops.
- `--verbose` diagnostics: audio metrics and timing per utterance.
- Configuration via `~/Library/Application Support/voicepipe/config.json`.

[Unreleased]: https://github.com/carloswestman/voicepipe/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/carloswestman/voicepipe/releases/tag/v0.1.0
