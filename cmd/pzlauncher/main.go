package main

import (
	"context"
	"flag"
	"fmt"
	"image/color"
	"os"
	"runtime"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/katachinosekai/pzlauncher/internal/gamecfg"
	"github.com/katachinosekai/pzlauncher/internal/install"
	"github.com/katachinosekai/pzlauncher/internal/update"
)

var version = "dev"

type ui struct {
	app         fyne.App
	win         fyne.Window
	ctx         context.Context
	client      *update.Client
	closers     []func()
	tabHooks    map[string]func()
	winSaved    winState
	signButtons *fyne.Container
	signCheck   func()
}

func (u *ui) onTab(title string, fn func()) {
	if u.tabHooks == nil {
		u.tabHooks = map[string]func(){}
	}
	u.tabHooks[title] = fn
}

func main() {
	showVersion := flag.Bool("version", false, "print the launcher version")
	cli := registerCLI()
	flag.Parse()
	a := app.NewWithID("io.github.kata.pzlauncher")
	if m := a.Metadata(); version == "dev" && m.ID != "" && m.Version != "" {
		version = m.Version
	}
	if version == "dev" {
		gamecfg.DefaultXmxGB = 6
	}
	if *showVersion {
		fmt.Println(version)
		return
	}
	if install.Elevated() && runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, "PZ Launcher must not run as root (sudo): files it creates in ~/Zomboid would belong to root and break the game. Start it as your normal user.")
		os.Exit(1)
	}
	if *cli.installPack != "" {
		os.Exit(runCLI(cli))
	}
	update.CleanupOld()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a.Settings().SetTheme(newAppTheme())
	w := a.NewWindow("PZ Launcher")
	u := &ui{app: a, win: w, ctx: ctx, client: update.NewClient()}
	confirmApprovals = u.askApprovals

	u.loadMusic()
	tabs := u.buildTabs()
	a.Lifecycle().SetOnStarted(func() {
		if u.signCheck != nil {
			go func() {
				time.Sleep(3 * time.Second)
				fyne.Do(u.signCheck)
			}()
		}
		if install.Elevated() {
			dialog.ShowInformation("Running as administrator", "PZ Launcher is running as administrator. Installing, changing launch options and restarting Steam are blocked: Steam and the game would start as administrator too. Close the launcher and start it normally.", w)
		}
		u.nativeRestore()
		u.askMusicOnce()
	})
	w.SetContent(container.NewBorder(nil, u.aboutBar(), nil, nil, u.withSignButton(tabs)))
	u.restoreWindow(fyne.NewSize(900, 820))
	u.watchWindow()
	w.SetCloseIntercept(func() {
		u.rememberWindow()
		w.Close()
	})
	w.SetOnClosed(func() {
		for _, f := range u.closers {
			f()
		}
		cancel()
	})
	w.ShowAndRun()
}

func (u *ui) aboutBar() fyne.CanvasObject {
	label := widget.NewLabel("PZ Launcher")
	var btn *widget.Button
	var rel *update.Release
	var check func(quiet bool)
	btn = widget.NewButton("Check for updates", func() {
		if rel != nil {
			r := rel
			dialog.ShowConfirm("Launcher update", fmt.Sprintf("Update the launcher from %s to %s?", version, r.Manifest.LauncherVersion), func(ok bool) {
				if ok {
					u.selfUpdate(r, label)
				}
			}, u.win)
			return
		}
		check(false)
	})
	check = func(quiet bool) {
		btn.Disable()
		if !quiet {
			label.SetText("PZ Launcher - checking for updates...")
		}
		go func() {
			defer fyne.Do(btn.Enable)
			r, err := u.client.Latest(u.ctx)
			if err != nil {
				if !quiet {
					fyne.Do(func() {
						label.SetText("PZ Launcher")
						dialog.ShowError(err, u.win)
					})
				}
				return
			}
			latest := r.Manifest.LauncherVersion
			if !update.LauncherOutdated(latest, version) || update.CurrentBinaryName() == "" || r.Asset(update.CurrentBinaryName()) == nil {
				if !quiet {
					fyne.Do(func() {
						msg := "PZ Launcher (up to date)"
						if version == "dev" {
							msg = "PZ Launcher (latest release " + latest + ")"
						}
						label.SetText(msg)
					})
				}
				return
			}
			fyne.Do(func() {
				rel = r
				label.SetText("PZ Launcher - " + latest + " available")
				btn.SetText("Update to " + latest)
				btn.Importance = widget.HighImportance
				btn.Refresh()
				if !quiet {
					btn.OnTapped()
				}
			})
		}()
	}
	if version != "dev" {
		go func() {
			time.Sleep(3 * time.Second)
			fyne.Do(func() { check(true) })
		}()
	}
	note := widget.NewLabel(disclaimer)
	note.Truncation = fyne.TextTruncateEllipsis
	about := widget.NewButton("About & credits", u.showAbout)
	var collect *widget.Button
	collect = widget.NewButton("Collect logs", func() { u.collectLogs(collect) })
	openLogs := widget.NewButton("Open logs folder", u.openLogsFolder)
	return container.NewBorder(nil, nil, label, container.NewHBox(collect, openLogs, about, btn), note)
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
				label.SetText("PZ Launcher")
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
				u.rememberWindow()
				u.app.Quit()
				os.Exit(0)
			}, u.win)
		})
	}()
}

func (u *ui) withSignButton(tabs *container.AppTabs) fyne.CanvasObject {
	if u.signButtons == nil {
		return tabs
	}
	const margin = 4
	barH := container.NewAppTabs(container.NewTabItem("X", widget.NewLabel(""))).MinSize().Height - widget.NewLabel("").MinSize().Height
	wIn := widget.NewButtonWithIcon("Sign Into Steam", theme.LoginIcon(), nil).MinSize()
	wOut := widget.NewButtonWithIcon("Sign Out of Steam", theme.LogoutIcon(), nil).MinSize()
	h := max(barH-2*margin-theme.Padding(), 24)
	size := fyne.NewSize(max(wIn.Width, wOut.Width), min(max(wIn.Height, wOut.Height), h))
	fixed := container.NewGridWrap(size, u.signButtons.Objects...)
	gap := canvas.NewRectangle(color.Transparent)
	gap.SetMinSize(fyne.NewSize(16, 1))
	right := container.NewHBox(u.miniPlayer(), gap, fixed)
	top := max((barH-size.Height-2*theme.Padding())/2, 0)
	row := container.New(layout.NewCustomPaddedLayout(top, 0, 0, 0), container.NewBorder(nil, nil, nil, right))
	return container.NewStack(tabs, container.NewVBox(row))
}
