package inject

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// TmuxSink delivers text into a tmux pane via send-keys. This is the opt-in
// power mode: it needs no macOS permission and can target any pane by id, which
// is what lets you talk to a specific background agent. Target is a tmux pane
// spec ("%3", "session:window.pane", or "" for the active pane).
type TmuxSink struct {
	Target string
}

func (s TmuxSink) Deliver(ctx context.Context, text string, submit bool) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	args := []string{"send-keys"}
	if s.Target != "" {
		args = append(args, "-t", s.Target)
	}
	// -l sends keys literally (no key-name interpretation); "--" guards against
	// text that begins with a dash.
	args = append(args, "-l", "--", text)
	if err := tmuxRun(ctx, args...); err != nil {
		return fmt.Errorf("tmux send-keys: %w", err)
	}

	if submit {
		enter := []string{"send-keys"}
		if s.Target != "" {
			enter = append(enter, "-t", s.Target)
		}
		enter = append(enter, "Enter")
		if err := tmuxRun(ctx, enter...); err != nil {
			return fmt.Errorf("tmux send Enter: %w", err)
		}
	}
	return nil
}

func tmuxRun(ctx context.Context, args ...string) error {
	return exec.CommandContext(ctx, "tmux", args...).Run()
}
