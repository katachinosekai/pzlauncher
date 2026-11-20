package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"github.com/katachinosekai/pzlauncher/internal/update"
)

const maxLogLines = 2000

type steps struct {
	bar   *widget.ProgressBar
	label *widget.Label
	names []string
}

func (s *steps) set(i int, frac float64, note string) {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	n := float64(len(s.names))
	s.bar.SetValue((float64(i) + frac) / n)
	text := fmt.Sprintf("Step %d/%d: %s", i+1, len(s.names), s.names[i])
	if note != "" {
		text += " - " + note
	}
	s.label.SetText(text)
}

func (u *ui) installTab() fyne.CanvasObject {
	installed := widget.NewLabel("")
	showInstalled := func() {
		st, err := update.LoadState()
		switch {
		case err != nil:
			installed.SetText("Installed pack: unknown (" + err.Error() + ")")
		case st.PackVersion == "":
			installed.SetText("Installed pack: none")
		default:
			installed.SetText("Installed pack: " + st.PackVersion)
		}
	}
	showInstalled()

	var lines []string
	logLabel := widget.NewLabel("")
	logLabel.Wrapping = fyne.TextWrapWord
	logLabel.TextStyle = fyne.TextStyle{Monospace: true}
	logScroll := container.NewVScroll(logLabel)
	appendLog := func(s string) {
		for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
			lines = append(lines, time.Now().Format("15:04:05 ")+l)
		}
		if len(lines) > maxLogLines {
			lines = lines[len(lines)-maxLogLines:]
		}
		logLabel.SetText(strings.Join(lines, "\n"))
		logScroll.ScrollToBottom()
	}
	logf := func(s string) { fyne.Do(func() { appendLog(s) }) }

	pr := &steps{bar: widget.NewProgressBar(), label: widget.NewLabel("Idle"),
		names: []string{"Open pack", "Install", "Save state"}}

	dryRun := widget.NewCheck("Dry run (show what would change)", nil)
	zipLabel := widget.NewLabel("No pack chosen")
	zipLabel.Truncation = fyne.TextTruncateEllipsis
	var zipPath string
	var pick, install *widget.Button

	busy := func(b bool) {
		if b {
			pick.Disable()
			install.Disable()
			return
		}
		pick.Enable()
		if zipPath != "" {
			install.Enable()
		}
	}

	pick = widget.NewButton("Choose PZ-Fixes.zip...", func() {
		d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
			if err != nil || r == nil {
				return
			}
			r.Close()
			zipPath = r.URI().Path()
			zipLabel.SetText(zipPath)
			busy(false)
		}, u.win)
		d.SetFilter(storage.NewExtensionFileFilter([]string{".zip"}))
		if home, err := os.UserHomeDir(); err == nil {
			for _, sub := range []string{"Downloads", "Desktop"} {
				if l, err := storage.ListerForURI(storage.NewFileURI(filepath.Join(home, sub))); err == nil {
					d.SetLocation(l)
					break
				}
			}
		}
		d.Show()
	})

	install = widget.NewButton("Install", func() {
		if zipPath == "" {
			return
		}
		zp := zipPath
		dry := dryRun.Checked
		busy(true)
		go func() {
			defer fyne.Do(func() { busy(false) })
			fail := func(step int, err error) {
				fyne.Do(func() {
					appendLog("error: " + err.Error())
					pr.set(step, 0, "failed")
				})
			}
			fyne.Do(func() { pr.set(0, 0, filepath.Base(zp)) })
			logf("opening " + zp)
			dir, v, err := update.OpenLocalPack(zp)
			if err != nil {
				fail(0, err)
				return
			}
			logf("extracted to " + dir)
			fyne.Do(func() { pr.set(1, 0, "") })
			summary, err := runInstall(u.ctx, dir, dry, logf)
			if err != nil {
				fail(1, err)
				return
			}
			if summary != "" {
				logf(summary)
			}
			if dry {
				logf("dry run finished, nothing recorded")
				fyne.Do(func() { pr.set(2, 1, "dry run") })
				return
			}
			fyne.Do(func() { pr.set(2, 0, "") })
			if err := update.MarkPackInstalled(v); err != nil {
				fail(2, err)
				return
			}
			logf("installed pack " + v)
			fyne.Do(func() {
				pr.set(2, 1, "done")
				showInstalled()
			})
		}()
	})
	install.Importance = widget.HighImportance
	install.Disable()

	top := container.NewVBox(
		installed,
		widget.NewLabel("Install or update a custom pack from the zip you were sent (for now: the PZ-Fixes pack). Quit the game first."),
		container.NewBorder(nil, nil, pick, nil, zipLabel),
		container.NewHBox(install, dryRun),
		pr.label,
		pr.bar,
		widget.NewLabelWithStyle("Log", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)
	return container.NewBorder(top, nil, nil, nil, logScroll)
}
