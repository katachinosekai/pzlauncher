package main

import (
	"errors"
	"fmt"
	"image/color"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/kata/pzlauncher/internal/play"
)

type serverRow struct {
	server  play.Server
	account play.Account
	hasAcct bool
}

func (r serverRow) label() string {
	s := fmt.Sprintf("%s (%s)", r.server.Name, r.server.Addr())
	if !r.hasAcct {
		return s + " - no saved account"
	}
	s += " as " + r.account.Username
	if !r.account.PasswordSaved {
		s += " - password not saved"
	}
	return s
}

func statusColor(st play.ServerStatus, known bool) color.Color {
	switch {
	case !known:
		return theme.Color(theme.ColorNameDisabled)
	case !st.Online:
		return theme.Color(theme.ColorNameError)
	case st.HasPlayers && st.MaxPlayers > 0 && st.Players >= st.MaxPlayers:
		return theme.Color(theme.ColorNameWarning)
	default:
		return theme.Color(theme.ColorNameSuccess)
	}
}

func rowParts(o fyne.CanvasObject) (name, info *widget.Label, img *canvas.Image) {
	for _, c := range o.(*fyne.Container).Objects {
		switch v := c.(type) {
		case *canvas.Image:
			img = v
		case *widget.Label:
			if name == nil {
				name = v
			} else {
				info = v
			}
		case *fyne.Container:
			for _, inner := range v.Objects {
				if l, ok := inner.(*widget.Label); ok {
					if name == nil {
						name = l
					} else {
						info = l
					}
					break
				}
			}
		}
	}
	return name, info, img
}

func (u *ui) playTab() fyne.CanvasObject {
	var (
		saves   []play.Save
		servers []serverRow
		target  *play.Target
		picked  = -1
		statMu  sync.Mutex
		stats   = map[string]play.ServerStatus{}
	)
	nickPath, _ := play.NicknamesPath()
	nicks, _ := play.LoadNicknames(nickPath)
	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord

	saveList := widget.NewList(
		func() int { return len(saves) + 1 },
		func() fyne.CanvasObject {
			img := canvas.NewImageFromResource(nil)
			img.FillMode = canvas.ImageFillContain
			img.SetMinSize(fyne.NewSize(64, 64))
			name := widget.NewLabel("")
			name.Truncation = fyne.TextTruncateEllipsis
			info := widget.NewLabel("")
			mid := container.NewVBox(layout.NewSpacer(), name, layout.NewSpacer())
			return container.NewBorder(nil, nil, img, container.NewCenter(info), mid)
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			name, info, img := rowParts(o)
			thumb := ""
			if id == 0 {
				name.SetText("Continue latest save")
				info.SetText("")
				if len(saves) > 0 {
					thumb = saves[0].Thumb()
					info.SetText(saves[0].Modified.Format("2006-01-02 15:04"))
				}
			} else {
				s := saves[id-1]
				name.SetText(nicks.Display(s))
				info.SetText(s.Modified.Format("2006-01-02 15:04"))
				thumb = s.Thumb()
			}
			img.File = thumb
			img.Resource = nil
			if thumb == "" {
				img.Resource = theme.FileImageIcon()
			}
			img.Refresh()
		},
	)
	serverList := widget.NewList(
		func() int { return len(servers) },
		func() fyne.CanvasObject {
			name := widget.NewLabel("")
			name.Truncation = fyne.TextTruncateEllipsis
			dot := canvas.NewCircle(theme.Color(theme.ColorNameDisabled))
			dotBox := container.NewGridWrap(fyne.NewSize(12, 12), dot)
			right := container.NewHBox(container.NewCenter(dotBox), widget.NewLabel(""))
			return container.NewBorder(nil, nil, nil, right, name)
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			r := servers[id]
			statMu.Lock()
			st, ok := stats[r.server.Addr()]
			statMu.Unlock()
			text := "checking..."
			if ok {
				text = st.String()
			}
			c := o.(*fyne.Container)
			right := c.Objects[1].(*fyne.Container)
			c.Objects[0].(*widget.Label).SetText(r.label())
			right.Objects[1].(*widget.Label).SetText(text)
			dot := right.Objects[0].(*fyne.Container).Objects[0].(*fyne.Container).Objects[0].(*canvas.Circle)
			dot.FillColor = statusColor(st, ok)
			dot.Refresh()
		},
	)

	launch := widget.NewButton("Launch", nil)
	launch.Importance = widget.HighImportance
	launch.Disable()
	nickBtn := widget.NewButton("Nickname...", nil)
	nickBtn.Disable()

	saveList.OnSelected = func(id widget.ListItemID) {
		serverList.UnselectAll()
		t := play.Latest()
		desc := "Continue latest save"
		picked = -1
		nickBtn.Disable()
		if id > 0 {
			t = play.SaveTarget(saves[id-1])
			desc = nicks.Display(saves[id-1])
			picked = id - 1
			nickBtn.Enable()
		}
		target = &t
		status.SetText("Selected: " + desc)
		launch.Enable()
	}
	serverList.OnSelected = func(id widget.ListItemID) {
		saveList.UnselectAll()
		picked = -1
		nickBtn.Disable()
		r := servers[id]
		account := ""
		if r.hasAcct {
			account = r.account.Username
		}
		t := play.ServerTarget(r.server, account)
		target = &t
		msg := "Selected: " + r.label()
		if !r.hasAcct || !r.account.PasswordSaved {
			msg += "\nJoin this server once from the in-game server list with \"save password\" ticked, or auto-join will stop at the menu."
		}
		status.SetText(msg)
		launch.Enable()
	}

	refresh := func() {
		var err error
		saves, err = play.ListSaves()
		if err != nil {
			status.SetText("Could not read saves: " + err.Error())
		}
		list, serr := play.ListServers()
		var rows []serverRow
		for _, s := range list {
			if len(s.Accounts) == 0 {
				rows = append(rows, serverRow{server: s})
			}
			for _, a := range s.Accounts {
				rows = append(rows, serverRow{server: s, account: a, hasAcct: true})
			}
		}
		statMu.Lock()
		servers = rows
		statMu.Unlock()
		if serr != nil {
			status.SetText("Could not read the server list: " + serr.Error())
		}
		target = nil
		launch.Disable()
		saveList.UnselectAll()
		serverList.UnselectAll()
		saveList.Refresh()
		serverList.Refresh()
		if err == nil && serr == nil {
			status.SetText(fmt.Sprintf("%d saves, %d saved server accounts. Pick one and press Launch.", len(saves), len(servers)))
		}
	}

	launch.OnTapped = func() {
		if target == nil {
			return
		}
		t := *target
		launch.Disable()
		status.SetText("Starting Project Zomboid through Steam...")
		go func() {
			err := play.Launch(t)
			fyne.Do(func() {
				launch.Enable()
				switch {
				case errors.Is(err, play.ErrRunning):
					status.SetText("Project Zomboid is already running. Close it first.")
				case err != nil:
					status.SetText("Launch failed: " + err.Error())
				default:
					status.SetText("Steam is starting the game. It loads straight into your pick once the main menu is up (needs the AutoLoadSave mod).")
				}
			})
		}()
	}

	nickBtn.OnTapped = func() {
		if picked < 0 || picked >= len(saves) {
			return
		}
		s := saves[picked]
		entry := widget.NewEntry()
		entry.SetText(nicks.Get(s))
		entry.SetPlaceHolder("leave empty to remove the nickname")
		dialog.ShowForm("Nickname for "+s.Label(), "Save", "Cancel",
			[]*widget.FormItem{widget.NewFormItem("Nickname", entry)},
			func(ok bool) {
				if !ok {
					return
				}
				if err := nicks.Set(s, entry.Text); err != nil {
					status.SetText("Could not save the nickname: " + err.Error())
					return
				}
				saveList.Refresh()
				status.SetText("Selected: " + nicks.Display(s))
			}, u.win)
	}

	pollStatus := func() {
		statMu.Lock()
		addrs := map[string]play.Server{}
		for _, r := range servers {
			addrs[r.server.Addr()] = r.server
		}
		statMu.Unlock()
		for addr, srv := range addrs {
			st := play.QueryServer(srv.IP, srv.Port, 2*time.Second)
			statMu.Lock()
			stats[addr] = st
			statMu.Unlock()
		}
		fyne.Do(serverList.Refresh)
	}
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-u.ctx.Done():
				return
			case <-t.C:
				pollStatus()
			}
		}
	}()

	refreshBtn := widget.NewButton("Refresh", func() {
		refresh()
		go pollStatus()
	})
	refresh()
	go pollStatus()

	lists := container.NewGridWithRows(2,
		container.NewBorder(widget.NewLabelWithStyle("Single player", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), nil, nil, nil, saveList),
		container.NewBorder(widget.NewLabelWithStyle("Servers", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), nil, nil, nil, serverList),
	)
	bottom := container.NewVBox(status, container.NewHBox(launch, nickBtn, refreshBtn))
	return container.NewBorder(nil, bottom, nil, nil, lists)
}
