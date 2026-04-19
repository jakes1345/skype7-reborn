// soundgen generates classic Shadow-style sound effects as WAV files.
// Run: go run ./cmd/soundgen
package main

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
)

const sampleRate = 44100

func main() {
	dir := filepath.Join("assets", "sounds")
	os.MkdirAll(dir, 0755)

	// Classic Shadow 7 sound approximations
	generate(dir, "Login.wav", loginSound())
	generate(dir, "login.wav", loginSound()) // duplicate name compat
	generate(dir, "MessageReceived.wav", messageReceivedSound())
	generate(dir, "message.wav", messageReceivedSound())
	generate(dir, "incoming.wav", messageReceivedSound())
	generate(dir, "CallIncoming.wav", callIncomingSound())
	generate(dir, "CallOutgoing.wav", callOutgoingSound())
	generate(dir, "CallEnd.wav", callEndSound())
	generate(dir, "CallError.wav", callErrorSound())

	// Sounds referenced in main.go that previously had no asset.
	generate(dir, "MessageOutgoing.wav", messageOutgoingSound())
	generate(dir, "MessageIncoming.wav", messageReceivedSound()) // alias for MessageReceived
	generate(dir, "FriendOnline.wav", friendOnlineSound())
	generate(dir, "CallHangup.wav", callEndSound()) // alias for CallEnd
	generate(dir, "EchoGreeting.wav", echoGreetingSound())
	generate(dir, "Beep.wav", beepSound())

	println("All sounds generated in", dir)
}

// messageOutgoingSound: a brief, bright "swoosh-pip" sent confirmation.
func messageOutgoingSound() []float64 {
	dur := 0.35
	buf := make([]float64, int(dur*sampleRate))
	addTone(buf, 1318.51, 0.0, 0.10, 0.35, true) // E6
	addTone(buf, 1567.98, 0.08, 0.18, 0.40, true) // G6
	return buf
}

// friendOnlineSound: gentle two-note ping in a rising fifth.
func friendOnlineSound() []float64 {
	dur := 0.55
	buf := make([]float64, int(dur*sampleRate))
	addTone(buf, 659.25, 0.0, 0.18, 0.40, true)  // E5
	addTone(buf, 987.77, 0.18, 0.30, 0.45, true) // B5
	return buf
}

// echoGreetingSound: warm three-note opening, used for the echo / sound test contact.
func echoGreetingSound() []float64 {
	dur := 1.6
	buf := make([]float64, int(dur*sampleRate))
	addTone(buf, 523.25, 0.00, 0.40, 0.45, true)  // C5
	addTone(buf, 659.25, 0.40, 0.40, 0.50, true)  // E5
	addTone(buf, 783.99, 0.80, 0.70, 0.55, true)  // G5
	return buf
}

// beepSound: short echo-test "beep" tone.
func beepSound() []float64 {
	dur := 0.25
	buf := make([]float64, int(dur*sampleRate))
	addTone(buf, 1000.0, 0.0, 0.22, 0.55, true)
	return buf
}

// loginSound: the classic ascending "boo-doo-doo-doo-DOOP" Shadow login
func loginSound() []float64 {
	dur := 1.8
	samples := int(dur * sampleRate)
	buf := make([]float64, samples)

	// Five ascending tones with slight overlap
	notes := []struct {
		freq     float64
		start    float64
		duration float64
		vol      float64
	}{
		{523.25, 0.0, 0.25, 0.5},   // C5
		{587.33, 0.2, 0.25, 0.55},  // D5
		{659.25, 0.4, 0.25, 0.6},   // E5
		{783.99, 0.6, 0.25, 0.65},  // G5
		{1046.50, 0.8, 0.8, 0.7},   // C6 (held longer)
	}

	for _, n := range notes {
		addTone(buf, n.freq, n.start, n.duration, n.vol, true)
	}

	return buf
}

// messageReceivedSound: short two-tone "ding-dong" chime
func messageReceivedSound() []float64 {
	dur := 0.6
	samples := int(dur * sampleRate)
	buf := make([]float64, samples)

	addTone(buf, 880.0, 0.0, 0.2, 0.4, true)   // A5
	addTone(buf, 1108.73, 0.15, 0.35, 0.45, true) // C#6

	return buf
}

// callIncomingSound: the iconic Shadow ringtone - repeating pattern ~4 seconds
func callIncomingSound() []float64 {
	dur := 4.0
	samples := int(dur * sampleRate)
	buf := make([]float64, samples)

	// Classic Shadow ring: alternating two-note pattern repeated
	for rep := 0; rep < 4; rep++ {
		offset := float64(rep) * 1.0
		// "brrr-RING brrr-RING" pattern
		addTone(buf, 440.0, offset+0.0, 0.15, 0.5, true)
		addTone(buf, 554.37, offset+0.05, 0.15, 0.4, true)
		addTone(buf, 659.25, offset+0.2, 0.25, 0.6, true)
		addTone(buf, 880.0, offset+0.25, 0.25, 0.5, true)
		// Gap
		addTone(buf, 440.0, offset+0.55, 0.15, 0.5, true)
		addTone(buf, 554.37, offset+0.6, 0.15, 0.4, true)
		addTone(buf, 659.25, offset+0.75, 0.2, 0.6, true)
	}

	return buf
}

// callOutgoingSound: standard ringback tone (two frequencies alternating)
func callOutgoingSound() []float64 {
	dur := 4.0
	samples := int(dur * sampleRate)
	buf := make([]float64, samples)

	// Standard North American ringback: 440+480 Hz, 2s on, 4s off (we do shorter cycle)
	for rep := 0; rep < 2; rep++ {
		offset := float64(rep) * 2.0
		addTone(buf, 440.0, offset, 1.0, 0.3, true)
		addTone(buf, 480.0, offset, 1.0, 0.3, true)
	}

	return buf
}

// callEndSound: descending tone
func callEndSound() []float64 {
	dur := 0.8
	samples := int(dur * sampleRate)
	buf := make([]float64, samples)

	addTone(buf, 880.0, 0.0, 0.3, 0.5, true)
	addTone(buf, 659.25, 0.2, 0.3, 0.45, true)
	addTone(buf, 440.0, 0.4, 0.4, 0.4, true)

	return buf
}

// callErrorSound: dissonant error buzz
func callErrorSound() []float64 {
	dur := 0.5
	samples := int(dur * sampleRate)
	buf := make([]float64, samples)

	addTone(buf, 200.0, 0.0, 0.2, 0.5, false)
	addTone(buf, 150.0, 0.2, 0.3, 0.4, false)

	return buf
}

// addTone adds a sine wave tone to the buffer with envelope
func addTone(buf []float64, freq, startSec, durSec, volume float64, smooth bool) {
	startSample := int(startSec * sampleRate)
	numSamples := int(durSec * sampleRate)

	for i := 0; i < numSamples; i++ {
		idx := startSample + i
		if idx >= len(buf) {
			break
		}

		t := float64(i) / sampleRate
		sample := math.Sin(2 * math.Pi * freq * t) * volume

		if smooth {
			// Apply ADSR-style envelope
			attackTime := 0.02
			releaseTime := 0.05
			pos := float64(i) / float64(numSamples)
			attackPos := attackTime / durSec
			releaseStart := 1.0 - (releaseTime / durSec)

			var env float64
			if pos < attackPos {
				env = pos / attackPos
			} else if pos > releaseStart {
				env = (1.0 - pos) / (1.0 - releaseStart)
			} else {
				env = 1.0
			}
			sample *= env
		}

		buf[idx] += sample
	}
}

// generate writes a buffer as a 16-bit PCM WAV file
func generate(dir, name string, samples []float64) {
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	// Normalize
	maxVal := 0.0
	for _, s := range samples {
		if math.Abs(s) > maxVal {
			maxVal = math.Abs(s)
		}
	}
	if maxVal > 0 {
		for i := range samples {
			samples[i] /= maxVal
		}
	}

	numSamples := len(samples)
	dataSize := numSamples * 2 // 16-bit = 2 bytes per sample
	fileSize := 36 + dataSize

	// RIFF header
	f.Write([]byte("RIFF"))
	binary.Write(f, binary.LittleEndian, uint32(fileSize))
	f.Write([]byte("WAVE"))

	// fmt chunk
	f.Write([]byte("fmt "))
	binary.Write(f, binary.LittleEndian, uint32(16))     // chunk size
	binary.Write(f, binary.LittleEndian, uint16(1))      // PCM
	binary.Write(f, binary.LittleEndian, uint16(1))      // mono
	binary.Write(f, binary.LittleEndian, uint32(sampleRate))
	binary.Write(f, binary.LittleEndian, uint32(sampleRate*2)) // byte rate
	binary.Write(f, binary.LittleEndian, uint16(2))      // block align
	binary.Write(f, binary.LittleEndian, uint16(16))     // bits per sample

	// data chunk
	f.Write([]byte("data"))
	binary.Write(f, binary.LittleEndian, uint32(dataSize))

	for _, s := range samples {
		val := int16(s * 32767 * 0.8) // 80% to avoid clipping
		binary.Write(f, binary.LittleEndian, val)
	}

	println("  Generated:", name)
}
