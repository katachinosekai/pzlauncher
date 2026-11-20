package main

import (
	"fmt"
	"os"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/katachinosekai/pzlauncher/internal/diag"
)

func beforeGameLaunch() {
	_, _ = diag.KeepLastLog()
}

func (u *ui) collectLogs(btn *widget.Button) {
	d := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
		if err != nil {
			dialog.ShowError(err, u.win)
			return
		}
		if w == nil {
			return
		}
		btn.Disable()
		btn.SetText("Collecting...")
		go func() {
			diag.KeepLastLog()
			files, err := diag.Collect(w, diag.DefaultCollectOptions(version))
			if cerr := w.Close(); err == nil {
				err = cerr
			}
			fyne.Do(func() {
				btn.Enable()
				btn.SetText("Collect logs")
				if err != nil {
					dialog.ShowError(fmt.Errorf("collecting logs failed: %w", err), u.win)
					return
				}
				dialog.ShowInformation("Logs collected", fmt.Sprintf("Saved %d files to\n%s\n\nSteam IDs and passwords were removed.", len(files), w.URI().Path()), u.win)
			})
		}()
	}, u.win)
	d.SetFileName("pz-logs-" + time.Now().Format("20060102-150405") + ".zip")
	d.Show()
}

func (u *ui) openLogsFolder() {
	dir, err := diag.LogsDir()
	if err != nil {
		dialog.ShowError(err, u.win)
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		dialog.ShowError(err, u.win)
		return
	}
	u.openURL(dir)
}
