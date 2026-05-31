// Package command parses spoken voicepipe commands in a `talk` session. An
// utterance is a command only when it starts with the configured wake word
// (e.g. "computer"); everything else is content for the connected agent. This
// keeps control and dictation cleanly separated.
package command

import (
	"strings"
	"unicode"
)

// Parsed is the result of interpreting one utterance.
type Parsed struct {
	IsCommand bool   // started with the wake word
	Verb      string // canonical verb: connect, panes, status, pause, resume, send, help, quit
	Arg       string // remainder (e.g. the agent name for connect)
}

// Parse returns whether the utterance is a command (starts with wakeWord) and,
// if so, its canonical verb and argument. The wake word alone maps to "help".
func Parse(utterance, wakeWord string) Parsed {
	fields := normalize(utterance)
	wake := strings.ToLower(strings.TrimSpace(wakeWord))
	if len(fields) == 0 || fields[0] != wake {
		return Parsed{}
	}
	if len(fields) == 1 {
		return Parsed{IsCommand: true, Verb: "help"}
	}
	verb := canonical(fields[1])
	arg := strings.Join(fields[2:], " ")
	if verb == "connect" {
		arg = strings.TrimSpace(strings.TrimPrefix(arg, "to ")) // "switch to backend" → "backend"
	}
	return Parsed{IsCommand: true, Verb: verb, Arg: arg}
}

// canonical maps spoken synonyms to a single verb.
func canonical(w string) string {
	switch w {
	case "pause", "mute", "hold":
		return "pause"
	case "resume", "unpause", "unmute", "continue":
		return "resume"
	case "connect", "switch", "go", "talk":
		return "connect"
	case "pane", "panes", "list", "agents":
		return "panes"
	case "status", "where", "who":
		return "status"
	case "send", "submit", "enter", "return":
		return "send"
	case "help", "commands", "command":
		return "help"
	case "quit", "exit", "bye", "goodbye":
		return "quit"
	default:
		return w // unknown; caller reports it
	}
}

// normalize lowercases, turns punctuation into spaces, and splits into words.
func normalize(s string) []string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsSpace(r) {
			return r
		}
		return ' '
	}, s)
	return strings.Fields(s)
}
