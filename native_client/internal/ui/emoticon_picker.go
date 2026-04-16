package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

type EmoticonPickerProps struct {
	Slicer     *AeroSlicer
	OnSelected func(shortcut string)
}

// EmoticonButton is a simple wrapper for pickable emoticons
type EmoticonButton struct {
	widget.BaseWidget
	emoji  *AnimatedEmoji
	action func()
}

func NewEmoticonButton(emoji *AnimatedEmoji, action func()) *EmoticonButton {
	eb := &EmoticonButton{emoji: emoji, action: action}
	eb.ExtendBaseWidget(eb)
	return eb
}

func (eb *EmoticonButton) Tapped(_ *fyne.PointEvent) {
	if eb.action != nil {
		eb.action()
	}
}

func (eb *EmoticonButton) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(eb.emoji)
}

func NewEmoticonPicker(props EmoticonPickerProps) fyne.CanvasObject {
	// Classic Skype 7 shortcuts
	shortcuts := []string{
		"(smile)", "(sad)", "(laugh)", "(cool)", "(surprised)",
		"(wink)", "(crying)", "(sweat)", "(speechless)", "(kiss)",
		"(cheeky)", "(blush)", "(wonder)", "(sleepy)", "(dull)",
		"(inlove)", "(egrin)", "(party)", "(beer)",
		"(dance)", "(rock)", "(punch)", "(flex)", "(highfive)",
	}

	grid := container.New(layout.NewGridLayout(5))
	for _, s := range shortcuts {
		shortcut := s
		emoji := NewAnimatedEmoji(shortcut, props.Slicer)
		if emoji == nil {
			continue
		}
		
		btn := NewEmoticonButton(emoji, func() {
			props.OnSelected(shortcut)
		})
		grid.Add(btn)
	}

	return container.NewScroll(grid)
}

func ShowEmoticonPopup(canvas fyne.Canvas, slicer *AeroSlicer, pos fyne.Position, onSelected func(string)) {
	picker := NewEmoticonPicker(EmoticonPickerProps{
		Slicer:     slicer,
		OnSelected: onSelected,
	})
	picker.Resize(fyne.NewSize(240, 300))
	
	pop := widget.NewPopUp(picker, canvas)
	pop.ShowAtPosition(pos)
}
