package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

type Phaze7Theme struct{}

var (
	PhazeBlue      = color.NRGBA{R: 0, G: 120, B: 215, A: 255} // Deep Phaze Cyan
	PhazeLightBlue = color.NRGBA{R: 225, G: 245, B: 255, A: 255}
	PhazeLightGray = color.NRGBA{R: 245, G: 245, B: 245, A: 255}
	PhazeDarkText  = color.NRGBA{R: 85, G: 87, B: 86, A: 255}
)

func (m Phaze7Theme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return color.White
	case theme.ColorNameInputBackground:
		return color.White
	case theme.ColorNamePrimary:
		return PhazeBlue
	case theme.ColorNameButton:
		return PhazeLightGray
	case theme.ColorNameScrollBar:
		return color.NRGBA{R: 0, G: 0, B: 0, A: 50}
	case theme.ColorNameSeparator:
		return color.NRGBA{R: 210, G: 210, B: 210, A: 255}
	case theme.ColorNameSuccess:
		return color.NRGBA{R: 125, G: 190, B: 0, A: 255}
	}
	return theme.DefaultTheme().Color(name, variant)
}

func (m Phaze7Theme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (m Phaze7Theme) Font(style fyne.TextStyle) fyne.Resource {
	// Load the 100% authentic Tahoma font from the Sovereign Vault
	return GetAssetResource("assets/fonts/Tahoma.ttf")
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
