package main

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/kata/pzlauncher/internal/update"
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
	latest := widget.NewLabel("Latest pack: not checked")
	var rel *update.Release

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
		names: []string{"Check latest release", "Download pack", "Verify and extract", "Install", "Save state"}}

	dryRun := widget.NewCheck("Dry run (show what would change)", nil)
	var check, install *widget.Button

	busy := func(b bool) {
		if b {
			check.Disable()
			install.Disable()
			return
		}
		check.Enable()
		if rel != nil && rel.Pack != nil {
			install.Enable()
		}
	}

	setLatest := func(r *update.Release) {
		rel = r
		if r == nil {
			latest.SetText("Latest pack: unavailable")
			install.SetText("Install")
			return
		}
		v := r.Manifest.PackVersion
		if v == "" {
			v = r.Tag
		}
		latest.SetText("Latest pack: " + v + " (release " + r.Tag + ")")
		st, _ := update.LoadState()
		switch {
		case st.PackVersion == "":
			install.SetText("Install")
		case update.Newer(v, st.PackVersion):
			install.SetText("Update to " + v)
		default:
			install.SetText("Reinstall")
		}
	}

	check = widget.NewButton("Check", func() {
		busy(true)
		pr.set(0, 0, "")
		go func() {
			r, err := u.client.Latest(u.ctx)
			fyne.Do(func() {
				if err != nil {
					appendLog("check failed: " + err.Error())
					pr.label.SetText("Check failed")
					setLatest(nil)
				} else {
					appendLog("latest release " + r.Tag)
					pr.set(0, 1, "done")
					setLatest(r)
				}
				busy(false)
			})
		}()
	})

	install = widget.NewButton("Install", func() {
		if rel == nil {
			return
		}
		r := rel
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
			fyne.Do(func() { pr.set(1, 0, r.Pack.Name) })
			logf("downloading " + r.Pack.Name)
			var lastPct int64 = -1
			dir, err := u.client.FetchPack(u.ctx, r, func(done, total int64) {
				if total <= 0 {
					return
				}
				pct := done * 100 / total
				if pct == lastPct {
					return
				}
				lastPct = pct
				fyne.Do(func() {
					if pct >= 100 {
						pr.set(2, 0.5, "")
					} else {
						pr.set(1, float64(pct)/100, fmt.Sprintf("%d%%", pct))
					}
				})
			})
			if err != nil {
				fail(1, err)
				return
			}
			logf("checksum ok, extracted to " + dir)
			fyne.Do(func() { pr.set(3, 0, "") })
			summary, err := runInstall(u.ctx, dir, dry, logf)
			if err != nil {
				fail(3, err)
				return
			}
			if summary != "" {
				logf(summary)
			}
			if dry {
				logf("dry run finished, nothing recorded")
				fyne.Do(func() { pr.set(4, 1, "dry run") })
				return
			}
			fyne.Do(func() { pr.set(4, 0, "") })
			v := r.Manifest.PackVersion
			if v == "" {
				v = r.Tag
			}
			if err := update.MarkPackInstalled(v); err != nil {
				fail(4, err)
				return
			}
			logf("installed pack " + v)
			fyne.Do(func() {
				pr.set(4, 1, "done")
				showInstalled()
				setLatest(r)
			})
		}()
	})
	install.Importance = widget.HighImportance
	install.Disable()

	top := container.NewVBox(
		installed,
		latest,
		container.NewHBox(check, install, dryRun),
		pr.label,
		pr.bar,
		widget.NewLabelWithStyle("Log", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)
	return container.NewBorder(top, nil, nil, nil, logScroll)
}
