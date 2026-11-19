package main

import (
	"fmt"
	"os"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/kata/pzlauncher/internal/diag"
)

func beforeGameLaunch() {
	_, _ = diag.KeepLastLog()
}

func (u *ui) logsTab() fyne.CanvasObject {
	dir, _ := diag.LogsDir()
	info := widget.NewLabel(fmt.Sprintf("Before every launch the previous game log is kept in %s (last %d per log).", dir, diag.KeepCount))
	info.Wrapping = fyne.TextWrapWord
	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord

	var collect *widget.Button
	collect = widget.NewButton("Collect logs", func() {
		d := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
			if err != nil {
				dialog.ShowError(err, u.win)
				return
			}
			if w == nil {
				return
			}
			collect.Disable()
			status.SetText("Collecting logs...")
			go func() {
				diag.KeepLastLog()
				files, err := diag.Collect(w, diag.DefaultCollectOptions(version))
				if cerr := w.Close(); err == nil {
					err = cerr
				}
				fyne.Do(func() {
					collect.Enable()
					if err != nil {
						status.SetText("Collecting failed: " + err.Error())
						dialog.ShowError(err, u.win)
						return
					}
					status.SetText(fmt.Sprintf("Saved %d files to %s. Steam IDs and passwords were removed.", len(files), w.URI().Path()))
				})
			}()
		}, u.win)
		d.SetFileName("pz-logs-" + time.Now().Format("20060102-150405") + ".zip")
		d.Show()
	})
	open := widget.NewButton("Open logs folder", func() {
		if dir == "" {
			return
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			dialog.ShowError(err, u.win)
			return
		}
		u.openURL(dir)
	})
	what := widget.NewLabel("The zip contains: kept and current game logs, installer logs, mods/default.txt, the mod list with versions, ProjectZomboid64.json and a short system summary (OS, RAM, launcher version, game folder).")
	what.Wrapping = fyne.TextWrapWord
	what.Importance = widget.LowImportance
	return container.NewVBox(info, container.NewHBox(collect, open), what, status)
}
