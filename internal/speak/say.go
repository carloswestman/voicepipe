// Package speak turns text into speech. The default backend is the macOS `say`
// command — built in, zero dependencies, which fits voicepipe's easy-install
// goal. An opt-in OpenAI-compatible backend (openai.go) adds nicer local (Kokoro)
// or hosted voices behind the same Speaker interface.
package speak

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
)

// Options configures spoken output.
type Options struct {
	Voice string // macOS voice name (empty = system default)
	Rate  int    // words per minute (0 = system default)
}

// Command builds (but does not start) the `say` command for text. The caller can
// Run it and Kill its Process to interrupt playback (used by `talk`'s Esc-to-stop).
func Command(ctx context.Context, text string, opts Options) *exec.Cmd {
	var args []string
	if opts.Voice != "" {
		args = append(args, "-v", opts.Voice)
	}
	if opts.Rate > 0 {
		args = append(args, "-r", strconv.Itoa(opts.Rate))
	}
	args = append(args, "--", text)
	return exec.CommandContext(ctx, "say", args...)
}

// Say speaks text aloud, blocking until done. Empty text is a no-op.
func Say(ctx context.Context, text string, opts Options) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return Command(ctx, text, opts).Run()
}

// Available reports whether the `say` command exists (it ships with macOS).
func Available() bool {
	_, err := exec.LookPath("say")
	return err == nil
}
