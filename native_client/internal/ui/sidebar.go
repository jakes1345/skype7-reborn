package ui

import (
	"image/color"

	"time"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type FriendInfo struct {
	Username string
	Status   string
	Avatar      string
	Mood        string
	DisplayName string
}

type SidebarProps struct {
	Username       string
	Status         string
	Mood           string
	AvatarPath     string
	Slicer         *AeroSlicer
	OnChatOpen     func(name string)
	OnChatWindow   func(name string)
	OnAddFriend    func()
	OnNewGroup     func()
	OnSearch       func(query string)
	OnSettings     func()
	OnProfile      func()
	OnDialCall     func(number string)
	OnStatusChange func(status string)
	RecentChats    []FriendInfo
	CompactMode    bool
}

func NewPhazeSidebar(props SidebarProps) fyne.CanvasObject {
	// 1. Profile Area
	avatarSize := float32(48)
	if props.CompactMode {
		avatarSize = 32
	}
	avatar := NewAvatarWithStatus(avatarSize, props.Status, props.AvatarPath)
	nameLabel := widget.NewLabelWithStyle(props.Username, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	
	// Real click handler on avatar area
	avatarBtn := widget.NewButton("", props.OnProfile)
	avatarBtn.Importance = widget.LowImportance
	
	var rightContent fyne.CanvasObject
	if !props.CompactMode {
		statusSelect := widget.NewSelect([]string{"Online", "Away", "Do Not Disturb", "Invisible"}, props.OnStatusChange)
		statusSelect.SetSelected(props.Status)
		rightContent = container.NewVBox(nameLabel, statusSelect)
	} else {
		rightContent = container.NewVBox(nameLabel)
	}

	profileHeader := container.NewHBox(
		container.NewStack(container.NewPadded(avatar), avatarBtn),
		rightContent,
	)

	profileBg := canvas.NewRectangle(color.NRGBA{R: 0, G: 175, B: 240, A: 255}) // Phaze Blue
	profileContainer := container.NewStack(profileBg, container.NewPadded(profileHeader))
	
	// Use Border to keep header at top without stretching
	sidebarHeader := container.NewVBox(profileContainer)




	// 2. Search & Buttons
	search := widget.NewEntry()
	search.SetPlaceHolder("Search...")
	
	actionButtons := container.NewGridWithColumns(3,
		widget.NewButtonWithIcon("Add", theme.ContentAddIcon(), props.OnAddFriend),
		widget.NewButtonWithIcon("Group", theme.ContentAddIcon(), props.OnNewGroup),
		widget.NewButtonWithIcon("Settings", theme.SettingsIcon(), props.OnSettings),
	)

	search.OnSubmitted = props.OnSearch

	// 3. Main List
	list := widget.NewList(
		func() int { return len(props.RecentChats) },
		func() fyne.CanvasObject {
			size := float32(36)
			if props.CompactMode {
				size = 24
			}
			return container.NewHBox(
				container.NewMax(NewAvatarWithStatus(size, "Offline", "")),
				widget.NewLabel("Contact Name"),
			)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			friend := props.RecentChats[i]
			label := o.(*fyne.Container).Objects[1].(*widget.Label)
			label.SetText(friend.Username)
			
			avatarWrap := o.(*fyne.Container).Objects[0].(*fyne.Container)
			size := float32(36)
			if props.CompactMode {
				size = 24
			}
			avatarWrap.Objects = []fyne.CanvasObject{NewAvatarWithStatus(size, friend.Status, friend.Avatar)}
			avatarWrap.Refresh()
		},
	)
	var lastID widget.ListItemID = -1
	var lastTime time.Time
	
	list.OnSelected = func(id widget.ListItemID) {
		now := time.Now()
		if id == lastID && now.Sub(lastTime) < 500*time.Millisecond {
			if props.OnChatWindow != nil {
				props.OnChatWindow(props.RecentChats[id].Username)
			}
		} else {
			props.OnChatOpen(props.RecentChats[id].Username)
		}
		lastID = id
		lastTime = now
		list.Unselect(id) // Prevent persistent highlight blocking re-clicks
	}

	// 4. Tabs
	tabs := container.NewAppTabs(
		container.NewTabItem("Recent", list),
		container.NewTabItem("Contacts", widget.NewLabel("Global Mesh Directory")),
		container.NewTabItem("Dial", NewPhazeDialpad(DialpadProps{OnCall: props.OnDialCall})),
	)

	sidebarContent := container.NewBorder(
		container.NewVBox(sidebarHeader, container.NewPadded(search), actionButtons, widget.NewSeparator()),
		nil, nil, nil,
		tabs,
	)

	bg := canvas.NewRectangle(color.NRGBA{R: 240, G: 245, B: 250, A: 255})

	return container.NewStack(bg, container.NewPadded(sidebarContent))
}
