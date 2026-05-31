package inject

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// TestClipboardCopyOnly verifies the copy-only path actually lands text on the
// system clipboard (read back via pbpaste on macOS).
func TestClipboardCopyOnly(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("read-back check is macOS-only")
	}
	want := "hello voicepipe clipboard"
	if err := (ClipboardSink{}).Deliver(context.Background(), want, false); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	out, err := exec.Command("pbpaste").Output()
	if err != nil {
		t.Fatalf("pbpaste: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}
}
