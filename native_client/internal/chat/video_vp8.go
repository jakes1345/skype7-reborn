//go:build !android
// +build !android

package chat

import (
	"fmt"
	"image"
	"io"
	"log"
	"sync"
	"time"

	"github.com/pion/mediadevices/pkg/codec/vpx"
	"github.com/pion/mediadevices/pkg/frame"
	"github.com/pion/mediadevices/pkg/io/video"
	"github.com/pion/mediadevices/pkg/prop"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/samplebuilder"
)

type vp8Sender struct {
	track *webrtc.TrackLocalStaticSample
	frame chan image.Image
	once  sync.Once
	done  chan struct{}
}

var (
	vp8SendersMu sync.Mutex
	vp8Senders   = map[string]*vp8Sender{}
)

const (
	vp8Width     = 640
	vp8Height    = 480
	vp8FrameRate = 15
	vp8Bitrate   = 500_000
)

// initVP8Sender creates a VP8 video track and attaches it to the peer connection.
// The encoder goroutine is started immediately and pulls images from a channel
// that WriteVideoFrameVP8 pushes into.
func (cm *CallManager) initVP8Sender(peerName string, pc *webrtc.PeerConnection) error {
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000},
		"video", "phaze-video-"+peerName,
	)
	if err != nil {
		return fmt.Errorf("create VP8 track: %w", err)
	}
	if _, err := pc.AddTrack(track); err != nil {
		return fmt.Errorf("add VP8 track: %w", err)
	}

	s := &vp8Sender{
		track: track,
		frame: make(chan image.Image, 4),
		done:  make(chan struct{}),
	}
	vp8SendersMu.Lock()
	vp8Senders[peerName] = s
	vp8SendersMu.Unlock()

	cm.Mu.Lock()
	cm.LocalVideoTracks[peerName] = track
	cm.videoEnabled[peerName] = true
	cm.Mu.Unlock()

	go s.encodeLoop()
	return nil
}

func (s *vp8Sender) encodeLoop() {
	params, err := vpx.NewVP8Params()
	if err != nil {
		log.Printf("[VP8] params: %v", err)
		return
	}
	params.BitRate = vp8Bitrate

	reader := video.ReaderFunc(func() (image.Image, func(), error) {
		select {
		case img, ok := <-s.frame:
			if !ok {
				return nil, nil, io.EOF
			}
			return img, func() {}, nil
		case <-s.done:
			return nil, nil, io.EOF
		}
	})

	encoder, err := params.BuildVideoEncoder(reader, prop.Media{
		Video: prop.Video{
			Width:       vp8Width,
			Height:      vp8Height,
			FrameRate:   vp8FrameRate,
			FrameFormat: frame.FormatI420,
		},
	})
	if err != nil {
		log.Printf("[VP8] build encoder: %v", err)
		return
	}
	defer encoder.Close()

	duration := time.Second / vp8FrameRate
	for {
		select {
		case <-s.done:
			return
		default:
		}
		data, release, err := encoder.Read()
		if err != nil {
			if err == io.EOF {
				return
			}
			log.Printf("[VP8] encode: %v", err)
			continue
		}
		if err := s.track.WriteSample(media.Sample{Data: data, Duration: duration}); err != nil {
			log.Printf("[VP8] write sample: %v", err)
		}
		if release != nil {
			release()
		}
	}
}

// WriteVideoFrameVP8 drops the image into the peer's VP8 encoder.
// Returns false if no VP8 sender exists (caller should fall back to JPEG).
func (cm *CallManager) WriteVideoFrameVP8(peerName string, img image.Image) bool {
	vp8SendersMu.Lock()
	s, ok := vp8Senders[peerName]
	vp8SendersMu.Unlock()
	if !ok {
		return false
	}
	select {
	case s.frame <- img:
	default:
		// Drop frame if encoder is backed up; better than blocking UI thread.
	}
	return true
}

// stopVP8Sender closes the encoder goroutine and removes the sender.
func stopVP8Sender(peerName string) {
	vp8SendersMu.Lock()
	s, ok := vp8Senders[peerName]
	delete(vp8Senders, peerName)
	vp8SendersMu.Unlock()
	if ok {
		s.once.Do(func() { close(s.done) })
	}
}

// drainVP8Receive reads RTP from a remote VP8 track, reassembles frames with a
// SampleBuilder, decodes them to images via libvpx, and invokes the per-peer
// video callback registered through OnRemoteVideoFrameFor.
func (cm *CallManager) drainVP8Receive(peerName string, remote *webrtc.TrackRemote) {
	log.Printf("[VP8] drain start for %s", peerName)

	pr, pw := io.Pipe()
	defer pw.Close()

	decoder, err := vpx.NewDecoder(pr, prop.Media{
		Video: prop.Video{
			Width:       vp8Width,
			Height:      vp8Height,
			FrameFormat: frame.FormatI420,
		},
	})
	if err != nil {
		log.Printf("[VP8] build decoder: %v", err)
		return
	}
	defer decoder.Close()

	// SampleBuilder pairs with VP8 depacketizer to reassemble full frames.
	builder := samplebuilder.New(50, &codecs.VP8Packet{}, 90000)

	// Decoder read loop: pull image.Image from decoder and fan out to cb.
	go func() {
		for {
			img, release, err := decoder.Read()
			if err != nil {
				if err != io.EOF {
					log.Printf("[VP8] decode: %v", err)
				}
				return
			}
			cm.Mu.Lock()
			cb := cm.remoteVideoCBs[peerName]
			cm.Mu.Unlock()
			if cb != nil && img != nil {
				cb(img)
			}
			if release != nil {
				release()
			}
		}
	}()

	// RTP read loop: depacketize and feed frames into the decoder pipe.
	for {
		pkt, _, err := remote.ReadRTP()
		if err != nil {
			log.Printf("[VP8] %s track closed: %v", peerName, err)
			return
		}
		builder.Push(pkt)
		for {
			sample := builder.Pop()
			if sample == nil {
				break
			}
			if _, err := pw.Write(sample.Data); err != nil {
				return
			}
		}
	}
}
