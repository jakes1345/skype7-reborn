package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func parseRichText(text string, slicer *AeroSlicer) []fyne.CanvasObject {
	var objects []fyne.CanvasObject
	pos := 0
	matches := emoticonRegex.FindAllStringIndex(text, -1)
	for _, m := range matches {
		start, end := m[0], m[1]
		if start > pos {
			objects = append(objects, widget.NewLabel(text[pos:start]))
		}
		tok := text[start:end]

		emoji := NewAnimatedEmoji(tok, slicer)
		if emoji != nil {
			emoji.imageObj.SetMinSize(fyne.NewSize(24, 24))
			objects = append(objects, emoji)
		} else {
			objects = append(objects, widget.NewLabel(tok))
		}
		pos = end
	}
	if pos < len(text) {
		objects = append(objects, widget.NewLabel(text[pos:]))
	}
	if len(objects) == 0 {
		objects = append(objects, widget.NewLabel(text))
	}
	return objects
}

func NewMessageBubble(author, text string, isMe bool, slicer *AeroSlicer) fyne.CanvasObject {
	var bg fyne.CanvasObject
	if isMe {
		// Phaze Premium Blue Gradient
		top := color.NRGBA{R: 225, G: 245, B: 255, A: 255}
		bottom := color.NRGBA{R: 210, G: 235, B: 250, A: 255}
		grad := canvas.NewLinearGradient(top, bottom, 90) // Vertical

		rect := canvas.NewRectangle(color.Transparent)
		rect.StrokeColor = color.NRGBA{R: 0, G: 175, B: 240, A: 40}
		rect.StrokeWidth = 1
		bg = container.NewStack(grad, rect)
	} else {
		// Classic Neutral Gray Gradient
		top := color.NRGBA{R: 248, G: 248, B: 248, A: 255}
		bottom := color.NRGBA{R: 235, G: 235, B: 235, A: 255}
		grad := canvas.NewLinearGradient(top, bottom, 90)

		rect := canvas.NewRectangle(color.Transparent)
		rect.StrokeColor = color.NRGBA{R: 0, G: 0, B: 0, A: 20}
		rect.StrokeWidth = 1
		bg = container.NewStack(grad, rect)
	}

	nameLabel := widget.NewLabelWithStyle(author, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	bodyContent := container.NewHBox(parseRichText(text, slicer)...)

	content := container.NewVBox(nameLabel, bodyContent)
	return container.NewStack(bg, container.NewPadded(content))
}
