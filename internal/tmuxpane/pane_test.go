package tmuxpane

import (
	"strings"
	"testing"
)

func TestReplyExtraction(t *testing.T) {
	baseline := "> hello\n\n│ old reply │\n"
	// Simulated pane after the agent answered: our echoed input, the reply,
	// a status/spinner line, and the empty input prompt.
	cur := "> fix the bug\n\nSure, I fixed the failing test.\n\n  esc to interrupt\n> \n"

	got := filterReply(delta(baseline, cur), []string{"fix the bug"})
	want := "Sure, I fixed the failing test."
	if got != want {
		t.Fatalf("filterReply = %q, want %q", got, want)
	}
}

func TestNewSpeech(t *testing.T) {
	full := "Understood, take the time you need. The patched binary is ready."
	partial := "Understood, take the time you need."

	// A block read as a partial, then again after it grew: only the new tail.
	if got := NewSpeech(full, []string{partial}); got != "The patched binary is ready." {
		t.Errorf("grown block tail = %q", got)
	}
	// Fully-spoken block (exact, and whitespace/case variant) → nothing new.
	if got := NewSpeech(partial, []string{partial}); got != "" {
		t.Errorf("exact repeat = %q, want empty", got)
	}
	if got := NewSpeech("the build passes  and ALL tests pass.", []string{"The build passes and all tests pass."}); got != "" {
		t.Errorf("reflow/case variant = %q, want empty", got)
	}
	// A genuinely new block (no shared prefix) is spoken in full.
	if got := NewSpeech("A different sentence.", []string{partial}); got != "A different sentence." {
		t.Errorf("distinct block = %q", got)
	}
	// Nothing spoken yet → speak it all (whitespace normalized to single spaces).
	if got := NewSpeech("Hello   there.", nil); got != "Hello there." {
		t.Errorf("first read = %q", got)
	}
}

func TestFilterDropsMechanics(t *testing.T) {
	// Real lines captured from a Claude Code pane: keep the prose, drop the
	// tool calls, diffs, results, and command output.
	lines := []string{
		"Updating the CHANGELOG, then committing.",
		"⏺ Update(CHANGELOG.md)",
		"⎿  Added 8 lines",
		"42 +- `talk` rewritten as a concurrent loop",
		"39    stale ones.",
		"Bash(export PATH=... && go build)",
		"51348c1..74719ed  main -> main",
		"❯ commit and push it", // input box (unsent) — must not be read
		"That's great to hear.",
	}
	got := filterReply(lines, nil)
	want := "Updating the CHANGELOG, then committing. That's great to hear."
	if got != want {
		t.Fatalf("filterReply = %q, want %q", got, want)
	}
}

func TestFilterDropsWrappedEcho(t *testing.T) {
	// A long input wraps across pane lines; the whole thing should still be
	// recognized as the user's echo and not read back.
	sent := []string{"Now I want to try a second test, write a test file and modify it."}
	lines := []string{
		"Now I want to try a second test, write a", // wrapped piece 1
		"test file and modify it.",                 // wrapped piece 2
		"Sure, I wrote and changed the file.",      // the actual reply
	}
	got := filterReply(lines, sent)
	want := "Sure, I wrote and changed the file."
	if got != want {
		t.Fatalf("filterReply = %q, want %q", got, want)
	}
}

func TestBlocksAndCleanBlock(t *testing.T) {
	capture := strings.Join([]string{
		"❯ Shut up, it is so sturdy.",
		"",
		"⏺ On it, committing the change first.",
		"",
		"⏺ Update(CHANGELOG.md)",
		"  ⎿  Added 8 lines",
		"   42 +- talk rewritten",
		"⏺ Pushed, it is live now.",
	}, "\n")
	blocks := Blocks(capture)
	if len(blocks) != 3 { // intro prose, Update tool, Pushed prose
		t.Fatalf("got %d blocks, want 3", len(blocks))
	}
	if got := CleanBlock(blocks[0], nil); got != "On it, committing the change first." {
		t.Errorf("block 0 = %q", got)
	}
	if got := CleanBlock(blocks[1], nil); got != "" { // tool block → nothing spoken
		t.Errorf("block 1 (tool) = %q, want empty", got)
	}
	if got := CleanBlock(blocks[2], nil); got != "Pushed, it is live now." {
		t.Errorf("block 2 = %q", got)
	}
}

func TestSanitizeForSpeech(t *testing.T) {
	cases := map[string]string{
		"⏺ Going great — and `talk` works! 🎙️": "Going great and talk works!",
		"● Hello, world.":                      "Hello, world.",
		"**bold** and _under_":                 "bold and under",
		"→ next step":                          "next step",
	}
	for in, want := range cases {
		if got := sanitizeForSpeech(in); got != want {
			t.Errorf("sanitizeForSpeech(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParsePanes(t *testing.T) {
	out := "%1\tdev:phid.1\tnode\t1\n%3\tdev:saas.2\tzsh\t0\n\n"
	panes := parsePanes(out)
	if len(panes) != 2 {
		t.Fatalf("got %d panes, want 2", len(panes))
	}
	if panes[0].ID != "%1" || panes[0].Location != "dev:phid.1" || panes[0].Command != "node" || !panes[0].Active {
		t.Fatalf("pane[0] parsed wrong: %+v", panes[0])
	}
	if panes[1].Active {
		t.Fatalf("pane[1] should be inactive: %+v", panes[1])
	}
}

func TestFilterDropsUIOnly(t *testing.T) {
	lines := []string{"╭─────────╮", "│ ✻ Thinking… │", "> ", "   ", "esc to interrupt"}
	if got := filterReply(lines, nil); got != "" {
		t.Fatalf("expected all UI noise dropped, got %q", got)
	}
}
