package audio

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/gen2brain/malgo"
)

// Stream opens the mic once and emits silence-trimmed 16 kHz mono utterances on
// the returned channel until ctx is cancelled — the persistent-capture path used
// by `talk`, so the mic stays live (steady indicator, no reopen lag) and nothing
// is lost between turns. While *muted is true, audio is discarded, which prevents
// recording TTS playback (the no-headphones echo loop). The channel is closed
// when the device stops.
func Stream(ctx context.Context, opts Options, muted *atomic.Bool) (<-chan []int16, error) {
	mctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("init audio context: %w", err)
	}
	devID, err := pickDevice(mctx, opts.DeviceSubstr)
	if err != nil {
		_ = mctx.Uninit()
		mctx.Free()
		return nil, err
	}

	deviceCfg := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceCfg.Capture.Format = malgo.FormatS16
	deviceCfg.Capture.Channels = channels
	deviceCfg.SampleRate = sampleRate
	deviceCfg.Alsa.NoMMap = 1
	if devID != nil {
		deviceCfg.Capture.DeviceID = devID.Pointer()
	}

	out := make(chan []int16, 8)
	silence := time.Duration(opts.SilenceMs) * time.Millisecond
	maxDur := time.Duration(opts.MaxSeconds) * time.Second
	preroll := int(padMs * sampleRate / 1000)
	const idleCap = sampleRate // bound the pre-speech buffer to ~1s of silence

	var (
		buf         []int16
		voicedMs    float64
		firstVoiced = -1
		lastVoiced  = 0
		speechSeen  bool
		lastVoice   = time.Now()
		started     = time.Now()
	)
	reset := func() {
		buf = buf[:0]
		voicedMs = 0
		firstVoiced, lastVoiced = -1, 0
		speechSeen = false
		lastVoice = time.Now()
		started = time.Now()
	}
	emit := func() {
		lo := firstVoiced - preroll
		if lo < 0 {
			lo = 0
		}
		hi := lastVoiced + preroll
		if hi > len(buf) {
			hi = len(buf)
		}
		clip := make([]int16, hi-lo)
		copy(clip, buf[lo:hi])
		select {
		case out <- clip:
		default: // consumer is behind; drop rather than block the audio callback
		}
	}

	onData := func(_, in []byte, _ uint32) {
		if muted.Load() { // discard audio during playback (no echo)
			if len(buf) > 0 {
				reset()
			}
			return
		}
		start := len(buf)
		frame := bytesToInt16(in)
		buf = append(buf, frame...)
		if rms(frame) > voiceThreshold {
			voicedMs += float64(len(frame)) * 1000 / float64(sampleRate)
			lastVoice = time.Now()
			if firstVoiced < 0 {
				firstVoiced = start
			}
			lastVoiced = len(buf)
			if voicedMs >= minVoicedStartMs {
				speechSeen = true
			}
		}
		// Keep the pre-speech buffer bounded during silence (no voiced frame yet).
		if firstVoiced < 0 && len(buf) > idleCap {
			tail := preroll
			if tail > len(buf) {
				tail = len(buf)
			}
			buf = append(buf[:0], buf[len(buf)-tail:]...)
		}
		switch {
		case speechSeen && time.Since(lastVoice) > silence:
			if voicedMs >= minSpeechMs {
				emit()
			}
			reset()
		case firstVoiced >= 0 && !speechSeen && time.Since(lastVoice) > silence:
			reset() // brief noise that never became speech
		case time.Since(started) > maxDur:
			if speechSeen && voicedMs >= minSpeechMs {
				emit()
			}
			reset()
		}
	}

	device, err := malgo.InitDevice(mctx.Context, deviceCfg, malgo.DeviceCallbacks{Data: onData})
	if err != nil {
		_ = mctx.Uninit()
		mctx.Free()
		return nil, fmt.Errorf("init capture device: %w", err)
	}
	if err := device.Start(); err != nil {
		device.Uninit()
		_ = mctx.Uninit()
		mctx.Free()
		return nil, fmt.Errorf("start capture: %w", err)
	}

	go func() {
		<-ctx.Done()
		_ = device.Stop()
		device.Uninit()
		_ = mctx.Uninit()
		mctx.Free()
		close(out)
	}()
	return out, nil
}
