// Package tmuxpane reads a tmux pane to detect when an agent has finished
// responding and to extract its reply — the "what to read" half of two-way
// voice. The extraction is heuristic: a Claude Code TUI is full of spinners,
// boxes, and tool output, so we diff against a baseline and filter UI noise.
package tmuxpane

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Pane describes one tmux pane, for the `panes` helper and target discovery.
type Pane struct {
	ID       string // e.g. "%3"
	Location string // e.g. "dev:reporting.1" (session:window.pane)
	Command  string // current foreground command
	Active   bool   // active pane in its window
}

// ListPanes returns every tmux pane across all sessions.
func ListPanes(ctx context.Context) ([]Pane, error) {
	const f = "#{pane_id}\t#{session_name}:#{window_name}.#{pane_index}\t#{pane_current_command}\t#{pane_active}"
	out, err := exec.CommandContext(ctx, "tmux", "list-panes", "-a", "-F", f).Output()
	if err != nil {
		return nil, fmt.Errorf("list tmux panes (is tmux running?): %w", err)
	}
	return parsePanes(string(out)), nil
}

// Window returns the window-name portion of a pane's location
// ("dev:reporting.1" → "reporting").
func (p Pane) Window() string {
	loc := p.Location
	if i := strings.IndexByte(loc, ':'); i >= 0 {
		loc = loc[i+1:]
	}
	if j := strings.LastIndexByte(loc, '.'); j >= 0 {
		loc = loc[:j]
	}
	return loc
}

// FindByName returns the first pane whose window name matches the spoken name
// (exact match preferred, then substring either way). Used as the fallback when
// no agent registry is configured.
func FindByName(ctx context.Context, name string) (Pane, bool, error) {
	panes, err := ListPanes(ctx)
	if err != nil {
		return Pane{}, false, err
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return Pane{}, false, nil
	}
	for _, p := range panes {
		if strings.ToLower(p.Window()) == name {
			return p, true, nil
		}
	}
	for _, p := range panes {
		w := strings.ToLower(p.Window())
		if strings.Contains(w, name) || strings.Contains(name, w) {
			return p, true, nil
		}
	}
	return Pane{}, false, nil
}

// WindowOf returns the window name for a target spec, or the target itself if it
// can't be resolved.
func WindowOf(ctx context.Context, target string) string {
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", target, "#{window_name}").Output()
	if err != nil {
		return target
	}
	if w := strings.TrimSpace(string(out)); w != "" {
		return w
	}
	return target
}

func parsePanes(out string) []Pane {
	var panes []Pane
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		p := strings.Split(line, "\t")
		if len(p) < 4 {
			continue
		}
		panes = append(panes, Pane{ID: p[0], Location: p[1], Command: p[2], Active: p[3] == "1"})
	}
	return panes
}

// ActivePane returns the id of the currently active tmux pane.
func ActivePane(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "#{pane_id}").Output()
	if err != nil {
		return "", fmt.Errorf("get active tmux pane (are you inside tmux?): %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ResolvePane canonicalizes a target spec (pane id or session:window.pane) to a
// pane id, validating that it exists. Returns a helpful error for typos.
func ResolvePane(ctx context.Context, target string) (string, error) {
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", target, "#{pane_id}").Output()
	id := strings.TrimSpace(string(out))
	if err != nil || id == "" {
		return "", fmt.Errorf("no tmux pane %q — run `voicepipe panes` to list targets", target)
	}
	return id, nil
}

// Capture returns the visible text of a tmux pane ("" target = active pane).
func Capture(ctx context.Context, target string) (string, error) {
	args := []string{"capture-pane", "-p"}
	if target != "" {
		args = append(args, "-t", target)
	}
	out, err := exec.CommandContext(ctx, "tmux", args...).Output()
	if err != nil {
		return "", fmt.Errorf("capture tmux pane: %w", err)
	}
	return string(out), nil
}

// CaptureFull captures the pane including recent scrollback (so long, scrolled
// replies aren't truncated to the visible screen). Returns "" on error.
func CaptureFull(ctx context.Context, target string) string {
	args := []string{"capture-pane", "-p", "-S", "-500"}
	if target != "" {
		args = append(args, "-t", target)
	}
	out, err := exec.CommandContext(ctx, "tmux", args...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// workingIndicator matches only the animated braille spinner — which moves just
// while the agent is generating. We deliberately do NOT match "esc to interrupt":
// in auto-accept mode that hint sits in the footer permanently, so it would read
// as forever-busy. Stability of the screen is the primary "done" signal; the
// spinner is a backup so a streaming pause isn't mistaken for done.
var workingIndicator = regexp.MustCompile(`[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏]`)

// Working reports whether a pane capture shows the agent actively working.
func Working(capture string) bool {
	return workingIndicator.MatchString(capture)
}

// NewReply returns the cleaned assistant text present in cur but not in prev,
// with the echoed input (sent) and UI noise removed — what the watcher speaks.
func NewReply(prev, cur string, sent []string) string {
	return filterReply(delta(prev, cur), sent)
}

// WaitForReply polls the pane until its content stops changing (the agent has
// finished responding), then returns the new text that appeared since baseline,
// with the echoed input `sent` and obvious UI noise removed. baseline is a
// capture taken just before the input was sent.
func WaitForReply(ctx context.Context, target, baseline, sent string) (string, error) {
	const (
		poll      = 400 * time.Millisecond
		stableFor = 1600 * time.Millisecond
		maxWait   = 120 * time.Second
	)
	start := time.Now()
	last := ""
	lastChange := time.Now()
	first := true
	changed := false

	for {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		cur, err := Capture(ctx, target)
		if err != nil {
			return "", err
		}
		if cur != baseline {
			changed = true
		}
		switch {
		case first || cur != last:
			last = cur
			lastChange = time.Now()
			first = false
		case changed && time.Since(lastChange) >= stableFor:
			return filterReply(delta(baseline, cur), []string{sent}), nil
		}
		if time.Since(start) >= maxWait {
			return filterReply(delta(baseline, cur), []string{sent}), nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(poll):
		}
	}
}

// delta returns the lines in cur that were not present in baseline, in order.
func delta(baseline, cur string) []string {
	seen := map[string]bool{}
	for _, l := range strings.Split(baseline, "\n") {
		seen[strings.TrimRight(l, " ")] = true
	}
	var out []string
	for _, l := range strings.Split(cur, "\n") {
		t := strings.TrimRight(l, " ")
		if t == "" || seen[t] {
			continue
		}
		out = append(out, t)
	}
	return out
}

var (
	// uiLine matches a line that is only box-drawing / separators / whitespace.
	uiLine = regexp.MustCompile(`^[\s│─╭╮╰╯├┤┌┐└┘▌▏▕·•*=_\-—]+$`)
	// statusLine matches spinner / status / token-count chrome.
	statusLine = regexp.MustCompile(`(?i)(esc to interrupt|tokens?|ctrl\+|✻|✶|✻|⏵|⎿|◯|↑|↓|\b\d+s\b)`)
)

// filterReply drops UI noise and any echoed input (sent — several messages may be
// in flight), returning a single spoken line.
func filterReply(lines []string, sent []string) string {
	var kept []string
	for _, l := range lines {
		s := strings.TrimSpace(l)
		if s == "" || uiLine.MatchString(s) || statusLine.MatchString(s) {
			continue
		}
		if strings.HasPrefix(s, ">") { // input prompt line
			continue
		}
		if matchesAny(s, sent) { // our own echoed input
			continue
		}
		kept = append(kept, s)
	}
	return sanitizeForSpeech(strings.Join(kept, " "))
}

func matchesAny(line string, sent []string) bool {
	for _, x := range sent {
		x = strings.TrimSpace(x)
		if x != "" && (strings.Contains(line, x) || strings.Contains(x, line)) {
			return true
		}
	}
	return false
}

// sanitizeForSpeech strips characters that text-to-speech would awkwardly name —
// the leading ● / ⏺ response marker, backticks, asterisks, box drawing, arrows,
// emoji — keeping letters, numbers, and basic punctuation so `say` reads clean
// prose instead of "black circle, backtick, ...".
func sanitizeForSpeech(s string) string {
	const keepPunct = ".,!?;:'\"()-’"
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r), unicode.IsNumber(r), unicode.IsSpace(r):
			b.WriteRune(r)
		case strings.ContainsRune(keepPunct, r):
			b.WriteRune(r)
		default:
			b.WriteRune(' ') // drop symbols/emoji, leave a gap
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
