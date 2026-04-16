package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"image/color"
)

type ChatViewProps struct {
	Name        string
	Status      string
	IsGroup     bool
	OnCall      func()
	OnSend      func(text string)
	OnSendFile  func()
}

func NewChatView(props ChatViewProps) *fyne.Container {
	// 1. Header
	icon := canvas.NewCircle(color.NRGBA{G: 200, B: 0, A: 255})
	icon.Resize(fyne.NewSize(12, 12))
	
	nameLabel := widget.NewLabelWithStyle(props.Name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	statusLabel := widget.NewLabelWithStyle(props.Status, fyne.TextAlignLeading, fyne.TextStyle{Italic: true})
	
	headerInfo := container.NewVBox(nameLabel, statusLabel)
	
	headerActions := container.NewHBox(
		widget.NewButtonWithIcon("Call", theme.ConfirmIcon(), props.OnCall),
		widget.NewButtonWithIcon("Video", theme.VisibilityIcon(), func() {}),
		widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() {}),
	)
	
	header := container.NewBorder(nil, nil, icon, headerActions, headerInfo)
	headerBg := canvas.NewRectangle(color.NRGBA{R: 250, G: 252, B: 255, A: 255})
	headerContainer := container.NewStack(headerBg, container.NewPadded(header))

	// 2. Message Area
	msgPlaceholder := widget.NewLabel("No messages yet.")

	// 3. Input Area
	input := widget.NewMultiLineEntry()
	input.SetPlaceHolder("Type a message here...")
	
	sendBtn := widget.NewButtonWithIcon("", theme.MailSendIcon(), func() {
		props.OnSend(input.Text)
		input.SetText("")
	})
	
	inputArea := container.NewBorder(nil, nil, nil, sendBtn, input)
	
	// Final Layout
	return container.NewBorder(
		headerContainer,
		container.NewVBox(widget.NewSeparator(), container.NewPadded(inputArea)),
		nil, nil,
		container.NewPadded(msgPlaceholder),
	)
}
