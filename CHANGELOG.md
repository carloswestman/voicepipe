# Changelog

All notable changes to voicepipe are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]
### Added
- `talk` shows a live, throbbing status line — orange `● listening…` (echoing the
  macOS mic indicator), then dim `thinking…` and `speaking…` — so you always know
  when the mic is hot and whose turn it is. Interactive only; hidden in `--verbose`.
- `talk` — two-way voice: speak to one agent (a tmux pane, default the active
  one) and hear its reply read aloud via macOS `say`. Reads the reply by
  diffing the pane after output settles and filtering UI noise, and sanitizes
  symbols/markers/emoji so speech stays clean. Options: `--pane`/`--target`,
  `--voice`, `--rate`. Config: `voice`, `speech_rate`.
- `panes` — list all tmux panes (id, location, command) to find a `talk` target.
- `agents` — list the configured agent registry and check each target still
  resolves to a live tmux pane (catches stale mappings).
- In-session voice commands for `talk`: a configurable wake word (`command_word`,
  default "computer") marks a command; everything else is sent to the connected
  agent. Commands: connect/switch to a named agent (sticky), panes, status,
  pause, resume, send, help, quit. `talk` prints the command banner on start.
- Agent registry (`agents` in config): map friendly names to tmux targets so you
  can say "computer connect to backend". voicepipe stays tool-agnostic — anything
  that accepts natural language. Falls back to tmux window-name matching.

- Esc-to-stop in `talk`: press Esc in the voicepipe pane to kill a reply that's
  being read aloud (emergency brake for long replies). Uses cbreak terminal mode
  while keeping Ctrl-C and output intact; auto-disabled when stdin isn't a TTY.
- Minimal terminal styling (`internal/ui`): bold/dim/accent and status colors
  across `talk`, `agents`, `panes`, `devices`, `doctor`, `init`, and errors.
  Auto-disables when output isn't a TTY or `NO_COLOR` is set — no dependencies.
- `talk` conversation view: dim cyan `→` for your message, green `←` with the
  agent's name for each reply, `⇄` for the connection — a consistent icon set.
- `talk` starts unconnected unless `--pane` or `default_agent` (config) is set,
  instead of silently targeting the active pane; content is held with a prompt to
  connect first. The in-session `agents` list marks the connected agent and flags
  stale ones.

### Changed
- `talk` reply reader is now **incremental / streaming**. It splits the agent's
  output into `⏺`-delimited blocks and speaks each prose block the instant a later
  block begins (the final block once the screen settles) — so in a multi-step turn
  the intro is read aloud while tools run, instead of waiting for the whole turn.
  Tool blocks are skipped; already-read blocks aren't repeated; Esc clears the
  backlog.
- `input_device` now defaults to empty, which **follows the macOS default input**
  — switch mics in the Mac sound controls and voicepipe uses what's selected
  (output already follows the default output). Set a substring (e.g. "MacBook")
  to pin a specific mic and avoid a Bluetooth headset's low-quality HFP mode.
- `talk` reply reader now speaks only natural-language prose. It strips agent
  machinery — tool calls (`Update(…)`, `Bash(…)`), their `⎿` results, diff/code
  lines (line numbers, `+`/`-`), git hashes, paths, and command output — plus the
  input box / prompt, paste placeholders, and your own echoed input (now matched
  whitespace/case-insensitively so wrapped messages are caught). Watcher timing
  tightened (poll 150 ms, settle 700 ms) for snappier read-back.
- `talk` rewritten as a concurrent loop. A **persistent mic stream** opens once
  and stays live (steady indicator, no reopen lag), muted only during playback so
  Ava is never recorded back (the no-headphones echo fix). **Sending never blocks**
  — speak several messages while the agent works and they queue into it like typed
  input. A separate **watcher** reads each settled reply aloud independently.
  Reply completion is detected by screen stability plus the animated spinner
  (ignoring the persistent "esc to interrupt" footer). **Esc** stops the current
  read and skips the backlog.
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
