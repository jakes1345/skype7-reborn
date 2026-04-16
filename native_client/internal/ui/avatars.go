package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

func brighten(c uint8, amount int) uint8 {
	v := int(c) + amount
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// NewAvatarWithStatus renders a circular avatar with a presence dot in the
// bottom-right corner — the authentic Tazher 7 look.
func NewAvatarWithStatus(size float32, status string, imagePath string) fyne.CanvasObject {
	var base fyne.CanvasObject

	if imagePath != "" {
		img := canvas.NewImageFromFile(imagePath)
		img.FillMode = canvas.ImageFillCover
		img.SetMinSize(fyne.NewSize(size, size))
		base = img
	} else {
		circle := canvas.NewCircle(color.NRGBA{R: 220, G: 220, B: 220, A: 255})
		circle.StrokeColor = color.NRGBA{R: 0, G: 0, B: 0, A: 20}
		circle.StrokeWidth = 1
		base = circle
	}

	statusColor := color.NRGBA{R: 120, G: 120, B: 120, A: 255} // Offline
	switch status {
	case "Online":
		statusColor = color.NRGBA{R: 125, G: 190, B: 0, A: 255}
	case "Away":
		statusColor = color.NRGBA{R: 255, G: 200, B: 0, A: 255}
	case "Do Not Disturb", "DND":
		statusColor = color.NRGBA{R: 230, G: 0, B: 0, A: 255}
	case "Invisible":
		statusColor = color.NRGBA{R: 180, G: 180, B: 180, A: 255}
	}

	// Status Orb (Aero Glass 3D Effect)
	highlightColor := color.NRGBA{
		R: brighten(statusColor.R, 60),
		G: brighten(statusColor.G, 60),
		B: brighten(statusColor.B, 60),
		A: 255,
	}
	badge := canvas.NewRadialGradient(highlightColor, statusColor)
	
	// White "High-Polish" outer frame
	frame := canvas.NewCircle(color.White)
	frame.StrokeColor = color.NRGBA{R: 0, G: 0, B: 0, A: 20}
	frame.StrokeWidth = 1

	// Badge is ~1/3 the avatar size, anchored to bottom-right via absolute positioning.
	badgeSize := size / 3
	wrap := container.NewStack(frame, container.NewPadded(base), container.NewWithoutLayout(badge))
	badge.Resize(fyne.NewSize(badgeSize, badgeSize))
	badge.Move(fyne.NewPos(size-badgeSize-2, size-badgeSize-2))
	
	// Ensure the container itself has a minimum size
	return container.NewGridWrap(fyne.NewSize(size, size), wrap)
}
