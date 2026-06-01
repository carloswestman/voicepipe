package speak

import "context"

// Speaker turns text into audible speech, blocking until playback finishes.
// Implementations must honor context cancellation so `talk` can interrupt a
// reply mid-playback (Esc-to-stop) — for backends that synthesize then play,
// cancelling aborts both phases.
type Speaker interface {
	Speak(ctx context.Context, text string) error
}

// SaySpeaker speaks via the macOS `say` command — the zero-dependency default
// that keeps voicepipe's easy-install story intact.
type SaySpeaker struct{ Opts Options }

// Speak says text aloud, blocking until done. Cancelling ctx kills `say`.
func (s SaySpeaker) Speak(ctx context.Context, text string) error {
	return Say(ctx, text, s.Opts)
}
