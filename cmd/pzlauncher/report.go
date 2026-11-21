package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"github.com/katachinosekai/pzlauncher/internal/diag"
	"github.com/katachinosekai/pzlauncher/internal/play"
	"github.com/katachinosekai/pzlauncher/internal/report"
)

const prefReportDir = "report.saveDir"

func (u *ui) watchCrashes() {
	go func() {
		was := false
		var started time.Time
		for {
			select {
			case <-u.ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			running, err := play.IsRunning()
			if err != nil || running == was {
				continue
			}
			was = running
			if running {
				started = time.Now().Add(-10 * time.Second)
				continue
			}
			if len(diag.CrashDumpsSince(diag.DefaultCollectOptions(version), started)) > 0 {
				fyne.Do(func() { u.showReport(true) })
			}
		}
	}()
}

func (u *ui) showReport(crash bool) {
	rc := report.NewClient()
	doing := widget.NewMultiLineEntry()
	doing.SetPlaceHolder("e.g. loading my save, joining the server, installing the pack")
	doing.Wrapping = fyne.TextWrapWord
	doing.SetMinRowsVisible(3)
	happened := widget.NewMultiLineEntry()
	happened.SetPlaceHolder("e.g. the game closed, froze on the loading screen, an error popped up")
	happened.Wrapping = fyne.TextWrapWord
	happened.SetMinRowsVisible(4)

	sendLogs := widget.NewCheck("Send my logs with the report", nil)
	logsNote := widget.NewLabel("Logs: game console and debug logs, Java crash dumps, launcher and installer logs, mod list, system summary, ProjectZomboid64.json. Steam IDs, passwords, keys and your user name are removed first.")
	logsNote.Wrapping = fyne.TextWrapWord

	saveDir := u.app.Preferences().String(prefReportDir)
	saveCopy := widget.NewCheck("Also save a copy of the logs for me", nil)
	dirLabel := widget.NewLabel("")
	dirLabel.Truncation = fyne.TextTruncateEllipsis
	showDir := func() {
		if saveDir == "" {
			dirLabel.SetText("Folder: (choose one)")
		} else {
			dirLabel.SetText("Folder: " + saveDir)
		}
	}
	showDir()
	chooseDir := widget.NewButton("Choose folder...", func() {
		d := dialog.NewFolderOpen(func(l fyne.ListableURI, err error) {
			if err != nil || l == nil {
				return
			}
			saveDir = l.Path()
			u.app.Preferences().SetString(prefReportDir, saveDir)
			saveCopy.SetChecked(true)
			showDir()
		}, u.win)
		if saveDir != "" {
			if l, err := storage.ListerForURI(storage.NewFileURI(saveDir)); err == nil {
				d.SetLocation(l)
			}
		}
		d.Show()
	})

	intro := "Tell us what went wrong. The report goes to the launcher's private issue tracker."
	if !rc.Configured() {
		intro = "Sending reports isn't set up in this launcher build yet. You can still save the logs and your description to a zip and share it yourself."
		sendLogs.Hide()
		logsNote.SetText("The zip has the game console and debug logs, Java crash dumps, launcher and installer logs, mod list, system summary and ProjectZomboid64.json. Steam IDs, passwords, keys and your user name are removed first.")
		saveCopy.SetText("Save the report and logs to a zip")
		saveCopy.SetChecked(true)
	}
	if crash {
		intro = "The game seems to have crashed (Java wrote a crash dump). " + intro
	}
	introLabel := widget.NewLabel(intro)
	introLabel.Wrapping = fyne.TextWrapWord

	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord
	var d dialog.Dialog
	action := "Send report"
	if !rc.Configured() {
		action = "Save"
	}
	var actionBtn *widget.Button
	actionBtn = widget.NewButton(action, func() {
		r := report.Report{Doing: doing.Text, Happened: happened.Text, LauncherVersion: version, OS: runtime.GOOS + "/" + runtime.GOARCH, Crash: crash}
		if err := r.Validate(); err != nil {
			status.SetText(err.Error())
			return
		}
		send := rc.Configured()
		wantLogs := (send && sendLogs.Checked) || saveCopy.Checked
		if saveCopy.Checked && saveDir == "" {
			status.SetText("Choose a folder for your copy first.")
			return
		}
		if !send && !saveCopy.Checked {
			status.SetText("Nothing to do: tick the box to save the zip.")
			return
		}
		actionBtn.Disable()
		status.SetText("Collecting logs...")
		go func() {
			var zipped []byte
			var saved string
			var err error
			if wantLogs {
				diag.KeepLastLog()
				o := diag.DefaultCollectOptions(version)
				o.Extra = map[string]string{"report.txt": r.Text()}
				var buf bytes.Buffer
				if _, err = diag.Collect(&buf, o); err == nil {
					zipped = buf.Bytes()
				}
			}
			if err == nil && saveCopy.Checked {
				saved = filepath.Join(saveDir, "pz-report-"+time.Now().Format("20060102-150405")+".zip")
				err = os.WriteFile(saved, zipped, 0o600)
			}
			if err == nil && send {
				fyne.Do(func() { status.SetText("Sending...") })
				if sendLogs.Checked {
					r.Logs = zipped
				}
				err = rc.Send(u.ctx, r)
			}
			fyne.Do(func() {
				actionBtn.Enable()
				if err != nil {
					status.SetText("")
					if errors.Is(err, report.ErrLogsTooBig) {
						err = fmt.Errorf("%w (tick \"Also save a copy\" to keep them)", err)
					}
					dialog.ShowError(err, u.win)
					return
				}
				d.Hide()
				msg := "Thanks, your report was sent."
				if !send {
					msg = "Saved."
				}
				if saved != "" {
					msg += "\n\nCopy saved to\n" + saved
				}
				dialog.ShowInformation("Report a problem", msg, u.win)
			})
		}()
	})
	actionBtn.Importance = widget.HighImportance
	cancel := widget.NewButton("Cancel", func() { d.Hide() })

	form := container.NewVBox(
		introLabel,
		widget.NewLabel("What were you doing?"), doing,
		widget.NewLabel("What happened?"), happened,
		sendLogs, logsNote,
		saveCopy, container.NewBorder(nil, nil, nil, chooseDir, dirLabel),
		status,
	)
	scroll := container.NewVScroll(form)
	scroll.SetMinSize(fyne.NewSize(640, 460))
	content := container.NewBorder(nil, container.NewHBox(layout.NewSpacer(), cancel, actionBtn), nil, nil, scroll)
	d = dialog.NewCustomWithoutButtons("Report a problem", content, u.win)
	d.Show()
	u.win.Canvas().Focus(doing)
}
