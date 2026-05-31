package inject

import (
	"context"
	"strings"
)

// KeystrokeSink types text into the currently focused window by synthesizing
// OS keyboard events. It works in any terminal or app — no tmux required — at
// the cost of a one-time OS permission (macOS Accessibility). It can only reach
// the focused window, so use the tmux sink when you need to target a specific
// background agent.
type KeystrokeSink struct{}

func (KeystrokeSink) Deliver(_ context.Context, text string, submit bool) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return emitText(text, submit)
}

// emitText is implemented per-OS (see keystroke_darwin.go / keystroke_other.go).
