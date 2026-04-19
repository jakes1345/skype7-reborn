package chat

import (
	"encoding/binary"
	"log"
	"sync"
	"time"

	"github.com/jfreymuth/pulse"
	"github.com/pion/webrtc/v3"
)

// Real-time audio I/O for Phaze calls.
//
// Codec: PCMU (G.711 µ-law), 8 kHz, mono. Pure Go, no cgo.
// Mic: PulseAudio capture @ 8 kHz s16le mono.
// Speaker: PulseAudio playback @ 8 kHz s16le mono.
// Packet cadence: 20 ms (160 samples per frame).

const (
	audioSampleRate = 8000
	audioFrameMs    = 20
	audioFrameSize  = audioSampleRate * audioFrameMs / 1000 // 160
)

// µ-law encode one 16-bit linear PCM sample per ITU-T G.711 (Sun reference).
func linearToULaw(sample int16) byte {
	const bias = 0x84
	const clip = 32635

	sign := byte(0)
	s := int32(sample)
	if s < 0 {
		s = -s
		sign = 0x80
	}
	if s > clip {
		s = clip
	}
	s += bias

	exponent := byte(7)
	for mask := int32(0x4000); s&mask == 0 && exponent > 0; mask >>= 1 {
		exponent--
	}
	mantissa := byte((s >> (exponent + 3)) & 0x0F)
	return ^(sign | (exponent << 4) | mantissa)
}

// µ-law decode one byte to 16-bit linear PCM.
func ulawToLinear(u byte) int16 {
	u = ^u
	sign := int16(1)
	if u&0x80 != 0 {
		sign = -1
	}
	seg := (u >> 4) & 0x07
	mant := int16(u & 0x0F)
	sample := ((mant << 3) + 0x84) << seg
	sample -= 0x84
	return sign * sample
}

func encodePCMUFrame(pcm []int16) []byte {
	out := make([]byte, len(pcm))
	for i, s := range pcm {
		out[i] = linearToULaw(s)
	}
	return out
}

func decodePCMUFrame(u []byte) []int16 {
	out := make([]int16, len(u))
	for i, b := range u {
		out[i] = ulawToLinear(b)
	}
	return out
}

// ---------- Pulse client (shared across all calls) ----------

var (
	pulseOnce   sync.Once
	pulseClient *pulse.Client
	pulseErr    error
)

func getPulse() (*pulse.Client, error) {
	pulseOnce.Do(func() {
		pulseClient, pulseErr = pulse.NewClient(
			pulse.ClientApplicationName("Private Phaze"),
		)
	})
	return pulseClient, pulseErr
}

// ---------- Audio pump: mic → PCMU → track ----------

type audioPump struct {
	cm       *CallManager
	peerName string
	stopCh   chan struct{}
	stream   *pulse.RecordStream
}

func (cm *CallManager) startAudioPump(peerName string) {
	cm.Mu.Lock()
	if _, ok := cm.LocalTracks[peerName]; !ok {
		cm.Mu.Unlock()
		return
	}
	// dedupe: only one pump per peer
	if cm.pumps == nil {
		cm.pumps = make(map[string]*audioPump)
	}
	if _, exists := cm.pumps[peerName]; exists {
		cm.Mu.Unlock()
		return
	}
	cm.Mu.Unlock()

	p := &audioPump{cm: cm, peerName: peerName, stopCh: make(chan struct{})}

	cm.Mu.Lock()
	cm.pumps[peerName] = p
	cm.Mu.Unlock()

	go p.loop()
}

func (cm *CallManager) stopAudioPump(peerName string) {
	cm.Mu.Lock()
	p := cm.pumps[peerName]
	delete(cm.pumps, peerName)
	cm.Mu.Unlock()
	if p != nil {
		close(p.stopCh)
		if p.stream != nil {
			p.stream.Stop()
			p.stream.Close()
		}
	}
}

func (p *audioPump) loop() {
	client, err := getPulse()
	if err != nil {
		log.Printf("[audio] pulse unavailable: %v — call will be muted outbound", err)
		p.runSilence()
		return
	}

	// Ring buffer of mic samples, drained 20ms at a time.
	sampleCh := make(chan int16, audioSampleRate) // 1s buffer

	stream, err := client.NewRecord(
		pulse.Int16Writer(func(buf []int16) (int, error) {
			for _, s := range buf {
				select {
				case sampleCh <- s:
				default:
					// drop if call side is slow
				}
			}
			return len(buf), nil
		}),
		pulse.RecordSampleRate(audioSampleRate),
		pulse.RecordMono,
	)
	if err != nil {
		log.Printf("[audio] mic open failed: %v — outbound muted", err)
		p.runSilence()
		return
	}
	p.stream = stream
	stream.Start()

	ticker := time.NewTicker(audioFrameMs * time.Millisecond)
	defer ticker.Stop()

	frame := make([]int16, audioFrameSize)
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			n := 0
			for n < audioFrameSize {
				select {
				case s := <-sampleCh:
					frame[n] = s
					n++
				default:
					frame[n] = 0
					n++
				}
			}
			var payload []byte
			if p.cm.isMuted(p.peerName) {
				payload = make([]byte, audioFrameSize)
				for i := range payload {
					payload[i] = 0xFF // PCMU silence
				}
			} else {
				payload = encodePCMUFrame(frame)
			}
			if err := p.cm.WriteAudio(p.peerName, payload, audioFrameMs); err != nil {
				return
			}
		}
	}
}

// runSilence keeps the RTP stream alive at the correct cadence when the mic
// can't be opened. Better than tearing down the call.
func (p *audioPump) runSilence() {
	ticker := time.NewTicker(audioFrameMs * time.Millisecond)
	defer ticker.Stop()
	silent := make([]byte, audioFrameSize)
	for i := range silent {
		silent[i] = 0xFF // PCMU silence
	}
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			if err := p.cm.WriteAudio(p.peerName, silent, audioFrameMs); err != nil {
				return
			}
		}
	}
}

// ---------- Remote: PCMU → speaker ----------

func drainRemote(peerName string, track *webrtc.TrackRemote) {
	client, err := getPulse()
	if err != nil {
		log.Printf("[audio] pulse unavailable for playback: %v", err)
		// still drain to keep the RTP pipe moving
		for {
			if _, _, err := track.ReadRTP(); err != nil {
				return
			}
		}
	}

	// Jitter buffer: ~200ms
	jitter := make(chan []int16, 10)

	stream, err := client.NewPlayback(
		pulse.Int16Reader(func(out []int16) (int, error) {
			n := 0
			for n < len(out) {
				select {
				case frame := <-jitter:
					copy(out[n:], frame)
					n += len(frame)
					if n > len(out) {
						n = len(out)
					}
				default:
					// underrun: fill silence
					for n < len(out) {
						out[n] = 0
						n++
					}
				}
			}
			return n, nil
		}),
		pulse.PlaybackSampleRate(audioSampleRate),
		pulse.PlaybackMono,
	)
	if err != nil {
		log.Printf("[audio] speaker open failed: %v", err)
		for {
			if _, _, err := track.ReadRTP(); err != nil {
				return
			}
		}
	}
	stream.Start()
	defer func() {
		stream.Stop()
		stream.Close()
	}()

	for {
		pkt, _, err := track.ReadRTP()
		if err != nil {
			log.Printf("[audio] %s track closed: %v", peerName, err)
			return
		}
		pcm := decodePCMUFrame(pkt.Payload)
		select {
		case jitter <- pcm:
		default:
			// buffer full, drop oldest
			select {
			case <-jitter:
			default:
			}
			jitter <- pcm
		}
	}
}

// unused helper to silence go vet about binary import when pion rtp is in play
var _ = binary.BigEndian

// ---------- Remote: Video frames ----------

type VideoFrameHandler func(peerName string, frame []byte, width, height int)

var remoteVideoHandler VideoFrameHandler

func SetRemoteVideoHandler(h VideoFrameHandler) {
	remoteVideoHandler = h
}

func DrainRemoteVideo(peerName string, track *webrtc.TrackRemote) {
	log.Printf("[Video] Starting video drain for %s, codec: %s", peerName, track.Codec().MimeType)
	for {
		pkt, _, err := track.ReadRTP()
		if err != nil {
			log.Printf("[Video] %s track closed: %v", peerName, err)
			return
		}
		if remoteVideoHandler != nil {
			remoteVideoHandler(peerName, pkt.Payload, 0, 0)
		}
	}
}
