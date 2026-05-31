package audio

import (
	"encoding/binary"
	"os"
)

// WriteWAV writes 16 kHz mono int16 samples to a canonical PCM WAV file.
// whisper.cpp reads this directly. Hand-rolled to avoid a dependency.
func WriteWAV(path string, samples []int16) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	dataBytes := len(samples) * 2
	var hdr [44]byte
	copy(hdr[0:], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:], uint32(36+dataBytes))
	copy(hdr[8:], "WAVE")
	copy(hdr[12:], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:], 16)                    // PCM fmt chunk size
	binary.LittleEndian.PutUint16(hdr[20:], 1)                     // PCM
	binary.LittleEndian.PutUint16(hdr[22:], channels)              // mono
	binary.LittleEndian.PutUint32(hdr[24:], sampleRate)            // 16000
	binary.LittleEndian.PutUint32(hdr[28:], sampleRate*channels*2) // byte rate
	binary.LittleEndian.PutUint16(hdr[32:], channels*2)            // block align
	binary.LittleEndian.PutUint16(hdr[34:], 16)                    // bits per sample
	copy(hdr[36:], "data")
	binary.LittleEndian.PutUint32(hdr[40:], uint32(dataBytes))

	if _, err := f.Write(hdr[:]); err != nil {
		return err
	}
	buf := make([]byte, dataBytes)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(s))
	}
	_, err = f.Write(buf)
	return err
}
