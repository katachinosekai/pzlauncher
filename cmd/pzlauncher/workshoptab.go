package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"

	"github.com/katachinosekai/pzlauncher/internal/install"
)

const prefCollection = "workshop.collection"

func (u *ui) modsTab() fyne.CanvasObject {
	return container.NewAppTabs(
		container.NewTabItem("Lua mods", u.luaModsTab()),
		container.NewTabItem("Java mods", u.javaModsTab()),
		container.NewTabItem("ZombieBuddy", u.zombieBuddyTab()),
		container.NewTabItem("Custom Pack", u.installTab()),
	)
}

func (u *ui) openURL(url string) {
	if err := install.DefaultSystem().OpenURL(url); err != nil {
		dialog.ShowError(err, u.win)
	}
}
