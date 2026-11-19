package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/kata/pzlauncher/internal/update"
)

var version = "dev"

type ui struct {
	app    fyne.App
	win    fyne.Window
	ctx    context.Context
	client *update.Client
}

func main() {
	showVersion := flag.Bool("version", false, "print the launcher version")
	cli := registerCLI()
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if *cli.installPack != "" {
		os.Exit(runCLI(cli))
	}
	update.CleanupOld()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := app.NewWithID("io.github.kata.pzlauncher")
	w := a.NewWindow("PZ Launcher")
	u := &ui{app: a, win: w, ctx: ctx, client: update.NewClient()}

	tabs := container.NewAppTabs(
		container.NewTabItem("Play", u.playTab()),
		container.NewTabItem("Install / Update", u.installTab()),
	)
	w.SetContent(container.NewBorder(nil, u.aboutBar(), nil, nil, tabs))
	w.Resize(fyne.NewSize(760, 560))
	w.SetOnClosed(cancel)
	w.ShowAndRun()
}

func (u *ui) aboutBar() fyne.CanvasObject {
	label := widget.NewLabel("PZ Launcher " + version)
	var btn *widget.Button
	btn = widget.NewButton("Check for launcher update", func() {
		btn.Disable()
		label.SetText("PZ Launcher " + version + " - checking...")
		go func() {
			defer fyne.Do(btn.Enable)
			rel, err := u.client.Latest(u.ctx)
			if err != nil {
				fyne.Do(func() {
					label.SetText("PZ Launcher " + version)
					dialog.ShowError(err, u.win)
				})
				return
			}
			latest := rel.Manifest.LauncherVersion
			if !update.LauncherOutdated(latest, version) {
				fyne.Do(func() {
					msg := "PZ Launcher " + version + " (up to date)"
					if version == "dev" {
						msg = "PZ Launcher dev build (latest release " + latest + ")"
					}
					label.SetText(msg)
				})
				return
			}
			fyne.Do(func() {
				label.SetText("PZ Launcher " + version + " - " + latest + " available")
				dialog.ShowConfirm("Launcher update", fmt.Sprintf("Update the launcher from %s to %s?", version, latest), func(ok bool) {
					if ok {
						u.selfUpdate(rel, label)
					}
				}, u.win)
			})
		}()
	})
	return container.NewHBox(label, layout.NewSpacer(), btn)
}

func (u *ui) selfUpdate(rel *update.Release, label *widget.Label) {
	go func() {
		path, err := u.client.SelfUpdate(u.ctx, rel, func(done, total int64) {
			if total > 0 {
				fyne.Do(func() { label.SetText(fmt.Sprintf("Downloading launcher... %d%%", done*100/total)) })
			}
		})
		fyne.Do(func() {
			if err != nil {
				label.SetText("PZ Launcher " + version)
				dialog.ShowError(err, u.win)
				return
			}
			label.SetText("Launcher updated to " + rel.Manifest.LauncherVersion)
			dialog.ShowConfirm("Launcher updated", "Restart the launcher now?", func(ok bool) {
				if !ok {
					return
				}
				if err := update.Restart(path); err != nil {
					dialog.ShowError(err, u.win)
					return
				}
				u.app.Quit()
				os.Exit(0)
			}, u.win)
		})
	}()
}
