// Package transcribe runs whisper.cpp on a captured utterance. v1 shells out to
// the whisper-cli binary (installed via `brew install whisper-cpp`), which keeps
// the build trivial. A later version can link libwhisper directly for lower
// latency.
package transcribe

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Options configures a transcription run.
type Options struct {
	WhisperBin string // e.g. "whisper-cli"
	ModelPath  string // GGML model file
	Language   string // e.g. "en"
	Prompt     string // initial prompt to bias vocabulary (may be empty)
}

// FromWAV transcribes a 16 kHz mono WAV file and returns the cleaned text.
func FromWAV(ctx context.Context, wavPath string, opts Options) (string, error) {
	if _, err := os.Stat(opts.ModelPath); err != nil {
		return "", fmt.Errorf("model not found at %s — run `voicepipe init`: %w", opts.ModelPath, err)
	}

	// -nt: no timestamps, -np: no progress prints, -otxt off (read stdout).
	args := []string{
		"-m", opts.ModelPath,
		"-f", wavPath,
		"-nt",
		"-np",
		"-sns", // suppress non-speech tokens (music/applause cues, etc.)
		"-l", orDefault(opts.Language, "en"),
	}
	if opts.Prompt != "" {
		args = append(args, "--prompt", opts.Prompt)
	}
	cmd := exec.CommandContext(ctx, opts.WhisperBin, args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("whisper failed: %w: %s", err, strings.TrimSpace(errBuf.String()))
	}
	return clean(out.String()), nil
}

var (
	// nonSpeechCue matches a line that is entirely a bracketed/parenthesized/
	// asterisked cue — e.g. "[BLANK_AUDIO]", "(upbeat music)", "*sad music*" —
	// which whisper hallucinates on silence or noise.
	nonSpeechCue = regexp.MustCompile(`^[\[(*][^\])*]*[\])*]$`)
	// sentenceRe splits text into rough sentences for de-duplication.
	sentenceRe = regexp.MustCompile(`[^.!?]+[.!?]?`)
	// hasAlnum is true if a string contains any letter or digit (real content).
	hasAlnum = regexp.MustCompile(`[\p{L}\p{N}]`)
)

// clean turns raw whisper output into deliverable text, or "" if the audio was
// silence/noise. It drops non-speech cues, collapses the repeated-line and
// repeated-sentence loops whisper falls into on non-speech, and rejects output
// with no real content (e.g. a lone "-").
func clean(s string) string {
	var kept []string
	var prevLine string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || nonSpeechCue.MatchString(line) {
			continue
		}
		if strings.EqualFold(line, prevLine) { // collapse repeated lines
			continue
		}
		kept = append(kept, line)
		prevLine = line
	}

	out := collapseRepeatedSentences(strings.Join(kept, " "))
	out = strings.TrimSpace(out)
	if !hasAlnum.MatchString(out) || utf8.RuneCountInString(out) < 2 {
		return "" // punctuation-only / single-char noise
	}
	return out
}

// collapseRepeatedSentences removes consecutive duplicate sentences, e.g.
// "I'm nervous. I'm nervous. I'm nervous." → "I'm nervous.".
func collapseRepeatedSentences(s string) string {
	var out []string
	var prev string
	for _, sent := range sentenceRe.FindAllString(s, -1) {
		t := strings.TrimSpace(sent)
		if t == "" {
			continue
		}
		if strings.EqualFold(t, prev) {
			continue
		}
		out = append(out, t)
		prev = t
	}
	return strings.Join(out, " ")
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
