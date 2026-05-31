//go:build !darwin

package inject

import (
	"fmt"
	"runtime"
)

// emitText is not yet implemented off macOS. Linux (ydotool/xdotool) and Windows
// (SendInput) backends are planned; tmux works everywhere in the meantime.
func emitText(_ string, _ bool) error {
	return fmt.Errorf("keystroke sink not yet supported on %s — use the tmux sink (--target) for now", runtime.GOOS)
}

// emitPaste is unimplemented off macOS; the clipboard sink still copies, the user
// pastes manually (or use copy-only mode).
func emitPaste(_ bool) error {
	return fmt.Errorf("auto-paste not supported on %s — text is on the clipboard, paste it manually", runtime.GOOS)
}
