// Package speak turns text into speech using the macOS `say` command — built in,
// zero dependencies, which fits voicepipe's easy-install goal.
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

// Say speaks text aloud, blocking until done. Empty text is a no-op.
func Say(ctx context.Context, text string, opts Options) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var args []string
	if opts.Voice != "" {
		args = append(args, "-v", opts.Voice)
	}
	if opts.Rate > 0 {
		args = append(args, "-r", strconv.Itoa(opts.Rate))
	}
	args = append(args, "--", text)
	return exec.CommandContext(ctx, "say", args...).Run()
}

// Available reports whether the `say` command exists (it ships with macOS).
func Available() bool {
	_, err := exec.LookPath("say")
	return err == nil
}
