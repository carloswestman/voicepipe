# voicepipe

Stream your voice to text straight into your terminal. Talk, and your words are
typed into whatever window you're focused on — any terminal (Ghostty, kitty,
Alacritty, iTerm2, …), editor, or app. Built for talking to
[Claude Code](https://claude.com/claude-code), neovim, or anything that takes text.

Transcription runs **fully local** with [whisper.cpp](https://github.com/ggerganov/whisper.cpp)
(Metal-accelerated on Apple Silicon). Nothing leaves your machine.

```
speak  →  whisper.cpp  →  text typed into your focused window
```

## Two ways to deliver text (sinks)

voicepipe decouples *what you said* from *where it lands*:

- **keystroke (default)** — synthesizes typing into the focused window. Works in
  **any** terminal or app; no tmux required. Needs a one-time macOS Accessibility
  grant (System Settings → Privacy & Security → Accessibility).
- **tmux (opt-in power mode)** — routes via `tmux send-keys`. No permission, and
  it can target **any pane by id**, so you can talk to a *specific* background
  Claude Code agent. Enable with `--target`/`--tmux` or `sink: "tmux"` in config.
- **clipboard** — copies the transcription (`--clipboard`), optionally auto-pasting
  (`--clipboard-paste`). Copy-only needs **zero permissions** — the universal
  fallback — and is the practical path on Wayland. Works on macOS (pbcopy), Linux
  (wl-clipboard/xclip), and Windows (clip).

The keystroke sink can only reach the *focused* window; multi-agent routing is the
tmux sink's superpower. Use whichever fits — or all three.

## Install

```sh
brew install go whisper-cpp tmux   # dependencies
git clone https://github.com/carloswestman/voicepipe
cd voicepipe
go build -o voicepipe ./cmd/voicepipe
sudo mv voicepipe /usr/local/bin/   # or anywhere on PATH

voicepipe init          # downloads the large-v3-turbo model + writes config
voicepipe doctor        # confirm whisper-cpp, the model, and sinks are ready
```

> tmux is optional — only needed for power-mode routing. A Homebrew tap
> (`brew install carloswestman/tap/voicepipe`) is the planned one-line install
> once releases are cut.

## Use

**Default (keystroke, works anywhere):** bind a key in your terminal/OS to run
`voicepipe capture` (add `--send` to submit). Record an utterance — it auto-stops
when you pause — and the text is typed into the focused window. The first run
prompts for Accessibility permission.

```sh
voicepipe capture          # type transcription into the focused window
voicepipe capture --send   # …and press Enter (fires a Claude Code prompt)
```

**Power mode (tmux per-pane routing):** add the binding from
`voicepipe tmux-install` to `~/.tmux.conf`:

- **`prefix + v`** — record and type into the pane you fired from.
- **`prefix + V`** — same, but submits the prompt.

Switch which agent you're talking to by switching tmux panes — voicepipe targets
the pane you fired from. (This is the one thing keystroke mode can't do: reach a
*background* window.)

## Microphone note (Sony WH-1000XM5 and other Bluetooth headsets)

By default `input_device` is empty, so voicepipe **follows the macOS default
input** — pick your mic in the Mac sound controls and voicepipe uses it (Ava's
output already follows the default output device too).

One caveat: using a Bluetooth headset's **microphone** forces macOS into the
low-quality HFP profile, which degrades both that mic *and* your audio output. If
you wear the headset to listen but want full quality, pin the built-in mic by
setting `input_device` to `"MacBook"`. Run `voicepipe devices` to see options.

## Configuration

Config lives at `~/Library/Application Support/voicepipe/config.json` on macOS
(run `voicepipe help` to see the exact path):

| field          | default                       | meaning                                  |
|----------------|-------------------------------|------------------------------------------|
| `input_device` | `""` (system default)         | mic name substring; empty follows the macOS default input |
| `model_path`   | `…/ggml-large-v3-turbo.bin`   | whisper.cpp GGML model                   |
| `whisper_bin`  | `whisper-cli`                 | whisper.cpp CLI to run                    |
| `send_enter`   | `false`                       | submit after injecting                    |
| `silence_ms`   | `1200`                        | trailing silence that ends an utterance   |
| `max_seconds`  | `30`                          | hard cap on one recording                 |
| `language`     | `en`                          | whisper language hint                     |
| `prompt`       | tooling vocabulary            | initial prompt biasing recognition (fix proper nouns) |
| `sink`         | `keystroke`                   | `keystroke` (focused window) or `tmux` (per-pane) |

## Status

Early MVP. Working: capture → whisper → inject, tmux routing, model download,
doctor checks. Planned: lighter models, true global hotkey opt-in, spoken
routing commands ("send to backend"), and a Homebrew tap.
