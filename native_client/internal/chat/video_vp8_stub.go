//go:build android
// +build android

package chat

import (
	"fmt"
	"image"

	"github.com/pion/webrtc/v3"
)

// Android build keeps the existing JPEG-over-DataChannel path. These stubs
// ensure the callers compile without pulling in libvpx on the NDK toolchain.

func (cm *CallManager) initVP8Sender(peerName string, pc *webrtc.PeerConnection) error {
	return fmt.Errorf("VP8 disabled on android build")
}

func (cm *CallManager) WriteVideoFrameVP8(peerName string, img image.Image) bool {
	return false
}

func stopVP8Sender(peerName string) {}

func (cm *CallManager) drainVP8Receive(peerName string, remote *webrtc.TrackRemote) {}
