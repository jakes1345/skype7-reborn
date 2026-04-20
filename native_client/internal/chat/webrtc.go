package chat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

type TurnConfig struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// iceServers returns the ICE server list. It prioritizes the dynamic TurnConfig
// provided by the Nexus server, falling back to public defaults.
func iceServers(dynamic *TurnConfig) []webrtc.ICEServer {
	servers := []webrtc.ICEServer{
		{URLs: []string{"stun:stun.l.google.com:19302"}},
	}

	if dynamic != nil && dynamic.URL != "" {
		servers = append(servers, webrtc.ICEServer{
			URLs:           []string{dynamic.URL},
			Username:       dynamic.Username,
			Credential:     dynamic.Password,
			CredentialType: webrtc.ICECredentialTypePassword,
		})
		return servers
	}

	if custom := os.Getenv("SHADOW_TURN_URL"); custom != "" {
		urls := strings.Split(custom, ",")
		for i, u := range urls {
			urls[i] = strings.TrimSpace(u)
		}
		servers = append(servers, webrtc.ICEServer{
			URLs:           urls,
			Username:       os.Getenv("SHADOW_TURN_USER"),
			Credential:     os.Getenv("SHADOW_TURN_PASS"),
			CredentialType: webrtc.ICECredentialTypePassword,
		})
		return servers
	}

	// Open Relay Project — free public TURN for open-source projects.
	// https://www.metered.ca/tools/openrelay/
	servers = append(servers, webrtc.ICEServer{
		URLs: []string{
			"turn:openrelay.metered.ca:80",
			"turn:openrelay.metered.ca:443",
			"turn:openrelay.metered.ca:443?transport=tcp",
		},
		Username:       "openrelayproject",
		Credential:     "openrelayproject",
		CredentialType: webrtc.ICECredentialTypePassword,
	})
	return servers
}

// CallManager handles Peer-to-Peer WebRTC connections
type CallManager struct {
	Mu                sync.Mutex
	Connections       map[string]*webrtc.PeerConnection
	DataChannels      map[string]*webrtc.DataChannel
	LocalTracks       map[string]*webrtc.TrackLocalStaticSample
	LocalVideoTracks  map[string]*webrtc.TrackLocalStaticSample
	RemoteTracks      map[string]*webrtc.TrackRemote
	RemoteVideoTracks map[string]*webrtc.TrackRemote
	API               *webrtc.API
	OnFile            func(peerName string, fileName string, totalSize int, data []byte)
	OnRemoteAudio     func(peerName string, track *webrtc.TrackRemote)
	OnRemoteVideo     func(peerName string, track *webrtc.TrackRemote, width, height int)

	remoteVideoCBs map[string]func(image.Image)

	// in-progress file receives
	rxState      map[string]*fileRx
	pumps        map[string]*audioPump
	muted        map[string]bool
	videoEnabled map[string]bool

	ICEServers []webrtc.ICEServer
}

// SetMuted controls whether the local mic is relayed to the peer.
// While muted the audio pump still ticks at the correct cadence (PCMU silence)
// so the RTP stream stays alive and the call doesn't time out.
func (cm *CallManager) SetMuted(peerName string, mute bool) {
	cm.Mu.Lock()
	if cm.muted == nil {
		cm.muted = make(map[string]bool)
	}
	cm.muted[peerName] = mute
	cm.Mu.Unlock()
}

func (cm *CallManager) isMuted(peerName string) bool {
	cm.Mu.Lock()
	defer cm.Mu.Unlock()
	return cm.muted[peerName]
}

type fileRx struct {
	name string
	size int
	buf  bytes.Buffer
}

func NewCallManager() *CallManager {
	settingEngine := webrtc.SettingEngine{}
	mediaEngine := &webrtc.MediaEngine{}

	// Register PCMU audio codec (G.711 µ-law) — pure-Go encodable/decodable at 8 kHz mono.
	if err := mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypePCMU,
			ClockRate: 8000,
			Channels:  1,
		},
		PayloadType: 0,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		log.Printf("Failed to register PCMU: %v", err)
	}

	// Register VP8 video codec — widely supported, good compression
	if err := mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeVP8,
			ClockRate: 90000,
		},
		PayloadType: 96,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		log.Printf("Failed to register VP8: %v", err)
	}

	// Register VP9 as fallback
	if err := mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeVP9,
			ClockRate: 90000,
		},
		PayloadType: 98,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		log.Printf("Failed to register VP9: %v", err)
	}

	// Register H264 as another fallback option
	if err := mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   90000,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42001f",
		},
		PayloadType: 100,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		log.Printf("Failed to register H264: %v", err)
	}

	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine), webrtc.WithSettingEngine(settingEngine))

	return &CallManager{
		Connections:       make(map[string]*webrtc.PeerConnection),
		DataChannels:      make(map[string]*webrtc.DataChannel),
		LocalTracks:       make(map[string]*webrtc.TrackLocalStaticSample),
		LocalVideoTracks:  make(map[string]*webrtc.TrackLocalStaticSample),
		RemoteTracks:      make(map[string]*webrtc.TrackRemote),
		RemoteVideoTracks: make(map[string]*webrtc.TrackRemote),
		rxState:           make(map[string]*fileRx),
		pumps:             make(map[string]*audioPump),
		videoEnabled:      make(map[string]bool),
		API:               api,
		ICEServers:        iceServers(nil),
	}
}

func (cm *CallManager) SetICEServers(dynamic *TurnConfig) {
	cm.ICEServers = iceServers(dynamic)
}

// addAudioTrack attaches a local PCMU audio track to the PC.
func (cm *CallManager) addAudioTrack(peerName string, pc *webrtc.PeerConnection) {
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU, ClockRate: 8000, Channels: 1},
		"audio", "phaze-"+peerName,
	)
	if err != nil {
		log.Printf("[WebRTC] create audio track: %v", err)
		return
	}
	if _, err := pc.AddTrack(track); err != nil {
		log.Printf("[WebRTC] add audio track: %v", err)
		return
	}
	cm.LocalTracks[peerName] = track

	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		log.Printf("[WebRTC] remote track from %s codec=%s type=%s", peerName, remote.Codec().MimeType, remote.Kind().String())

		if remote.Kind() == webrtc.RTPCodecTypeVideo {
			cm.Mu.Lock()
			cm.RemoteVideoTracks[peerName] = remote
			cb := cm.OnRemoteVideo
			cm.Mu.Unlock()
			if cb != nil {
				cb(peerName, remote, 640, 480)
			}
			if strings.EqualFold(remote.Codec().MimeType, webrtc.MimeTypeVP8) {
				go cm.drainVP8Receive(peerName, remote)
			} else {
				go DrainRemoteVideo(peerName, remote)
			}
		} else {
			cm.Mu.Lock()
			cm.RemoteTracks[peerName] = remote
			cb := cm.OnRemoteAudio
			cm.Mu.Unlock()
			if cb != nil {
				cb(peerName, remote)
			}
			go drainRemote(peerName, remote)
		}
	})

	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		if s == webrtc.PeerConnectionStateConnected {
			cm.startAudioPump(peerName)
		}
	})
}

// WriteAudio pushes a PCMU frame to the peer.
func (cm *CallManager) WriteAudio(peerName string, frame []byte, durationMs int) error {
	cm.Mu.Lock()
	track, ok := cm.LocalTracks[peerName]
	cm.Mu.Unlock()
	if !ok {
		return fmt.Errorf("no local track for %s", peerName)
	}
	return track.WriteSample(media.Sample{Data: frame, Duration: time.Duration(durationMs) * time.Millisecond})
}

// AddVideoTrack adds a real VP8 RTP track when libvpx is available (desktop
// builds). On android it falls back to the JPEG DataChannel path.
func (cm *CallManager) AddVideoTrack(peerName string) error {
	cm.Mu.Lock()
	pc, ok := cm.Connections[peerName]
	cm.Mu.Unlock()
	if !ok {
		return fmt.Errorf("no peer connection for %s", peerName)
	}

	if err := cm.initVP8Sender(peerName, pc); err != nil {
		log.Printf("[WebRTC] VP8 unavailable for %s (%v) — using JPEG DataChannel", peerName, err)
		cm.Mu.Lock()
		cm.videoEnabled[peerName] = true
		cm.Mu.Unlock()
		return nil
	}
	log.Printf("[WebRTC] VP8 RTP track active for %s", peerName)
	return nil
}

// WriteVideoFrame ships a frame to the peer. Prefers the VP8 RTP track when
// available; falls back to JPEG-over-DataChannel otherwise (android, or when
// VP8 encoder failed to start).
func (cm *CallManager) WriteVideoFrame(peerName string, img image.Image, _ int) error {
	if cm.WriteVideoFrameVP8(peerName, img) {
		return nil
	}

	cm.Mu.Lock()
	dc, ok := cm.DataChannels[peerName]
	enabled := cm.videoEnabled[peerName]
	cm.Mu.Unlock()

	if !ok || !enabled {
		return nil
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 60}); err != nil {
		return err
	}
	hdr, _ := json.Marshal(struct {
		Size int `json:"size"`
	}{buf.Len()})
	frame := append([]byte("VID"), append(hdr, '\n')...)
	frame = append(frame, buf.Bytes()...)
	return dc.Send(frame)
}

// OnRemoteVideoFrameFor registers a per-peer callback fired when a JPEG
// video frame arrives over the DataChannel from peerName. Pass nil to clear.
func (cm *CallManager) OnRemoteVideoFrameFor(peerName string, cb func(image.Image)) {
	cm.Mu.Lock()
	defer cm.Mu.Unlock()
	if cm.remoteVideoCBs == nil {
		cm.remoteVideoCBs = make(map[string]func(image.Image))
	}
	if cb == nil {
		delete(cm.remoteVideoCBs, peerName)
		return
	}
	cm.remoteVideoCBs[peerName] = cb
}

// EnableVideo enables or disables video for a peer.
func (cm *CallManager) EnableVideo(peerName string, enabled bool) {
	cm.Mu.Lock()
	defer cm.Mu.Unlock()
	cm.videoEnabled[peerName] = enabled
	log.Printf("[WebRTC] Video %s for %s", map[bool]string{true: "enabled", false: "disabled"}[enabled], peerName)
}

// CreateOffer prepares a new PeerConnection and generates an SDP offer
func (cm *CallManager) CreateOffer(peerName string, config webrtc.Configuration, onICECandidate func(*webrtc.ICECandidate)) (*webrtc.PeerConnection, string, error) {
	cm.Mu.Lock()
	defer cm.Mu.Unlock()

	// Merge our pre-configured ICEServers into the passed config if none are present
	if len(config.ICEServers) == 0 {
		config.ICEServers = cm.ICEServers
	}

	pc, err := cm.API.NewPeerConnection(config)
	if err != nil {
		return nil, "", err
	}

	// Create a dedicated signaling and file data channel
	dc, err := pc.CreateDataChannel("phaze-data", nil)
	if err != nil {
		return nil, "", err
	}
	cm.setupDataChannel(peerName, dc)

	cm.addAudioTrack(peerName, pc)

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		if onICECandidate != nil {
			onICECandidate(c)
		}
	})

	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		log.Printf("[WebRTC] Peer Connection State with %s has changed: %s", peerName, s.String())
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return nil, "", err
	}

	err = pc.SetLocalDescription(offer)
	if err != nil {
		return nil, "", err
	}

	cm.Connections[peerName] = pc

	sdpBytes, _ := json.Marshal(offer)
	return pc, string(sdpBytes), nil
}

// HandleOffer receives an SDP offer, prepares a PeerConnection, and generates an SDP answer
func (cm *CallManager) HandleOffer(peerName string, config webrtc.Configuration, offerSDP string, onICECandidate func(*webrtc.ICECandidate)) (*webrtc.PeerConnection, string, error) {
	cm.Mu.Lock()
	defer cm.Mu.Unlock()

	if len(config.ICEServers) == 0 {
		config.ICEServers = cm.ICEServers
	}

	pc, err := cm.API.NewPeerConnection(config)
	if err != nil {
		return nil, "", err
	}

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		dc.OnOpen(func() {
			cm.setupDataChannel(peerName, dc)
		})
	})

	cm.addAudioTrack(peerName, pc)

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		if onICECandidate != nil {
			onICECandidate(c)
		}
	})

	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		log.Printf("[WebRTC] Peer Connection State with %s has changed: %s", peerName, s.String())
	})

	var offer webrtc.SessionDescription
	json.Unmarshal([]byte(offerSDP), &offer)

	err = pc.SetRemoteDescription(offer)
	if err != nil {
		return nil, "", err
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return nil, "", err
	}

	err = pc.SetLocalDescription(answer)
	if err != nil {
		return nil, "", err
	}

	cm.Connections[peerName] = pc

	sdpBytes, _ := json.Marshal(answer)
	return pc, string(sdpBytes), nil
}

// HandleAnswer processes the received SDP answer
func (cm *CallManager) HandleAnswer(peerName string, answerSDP string) error {
	cm.Mu.Lock()
	pc, ok := cm.Connections[peerName]
	cm.Mu.Unlock()

	if !ok {
		return nil
	}

	var answer webrtc.SessionDescription
	json.Unmarshal([]byte(answerSDP), &answer)

	return pc.SetRemoteDescription(answer)
}

// AddICECandidate adds a trickled ICE candidate
func (cm *CallManager) AddICECandidate(peerName string, candidateStr string) error {
	cm.Mu.Lock()
	pc, ok := cm.Connections[peerName]
	cm.Mu.Unlock()

	if !ok {
		return nil
	}

	var candidate webrtc.ICECandidateInit
	json.Unmarshal([]byte(candidateStr), &candidate)

	return pc.AddICECandidate(candidate)
}

// EndCall cleans up the connection
func (cm *CallManager) EndCall(peerName string) {
	cm.stopAudioPump(peerName)

	cm.Mu.Lock()
	defer cm.Mu.Unlock()

	if pc, ok := cm.Connections[peerName]; ok {
		pc.Close()
		delete(cm.Connections, peerName)
		delete(cm.DataChannels, peerName)
	}
	delete(cm.LocalTracks, peerName)
	delete(cm.RemoteTracks, peerName)
}

func (cm *CallManager) setupDataChannel(peerName string, dc *webrtc.DataChannel) {
	log.Printf("[WebRTC] DataChannel '%s' opened with %s", dc.Label(), peerName)
	cm.Mu.Lock()
	cm.DataChannels[peerName] = dc
	cm.Mu.Unlock()

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		data := msg.Data
		if len(data) == 0 {
			return
		}
		switch {
		case len(data) >= 3 && bytes.Equal(data[:3], []byte("VID")):
			nl := bytes.IndexByte(data, '\n')
			if nl < 0 {
				return
			}
			img, err := jpeg.Decode(bytes.NewReader(data[nl+1:]))
			if err != nil {
				log.Printf("[WebRTC] bad video frame from %s: %v", peerName, err)
				return
			}
			cm.Mu.Lock()
			cb := cm.remoteVideoCBs[peerName]
			cm.Mu.Unlock()
			if cb != nil {
				cb(img)
			}
		case len(data) >= 3 && bytes.Equal(data[:3], []byte("HDR")):
			nl := bytes.IndexByte(data, '\n')
			if nl < 0 {
				return
			}
			var h struct {
				Name string `json:"name"`
				Size int    `json:"size"`
			}
			if err := json.Unmarshal(data[3:nl], &h); err != nil {
				log.Printf("[WebRTC] bad file header from %s: %v", peerName, err)
				return
			}
			cm.Mu.Lock()
			cm.rxState[peerName] = &fileRx{name: h.Name, size: h.Size}
			cm.Mu.Unlock()
		case data[0] == 'D':
			cm.Mu.Lock()
			rx := cm.rxState[peerName]
			cm.Mu.Unlock()
			if rx != nil {
				rx.buf.Write(data[1:])
			}
		case len(data) >= 3 && bytes.Equal(data[:3], []byte("END")):
			cm.Mu.Lock()
			rx := cm.rxState[peerName]
			delete(cm.rxState, peerName)
			cm.Mu.Unlock()
			if rx != nil && cm.OnFile != nil {
				cm.OnFile(peerName, rx.name, rx.size, rx.buf.Bytes())
			}
		}
	})
}

func (cm *CallManager) SendFile(peerName, fileName string, data []byte) error {
	cm.Mu.Lock()
	dc, ok := cm.DataChannels[peerName]
	cm.Mu.Unlock()

	if !ok {
		return fmt.Errorf("no open DataChannel to %s", peerName)
	}

	// Framed protocol: HDR<json-header>\n<chunk>...<chunk>
	hdr, _ := json.Marshal(struct {
		Name string `json:"name"`
		Size int    `json:"size"`
	}{fileName, len(data)})
	if err := dc.Send(append([]byte("HDR"), append(hdr, '\n')...)); err != nil {
		return err
	}

	const chunkSize = 16 * 1024
	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}
		if err := dc.Send(append([]byte{'D'}, data[i:end]...)); err != nil {
			return err
		}
	}
	return dc.Send([]byte("END"))
}
