package main

import (
	"errors"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/kata/pzlauncher/internal/javamods"
)

func (u *ui) javaModsTab() fyne.CanvasObject {
	note := widget.NewLabel("Java mods run code outside the game's Lua sandbox and aren't officially supported by The Indie Stone. Only approve mods you trust.")
	note.Wrapping = fyne.TextWrapWord
	note.Importance = widget.WarningImportance
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
			return container.NewVBox(title, detail, container.NewBorder(nil, nil, nil, container.NewHBox(approve, deny), status))
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
			approve := btns.Objects[0].(*widget.Button)
			deny := btns.Objects[1].(*widget.Button)

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
	reload = func() {
		refresh.Disable()
		summary.SetText("Scanning mods...")
		go func() {
			st, err := javamods.DefaultStore()
			var es []javamods.Entry
			if err == nil {
				es, err = st.Load()
			}
			ms := javamods.List(javamods.DefaultRoots(), es)
			fyne.Do(func() {
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
	refresh = widget.NewButton("Refresh", reload)
	top := container.NewVBox(note, container.NewHBox(refresh, layout.NewSpacer()), summary)
	return container.NewBorder(top, nil, nil, nil, list)
}
