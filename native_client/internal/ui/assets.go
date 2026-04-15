package ui

import (
	"bytes"
	"image"
	"image/png"
	_ "image/png"
	"os"

	"fyne.io/fyne/v2"
)

// AeroSlicer extracts pixel-perfect UI elements from the original Skype 7
// master sprite sheet.
type AeroSlicer struct {
	MasterSheet image.Image
}

func NewAeroSlicer(path string) (*AeroSlicer, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	return &AeroSlicer{MasterSheet: img}, nil
}

// Slice crops a rect from the master sheet and re-encodes it as a PNG
// packaged in a fyne.Resource ready for widgets/icons.
func (a *AeroSlicer) Slice(name string, x, y, w, h int) fyne.Resource {
	if a.MasterSheet == nil {
		return nil
	}
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	si, ok := a.MasterSheet.(subImager)
	if !ok {
		return nil
	}
	sub := si.SubImage(image.Rect(x, y, x+w, y+h))

	var buf bytes.Buffer
	if err := png.Encode(&buf, sub); err != nil {
		return nil
	}
	return fyne.NewStaticResource(name, buf.Bytes())
}

// GetStatusIcon returns the Skype 7 presence dot for a given state.
// Coordinates correspond to the 12x12 dots strip on the master spritesheet.
func (a *AeroSlicer) GetStatusIcon(presence string) fyne.Resource {
	var x int
	switch presence {
	case "Online":
		x = 0
	case "Away":
		x = 14
	case "Do Not Disturb", "DND":
		x = 28
	default:
		x = 42 // Offline / Invisible
	}
	return a.Slice("status_"+presence+".png", x, 0, 12, 12)
}
