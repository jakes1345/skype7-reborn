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

type FriendInfo struct {
	Username string
	Status   string
	Avatar   string
	Mood     string
}

type SidebarProps struct {
	Username    string
	Status      string
	Mood        string
	AvatarPath  string
	Slicer      *AeroSlicer
	OnChatOpen  func(name string)
	OnAddFriend func()
	OnNewGroup  func()
	OnProfile   func()
	RecentChats []FriendInfo
}

func NewTazherSidebar(props SidebarProps) fyne.CanvasObject {
	// 1. Profile Area
	avatar := NewAvatarWithStatus(48, props.Status, props.AvatarPath)
	nameLabel := widget.NewLabelWithStyle(props.Username, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	moodLabel := widget.NewLabelWithStyle(props.Mood, fyne.TextAlignLeading, fyne.TextStyle{Italic: true})
	if props.Mood == "" {
		moodLabel.SetText("Set mood...")
	}

	profile := container.NewHBox(
		container.NewPadded(widget.NewButton("", props.OnProfile)), // Overlay on avatar area
		container.NewVBox(layout.NewSpacer(), nameLabel, moodLabel, layout.NewSpacer()),
	)
	
	// Real click handler on avatar area
	avatarBtn := widget.NewButton("", props.OnProfile)
	avatarBtn.Importance = widget.LowImportance
	
	profileHeader := container.NewHBox(
		container.NewStack(container.NewPadded(avatar), avatarBtn),
		container.NewVBox(layout.NewSpacer(), nameLabel, moodLabel, layout.NewSpacer()),
	)

	profileBg := canvas.NewRectangle(color.NRGBA{R: 0, G: 175, B: 240, A: 255}) // Tazher Blue
	profileContainer := container.NewStack(profileBg, container.NewPadded(profileHeader))
	
	// Use Border to keep header at top without stretching
	sidebarHeader := container.NewVBox(profileContainer)




	// 2. Search & Buttons
	search := widget.NewEntry()
	search.SetPlaceHolder("Search...")
	
	actionButtons := container.NewGridWithColumns(3,
		widget.NewButtonWithIcon("Add", theme.ContentAddIcon(), props.OnAddFriend),
		widget.NewButtonWithIcon("Search", theme.SearchIcon(), func() {}),
		widget.NewButtonWithIcon("Settings", theme.SettingsIcon(), func() {}),
	)

	// 3. Main List
	list := widget.NewList(
		func() int { return len(props.RecentChats) },
		func() fyne.CanvasObject {
			return container.NewHBox(
				canvas.NewCircle(color.Gray{Y: 128}), // Placeholder status dot
				widget.NewLabel("Contact Name"),
			)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			friend := props.RecentChats[i]
			o.(*fyne.Container).Objects[1].(*widget.Label).SetText(friend.Username)
			
			dot := o.(*fyne.Container).Objects[0].(*canvas.Circle)
			if friend.Status == "online" {
				dot.FillColor = color.NRGBA{R: 50, G: 220, B: 50, A: 255} // Green
			} else {
				dot.FillColor = color.NRGBA{R: 180, G: 180, B: 180, A: 255} // Gray
			}
			dot.Resize(fyne.NewSize(12, 12))
			dot.Refresh()
		},
	)
	list.OnSelected = func(id widget.ListItemID) {
		props.OnChatOpen(props.RecentChats[id].Username)
	}

	// 4. Tabs
	tabs := container.NewAppTabs(
		container.NewTabItem("Recent", list),
		container.NewTabItem("Contacts", widget.NewLabel("Contacts coming soon...")),
	)

	sidebarContent := container.NewBorder(
		container.NewVBox(sidebarHeader, container.NewPadded(search), actionButtons, widget.NewSeparator()),
		nil, nil, nil,
		tabs,
	)

	bg := canvas.NewRectangle(color.NRGBA{R: 240, G: 245, B: 250, A: 255})

	return container.NewStack(bg, container.NewPadded(sidebarContent))
}
