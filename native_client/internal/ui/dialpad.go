package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

type DialpadProps struct {
	OnCall func(number string)
}

func NewTazherDialpad(props DialpadProps) fyne.CanvasObject {
	numberEntry := widget.NewEntry()
	numberEntry.SetPlaceHolder("+1 123 456 7890")
	numberEntry.TextStyle = fyne.TextStyle{Bold: true}

	keys := []string{
		"1", "2", "3",
		"4", "5", "6",
		"7", "8", "9",
		"*", "0", "#",
	}

	grid := container.New(layout.NewGridLayout(3))
	for _, k := range keys {
		key := k
		btn := widget.NewButton(key, func() {
			numberEntry.SetText(numberEntry.Text + key)
		})
		grid.Add(btn)
	}

	callBtn := widget.NewButton("Call", func() {
		props.OnCall(numberEntry.Text)
	})
	callBtn.Importance = widget.HighImportance

	clearBtn := widget.NewButton("Clear", func() {
		numberEntry.SetText("")
	})

	return container.NewPadded(
		container.NewVBox(
			widget.NewLabelWithStyle("Phone Dialer", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
			numberEntry,
			layout.NewSpacer(),
			grid,
			layout.NewSpacer(),
			container.NewGridWithColumns(2, clearBtn, callBtn),
		),
	)
}
