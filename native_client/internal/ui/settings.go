package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

type SettingsProps struct {
	DisplayName     string
	Mood            string
	PhoneNumber     string
	IsPhoneVerified bool
	OnSaveProfile   func(name, mood string)
	OnLinkPhone     func(number string)
	OnVerifyPhone   func(code string)
}

func ShowTazherSettings(window fyne.Window, props SettingsProps) {
	// Account Tab
	nameEntry := widget.NewEntry()
	nameEntry.SetText(props.DisplayName)
	moodEntry := widget.NewEntry()
	moodEntry.SetText(props.Mood)

	accountTab := container.NewVBox(
		widget.NewLabelWithStyle("Account Identity", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewForm(
			widget.NewFormItem("Display Name", nameEntry),
			widget.NewFormItem("Mood Message", moodEntry),
		),
		widget.NewButton("Save Profile", func() {
			props.OnSaveProfile(nameEntry.Text, moodEntry.Text)
		}),
	)

	// Identity/Phone Tab
	phoneEntry := widget.NewEntry()
	phoneEntry.SetPlaceHolder("+1...")
	phoneEntry.SetText(props.PhoneNumber)

	status := "Not Linked"
	if props.IsPhoneVerified {
		status = "Verified ✅"
	}

	codeEntry := widget.NewEntry()
	codeEntry.SetPlaceHolder("SMS code")

	identityTab := container.NewVBox(
		widget.NewLabelWithStyle("Phone Linking (Caller ID)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Current Status: "+status),
		widget.NewForm(
			widget.NewFormItem("Phone Number", phoneEntry),
		),
		widget.NewButton("Link Phone via SMS", func() {
			props.OnLinkPhone(phoneEntry.Text)
			dialog.ShowInformation("TAZHER", "Verification code sent to "+phoneEntry.Text, window)
		}),
		widget.NewSeparator(),
		widget.NewForm(
			widget.NewFormItem("Enter Code", codeEntry),
		),
		widget.NewButton("Verify Code", func() {
			props.OnVerifyPhone(codeEntry.Text)
		}),
	)

	tabs := container.NewAppTabs(
		container.NewTabItem("Account", accountTab),
		container.NewTabItem("Identity", identityTab),
		container.NewTabItem("Mesh", widget.NewLabel("P2P Mesh Settings coming soon...")),
	)

	d := dialog.NewCustom("Tazher Settings", "Close", tabs, window)
	d.Resize(fyne.NewSize(450, 400))
	d.Show()
}
