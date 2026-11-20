package main

import (
	"errors"
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/katachinosekai/pzlauncher/internal/javamods"
	"github.com/katachinosekai/pzlauncher/internal/play"
)

func (u *ui) javaModsTab() fyne.CanvasObject {
	noteText := widget.NewLabel("Java mods run code outside the game's Lua sandbox and aren't officially supported by The Indie Stone. Only approve mods you trust.")
	noteText.Wrapping = fyne.TextWrapWord
	noteText.Importance = widget.WarningImportance
	note := warningCard(noteText)
	summary := widget.NewLabel("Press Refresh to list installed Java mods.")
	summary.Wrapping = fyne.TextWrapWord

	var mods []*javamods.JavaMod
	var entries []javamods.Entry
	var store *javamods.Store
	var reload func()

	list := widget.NewList(
		func() int { return len(mods) },
		func() fyne.CanvasObject {
			title := widget.NewLabel("title")
			title.TextStyle = fyne.TextStyle{Bold: true}
			title.Truncation = fyne.TextTruncateEllipsis
			detail := widget.NewLabel("detail")
			detail.Truncation = fyne.TextTruncateEllipsis
			detail.Importance = widget.LowImportance
			status := widget.NewLabel("status")
			status.Truncation = fyne.TextTruncateEllipsis
			approve := widget.NewButton("Approve this jar", nil)
			deny := widget.NewButton("Deny", nil)
			settings := widget.NewButtonWithIcon("Mod settings", theme.SettingsIcon(), nil)
			return container.New(layout.NewCustomPaddedVBoxLayout(-theme.Padding()*2), title, detail, container.NewBorder(nil, nil, nil, container.NewHBox(settings, approve, deny), status))
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i >= len(mods) {
				return
			}
			m := mods[i]
			box := o.(*fyne.Container)
			title := box.Objects[0].(*widget.Label)
			detail := box.Objects[1].(*widget.Label)
			row := box.Objects[2].(*fyne.Container)
			status := row.Objects[0].(*widget.Label)
			btns := row.Objects[1].(*fyne.Container)
			settings := btns.Objects[0].(*widget.Button)
			approve := btns.Objects[1].(*widget.Button)
			deny := btns.Objects[2].(*widget.Button)
			modID, modName := m.ID, m.Name
			settings.OnTapped = func() { u.modSettingsDialog(modID, modName) }

			ws := m.WorkshopID
			if ws == "" {
				ws = "-"
			}
			title.SetText(fmt.Sprintf("%s  (id %s, Workshop %s, %s)", m.Name, m.ID, ws, m.Source))
			zbs := "no .zbs signature"
			if m.HasZBS {
				zbs = ".zbs signature present"
			}
			detail.SetText(fmt.Sprintf("sha256 %s  |  %s  |  %s", m.ShortHash(), zbs, m.Jar))
			status.SetText("Status: " + m.Status.String())
			switch m.Status {
			case javamods.Approved:
				status.Importance = widget.SuccessImportance
			case javamods.Denied, javamods.NoJar:
				status.Importance = widget.DangerImportance
			case javamods.ApprovedOlder, javamods.DeniedOlder:
				status.Importance = widget.WarningImportance
			default:
				status.Importance = widget.MediumImportance
			}
			status.Refresh()
			canAct := m.JarHash != "" && store != nil
			if canAct && m.Status != javamods.Approved {
				approve.Enable()
			} else {
				approve.Disable()
			}
			if canAct && m.Status != javamods.Denied {
				deny.Enable()
			} else {
				deny.Disable()
			}
			set := func(decision bool) {
				if store == nil {
					return
				}
				_, err := store.Set(m, decision)
				if err != nil {
					if errors.Is(err, javamods.ErrGameRunning) {
						dialog.ShowInformation("Game is running", err.Error(), u.win)
					} else {
						dialog.ShowError(err, u.win)
					}
					return
				}
				reload()
			}
			approve.OnTapped = func() {
				dialog.ShowConfirm("Approve Java mod", fmt.Sprintf("Approve %s (jar %s)? It will run code outside the Lua sandbox.", m.Name, m.ShortHash()), func(ok bool) {
					if ok {
						set(true)
					}
				}, u.win)
			}
			deny.OnTapped = func() { set(false) }
		},
	)

	var refresh *widget.Button
	scanning := false
	reload = func() {
		if scanning {
			return
		}
		scanning = true
		refresh.Disable()
		summary.SetText("Scanning mods...")
		go func() {
			st, err := javamods.DefaultStore()
			var es []javamods.Entry
			if err == nil {
				es, err = st.Load()
			}
			var ms []*javamods.JavaMod
			if idx, serr := play.ScanInstalled(); serr == nil {
				ms = javamods.List(idx.Index, es)
			}
			fyne.Do(func() {
				scanning = false
				refresh.Enable()
				mods, entries = ms, es
				store = st
				if err != nil {
					store = nil
					summary.SetText("Could not read ZombieBuddy approvals: " + err.Error())
					list.Refresh()
					return
				}
				counts := map[javamods.Status]int{}
				for _, m := range mods {
					counts[m.Status]++
				}
				summary.SetText(fmt.Sprintf("%d Java mods: %d approved, %d need re-approval, %d denied, %d never seen. Approvals file: %s (%d entries)",
					len(mods), counts[javamods.Approved], counts[javamods.ApprovedOlder], counts[javamods.Denied]+counts[javamods.DeniedOlder], counts[javamods.NeverSeen], st.Path, len(entries)))
				list.Refresh()
			})
		}()
	}
	refresh = widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), reload)
	refreshRow := container.New(layout.NewCustomPaddedLayout(6, 6, 2, 0), container.NewHBox(refresh, layout.NewSpacer()))
	top := container.New(layout.NewCustomPaddedVBoxLayout(2), note, refreshRow, summary)
	content := container.NewBorder(top, nil, nil, nil, list)
	t := time.NewTicker(javaModsRefresh)
	u.closers = append(u.closers, t.Stop)
	go func() {
		time.Sleep(300 * time.Millisecond)
		fyne.Do(reload)
		for range t.C {
			fyne.Do(func() {
				if content.Visible() {
					reload()
				}
			})
		}
	}()
	return content
}

const javaModsRefresh = 20 * time.Second

type noteTheme struct{ fyne.Theme }

func (t noteTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameText:
		return t.Theme.Size(n) - 1.5
	case theme.SizeNamePadding:
		return 2
	case theme.SizeNameInnerPadding:
		return 5
	}
	return t.Theme.Size(n)
}

func warningCard(o fyne.CanvasObject) fyne.CanvasObject {
	warn := theme.Color(theme.ColorNameWarning)
	r, g, b, _ := warn.RGBA()
	tint := func(a uint8) color.Color { return color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), a} }
	bg := canvas.NewRectangle(tint(0x1e))
	bg.CornerRadius = 8
	bg.StrokeColor = tint(0x55)
	bg.StrokeWidth = 1
	return container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(2, 2, 6, 6), container.NewThemeOverride(o, noteTheme{fyne.CurrentApp().Settings().Theme()})))
}
