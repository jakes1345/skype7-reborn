package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

type EmoticonPickerProps struct {
	OnSelected func(shortcut string)
}

func NewEmoticonPicker(props EmoticonPickerProps) fyne.CanvasObject {
	// Classic Skype 7 shortcuts
	shortcuts := []string{
		"(smile)", "(sad)", "(laugh)", "(cool)", "(surprised)",
		"(wink)", "(crying)", "(sweat)", "(speechless)", "(kiss)",
		"(cheeky)", "(blush)", "(wonder)", "(sleepy)", "(dull)",
		"(inlove)", "(egrin)", "(finger)", "(party)", "(beer)",
		"(dance)", "(rock)", "(punch)", "(flex)", "(highfive)",
	}

	grid := container.New(layout.NewGridLayout(5))
	for _, s := range shortcuts {
		shortcut := s
		// In a full implementation, we'd use NewEmoticonImage here.
		// For the picker grid, we'll use buttons with the shortcut as label for now.
		btn := widget.NewButton(shortcut, func() {
			props.OnSelected(shortcut)
		})
		btn.Importance = widget.LowImportance
		grid.Add(btn)
	}

	return container.NewScroll(grid)
}

func ShowEmoticonPopup(canvas fyne.Canvas, pos fyne.Position, onSelected func(string)) {
	picker := NewEmoticonPicker(EmoticonPickerProps{OnSelected: onSelected})
	picker.Resize(fyne.NewSize(200, 250))
	
	pop := widget.NewPopUp(picker, canvas)
	pop.ShowAtPosition(pos)
}
