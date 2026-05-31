package command

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		cmd  bool
		verb string
		arg  string
	}{
		{"Computer, connect to backend.", true, "connect", "backend"},
		{"computer switch to reporting", true, "connect", "reporting"},
		{"computer connect saas", true, "connect", "saas"},
		{"computer pause", true, "pause", ""},
		{"computer resume", true, "resume", ""},
		{"COMPUTER quit", true, "quit", ""},
		{"computer panes", true, "panes", ""},
		{"computer", true, "help", ""},
		{"run the failing tests", false, "", ""},
		{"let's check the backend", false, "", ""},
	}
	for _, c := range cases {
		p := Parse(c.in, "computer")
		if p.IsCommand != c.cmd || p.Verb != c.verb || p.Arg != c.arg {
			t.Errorf("Parse(%q) = %+v, want {cmd:%v verb:%q arg:%q}", c.in, p, c.cmd, c.verb, c.arg)
		}
	}
}
