package main

import (
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

const disclaimer = "Unofficial fan tool, not affiliated with The Indie Stone"

const creditsText = `## Unofficial fan tool

This launcher is a free, non-commercial fan project. It is not affiliated with, endorsed by or supported by The Indie Stone. Please don't report problems with it or with the pack's mods to The Indie Stone.

## Project Zomboid

Project Zomboid is made by **The Indie Stone**. All rights to the game, its name and its assets belong to them. The launcher starts the game through Steam and does not change or redistribute any game files.

## Mods

The mods installed through the pack come from the Steam Workshop and belong to their authors. Thanks to every author in the collection, and to **zed-0xff** for ZombieBuddy, which the pack's Java mods build on. Java mods are not officially supported by The Indie Stone.

## Built with

- **Go**: BSD 3-Clause License
- **Fyne** (fyne.io): BSD 3-Clause License
- **modernc.org/sqlite**: BSD 3-Clause License
- **golang.org/x/sys**: BSD 3-Clause License

## Launcher

Made by **katachinosekai**.`

func (u *ui) showAbout() {
	text := widget.NewRichTextFromMarkdown(creditsText + "\n\nVersion: " + version)
	text.Wrapping = fyne.TextWrapWord
	links := container.NewHBox()
	for _, l := range []struct{ name, link string }{
		{"Mod collection", "https://steamcommunity.com/sharedfiles/filedetails/?id=3825994973"},
		{"ZombieBuddy", "https://github.com/zed-0xff/ZombieBuddy"},
		{"The Indie Stone", "https://projectzomboid.com"},
	} {
		if parsed, err := url.Parse(l.link); err == nil {
			links.Add(widget.NewHyperlink(l.name, parsed))
		}
	}
	body := container.NewBorder(nil, links, nil, nil, container.NewVScroll(text))
	d := dialog.NewCustom("About & credits", "Close", body, u.win)
	d.Resize(fyne.NewSize(560, 480))
	d.Show()
}
