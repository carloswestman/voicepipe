package speak

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// OpenAIOptions configures an OpenAI-compatible /audio/speech backend. The same
// client serves a local Kokoro server (BaseURL on localhost, no APIKey — nicer
// offline voices) and a hosted provider (BaseURL + APIKey); only the URL and key
// differ. `say` stays the default, so this is strictly opt-in.
type OpenAIOptions struct {
	BaseURL string // API base, e.g. http://127.0.0.1:8880/v1
	APIKey  string // bearer token for a hosted provider; empty for a local server
	Voice   string // voice id, e.g. "af_heart" (Kokoro) or "alloy" (OpenAI)
	Model   string // model name, e.g. "kokoro" or "tts-1"
	Format  string // audio format afplay can play (mp3, wav, aac)
}

// OpenAISpeaker synthesizes speech over an OpenAI-compatible HTTP API and plays
// it with afplay. Playback is interruptible: cancelling the context aborts the
// synthesis request and the afplay process alike.
type OpenAISpeaker struct {
	Opts   OpenAIOptions
	client *http.Client
}

// NewOpenAISpeaker fills in sensible defaults (Kokoro voice/model, mp3) so a
// minimal config just works against a local Kokoro server.
func NewOpenAISpeaker(o OpenAIOptions) *OpenAISpeaker {
	if o.Voice == "" {
		o.Voice = "af_heart"
	}
	if o.Model == "" {
		o.Model = "kokoro"
	}
	if o.Format == "" {
		o.Format = "mp3"
	}
	return &OpenAISpeaker{Opts: o, client: &http.Client{}}
}

// Speak synthesizes text and plays it, blocking until playback finishes. Empty
// text is a no-op. Cancelling ctx interrupts synthesis or playback.
func (s *OpenAISpeaker) Speak(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	audio, err := s.synth(ctx, text)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp("", "voicepipe-tts-*."+s.Opts.Format)
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(audio); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return exec.CommandContext(ctx, "afplay", f.Name()).Run()
}

func (s *OpenAISpeaker) synth(ctx context.Context, text string) ([]byte, error) {
	body, _ := json.Marshal(map[string]any{
		"model":           s.Opts.Model,
		"input":           text,
		"voice":           s.Opts.Voice,
		"response_format": s.Opts.Format,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint("/audio/speech"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.Opts.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.Opts.APIKey)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tts server %d: %s", resp.StatusCode, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (s *OpenAISpeaker) endpoint(path string) string {
	return strings.TrimRight(s.Opts.BaseURL, "/") + path
}

// Reachable reports whether the TTS endpoint answers at all (any HTTP response
// counts — a 404 still means the server is up). Used to decide whether to launch
// a managed server, and by `doctor`.
func (s *OpenAISpeaker) Reachable(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint("/audio/voices"), nil)
	if err != nil {
		return err
	}
	if s.Opts.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.Opts.APIKey)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// ServerProc is a local TTS server launched and owned by voicepipe for the
// duration of a `talk` session.
type ServerProc struct{ cmd *exec.Cmd }

// StartServer launches a local OpenAI-compatible TTS server via commandLine
// (e.g. a speech-server binary path, or "docker run …") and blocks until reach
// reports the endpoint ready. The process is stopped with Close. Lifecycle is
// managed explicitly (not tied to ctx) so Close fully reaps it; ctx only bounds
// the readiness wait. The first launch may download models, so timeout should be
// generous.
func StartServer(ctx context.Context, commandLine string, reach func(context.Context) error, timeout time.Duration) (*ServerProc, error) {
	argv := strings.Fields(commandLine)
	if len(argv) == 0 {
		return nil, fmt.Errorf("empty server command")
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		return nil, fmt.Errorf("%s not found", argv[0])
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s := &ServerProc{cmd: cmd}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			s.Close()
			return nil, ctx.Err()
		}
		if reach(ctx) == nil {
			return s, nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	s.Close()
	return nil, fmt.Errorf("TTS server did not become ready in %s", timeout)
}

// Close stops the managed server process.
func (s *ServerProc) Close() {
	if s != nil && s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_, _ = s.cmd.Process.Wait()
	}
}
