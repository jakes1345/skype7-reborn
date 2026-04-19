package ui

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func brighten(c uint8, amount int) uint8 {
	v := int(c) + amount
	if v > 255 {
		return 255
	}
	return uint8(v)
}

var avatarColors = []color.NRGBA{
	{R: 0, G: 120, B: 215, A: 255},
	{R: 0, G: 150, B: 136, A: 255},
	{R: 156, G: 39, B: 176, A: 255},
	{R: 244, G: 67, B: 54, A: 255},
	{R: 255, G: 152, B: 0, A: 255},
	{R: 96, G: 125, B: 139, A: 255},
	{R: 233, G: 30, B: 99, A: 255},
	{R: 139, G: 195, B: 74, A: 255},
}

func getAvatarColor(name string) color.NRGBA {
	hash := 0
	for _, c := range name {
		hash = hash*31 + int(c)
	}
	return avatarColors[hash%len(avatarColors)]
}

func getInitials(name string) string {
	parts := strings.Fields(name)
	if len(parts) >= 2 {
		return strings.ToUpper(string(parts[0][0]) + string(parts[1][0]))
	}
	if len(name) >= 2 {
		return strings.ToUpper(name[:2])
	}
	return strings.ToUpper(name)
}

func NewAvatarWithStatus(size float32, status string, imagePath string) fyne.CanvasObject {
	var base fyne.CanvasObject

	if imagePath != "" {
		img := canvas.NewImageFromFile(imagePath)
		img.FillMode = canvas.ImageFillCover
		img.SetMinSize(fyne.NewSize(size, size))
		base = img
	} else {
		bg := canvas.NewRectangle(color.NRGBA{R: 220, G: 220, B: 220, A: 255})
		bg.StrokeColor = color.NRGBA{R: 0, G: 0, B: 0, A: 20}
		bg.StrokeWidth = 1
		base = bg
	}

	statusColor := color.NRGBA{R: 120, G: 120, B: 120, A: 255}
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

	highlightColor := color.NRGBA{
		R: brighten(statusColor.R, 60),
		G: brighten(statusColor.G, 60),
		B: brighten(statusColor.B, 60),
		A: 255,
	}
	badge := canvas.NewRadialGradient(highlightColor, statusColor)

	frame := canvas.NewCircle(color.White)
	frame.StrokeColor = color.NRGBA{R: 0, G: 0, B: 0, A: 20}
	frame.StrokeWidth = 1

	badgeSize := size / 3
	wrap := container.NewStack(frame, container.NewPadded(base), container.NewWithoutLayout(badge))
	badge.Resize(fyne.NewSize(badgeSize, badgeSize))
	badge.Move(fyne.NewPos(size-badgeSize-2, size-badgeSize-2))

	return container.NewGridWrap(fyne.NewSize(size, size), wrap)
}

func NewAvatarWithInitials(size float32, name string, status string) fyne.CanvasObject {
	bgColor := getAvatarColor(name)

	bg := canvas.NewRectangle(bgColor)

	initials := widget.NewLabel(getInitials(name))
	initials.Alignment = fyne.TextAlignCenter

	bgContainer := container.NewMax(bg, initials)
	bgContainer.Resize(fyne.NewSize(size, size))

	return NewAvatarWithStatusFromBase(size, status, bgContainer)
}

func NewAvatarWithStatusFromBase(size float32, status string, base fyne.CanvasObject) fyne.CanvasObject {
	statusColor := color.NRGBA{R: 120, G: 120, B: 120, A: 255}
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

	highlightColor := color.NRGBA{
		R: brighten(statusColor.R, 60),
		G: brighten(statusColor.G, 60),
		B: brighten(statusColor.B, 60),
		A: 255,
	}
	badge := canvas.NewRadialGradient(highlightColor, statusColor)

	frame := canvas.NewCircle(color.White)
	frame.StrokeColor = color.NRGBA{R: 0, G: 0, B: 0, A: 20}
	frame.StrokeWidth = 1

	badgeSize := size / 3
	wrap := container.NewStack(frame, container.NewPadded(base), container.NewWithoutLayout(badge))
	badge.Resize(fyne.NewSize(badgeSize, badgeSize))
	badge.Move(fyne.NewPos(size-badgeSize-2, size-badgeSize-2))

	return container.NewGridWrap(fyne.NewSize(size, size), wrap)
}
