# voicepipe

**Talk to your terminal agents by voice — hands-free, fully local.** Speak to
[Claude Code](https://claude.com/claude-code) (or anything in a tmux pane that
takes natural language), and hear its replies read back to you. Built for the
world where you're running several agents at once and can't watch every pane.

Transcription runs **fully on-device** with
[whisper.cpp](https://github.com/ggerganov/whisper.cpp) (Metal-accelerated on
Apple Silicon); replies are spoken with the built-in macOS voice. Nothing leaves
your machine.

```
you speak ─▶ whisper ─▶ the connected agent ─▶ its reply ─▶ spoken back to you
```

<!-- DEMO: drop a 30-second screen recording here (talk to an agent, hear it
reply). This is the single highest-leverage thing for adoption — record it
before launch and place it at the very top. -->

It also does plain one-way **dictation** — voice-to-text into any terminal,
editor, or app — when that's all you need.

---

## Install

```sh
brew install carloswestman/tap/voicepipe

voicepipe init       # downloads the whisper model (~1.5 GB) and writes config
voicepipe doctor     # confirm whisper-cpp, the model, and sinks are ready
```

macOS-focused (it uses CoreAudio + macOS text-to-speech). The formula pulls in
`whisper-cpp` and `tmux` for you.

<details>
<summary>Build from source</summary>

```sh
brew install go whisper-cpp tmux
git clone https://github.com/carloswestman/voicepipe
cd voicepipe
go build -o voicepipe ./cmd/voicepipe
```
</details>

## Quickstart — talk to your agents

`talk` is the two-way mode. Point it at an agent's tmux pane and have a
conversation: you speak, it types into the pane, and the agent's reply is read
aloud as it streams.

1. **List your panes** and note which run your agents:
   ```sh
   voicepipe panes
   ```
2. **Name them** in `~/Library/Application Support/voicepipe/config.json` under
   `agents` (friendly name → tmux target), then check them:
   ```jsonc
   "agents": { "backend": "dev:api.1", "web": "dev:web.1" }
   ```
   ```sh
   voicepipe agents     # lists your agents and flags any that are offline
   ```
3. **Start talking:**
   ```sh
   voicepipe talk
   ```
   Then say **"computer connect to backend"**, and just talk. Your message goes
   to that agent, and its reply is spoken back. Switch with **"computer connect
   to web"**, and your tmux view follows along.

A live status line shows when the mic is hot (orange **listening**), when the
agent is working (**thinking**), and when a reply is being read (**speaking**).

## Voice commands

Anything you say is sent to the connected agent **unless** it starts with the
wake word (default **`computer`**, configurable). Commands:

| say `computer …` | does |
|---|---|
| `connect to <name>` | switch which agent you're talking to (sticky) |
| `agents` | list your agents and which is connected |
| `status` | say which agent you're connected to |
| `pause` / `resume` | stop / start sending your speech (still hears commands) |
| `send` | press Enter in the agent's pane |
| `help` | read the command list |
| `quit` | exit talk |

**Noisy room?** Start with `voicepipe talk --ptt` (or set `push_to_talk: true`) for
push-to-talk: the mic stays closed until you press **space** to toggle it on, and
again to mute — so café chatter and background noise don't keep firing the
recognizer. The status line shows `muted · space to talk` when it's closed.

You can **keep talking while the agent works** — messages queue into it like typed
input. Press **Esc** in the voicepipe pane to stop a reply that's reading and skip
the rest.

## Dictation mode (one-way)

When you just want voice-to-text, `capture` and `listen` type your words into a
window via a **sink**:

- **keystroke** (default) — types into the focused window; works in any app
  (needs a one-time macOS Accessibility grant).
- **tmux** — routes to a specific pane via `send-keys` (`--target`/`--tmux`); no
  permission.
- **clipboard** — copies (`--clipboard`) or auto-pastes (`--clipboard-paste`);
  zero permissions, the cross-platform fallback.

```sh
voicepipe capture            # speak once → typed into the focused window
voicepipe capture --send     # …and press Enter
voicepipe listen             # keep listening, auto-submit each utterance
voicepipe type "some text"   # type given text (testing)
```

## Microphone & headset

`input_device` is empty by default, so voicepipe **follows the macOS default
input** — switch mics in the Mac sound controls and it uses whatever's selected
(spoken replies already follow the default output). Caveat: using a Bluetooth
headset's *mic* forces macOS into low-quality HFP, degrading both that mic and
your audio out; pin the built-in mic with `"input_device": "MacBook"` if you wear
the headset only to listen. `voicepipe devices` lists options.

## Configuration

`~/Library/Application Support/voicepipe/config.json` (run `voicepipe help` for
the path). Highlights:

| field | default | meaning |
|---|---|---|
| `input_device` | `""` | mic substring; empty follows the macOS default input |
| `sink` | `keystroke` | dictation delivery: `keystroke`, `tmux`, or `clipboard` |
| `command_word` | `computer` | wake word that marks a voice command in `talk` |
| `agents` | `{}` | friendly name → tmux target (pane id or `session:window.pane`) |
| `default_agent` | `""` | agent `talk` auto-connects to on start |
| `focus_on_connect` | `true` | bring the agent's pane into view on connect |
| `voice` / `speech_rate` | system | reply voice: macOS `say` voice + words-per-minute (`tts: say`), or the backend voice id like `af_heart` (`tts: openai`) |
| `silence_ms` | `1200` | trailing silence that ends an utterance |
| `prompt` | tooling vocab | whisper initial prompt to fix proper nouns |
| `tts` | `say` | speech backend: `say` (built in) or `openai` (Kokoro/hosted) |

## Nicer voices (optional)

Replies use the built-in macOS `say` by default — zero setup. If you want a more
natural voice, voicepipe can speak through any **OpenAI-compatible** TTS server,
so the same setting drives a **local [Kokoro](https://github.com/hexgrad/kokoro)
server** (free, fully offline) or a hosted provider (`tts_api_key` + `tts_base_url`).

Point it at a local Kokoro server — for example
[Kokoro-FastAPI](https://github.com/remsky/Kokoro-FastAPI):

```bash
docker run -d -p 8880:8880 ghcr.io/remsky/kokoro-fastapi-cpu:latest
```

then set in your config:

```json
{
  "tts": "openai",
  "tts_base_url": "http://127.0.0.1:8880/v1",
  "tts_model": "kokoro",
  "voice": "af_heart"
}
```

`voicepipe doctor` checks the endpoint is reachable. Want voicepipe to start the
server for you and stop it on exit? Set `tts_server_cmd` to the launch command
(e.g. a [macos-speech-server](https://github.com/dokterbob/macos-speech-server)
binary that runs Kokoro on the Apple Neural Engine) — it's launched only if the
endpoint isn't already up. If anything's unreachable, `talk` falls back to `say`.

## Commands

`init` · `talk` · `capture` · `listen` · `type` · `panes` · `agents` · `devices`
· `doctor` · `tmux-install`. Run `voicepipe help` for details.

## How it works

`talk` sends your transcribed speech into the agent's tmux pane with `send-keys`,
then watches the pane and reads each completed prose block aloud — splitting the
output on Claude Code's block markers so it speaks the answer and skips tool
calls, diffs, and command output. It's a heuristic over the terminal UI, so a
major TUI redesign could need a tune-up.

## License

MIT — see [LICENSE](LICENSE).
