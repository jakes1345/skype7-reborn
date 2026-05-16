package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

type Phaze7Theme struct{}

var (
	// PhazeBlue — primary chrome (Skype-inspired cyan; product name is Phaze).
	PhazeBlue      = color.NRGBA{R: 0, G: 175, B: 240, A: 255}
	PhazeLightBlue = color.NRGBA{R: 225, G: 245, B: 255, A: 255}
	PhazeLightGray = color.NRGBA{R: 245, G: 245, B: 245, A: 255}
	PhazeShell     = color.NRGBA{R: 237, G: 244, B: 252, A: 255} // window / list well
	PhazeDarkText  = color.NRGBA{R: 38, G: 38, B: 38, A: 255}
	PhazePanel     = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	PhazeSeparator = color.NRGBA{R: 200, G: 214, B: 230, A: 255}
)

func (m Phaze7Theme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return PhazeShell
	case theme.ColorNameInputBackground:
		return PhazePanel
	case theme.ColorNamePrimary:
		return PhazeBlue
	case theme.ColorNameButton:
		return PhazeLightGray
	case theme.ColorNameForeground:
		return PhazeDarkText
	case theme.ColorNamePlaceHolder:
		return color.NRGBA{R: 120, G: 130, B: 140, A: 255}
	case theme.ColorNameHover:
		return PhazeLightBlue
	case theme.ColorNameSelection:
		return PhazeLightBlue
	case theme.ColorNameFocus:
		return PhazeBlue
	case theme.ColorNameScrollBar:
		return color.NRGBA{R: 0, G: 0, B: 0, A: 55}
	case theme.ColorNameSeparator:
		return PhazeSeparator
	case theme.ColorNameSuccess:
		return color.NRGBA{R: 125, G: 190, B: 0, A: 255}
	case theme.ColorNameWarning:
		return color.NRGBA{R: 255, G: 185, B: 0, A: 255}
	case theme.ColorNameError:
		return color.NRGBA{R: 200, G: 40, B: 40, A: 255}
	}
	return theme.DefaultTheme().Color(name, variant)
}

func (m Phaze7Theme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (m Phaze7Theme) Font(style fyne.TextStyle) fyne.Resource {
	r := GetAssetResource("fonts/Tahoma.ttf")
	if len(r.Content()) > 1024 {
		return r
	}
	return theme.DefaultTheme().Font(style)
}

func (m Phaze7Theme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 8
	case theme.SizeNameText:
		return 14
	case theme.SizeNameInputBorder:
		return 1
	case theme.SizeNameCaptionText:
		return 11
	case theme.SizeNameInlineIcon:
		return 20
	case theme.SizeNameScrollBar:
		return 12
	}
	return theme.DefaultTheme().Size(name)
}
