# Contributing to voicepipe

Thanks for your interest! voicepipe is a small Go CLI, currently macOS-focused
(it uses cgo for CoreAudio capture and CoreGraphics keystroke synthesis).

## Build & run

```sh
go build -o voicepipe ./cmd/voicepipe
./voicepipe doctor
```

## Before opening a PR

```sh
gofmt -w .      # format
go vet ./...    # static checks
go test ./...   # tests
```

CI runs these on macOS; please make sure they pass locally first. Keep changes
small and focused, and match the surrounding style and comment density.

## Runtime dependencies

- Go 1.22+
- [whisper-cpp](https://github.com/ggerganov/whisper.cpp) (`brew install whisper-cpp`) — transcription
- tmux (optional) — only for the tmux sink

## Project layout

- `cmd/voicepipe` — CLI entrypoint and command routing
- `internal/audio` — microphone capture, VAD, WAV encoding
- `internal/transcribe` — whisper.cpp invocation and output cleaning
- `internal/inject` — delivery sinks (keystroke, tmux, clipboard)
- `internal/config` — configuration loading/saving
