package inject

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ClipboardSink copies text to the system clipboard. With AutoPaste it also
// synthesizes the paste shortcut (and Enter if submitting). Copy-only mode needs
// no OS permission at all — the truest universal fallback — and is the practical
// path on Wayland where keystroke synthesis isn't wired up yet. Paste-based
// delivery is also more robust than per-character typing for long transcriptions.
type ClipboardSink struct {
	AutoPaste bool
}

func (s ClipboardSink) Deliver(ctx context.Context, text string, submit bool) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if err := clipboardWrite(ctx, text); err != nil {
		return err
	}
	if !s.AutoPaste {
		fmt.Fprintln(os.Stderr, "voicepipe: copied to clipboard — paste to insert")
		return nil
	}
	return emitPaste(submit)
}

// clipboardWrite pipes text into the platform's clipboard tool.
func clipboardWrite(ctx context.Context, text string) error {
	name, args := clipboardCmd()
	if name == "" {
		return fmt.Errorf("no clipboard tool found for %s — install wl-clipboard or xclip", runtime.GOOS)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clipboard (%s): %w", name, err)
	}
	return nil
}

// clipboardCmd returns the clipboard-write command for this OS, preferring
// Wayland's wl-copy then X11's xclip/xsel on Linux.
func clipboardCmd() (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "pbcopy", nil
	case "windows":
		return "clip", nil
	case "linux":
		for _, c := range [][]string{
			{"wl-copy"},
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
		} {
			if _, err := exec.LookPath(c[0]); err == nil {
				return c[0], c[1:]
			}
		}
	}
	return "", nil
}

// ClipboardAvailable reports whether a clipboard tool exists (used by doctor).
func ClipboardAvailable() bool {
	name, _ := clipboardCmd()
	if name == "" {
		return false
	}
	_, err := exec.LookPath(name)
	return err == nil
}
