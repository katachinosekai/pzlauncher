package main

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/kata/pzlauncher/internal/install"
	"github.com/kata/pzlauncher/internal/workshop"
)

const prefCollection = "workshop.collection"

func (u *ui) toolsTab() fyne.CanvasObject {
	inner := container.NewAppTabs(
		container.NewTabItem("Workshop", u.workshopTab()),
		container.NewTabItem("Java mods", u.javaModsTab()),
		container.NewTabItem("Logs", u.logsTab()),
	)
	return inner
}

func (u *ui) openURL(url string) {
	if err := install.DefaultSystem().OpenURL(url); err != nil {
		dialog.ShowError(err, u.win)
	}
}

func (u *ui) workshopTab() fyne.CanvasObject {
	prefs := u.app.Preferences()
	colEntry := widget.NewEntry()
	colEntry.SetText(prefs.StringWithFallback(prefCollection, workshop.DefaultCollectionID))
	summary := widget.NewLabel("Press Refresh to compare the pack's Workshop collection with your downloaded mods.")
	summary.Wrapping = fyne.TextWrapWord
	hint := widget.NewLabel("Steam often doesn't download new subscriptions while the game is running. If downloads stall, close the game and restart Steam.")
	hint.Wrapping = fyne.TextWrapWord
	hint.Importance = widget.LowImportance

	var items []workshop.Item
	list := widget.NewList(
		func() int { return len(items) },
		func() fyne.CanvasObject {
			title := widget.NewLabel("title")
			title.Truncation = fyne.TextTruncateEllipsis
			state := widget.NewLabel("state")
			state.Importance = widget.LowImportance
			btn := widget.NewButton("Subscribe", nil)
			return container.NewBorder(nil, nil, nil, container.NewHBox(state, btn), title)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i >= len(items) {
				return
			}
			it := items[i]
			b := o.(*fyne.Container)
			title := b.Objects[0].(*widget.Label)
			right := b.Objects[1].(*fyne.Container)
			name := it.Title
			if name == "" {
				name = "(title unknown)"
			}
			title.SetText(name + "  [" + it.ID + "]")
			right.Objects[0].(*widget.Label).SetText(it.State.String())
			btn := right.Objects[1].(*widget.Button)
			id := it.ID
			btn.OnTapped = func() { u.openURL(workshop.PageURL(id)) }
		},
	)

	var refresh *widget.Button
	refresh = widget.NewButton("Refresh", func() {
		cid := strings.TrimSpace(colEntry.Text)
		if cid == "" {
			cid = workshop.DefaultCollectionID
		}
		prefs.SetString(prefCollection, cid)
		refresh.Disable()
		summary.SetText("Checking collection " + cid + "...")
		go func() {
			r, err := workshop.Check(u.ctx, workshop.Options{CollectionID: cid})
			fyne.Do(func() {
				refresh.Enable()
				if err != nil {
					summary.SetText("Check failed: " + err.Error())
					items = nil
					list.Refresh()
					return
				}
				items = r.Missing
				list.Refresh()
				msg := fmt.Sprintf("%d of %d collection items downloaded.", r.Installed, r.Total)
				if len(r.Missing) == 0 {
					msg += " Everything is there."
				} else {
					var subs, notSubs int
					for _, m := range r.Missing {
						switch m.State {
						case workshop.Downloading:
							subs++
						case workshop.NotSubscribed:
							notSubs++
						}
					}
					msg += fmt.Sprintf(" %d missing", len(r.Missing))
					if r.ACFFound {
						msg += fmt.Sprintf(" (%d subscribed but not downloaded yet, %d not subscribed)", subs, notSubs)
					}
					msg += "."
				}
				if r.TitleErr != nil {
					msg += " Titles unavailable: " + r.TitleErr.Error()
				}
				summary.SetText(msg)
			})
		}()
	})
	openCol := widget.NewButton("Open collection page", func() {
		cid := strings.TrimSpace(colEntry.Text)
		if cid == "" {
			cid = workshop.DefaultCollectionID
		}
		u.openURL(workshop.PageURL(cid))
	})
	top := container.NewVBox(
		container.NewBorder(nil, nil, widget.NewLabel("Collection id"), container.NewHBox(refresh, openCol), colEntry),
		summary,
		hint,
		widget.NewLabel("Missing items:"),
	)
	return container.NewBorder(top, nil, nil, nil, list)
}
