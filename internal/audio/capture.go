// Package audio captures microphone input as 16 kHz mono PCM — the format
// whisper.cpp wants — using malgo (miniaudio). miniaudio is compiled in, so
// there is no runtime dependency on sox/ffmpeg/PortAudio; the binary stays
// self-contained and brew-installable.
package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/gen2brain/malgo"
)

const (
	sampleRate = 16000 // whisper.cpp expects 16 kHz
	channels   = 1
)

// Options controls how a single utterance is captured.
type Options struct {
	// DeviceSubstr selects a capture device by case-insensitive name substring.
	// Empty uses the system default.
	DeviceSubstr string
	// SilenceMs ends the utterance after this much trailing silence.
	SilenceMs int
	// MaxSeconds hard-caps the recording length.
	MaxSeconds int
}

// Stats reports what was captured, for diagnostics (e.g. --verbose). It is
// returned even when the clip is rejected as noise, so callers can explain why.
type Stats struct {
	DurationMs float64 // total captured audio
	VoicedMs   float64 // audio above the speech threshold
	PeakRMS    float64 // loudest frame seen
	SentMs     float64 // audio actually sent on (after trimming silence)
}

// CaptureUtterance records until the speaker pauses (energy-based VAD) and
// returns 16 kHz mono int16 samples plus capture Stats. Samples are nil when the
// clip is rejected as noise/silence. It blocks until the utterance completes,
// MaxSeconds elapses, or ctx is cancelled.
func CaptureUtterance(ctx context.Context, opts Options) ([]int16, Stats, error) {
	mctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, Stats{}, fmt.Errorf("init audio context: %w", err)
	}
	defer func() { _ = mctx.Uninit(); mctx.Free() }()

	devID, err := pickDevice(mctx, opts.DeviceSubstr)
	if err != nil {
		return nil, Stats{}, err
	}

	deviceCfg := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceCfg.Capture.Format = malgo.FormatS16
	deviceCfg.Capture.Channels = channels
	deviceCfg.SampleRate = sampleRate
	deviceCfg.Alsa.NoMMap = 1
	if devID != nil {
		deviceCfg.Capture.DeviceID = devID.Pointer()
	}

	var (
		samples     []int16
		voicedMs    float64 // total audio above the speech threshold
		peakRMS     float64 // loudest frame, for diagnostics
		firstVoiced = -1    // sample index of first voiced frame (-1 = none yet)
		lastVoiced  = 0     // sample index just past the last voiced frame
		speechSeen  bool
		lastVoice   = time.Now()
		started     = time.Now()
		done        = make(chan struct{})
		closeOnce   bool
	)

	silence := time.Duration(opts.SilenceMs) * time.Millisecond
	maxDur := time.Duration(opts.MaxSeconds) * time.Second

	finish := func() {
		if !closeOnce {
			closeOnce = true
			close(done)
		}
	}

	onData := func(_, in []byte, frameCount uint32) {
		start := len(samples)
		frame := bytesToInt16(in)
		samples = append(samples, frame...)

		r := rms(frame)
		if r > peakRMS {
			peakRMS = r
		}
		if r > voiceThreshold {
			voicedMs += float64(len(frame)) * 1000 / float64(sampleRate)
			lastVoice = time.Now()
			// Remember the voiced span so we can trim surrounding silence later.
			if firstVoiced < 0 {
				firstVoiced = start
			}
			lastVoiced = len(samples)
			// Only treat it as speech once enough *sustained* voiced audio has
			// accumulated — a single noise spike (a bite, a key press) won't pass.
			if voicedMs >= minVoicedStartMs {
				speechSeen = true
			}
		}
		// End the utterance once we've heard speech and then a pause, or if we
		// hit the hard cap.
		if speechSeen && time.Since(lastVoice) > silence {
			finish()
		}
		if time.Since(started) > maxDur {
			finish()
		}
	}

	device, err := malgo.InitDevice(mctx.Context, deviceCfg, malgo.DeviceCallbacks{Data: onData})
	if err != nil {
		return nil, Stats{}, fmt.Errorf("init capture device: %w", err)
	}
	defer device.Uninit()

	if err := device.Start(); err != nil {
		return nil, Stats{}, fmt.Errorf("start capture: %w", err)
	}

	select {
	case <-done:
	case <-ctx.Done():
	}
	_ = device.Stop()

	stats := Stats{
		DurationMs: float64(len(samples)) * 1000 / float64(sampleRate),
		VoicedMs:   voicedMs,
		PeakRMS:    peakRMS,
	}
	// Reject clips without enough real voiced audio — this is what stops whisper
	// from hallucinating sentences out of silence or background noise.
	if !speechSeen || voicedMs < minSpeechMs {
		return nil, stats, nil
	}

	// Trim to the voiced span (+ padding) so whisper processes speech, not the
	// dead air before/after you spoke — much faster and less hallucination surface.
	pad := int(padMs * sampleRate / 1000)
	lo := firstVoiced - pad
	if lo < 0 {
		lo = 0
	}
	hi := lastVoiced + pad
	if hi > len(samples) {
		hi = len(samples)
	}
	trimmed := samples[lo:hi]
	stats.SentMs = float64(len(trimmed)) * 1000 / float64(sampleRate)
	return trimmed, stats, nil
}

const (
	// voiceThreshold is the RMS above which a frame counts as speech. Tuned for
	// the built-in MacBook mic; a config knob can come later if needed.
	voiceThreshold = 500.0
	// minVoicedStartMs is how much sustained voiced audio is needed before we
	// consider an utterance to have started (filters brief noise spikes).
	minVoicedStartMs = 150.0
	// minSpeechMs is the total voiced audio required to accept an utterance;
	// below this we treat the clip as noise and return nothing.
	minSpeechMs = 350.0
	// padMs is how much audio to keep on each side of the voiced span when
	// trimming silence, so word onsets/tails aren't clipped.
	padMs = 200
)

func rms(frame []int16) float64 {
	if len(frame) == 0 {
		return 0
	}
	var sum float64
	for _, s := range frame {
		v := float64(s)
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(frame)))
}

func bytesToInt16(b []byte) []int16 {
	out := make([]int16, len(b)/2)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(b[i*2:]))
	}
	return out
}

// pickDevice returns the capture device whose name contains substr (case
// insensitive), or nil for the system default.
func pickDevice(ctx *malgo.AllocatedContext, substr string) (*malgo.DeviceID, error) {
	if substr == "" {
		return nil, nil
	}
	infos, err := ctx.Devices(malgo.Capture)
	if err != nil {
		return nil, fmt.Errorf("list capture devices: %w", err)
	}
	want := strings.ToLower(substr)
	for i := range infos {
		if strings.Contains(strings.ToLower(infos[i].Name()), want) {
			id := infos[i].ID
			return &id, nil
		}
	}
	// No match — fall back to default rather than failing the capture.
	return nil, nil
}

// ListDevices returns the names of available capture devices for `voicepipe devices`.
func ListDevices() ([]string, error) {
	mctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = mctx.Uninit(); mctx.Free() }()

	infos, err := mctx.Devices(malgo.Capture)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(infos))
	for i := range infos {
		names = append(names, infos[i].Name())
	}
	return names, nil
}
