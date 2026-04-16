package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
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

type ChatView struct {
	Container *fyne.Container
	Pulsar    *TazherPulsar
}

func NewChatView(props ChatViewProps) *ChatView {
	pulsar := NewTazherPulsar()
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

	emojiBtn := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() {
		// Just show at 0,0 for now as simple test
		ShowEmoticonPopup(fyne.CurrentApp().Driver().AllWindows()[0].Canvas(), fyne.NewPos(100, 100), func(s string) {
			input.SetText(input.Text + s)
		})
	})
	
	fileBtn := widget.NewButtonWithIcon("", theme.FileIcon(), props.OnSendFile)
	
	sendBtn := widget.NewButtonWithIcon("", theme.MailSendIcon(), func() {
		if input.Text != "" {
			props.OnSend(input.Text)
			input.SetText("")
		}
	})
	
	leftActions := container.NewHBox(emojiBtn, fileBtn)
	inputArea := container.NewBorder(nil, nil, leftActions, sendBtn, container.NewPadded(input))
	
	// Final Layout
	main := container.NewBorder(
		headerContainer,
		container.NewVBox(
			container.NewHBox(layout.NewSpacer(), pulsar.Container, layout.NewSpacer()),
			widget.NewSeparator(), 
			container.NewPadded(inputArea),
		),
		nil, nil,
		container.NewPadded(msgPlaceholder),
	)
	return &ChatView{Container: main, Pulsar: pulsar}
}
