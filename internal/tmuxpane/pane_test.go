package tmuxpane

import "testing"

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
