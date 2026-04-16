package ui

import (
	"image"
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"github.com/pion/mediadevices"
	"github.com/pion/mediadevices/pkg/prop"

	_ "github.com/pion/mediadevices/pkg/driver/camera" // Register camera driver
)

type VideoPreview struct {
	Container fyne.CanvasObject
	Image     *canvas.Image
	Stop      chan bool
}

func NewVideoPreview(width, height int) *VideoPreview {
	img := canvas.NewImageFromImage(image.NewNRGBA(image.Rect(0, 0, width, height)))
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(float32(width), float32(height)))

	return &VideoPreview{
		Container: container.NewMax(img),
		Image:     img,
		Stop:      make(chan bool),
	}
}

func (v *VideoPreview) Start() {
	s, err := mediadevices.GetUserMedia(mediadevices.MediaStreamConstraints{
		Video: func(c *mediadevices.MediaTrackConstraints) {
			c.Width = prop.Int(640)
			c.Height = prop.Int(480)
			c.FrameRate = prop.Float(30)
		},
	})
	if err != nil {
		log.Printf("[Video] Error: %v", err)
		return
	}

	tracks := s.GetVideoTracks()
	if len(tracks) == 0 {
		log.Printf("[Video] Error: No video tracks found in stream")
		return
	}
	
	track := tracks[0]
	videoTrack := track.(*mediadevices.VideoTrack)
	
	reader := videoTrack.NewReader(false)

	go func() {
		defer videoTrack.Close()
		for {
			select {
			case <-v.Stop:
				return
			default:
				frame, release, err := reader.Read()
				if err != nil {
					log.Printf("[Video] Read error: %v", err)
					return
				}
				v.Image.Image = frame
				v.Image.Refresh()
				release()
			}
		}
	}()
}
