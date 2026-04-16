package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

type Tazher7Theme struct{}

var (
	TazherBlue      = color.NRGBA{R: 0, G: 120, B: 215, A: 255} // Deep Tazher Cyan
	TazherLightBlue = color.NRGBA{R: 225, G: 245, B: 255, A: 255}
	TazherLightGray = color.NRGBA{R: 245, G: 245, B: 245, A: 255}
	TazherDarkText  = color.NRGBA{R: 85, G: 87, B: 86, A: 255}
)

func (m Tazher7Theme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return color.White
	case theme.ColorNameInputBackground:
		return color.White
	case theme.ColorNamePrimary:
		return TazherBlue
	case theme.ColorNameButton:
		return TazherLightGray
	case theme.ColorNameScrollBar:
		return color.NRGBA{R: 0, G: 0, B: 0, A: 50}
	case theme.ColorNameSeparator:
		return color.NRGBA{R: 210, G: 210, B: 210, A: 255}
	case theme.ColorNameSuccess:
		return color.NRGBA{R: 125, G: 190, B: 0, A: 255}
	}
	return theme.DefaultTheme().Color(name, variant)
}

func (m Tazher7Theme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (m Tazher7Theme) Font(style fyne.TextStyle) fyne.Resource {
	// Load the 100% authentic Tahoma font we just acquired
	res, err := fyne.LoadResourceFromPath("assets/fonts/Tahoma.ttf")
	if err == nil {
		return res
	}
	return theme.DefaultTheme().Font(style)
}

func (m Tazher7Theme) Size(name fyne.ThemeSizeName) float32 {
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
