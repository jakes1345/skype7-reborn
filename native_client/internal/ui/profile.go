package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type ProfileProps struct {
	Username      string
	DisplayName   string
	Mood          string
	AvatarPath    string
	Email         string
	P2PAddr       string
	OnSave        func(mood, displayName string)
	OnAvatarClick func()
}

func NewProfileEditor(props ProfileProps) fyne.CanvasObject {
	// Header Section (Classic Blue Gradient feel)
	nameEntry := widget.NewEntry()
	nameEntry.SetText(props.DisplayName)
	nameEntry.SetPlaceHolder("Display Name")
	
	moodEntry := widget.NewEntry()
	moodEntry.SetText(props.Mood)
	moodEntry.SetPlaceHolder("What's on your mind?")
	
	avatar := NewAvatarWithStatus(96, "Online", props.AvatarPath)
	avatarBtn := widget.NewButton("", props.OnAvatarClick)
	avatarBtn.Importance = widget.LowImportance
	
	header := container.NewHBox(
		container.NewStack(avatar, avatarBtn),
		container.NewVBox(
			widget.NewLabelWithStyle(props.Username, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewGridWithColumns(1, nameEntry, moodEntry),
		),
	)

	// Details Section
	form := widget.NewForm(
		widget.NewFormItem("Email", widget.NewLabel(props.Email)),
		widget.NewFormItem("P2P Mesh Addr", widget.NewLabel(props.P2PAddr)),
	)
	
	// Glass Bottom Action Bar
	saveBtn := widget.NewButtonWithIcon("Save Changes", theme.ConfirmIcon(), func() {
		props.OnSave(moodEntry.Text, nameEntry.Text)
	})
	saveBtn.Importance = widget.HighImportance

	content := container.NewVBox(
		container.NewPadded(header),
		widget.NewSeparator(),
		container.NewPadded(form),
		layout.NewSpacer(),
		container.NewPadded(saveBtn),
	)

	bg := canvas.NewRectangle(color.White)
	return container.NewStack(bg, content)
}
