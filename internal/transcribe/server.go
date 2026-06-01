package transcribe

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Transcriber turns a 16 kHz mono WAV file into text.
type Transcriber interface {
	Transcribe(ctx context.Context, wavPath string) (string, error)
}

// CLI transcribes by spawning whisper-cli per call — simple, but the model
// reloads every time (~1s+ of fixed cost per utterance). Fine for one-shot use.
type CLI struct{ Opts Options }

func (c CLI) Transcribe(ctx context.Context, wavPath string) (string, error) {
	return FromWAV(ctx, wavPath, c.Opts)
}

// Server keeps a whisper-server process warm — the model loads once and stays
// resident — and transcribes over its local HTTP API. For multi-utterance modes
// like `talk` this roughly halves per-utterance latency vs spawning the CLI.
type Server struct {
	cmd    *exec.Cmd
	url    string
	client *http.Client
}

// StartServer launches a warm whisper-server and blocks until it's ready (model
// loaded). Returns an error if whisper-server or the model is missing, so callers
// can fall back to the CLI.
func StartServer(ctx context.Context, opts Options) (*Server, error) {
	bin := serverBin(opts.WhisperBin)
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("%s not found", bin)
	}
	if _, err := os.Stat(opts.ModelPath); err != nil {
		return nil, fmt.Errorf("model not found at %s", opts.ModelPath)
	}
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	args := []string{
		"-m", opts.ModelPath,
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(port),
		"-l", orDefault(opts.Language, "en"),
	}
	if opts.Prompt != "" {
		args = append(args, "--prompt", opts.Prompt, "--carry-initial-prompt")
	}
	cmd := exec.Command(bin, args...) // lifecycle managed via Close, not ctx
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s := &Server{
		cmd:    cmd,
		url:    fmt.Sprintf("http://127.0.0.1:%d/inference", port),
		client: &http.Client{},
	}
	base := fmt.Sprintf("http://127.0.0.1:%d/", port)
	if err := waitReady(ctx, base, 30*time.Second); err != nil {
		s.Close()
		return nil, err
	}
	_ = s.warmup(ctx) // trigger one-time GPU shader compile so utterance #1 is fast
	return s, nil
}

func (s *Server) Transcribe(ctx context.Context, wavPath string) (string, error) {
	f, err := os.Open(wavPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return s.post(ctx, f)
}

func (s *Server) post(ctx context.Context, audio io.Reader) (string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, _ := w.CreateFormFile("file", "audio.wav")
	if _, err := io.Copy(fw, audio); err != nil {
		return "", err
	}
	_ = w.WriteField("response_format", "text")
	_ = w.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("whisper-server: %s", strings.TrimSpace(string(out)))
	}
	return clean(string(out)), nil
}

func (s *Server) warmup(ctx context.Context) error {
	_, err := s.post(ctx, bytes.NewReader(silentWAV(160))) // ~10ms of silence
	return err
}

// Close stops the whisper-server process.
func (s *Server) Close() {
	if s != nil && s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_, _ = s.cmd.Process.Wait()
	}
}

func waitReady(ctx context.Context, base string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base, nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("whisper-server did not become ready in %s", timeout)
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func serverBin(whisperBin string) string {
	if strings.Contains(whisperBin, "whisper-cli") {
		return strings.Replace(whisperBin, "whisper-cli", "whisper-server", 1)
	}
	return "whisper-server"
}

// silentWAV returns a minimal 16 kHz mono PCM16 WAV with n samples of silence,
// for warming up the server.
func silentWAV(n int) []byte {
	dataBytes := n * 2
	buf := make([]byte, 44+dataBytes)
	copy(buf[0:], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:], uint32(36+dataBytes))
	copy(buf[8:], "WAVE")
	copy(buf[12:], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:], 16)
	binary.LittleEndian.PutUint16(buf[20:], 1)     // PCM
	binary.LittleEndian.PutUint16(buf[22:], 1)     // mono
	binary.LittleEndian.PutUint32(buf[24:], 16000) // sample rate
	binary.LittleEndian.PutUint32(buf[28:], 16000*2)
	binary.LittleEndian.PutUint16(buf[32:], 2)
	binary.LittleEndian.PutUint16(buf[34:], 16)
	copy(buf[36:], "data")
	binary.LittleEndian.PutUint32(buf[40:], uint32(dataBytes))
	return buf
}
