package main

import (
	"errors"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
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

func (u *ui) playTab() fyne.CanvasObject {
	var (
		saves   []play.Save
		servers []serverRow
		target  *play.Target
	)
	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord

	saveList := widget.NewList(
		func() int { return len(saves) + 1 },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			l := o.(*widget.Label)
			if id == 0 {
				l.SetText("Continue latest save")
				return
			}
			s := saves[id-1]
			l.SetText(fmt.Sprintf("%s    %s", s.Label(), s.Modified.Format("2006-01-02 15:04")))
		},
	)
	serverList := widget.NewList(
		func() int { return len(servers) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, o fyne.CanvasObject) { o.(*widget.Label).SetText(servers[id].label()) },
	)

	launch := widget.NewButton("Launch", nil)
	launch.Importance = widget.HighImportance
	launch.Disable()

	saveList.OnSelected = func(id widget.ListItemID) {
		serverList.UnselectAll()
		t := play.Latest()
		desc := "Continue latest save"
		if id > 0 {
			t = play.SaveTarget(saves[id-1])
			desc = saves[id-1].Label()
		}
		target = &t
		status.SetText("Selected: " + desc)
		launch.Enable()
	}
	serverList.OnSelected = func(id widget.ListItemID) {
		saveList.UnselectAll()
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
		servers = servers[:0]
		for _, s := range list {
			if len(s.Accounts) == 0 {
				servers = append(servers, serverRow{server: s})
			}
			for _, a := range s.Accounts {
				servers = append(servers, serverRow{server: s, account: a, hasAcct: true})
			}
		}
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

	refreshBtn := widget.NewButton("Refresh", refresh)
	refresh()

	lists := container.NewGridWithRows(2,
		container.NewBorder(widget.NewLabelWithStyle("Single player", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), nil, nil, nil, saveList),
		container.NewBorder(widget.NewLabelWithStyle("Servers", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), nil, nil, nil, serverList),
	)
	bottom := container.NewVBox(status, container.NewHBox(launch, refreshBtn))
	return container.NewBorder(nil, bottom, nil, nil, lists)
}
