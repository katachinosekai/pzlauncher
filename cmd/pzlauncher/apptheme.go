package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

type appTheme struct{ fyne.Theme }

var mutedColor = color.NRGBA{R: 0x8c, G: 0x8c, B: 0x8c, A: 0xff}

func (t appTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if n == theme.ColorNameDisabled {
		return mutedColor
	}
	return t.Theme.Color(n, v)
}

func newAppTheme() fyne.Theme { return appTheme{theme.DefaultTheme()} }
