package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"

	"github.com/katachinosekai/pzlauncher/internal/install"
)

const prefCollection = "workshop.collection"

func (u *ui) toolsTab() fyne.CanvasObject {
	inner := container.NewAppTabs(
		container.NewTabItem("Java mods", u.javaModsTab()),
		container.NewTabItem("Logs", u.logsTab()),
		container.NewTabItem("ZombieBuddy", u.zombieBuddyTab()),
	)
	return inner
}

func (u *ui) openURL(url string) {
	if err := install.DefaultSystem().OpenURL(url); err != nil {
		dialog.ShowError(err, u.win)
	}
}
