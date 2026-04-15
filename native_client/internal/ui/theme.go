package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

type Skype7Theme struct{}

var (
	SkypeBlue      = color.NRGBA{R: 0, G: 175, B: 240, A: 255}
	SkypeLightBlue = color.NRGBA{R: 225, G: 245, B: 255, A: 255}
	SkypeLightGray = color.NRGBA{R: 245, G: 245, B: 245, A: 255}
	SkypeDarkText  = color.NRGBA{R: 85, G: 87, B: 86, A: 255}
)

func (m Skype7Theme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return color.White
	case theme.ColorNameInputBackground:
		return color.White
	case theme.ColorNamePrimary:
		return SkypeBlue
	case theme.ColorNameButton:
		return SkypeLightGray
	case theme.ColorNameScrollBar:
		return color.NRGBA{R: 0, G: 0, B: 0, A: 50}
	case theme.ColorNameSeparator:
		return color.NRGBA{R: 210, G: 210, B: 210, A: 255}
	case theme.ColorNameSuccess:
		return color.NRGBA{R: 125, G: 190, B: 0, A: 255}
	}
	return theme.DefaultTheme().Color(name, variant)
}

func (m Skype7Theme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (m Skype7Theme) Font(style fyne.TextStyle) fyne.Resource {
	// Load the 100% authentic Tahoma font we just acquired
	res, err := fyne.LoadResourceFromPath("assets/fonts/Tahoma.ttf")
	if err == nil {
		return res
	}
	return theme.DefaultTheme().Font(style)
}

func (m Skype7Theme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 4
	case theme.SizeNameText:
		return 13
	case theme.SizeNameInputBorder:
		return 1
	}
	return theme.DefaultTheme().Size(name)
}
