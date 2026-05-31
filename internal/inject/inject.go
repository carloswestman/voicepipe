// Package inject delivers transcribed text to wherever the user wants it. The
// Sink interface decouples "what was said" from "how it lands", so voicepipe can
// support the broad audience (keystroke into any focused app — the default) and
// the tmux power-user (precise per-pane routing that lets you talk to specific
// agents) from one binary.
package inject

import "context"

// Sink kinds.
const (
	KindKeystroke = "keystroke"
	KindTmux      = "tmux"
	KindClipboard = "clipboard"
)

// Sink delivers text. submit asks the sink to also press Enter afterwards
// (e.g. to fire a Claude Code prompt).
type Sink interface {
	Deliver(ctx context.Context, text string, submit bool) error
}

// Selection describes which sink to build and how to configure it.
type Selection struct {
	Kind           string // one of the Kind* constants; defaults to keystroke
	TmuxTarget     string // pane spec for the tmux sink
	ClipboardPaste bool   // for the clipboard sink: auto-paste after copying
}

// Choose builds the sink described by sel. Unknown kinds fall back to keystroke.
func Choose(sel Selection) Sink {
	switch sel.Kind {
	case KindTmux:
		return TmuxSink{Target: sel.TmuxTarget}
	case KindClipboard:
		return ClipboardSink{AutoPaste: sel.ClipboardPaste}
	default:
		return KeystrokeSink{}
	}
}
