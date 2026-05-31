package transcribe

import "testing"

func TestCleanFiltersHallucinations(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"real speech", "Hello, this is Carlos.", "Hello, this is Carlos."},
		{"blank audio cue", "[BLANK_AUDIO]", ""},
		{"music cue parens", "(upbeat music)", ""},
		{"music cue asterisk", "*sad music*", ""},
		{"lone dash", "-", ""},
		{"punctuation only", "...", ""},
		{"repeated sentence loop", "I'm a little bit nervous. I'm a little bit nervous. I'm a little bit nervous.", "I'm a little bit nervous."},
		{"repeated lines", "Tmux.\nTmux.\nTmux.", "Tmux."},
		{"cue then speech", "(music)\nLet's run the tests.", "Let's run the tests."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := clean(c.in); got != c.want {
				t.Fatalf("clean(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
