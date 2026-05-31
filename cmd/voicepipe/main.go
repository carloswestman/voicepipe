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
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/carloswestman/voicepipe/internal/audio"
	"github.com/carloswestman/voicepipe/internal/command"
	"github.com/carloswestman/voicepipe/internal/config"
	"github.com/carloswestman/voicepipe/internal/inject"
	"github.com/carloswestman/voicepipe/internal/speak"
	"github.com/carloswestman/voicepipe/internal/tmuxpane"
	"github.com/carloswestman/voicepipe/internal/transcribe"
	"github.com/carloswestman/voicepipe/internal/ui"
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
	case "agents":
		err = cmdAgents(ctx)
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
		fmt.Fprintln(os.Stderr, ui.Red("error:"), err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(ui.Bold("voicepipe") + ui.Dim(" — stream voice to text into your terminal"))
	fmt.Print(`
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
                                 in-session voice commands: say "computer help" (connect,
                                 agents, status, pause, resume, send, quit; word configurable)
  voicepipe type <text>          type given text via the keystroke sink (test typing)
  voicepipe panes                list tmux panes (find a --target for talk)
  voicepipe agents               list configured agents and whether each is alive
  voicepipe devices              list microphone input devices
  voicepipe doctor               check that whisper-cpp, the model, and sinks are ready
  voicepipe tmux-install         print the tmux binding for power-mode routing
`)
	fmt.Println("\n" + ui.Dim("config: "+config.Path()))
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
func recordOnce(ctx context.Context, cfg config.Config, verbose bool, onState func(string)) (string, error) {
	if onState != nil {
		onState("listening")
	}
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
	if onState != nil {
		onState("thinking")
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

	text, err := recordOnce(ctx, cfg, verbose, nil)
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
		text, err := recordOnce(ctx, cfg, verbose, nil)
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

	// A target is only set when given explicitly; otherwise start unconnected and
	// wait for a `connect` command — don't silently target the active pane.
	if target != "" {
		if target, err = tmuxpane.ResolvePane(ctx, target); err != nil {
			return err
		}
		if own := os.Getenv("TMUX_PANE"); own != "" && own == target {
			fmt.Fprintf(os.Stderr, "voicepipe: warning — target %s is this pane (voicepipe's own); pick a different agent.\n", target)
		}
	}

	agentLabel := "" // name shown on replies; set when connected
	if target != "" {
		agentLabel = tmuxpane.WindowOf(ctx, target)
	} else if cfg.DefaultAgent != "" {
		// Auto-connect to the configured default agent on start.
		if id, label, ok, e := resolveAgent(ctx, cfg.Agents, cfg.DefaultAgent); e == nil && ok {
			target, agentLabel = id, label
		} else {
			fmt.Fprintf(os.Stderr, "voicepipe: default agent %q not found — starting unconnected\n", cfg.DefaultAgent)
		}
	}
	sopts := speak.Options{Voice: voice, Rate: rate}
	wake := cfg.CommandWord
	// Bias whisper toward the wake word, command verbs, and agent names so spoken
	// commands like "computer panes" and "connect to agent two" transcribe right.
	cfg.Prompt = talkPrompt(cfg)
	say := func(s string) { _ = speak.Say(ctx, s, sopts) }

	// Esc-to-stop: while a reply is being read aloud, pressing Esc in this pane
	// kills playback. Active only when stdin is a terminal (cbreak succeeds).
	var (
		sayMu     sync.Mutex
		activeSay *exec.Cmd
	)
	setSay := func(c *exec.Cmd) { sayMu.Lock(); activeSay = c; sayMu.Unlock() }
	stopSay := func() {
		sayMu.Lock()
		if activeSay != nil && activeSay.Process != nil {
			_ = activeSay.Process.Kill()
		}
		sayMu.Unlock()
	}
	escStop := false
	if restore, ok := enterCbreak(); ok {
		defer restore()
		escStop = true
		go func() {
			buf := make([]byte, 1)
			for {
				n, err := os.Stdin.Read(buf)
				if err != nil {
					return
				}
				if n > 0 && buf[0] == 0x1b { // Esc
					stopSay()
				}
			}
		}()
	}
	// speakReply reads a reply aloud, interruptible by Esc.
	speakReply := func(reply string) {
		c := speak.Command(ctx, reply, sopts)
		setSay(c)
		_ = c.Run()
		setSay(nil)
	}

	// Live throbbing status line: listening / thinking / speaking. The throb runs
	// in a goroutine while the main loop is blocked in a wait; clearStatus joins it
	// (so it finishes clearing the line) before any permanent print. Interactive
	// only, and suppressed in verbose mode (which prints its own logs).
	var stopThrob func()
	throb := func(label string, style func(string) string) {
		if !escStop || verbose {
			return
		}
		if stopThrob != nil {
			stopThrob()
		}
		done, stopped := make(chan struct{}), make(chan struct{})
		go func() {
			defer close(stopped)
			frames := []string{"·", "•", "●", "•"} // pulse small → large → small
			for i := 0; ; i++ {
				fmt.Fprintf(os.Stderr, "\r\x1b[K%s", style(frames[i%len(frames)]+" "+label))
				select {
				case <-done:
					fmt.Fprint(os.Stderr, "\r\x1b[K")
					return
				case <-time.After(230 * time.Millisecond):
				}
			}
		}()
		stopThrob = func() { close(done); <-stopped; stopThrob = nil }
	}
	clearStatus := func() {
		if stopThrob != nil {
			stopThrob()
		}
	}
	var onState func(string)
	if !verbose {
		onState = func(state string) {
			switch state {
			case "listening":
				throb("listening…", ui.Orange) // orange = mic hot / your turn (matches macOS mic dot)
			case "thinking":
				throb("thinking…", ui.Dim)
			}
		}
	}

	// sendContent delivers one message to the connected pane and speaks the reply.
	sendContent := func(text string) {
		baseline, err := tmuxpane.Capture(ctx, target)
		if err != nil {
			fmt.Fprintln(os.Stderr, "voicepipe: capture:", err)
			return
		}
		if err := (inject.TmuxSink{Target: target}).Deliver(ctx, text, true); err != nil {
			fmt.Fprintln(os.Stderr, "voicepipe: deliver:", err)
			return
		}
		fmt.Fprintf(os.Stderr, "  %s %s\n", ui.Accent("→"), ui.Dim(text))
		throb("thinking…", ui.Dim)
		reply, err := tmuxpane.WaitForReply(ctx, target, baseline, text)
		clearStatus()
		if err != nil {
			fmt.Fprintln(os.Stderr, "voicepipe: reply:", err)
			return
		}
		if reply == "" {
			if verbose {
				fmt.Fprintln(os.Stderr, "[reply]   (nothing new to read)")
			}
			return
		}
		fmt.Fprintf(os.Stderr, "  %s %s %s\n", ui.Green("←"), ui.Green(agentLabel+":"), reply)
		throb("speaking…", ui.Dim)
		speakReply(reply)
		clearStatus()
	}

	talkBanner(agentLabel, target, wake, cfg.Agents)
	if escStop {
		fmt.Fprintln(os.Stderr, "  "+ui.Dim("press Esc to stop a reply mid-playback"))
	}
	paused := false

	for ctx.Err() == nil {
		text, err := recordOnce(ctx, cfg, verbose, onState)
		clearStatus()
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

		cmd := command.Parse(text, wake)
		if !cmd.IsCommand {
			if paused {
				if verbose {
					fmt.Fprintf(os.Stderr, "  (paused — say \"%s resume\") ignored: %s\n", wake, text)
				}
				continue
			}
			if target == "" {
				fmt.Fprintf(os.Stderr, "  %s\n", ui.Dim("not connected — say \""+wake+" connect to <agent>\""))
				say("Not connected. Say " + wake + " connect to an agent.")
				continue
			}
			sendContent(text)
			continue
		}

		switch cmd.Verb {
		case "pause":
			paused = true
			fmt.Fprintf(os.Stderr, "  %s %s\n", ui.Yellow("⏸"), ui.Dim("paused — say \""+wake+" resume\" to continue"))
			say("Paused.")
		case "resume":
			paused = false
			fmt.Fprintf(os.Stderr, "  %s %s\n", ui.Green("▶"), ui.Dim("resumed"))
			say("Resumed.")
		case "connect":
			id, label, ok, e := resolveAgent(ctx, cfg.Agents, cmd.Arg)
			if e != nil {
				fmt.Fprintln(os.Stderr, "voicepipe: connect:", e)
				continue
			}
			if !ok {
				fmt.Fprintf(os.Stderr, "  %s\n", ui.Yellow("no agent matching \""+cmd.Arg+"\""))
				say("No agent matching " + cmd.Arg)
				continue
			}
			target = id
			agentLabel = label
			fmt.Fprintf(os.Stderr, "  %s connected to %s %s\n", ui.Accent("⇄"), ui.Accent(label), ui.Dim("· "+id))
			say("Connected to " + label)
		case "panes":
			listAgentsAloud(ctx, cfg.Agents, target, say)
		case "status":
			if target == "" {
				fmt.Fprintln(os.Stderr, "  "+ui.Dim("not connected"))
				say("Not connected.")
			} else {
				win := tmuxpane.WindowOf(ctx, target)
				fmt.Fprintf(os.Stderr, "  %s connected to %s %s\n", ui.Accent("⇄"), ui.Accent(win), ui.Dim("· "+target))
				say("Connected to " + win)
			}
		case "send":
			if err := (inject.TmuxSink{Target: target}).Submit(ctx); err != nil {
				fmt.Fprintln(os.Stderr, "voicepipe: send:", err)
			} else {
				fmt.Fprintf(os.Stderr, "  %s %s\n", ui.Dim("⏎"), ui.Dim("sent"))
			}
		case "help":
			printCommandHelp(wake)
			say("Commands: connect, agents, status, pause, resume, send, help, and quit.")
		case "quit":
			say("Goodbye.")
			fmt.Fprintln(os.Stderr, "\nvoicepipe: stopped.")
			return nil
		default:
			fmt.Fprintf(os.Stderr, "  unknown command %q — say \"%s help\"\n", cmd.Verb, wake)
			say("Unknown command. Say " + wake + " help.")
		}
	}
	fmt.Fprintln(os.Stderr, "\nvoicepipe: stopped.")
	return nil
}

// resolveAgent maps a spoken name to a pane id: registry first, then a tmux
// window-name search. label is the friendly/window name for feedback.
func resolveAgent(ctx context.Context, agents map[string]string, name string) (id, label string, ok bool, err error) {
	key := normalizeName(name)
	if key == "" {
		return "", "", false, nil
	}
	for k, target := range agents {
		if normalizeName(k) == key { // number-aware: "agent two" == "agent 2"
			if id, err = tmuxpane.ResolvePane(ctx, target); err != nil {
				return "", "", false, err
			}
			return id, k, true, nil
		}
	}
	pane, found, err := tmuxpane.FindByName(ctx, name)
	if err != nil {
		return "", "", false, err
	}
	if found {
		return pane.ID, pane.Window(), true, nil
	}
	return "", "", false, nil
}

func talkBanner(agentLabel, target, wake string, agents map[string]string) {
	label := func(s string) string { return ui.Dim(fmt.Sprintf("%-10s", s)) }
	fmt.Fprintln(os.Stderr, ui.Bold("voicepipe")+ui.Dim(" · talk"))
	if target != "" {
		fmt.Fprintf(os.Stderr, "  %s%s%s\n", label("connected"), ui.Accent(agentLabel), ui.Dim(" · "+target))
	} else {
		fmt.Fprintf(os.Stderr, "  %s%s\n", label("agent"), ui.Dim("not connected — say \""+wake+" connect to <agent>\""))
	}
	if len(agents) > 0 {
		fmt.Fprintf(os.Stderr, "  %s%s\n", label("agents"), strings.Join(sortedKeys(agents), ui.Dim("  ")))
	}
	fmt.Fprintf(os.Stderr, "  %s%s  %s  %s\n", label("commands"),
		ui.Dim("say"), "\""+wake+" help\"", ui.Dim("· Ctrl-C to quit"))
	fmt.Fprintln(os.Stderr, "  "+ui.Dim("speak to message the connected agent; replies are read aloud."))
}

func printCommandHelp(wake string) {
	fmt.Fprintf(os.Stderr, "  %s %s\n", ui.Accent("?"), ui.Bold("commands")+ui.Dim(" · say \""+wake+"\" first"))
	for _, c := range [][2]string{
		{"connect <agent>", "switch which agent you talk to"},
		{"agents", "list your agents"},
		{"status", "say which agent you're connected to"},
		{"pause", "stop sending your speech (still hears commands)"},
		{"resume", "start sending again"},
		{"send", "press Enter in the agent's pane"},
		{"help", "show this list"},
		{"quit", "exit talk"},
	} {
		fmt.Fprintf(os.Stderr, "    %s %-16s %s\n", ui.Dim("·"), c[0], ui.Dim(c[1]))
	}
}

func listAgentsAloud(ctx context.Context, agents map[string]string, current string, say func(string)) {
	if len(agents) > 0 {
		names := sortedKeys(agents)
		connected := ""
		for _, n := range names {
			id, e := tmuxpane.ResolvePane(ctx, agents[n])
			switch {
			case e != nil:
				fmt.Fprintf(os.Stderr, "  %s %s %s\n", ui.Red("●"), ui.Dim(fmt.Sprintf("%-14s", n)), ui.Dim("offline"))
			case id == current:
				connected = n
				fmt.Fprintf(os.Stderr, "  %s %s %s\n", ui.Accent("⇄"), ui.Accent(fmt.Sprintf("%-14s", n)), ui.Dim("connected"))
			default:
				fmt.Fprintf(os.Stderr, "  %s %-14s %s\n", ui.Green("●"), n, ui.Dim("· "+id))
			}
		}
		msg := "Agents: " + strings.Join(names, ", ")
		if connected != "" {
			msg += ". Connected to " + connected + "."
		}
		say(msg)
		return
	}
	// No registry: fall back to listing tmux windows.
	panes, err := tmuxpane.ListPanes(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "voicepipe: panes:", err)
		return
	}
	var windows []string
	seen := map[string]bool{}
	for _, p := range panes {
		w := p.Window()
		fmt.Fprintf(os.Stderr, "  %-5s %s\n", p.ID, p.Location)
		if !seen[w] {
			seen[w] = true
			windows = append(windows, w)
		}
	}
	say("Windows: " + strings.Join(windows, ", "))
}

// talkPrompt augments the whisper prompt with the command vocabulary and agent
// names so spoken commands are recognized reliably.
func talkPrompt(cfg config.Config) string {
	parts := []string{cfg.Prompt}
	parts = append(parts, "Voice commands: "+cfg.CommandWord+
		" connect, agents, status, pause, resume, send, help, quit.")
	if len(cfg.Agents) > 0 {
		parts = append(parts, "Agents: "+strings.Join(sortedKeys(cfg.Agents), ", ")+".")
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

var numberWords = map[string]string{
	"zero": "0", "one": "1", "two": "2", "three": "3", "four": "4", "five": "5",
	"six": "6", "seven": "7", "eight": "8", "nine": "9", "ten": "10",
}

// normalizeName lowercases a spoken/registry name and converts number words to
// digits so "agent two" and "agent 2" match the same agent (whisper varies).
func normalizeName(s string) string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(s)))
	for i, f := range fields {
		if d, ok := numberWords[f]; ok {
			fields[i] = d
		}
	}
	return strings.Join(fields, " ")
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
	fmt.Println(ui.Green("✓") + " wrote config " + ui.Dim(config.Path()))

	if _, err := os.Stat(cfg.ModelPath); err == nil {
		fmt.Println(ui.Green("✓") + " model present " + ui.Dim(cfg.ModelPath))
		return nil
	}
	if err := os.MkdirAll(config.ModelDir(), 0o755); err != nil {
		return err
	}
	fmt.Println(ui.Dim("downloading model (~1.5 GB) to " + cfg.ModelPath))
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
	fmt.Println(ui.Bold("tmux panes") + ui.Dim(" · pass the id or location to --target"))
	for _, p := range panes {
		active := ""
		if p.Active {
			active = "  " + ui.Green("●")
		}
		fmt.Printf("  %s  %-28s  %s%s\n", ui.Accent(fmt.Sprintf("%-5s", p.ID)), p.Location, ui.Dim("["+p.Command+"]"), active)
	}
	return nil
}

// cmdAgents lists the configured agent registry and checks whether each target
// still resolves to a live tmux pane (catches stale mappings).
func cmdAgents(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if len(cfg.Agents) == 0 {
		fmt.Println("no agents configured.")
		fmt.Println("add them under \"agents\" in", config.Path())
		fmt.Println("find targets with `voicepipe panes`.")
		return nil
	}
	fmt.Println(ui.Bold("agents") + ui.Dim(" · say \""+cfg.CommandWord+" connect to <name>\""))
	for _, name := range sortedKeys(cfg.Agents) {
		target := cfg.Agents[name]
		if id, e := tmuxpane.ResolvePane(ctx, target); e != nil {
			fmt.Printf("  %s %-14s %s\n", ui.Red("●"), name, ui.Dim("→ "+target+"  (not found)"))
		} else {
			fmt.Printf("  %s %-14s %s\n", ui.Green("●"), name, ui.Dim("→ "+id+"  ("+tmuxpane.WindowOf(ctx, id)+")"))
		}
	}
	return nil
}

func cmdDevices() error {
	names, err := audio.ListDevices()
	if err != nil {
		return err
	}
	fmt.Println(ui.Bold("input devices"))
	for _, n := range names {
		fmt.Println("  "+ui.Dim("·"), n)
	}
	fmt.Println("\n" + ui.Dim("set the input_device field in "+config.Path()))
	return nil
}

func cmdDoctor() error {
	cfg, _ := config.Load()
	ok := true
	fmt.Println(ui.Bold("doctor"))
	required := func(label string, good bool, hint string) {
		dot := ui.Green("●")
		if !good {
			dot = ui.Red("●")
			ok = false
		}
		fmt.Printf("  %s %s\n", dot, label)
		if !good && hint != "" {
			fmt.Println("    " + ui.Dim("→ "+hint))
		}
	}
	info := func(label, note string) {
		fmt.Printf("  %s %s\n", ui.Dim("○"), label)
		if note != "" {
			fmt.Println("    " + ui.Dim("→ "+note))
		}
	}

	// Required for any sink: transcription.
	required("whisper-cli on PATH", onPath(cfg.WhisperBin), "brew install whisper-cpp")
	_, modelErr := os.Stat(cfg.ModelPath)
	required("model present", modelErr == nil, "voicepipe init")

	// Sink-specific guidance.
	fmt.Printf("\n%s %s\n", ui.Dim("sink"), ui.Accent(cfg.Sink))
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
	fmt.Println("\n" + ui.Green("✓ all good") + ui.Dim(" — try voicepipe capture --send"))
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
