// Command voicepipe streams voice to text into your terminal: speak, whisper.cpp
// transcribes locally, and the text is typed into the focused window. tmux users
// can opt into per-pane routing to talk to specific Claude Code agents.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/carloswestman/voicepipe/internal/audio"
	"github.com/carloswestman/voicepipe/internal/config"
	"github.com/carloswestman/voicepipe/internal/inject"
	"github.com/carloswestman/voicepipe/internal/speak"
	"github.com/carloswestman/voicepipe/internal/tmuxpane"
	"github.com/carloswestman/voicepipe/internal/transcribe"
)

const modelURL = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-large-v3-turbo.bin"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "capture":
		err = cmdCapture(ctx, os.Args[2:])
	case "listen":
		err = cmdListen(ctx, os.Args[2:])
	case "talk":
		err = cmdTalk(ctx, os.Args[2:])
	case "type":
		err = cmdType(ctx, os.Args[2:])
	case "init":
		err = cmdInit(ctx)
	case "panes":
		err = cmdPanes(ctx)
	case "devices":
		err = cmdDevices()
	case "doctor":
		err = cmdDoctor()
	case "tmux-install":
		err = cmdTmuxInstall()
	case "version", "-v", "--version":
		fmt.Println("voicepipe dev")
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`voicepipe — stream voice to text into your terminal

usage:
  voicepipe init                 download the whisper model and write config
  voicepipe capture              record one utterance, type it into the focused window
                                 (--send submits with Enter)
            [--tmux | --target P] opt into tmux routing (no permission, per-pane)
            [--clipboard]         copy to clipboard instead (--clipboard-paste auto-pastes)
  voicepipe listen [flags]       keep listening: speak → pause → send, repeatedly
                                 (auto-submits each utterance; --no-send to disable;
                                  same sink flags as capture; Ctrl-C to stop)
            [--verbose | -v]      (capture/listen) log audio metrics + timing per utterance
  voicepipe talk [--pane P]      two-way: speak to an agent (pane P, default active),
                                 hear its reply read aloud (--voice NAME, --rate WPM)
                                 (--pane and --target are aliases; P from "voicepipe panes")
  voicepipe type <text>          type given text via the keystroke sink (test typing)
  voicepipe panes                list tmux panes (find a --target for talk)
  voicepipe devices              list microphone input devices
  voicepipe doctor               check that whisper-cpp, the model, and sinks are ready
  voicepipe tmux-install         print the tmux binding for power-mode routing

config: ` + config.Path() + `
`)
}

// sinkFromFlags builds the delivery sink and submit flag from config defaults,
// overridden by capture/listen flags. Shared by `capture` and `listen`.
// defaultSubmit sets the baseline (listen submits each utterance by default,
// capture does not); --send / --no-send override it either way.
func sinkFromFlags(cfg config.Config, args []string, defaultSubmit bool) (inject.Sink, bool) {
	target := ""
	sendEnter := defaultSubmit || cfg.SendEnter
	kind := cfg.Sink
	clipboardPaste := cfg.ClipboardPaste
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--target", "-t":
			if i+1 < len(args) {
				target = args[i+1]
				i++
			}
			kind = inject.KindTmux // a target only makes sense for the tmux sink
		case "--tmux":
			kind = inject.KindTmux
		case "--clipboard":
			kind = inject.KindClipboard
		case "--clipboard-paste":
			kind = inject.KindClipboard
			clipboardPaste = true
		case "--send", "-s":
			sendEnter = true
		case "--no-send":
			sendEnter = false
		}
	}
	sink := inject.Choose(inject.Selection{
		Kind:           kind,
		TmuxTarget:     target,
		ClipboardPaste: clipboardPaste,
	})
	return sink, sendEnter
}

// recordOnce captures one spoken utterance and returns its transcription
// (empty string if nothing was heard). When verbose, it logs capture metrics and
// timing to stderr to help explain what happened.
func recordOnce(ctx context.Context, cfg config.Config, verbose bool) (string, error) {
	capStart := time.Now()
	samples, stats, err := audio.CaptureUtterance(ctx, audio.Options{
		DeviceSubstr: cfg.InputDevice,
		SilenceMs:    cfg.SilenceMs,
		MaxSeconds:   cfg.MaxSeconds,
	})
	if err != nil {
		return "", err
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "[audio]   %.2fs captured → %.2fs sent, %.2fs voiced, peak RMS %.0f (capture %.2fs)\n",
			stats.DurationMs/1000, stats.SentMs/1000, stats.VoicedMs/1000, stats.PeakRMS, time.Since(capStart).Seconds())
	}
	if len(samples) == 0 {
		if verbose {
			fmt.Fprintln(os.Stderr, "[audio]   rejected: not enough voiced audio (noise/silence)")
		}
		return "", nil
	}
	wav := filepath.Join(os.TempDir(), "voicepipe.wav")
	if err := audio.WriteWAV(wav, samples); err != nil {
		return "", err
	}
	defer os.Remove(wav)

	txStart := time.Now()
	text, err := transcribe.FromWAV(ctx, wav, transcribe.Options{
		WhisperBin: cfg.WhisperBin,
		ModelPath:  cfg.ModelPath,
		Language:   cfg.Language,
		Prompt:     cfg.Prompt,
	})
	if verbose && err == nil {
		fmt.Fprintf(os.Stderr, "[whisper] transcribed in %.2fs: %q\n", time.Since(txStart).Seconds(), text)
	}
	return text, err
}

// hasFlag reports whether any of the given flag names appears in args.
func hasFlag(args []string, names ...string) bool {
	for _, a := range args {
		for _, n := range names {
			if a == n {
				return true
			}
		}
	}
	return false
}

func cmdCapture(ctx context.Context, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	sink, sendEnter := sinkFromFlags(cfg, args, false) // one-shot: don't submit by default
	verbose := hasFlag(args, "--verbose", "-v")

	text, err := recordOnce(ctx, cfg, verbose)
	if err != nil {
		return err
	}
	if text == "" {
		fmt.Fprintln(os.Stderr, "voicepipe: nothing heard")
		return nil
	}
	return sink.Deliver(ctx, text, sendEnter)
}

// cmdListen runs capture→transcribe→inject in a loop until interrupted (Ctrl-C),
// so you can talk continuously. With the keystroke sink, each utterance lands in
// whatever window is focused at that moment — switch focus to talk to a different
// agent. Add --send to submit each utterance (conversational mode).
func cmdListen(ctx context.Context, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	sink, sendEnter := sinkFromFlags(cfg, args, true) // conversational: submit each utterance
	verbose := hasFlag(args, "--verbose", "-v")

	fmt.Fprintln(os.Stderr, "voicepipe: listening — speak, pause to send. Ctrl-C to stop.")
	for ctx.Err() == nil {
		text, err := recordOnce(ctx, cfg, verbose)
		if err != nil {
			if ctx.Err() != nil {
				break // interrupted mid-capture
			}
			fmt.Fprintln(os.Stderr, "voicepipe: error:", err)
			continue
		}
		if text == "" {
			continue
		}
		if err := sink.Deliver(ctx, text, sendEnter); err != nil {
			fmt.Fprintln(os.Stderr, "voicepipe: deliver:", err)
			continue
		}
		fmt.Fprintf(os.Stderr, "  → %s\n", text)
	}
	fmt.Fprintln(os.Stderr, "\nvoicepipe: stopped.")
	return nil
}

// cmdTalk is two-way voice: speak to one agent (a tmux pane) and hear its reply
// read aloud. You talk → it types + submits into the pane → it waits for the
// reply to settle → speaks the new text via `say`. Defaults the target to the
// active pane. This is the foundation for hands-free, eyes-free agent control.
func cmdTalk(ctx context.Context, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if !speak.Available() {
		return fmt.Errorf("`say` not found — talk needs macOS text-to-speech")
	}

	target := ""
	voice := cfg.Voice
	rate := cfg.SpeechRate
	verbose := hasFlag(args, "--verbose", "-v")
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--target", "--pane", "-t":
			if i+1 < len(args) {
				target = args[i+1]
				i++
			}
		case "--voice":
			if i+1 < len(args) {
				voice = args[i+1]
				i++
			}
		case "--rate":
			if i+1 < len(args) {
				if n, e := strconv.Atoi(args[i+1]); e == nil {
					rate = n
				}
				i++
			}
		case "--verbose", "-v":
			// captured via hasFlag above; accept so it isn't treated as unknown
		default:
			// Surface typos loudly instead of silently misfiring. A bare value is
			// accepted as the target (e.g. `talk dev:whisper.0`).
			if strings.HasPrefix(a, "-") {
				return fmt.Errorf("unknown flag: %s (see `voicepipe help`)", a)
			}
			if target != "" {
				return fmt.Errorf("unexpected argument: %s", a)
			}
			target = a
		}
	}

	if target == "" {
		if target, err = tmuxpane.ActivePane(ctx); err != nil {
			return err
		}
	} else if target, err = tmuxpane.ResolvePane(ctx, target); err != nil {
		return err
	}

	// Guard the footgun: targeting voicepipe's own pane types into its own shell.
	if own := os.Getenv("TMUX_PANE"); own != "" && own == target {
		fmt.Fprintf(os.Stderr, "voicepipe: warning — target %s is this pane (voicepipe's own); you likely want a different agent. See `voicepipe panes`.\n", target)
	}

	sink := inject.TmuxSink{Target: target}
	sopts := speak.Options{Voice: voice, Rate: rate}

	fmt.Fprintf(os.Stderr, "voicepipe: talking with pane %s — speak, pause to send; replies read aloud. Ctrl-C to stop.\n", target)
	for ctx.Err() == nil {
		text, err := recordOnce(ctx, cfg, verbose)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			fmt.Fprintln(os.Stderr, "voicepipe: error:", err)
			continue
		}
		if text == "" {
			continue
		}

		// Baseline the pane before sending so the reply diff excludes prior output.
		baseline, err := tmuxpane.Capture(ctx, target)
		if err != nil {
			fmt.Fprintln(os.Stderr, "voicepipe: capture:", err)
			continue
		}
		if err := sink.Deliver(ctx, text, true); err != nil {
			fmt.Fprintln(os.Stderr, "voicepipe: deliver:", err)
			continue
		}
		fmt.Fprintf(os.Stderr, "  → %s\n", text)

		reply, err := tmuxpane.WaitForReply(ctx, target, baseline, text)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			fmt.Fprintln(os.Stderr, "voicepipe: reply:", err)
			continue
		}
		if reply == "" {
			if verbose {
				fmt.Fprintln(os.Stderr, "[reply]   (nothing new to read)")
			}
			continue
		}
		fmt.Fprintf(os.Stderr, "  ◀ %s\n", reply)
		if err := speak.Say(ctx, reply, sopts); err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "voicepipe: say:", err)
		}
	}
	fmt.Fprintln(os.Stderr, "\nvoicepipe: stopped.")
	return nil
}

// cmdType types its argument via the keystroke sink — a quick way to test typing
// (and the Accessibility grant) without recording audio. Give it a 2s head start
// so you can focus the target window.
func cmdType(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: voicepipe type <text>")
	}
	text := strings.Join(args, " ")
	fmt.Fprintln(os.Stderr, "voicepipe: typing in 2s — focus the target window…")
	time.Sleep(2 * time.Second)
	return inject.KeystrokeSink{}.Deliver(ctx, text, false)
}

func cmdInit(ctx context.Context) error {
	cfg := config.Default()
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Println("wrote config:", config.Path())

	if _, err := os.Stat(cfg.ModelPath); err == nil {
		fmt.Println("model already present:", cfg.ModelPath)
		return nil
	}
	if err := os.MkdirAll(config.ModelDir(), 0o755); err != nil {
		return err
	}
	fmt.Println("downloading model (~1.5 GB) to", cfg.ModelPath)
	return download(ctx, modelURL, cfg.ModelPath)
}

func download(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: status %s", url, resp.Status)
	}

	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()
	return os.Rename(tmp, dest)
}

// cmdPanes lists every tmux pane so you can pick a --target for `talk`.
func cmdPanes(ctx context.Context) error {
	panes, err := tmuxpane.ListPanes(ctx)
	if err != nil {
		return err
	}
	if len(panes) == 0 {
		fmt.Println("no tmux panes found")
		return nil
	}
	fmt.Println("tmux panes (pass the id or location to --target):")
	for _, p := range panes {
		active := ""
		if p.Active {
			active = "  (active)"
		}
		fmt.Printf("  %-5s  %-28s  [%s]%s\n", p.ID, p.Location, p.Command, active)
	}
	return nil
}

func cmdDevices() error {
	names, err := audio.ListDevices()
	if err != nil {
		return err
	}
	fmt.Println("input devices:")
	for _, n := range names {
		fmt.Println("  -", n)
	}
	fmt.Println("\nset the one you want with the input_device field in", config.Path())
	return nil
}

func cmdDoctor() error {
	cfg, _ := config.Load()
	ok := true
	required := func(label string, good bool, hint string) {
		mark := "ok"
		if !good {
			mark = "MISSING"
			ok = false
		}
		fmt.Printf("  [%s] %s\n", mark, label)
		if !good && hint != "" {
			fmt.Println("        →", hint)
		}
	}
	info := func(label, note string) {
		fmt.Printf("  [info] %s\n", label)
		if note != "" {
			fmt.Println("        →", note)
		}
	}

	// Required for any sink: transcription.
	required("whisper-cli on PATH", onPath(cfg.WhisperBin), "brew install whisper-cpp")
	_, modelErr := os.Stat(cfg.ModelPath)
	required("model present", modelErr == nil, "voicepipe init")

	// Sink-specific guidance.
	fmt.Printf("\nsink: %s\n", cfg.Sink)
	switch cfg.Sink {
	case inject.KindTmux:
		required("tmux on PATH", onPath("tmux"), "brew install tmux")
	case inject.KindClipboard:
		required("clipboard tool available", inject.ClipboardAvailable(), "macOS has pbcopy; on Linux install wl-clipboard or xclip")
		if cfg.ClipboardPaste {
			info("auto-paste enabled", "needs Accessibility on macOS: System Settings → Privacy & Security → Accessibility")
		}
	default:
		info("keystroke sink (default)", "grant Accessibility on first use: System Settings → Privacy & Security → Accessibility")
		if onPath("tmux") {
			info("tmux available", "opt into per-pane routing with `voicepipe tmux-install`")
		}
	}

	if !ok {
		return fmt.Errorf("doctor found problems")
	}
	fmt.Println("\nall good — try `voicepipe capture --send`")
	return nil
}

func cmdTmuxInstall() error {
	bin, err := os.Executable()
	if err != nil || bin == "" {
		bin = "voicepipe"
	}
	fmt.Printf(`add this to ~/.tmux.conf, then run `+"`tmux source-file ~/.tmux.conf`"+`:

  # voicepipe: hold-to-talk into the focused pane (prefix + v)
  bind v run-shell -b "%s capture --target '#{pane_id}'"

  # variant that also submits the prompt (prefix + V)
  bind V run-shell -b "%s capture --send --target '#{pane_id}'"
`, bin, bin)
	return nil
}

func onPath(name string) bool {
	if name == "" {
		return false
	}
	// Absolute/relative paths: stat them; bare names: search PATH.
	if filepath.IsAbs(name) {
		_, err := os.Stat(name)
		return err == nil
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}
