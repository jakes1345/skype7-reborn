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

// TazherHome creates the classic 2-column "Tazher Home" view.
func NewTazherHome(username, mood string, slicer *AeroSlicer, onMoodChange func(string)) fyne.CanvasObject {
	// --- Sidebar / Left Column (The Hood) ---
	avatar := canvas.NewImageFromFile("assets/default_avatar.png")
	avatar.SetMinSize(fyne.NewSize(80, 80))
	avatar.FillMode = canvas.ImageFillContain

	logo := canvas.NewImageFromFile("assets/tazher_logo.png")
	logo.SetMinSize(fyne.NewSize(120, 60))
	logo.FillMode = canvas.ImageFillContain

	welcome := canvas.NewText(username, color.Black)
	welcome.TextStyle = fyne.TextStyle{Bold: true}
	welcome.TextSize = 28 // Flagship size
	welcome.Alignment = fyne.TextAlignCenter

	slogan := canvas.NewText("Don't stop til you've had enough", color.NRGBA{R: 255, G: 255, B: 255, A: 180})
	slogan.TextStyle = fyne.TextStyle{Italic: true}
	slogan.TextSize = 14
	slogan.Alignment = fyne.TextAlignCenter

	moodEntry := widget.NewEntry()
	moodEntry.SetPlaceHolder("Tell your friends what you're up to...")
	moodEntry.SetText(mood)
	moodEntry.OnSubmitted = onMoodChange

	statusIcon := canvas.NewImageFromResource(theme.QuestionIcon())
	if slicer != nil {
		statusIcon = canvas.NewImageFromResource(slicer.GetStatusIcon("Online"))
	}
	statusIcon.FillMode = canvas.ImageFillContain
	statusIcon.SetMinSize(fyne.NewSize(14, 14))
	statusContainer := container.NewCenter(statusIcon)
	statusContainer.Resize(fyne.NewSize(16, 16))

	// The Hood Gradient Background (Tazher Aero Blue)
	hoodBg := canvas.NewLinearGradient(
		color.NRGBA{R: 0, G: 160, B: 245, A: 255},
		color.NRGBA{R: 45, G: 190, B: 255, A: 255},
		0,
	)

	hoodContent := container.NewBorder(
		nil, nil,
		container.NewHBox(container.NewPadded(avatar), layout.NewSpacer()),
		container.NewPadded(logo),
		container.NewVBox(
			layout.NewSpacer(),
			welcome,
			slogan,
			container.NewHBox(layout.NewSpacer(), statusContainer, widget.NewLabelWithStyle("Online", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}), layout.NewSpacer()),
			container.NewHBox(layout.NewSpacer(), container.NewGridWrap(fyne.NewSize(300, 40), moodEntry), layout.NewSpacer()),
			layout.NewSpacer(),

		),
	)

	hood := container.NewStack(hoodBg, container.NewPadded(hoodContent))

	// --- Feed / Feed Column ---
	feedHeader := widget.NewLabelWithStyle("What's new with your friends?", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	
	// Tiles (Tazher 7 Home style)
	tiles := container.NewGridWithColumns(2,
		createHomeTile("What's new?", "Check out the latest updates from your friends.", theme.InfoIcon()),
		createHomeTile("Add Contacts", "Find people you know on Tazher.", theme.AccountIcon()),
		createHomeTile("Call Phones", "Low rates on calls to mobiles and landlines.", theme.SearchIcon()),
		createHomeTile("Video Message", "Record and send a personal video message.", theme.VisibilityIcon()),
	)

	// Feed items
	socialFeed := container.NewVBox(
		widget.NewLabel("No new updates yet."),
		widget.NewButton("Find Friends", func() {}),
	)

	feedScroll := container.NewVScroll(container.NewPadded(container.NewVBox(feedHeader, tiles, widget.NewSeparator(), socialFeed)))

	// --- Right Column (Widgets) ---
	onlineNowHeader := widget.NewLabelWithStyle("Online Now", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	onlineNowList := widget.NewLabel("No contacts online")
	
	shortcutsHeader := widget.NewLabelWithStyle("Shortcuts", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	shortcuts := container.NewVBox(
		widget.NewButton("Add a Contact", func() {}),
		widget.NewButton("Call Phones", func() {}),
		widget.NewButton("Video Message", func() {}),
	)

	rightCol := container.NewVBox(
		onlineNowHeader, onlineNowList,
		widget.NewSeparator(),
		shortcutsHeader, shortcuts,
	)

	// Main Layout: Split Feed (Left/Center) and Widgets (Right)
	mainContent := container.NewHSplit(
		feedScroll,
		container.NewPadded(rightCol),
	)
	mainContent.SetOffset(0.7)

	return container.NewBorder(
		container.NewPadded(hood),
		nil, nil, nil,
		mainContent,
	)
}

func createHomeTile(title, desc string, icon fyne.Resource) fyne.CanvasObject {
	titleLabel := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	descLabel := widget.NewLabel(desc)
	descLabel.Wrapping = fyne.TextWrapWord

	img := canvas.NewImageFromResource(icon)
	img.SetMinSize(fyne.NewSize(32, 32))
	img.FillMode = canvas.ImageFillContain

	cardBg := canvas.NewRectangle(color.NRGBA{R: 245, G: 250, B: 255, A: 255})
	cardBg.StrokeColor = color.NRGBA{R: 200, G: 220, B: 240, A: 255}
	cardBg.StrokeWidth = 1

	content := container.NewHBox(
		container.NewPadded(img),
		container.NewVBox(layout.NewSpacer(), titleLabel, descLabel, layout.NewSpacer()),
	)

	return container.NewStack(cardBg, container.NewPadded(container.NewPadded(content)))
}

