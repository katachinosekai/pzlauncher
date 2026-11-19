package main

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/kata/pzlauncher/internal/gamecfg"
	"github.com/kata/pzlauncher/internal/play"
	"github.com/kata/pzlauncher/internal/savebak"
)

type serverRow struct {
	server  play.Server
	account play.Account
	hasAcct bool
	dupName bool
}

func (r serverRow) key() string {
	if !r.hasAcct {
		return r.server.Addr()
	}
	return r.server.Addr() + "|" + r.account.Username
}

func (r serverRow) title() string {
	if r.dupName && r.hasAcct {
		return r.server.Name + " - " + r.account.Username
	}
	return r.server.Name
}

func (r serverRow) details() string {
	login := "Login: no saved account"
	if r.hasAcct {
		login = "Login: " + r.account.Username
		if r.account.PasswordSaved {
			login += " (password saved)"
		} else {
			login += " (password not saved)"
		}
	}
	srvPwd := "Server password: none saved"
	if r.server.ServerPasswordSaved {
		srvPwd = "Server password: saved"
	}
	return "Address: " + r.server.Addr() + "\n" + login + "\n" + srvPwd
}

func (r serverRow) accountPtr() *play.Account {
	if !r.hasAcct {
		return nil
	}
	a := r.account
	return &a
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

func setIssueIcon(ic *widget.Icon, r play.CheckResult, known bool) {
	switch {
	case !known:
		ic.Hide()
	case r.Blocking():
		ic.SetResource(theme.NewErrorThemedResource(theme.ErrorIcon()))
		ic.Show()
	case r.HasWarnings():
		ic.SetResource(theme.NewWarningThemedResource(theme.WarningIcon()))
		ic.Show()
	default:
		ic.Hide()
	}
}

type rowRefs struct {
	name, info *widget.Label
	img        *canvas.Image
	bg         *canvas.Rectangle
	icon       *widget.Icon
}

func findParts(o fyne.CanvasObject) rowRefs {
	var r rowRefs
	var walk func(fyne.CanvasObject)
	walk = func(c fyne.CanvasObject) {
		switch v := c.(type) {
		case *canvas.Image:
			if r.img == nil {
				r.img = v
			}
		case *canvas.Rectangle:
			if r.bg == nil {
				r.bg = v
			}
		case *widget.Icon:
			if r.icon == nil {
				r.icon = v
			}
		case *widget.Label:
			if r.name == nil {
				r.name = v
			} else if r.info == nil {
				r.info = v
			}
		case *fyne.Container:
			for _, inner := range v.Objects {
				walk(inner)
			}
		}
	}
	walk(o)
	return r
}

type serverParts struct {
	name, status, details *widget.Label
	dot                   *canvas.Circle
	icon                  *widget.Icon
	toggle                *widget.Button
}

func newServerItem() (fyne.CanvasObject, *serverParts) {
	p := &serverParts{
		name:    widget.NewLabel(""),
		status:  widget.NewLabel(""),
		details: widget.NewLabel(""),
		dot:     canvas.NewCircle(theme.Color(theme.ColorNameDisabled)),
		icon:    widget.NewIcon(nil),
		toggle:  widget.NewButtonWithIcon("", theme.MenuDropDownIcon(), nil),
	}
	p.name.Truncation = fyne.TextTruncateEllipsis
	p.details.Importance = widget.LowImportance
	p.details.Hide()
	p.icon.Hide()
	p.toggle.Importance = widget.LowImportance
	dotBox := container.NewGridWrap(fyne.NewSize(12, 12), p.dot)
	right := container.NewHBox(container.NewCenter(dotBox), p.status, p.icon, p.toggle)
	top := container.NewBorder(nil, nil, nil, right, p.name)
	details := container.New(layout.NewCustomPaddedLayout(0, 0, 16, 0), p.details)
	return container.NewVBox(top, details), p
}

func newSaveItem() fyne.CanvasObject {
	img := canvas.NewImageFromResource(nil)
	img.FillMode = canvas.ImageFillContain
	img.CornerRadius = 8
	img.SetMinSize(fyne.NewSize(64, 64))
	name := widget.NewLabel("")
	name.Truncation = fyne.TextTruncateEllipsis
	info := widget.NewLabel("")
	icon := widget.NewIcon(nil)
	icon.Hide()
	mid := container.NewVBox(layout.NewSpacer(), name, layout.NewSpacer())
	bg := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	bg.CornerRadius = 10
	right := container.NewHBox(container.NewCenter(icon), container.NewCenter(info))
	inner := container.New(layout.NewCustomPaddedLayout(4, 4, 4, 8), container.NewBorder(nil, nil, img, right, mid))
	return container.New(layout.NewCustomPaddedLayout(3, 3, 0, 0), container.NewStack(bg, inner))
}

func (u *ui) playTab() fyne.CanvasObject {
	var (
		saves        []play.Save
		servers      []serverRow
		target       *play.Target
		targetKey    string
		selectedDesc string
		picked       = -1
		selectedSave = -1
		statMu       sync.Mutex
		stats        = map[string]play.ServerStatus{}
		saveChecks   = map[string]play.CheckResult{}
		latestCheck  *play.CheckResult
		serverChecks = map[string]play.CheckResult{}
		expanded     = map[string]bool{}
		itemParts    = map[fyne.CanvasObject]*serverParts{}
	)
	nickPath, _ := play.NicknamesPath()
	nicks, _ := play.LoadNicknames(nickPath)
	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord
	statusScroll := container.NewVScroll(status)
	statusScroll.SetMinSize(fyne.NewSize(0, 96))

	launch := widget.NewButton("Launch", nil)
	launch.Importance = widget.HighImportance
	launch.Disable()

	currentCheck := func() (play.CheckResult, bool) {
		statMu.Lock()
		defer statMu.Unlock()
		switch {
		case target == nil:
			return play.CheckResult{}, false
		case target.Kind == play.KindLatest:
			if latestCheck == nil {
				return play.CheckResult{}, false
			}
			return *latestCheck, true
		case target.Kind == play.KindSave:
			r, ok := saveChecks[targetKey]
			return r, ok
		default:
			r, ok := serverChecks[targetKey]
			return r, ok
		}
	}

	showSelection := func() {
		if target == nil {
			return
		}
		r, known := currentCheck()
		msg := "Selected: " + selectedDesc
		switch {
		case !known:
			msg += "\nChecking..."
			launch.Disable()
		case len(r.Issues) == 0:
			msg += "\nAll checks passed."
			launch.Enable()
		default:
			msg += "\n" + r.Text()
			if r.Blocking() {
				launch.Disable()
			} else {
				launch.Enable()
			}
		}
		status.SetText(msg)
		statusScroll.ScrollToTop()
	}

	saveList := widget.NewList(
		func() int { return len(saves) + 1 },
		newSaveItem,
		func(id widget.ListItemID, o fyne.CanvasObject) {
			refs := findParts(o)
			name, info, img := refs.name, refs.info, refs.img
			refs.bg.FillColor = theme.Color(theme.ColorNameInputBackground)
			if id == selectedSave {
				refs.bg.FillColor = theme.Color(theme.ColorNameSelection)
			}
			refs.bg.Refresh()
			thumb := ""
			var check play.CheckResult
			known := false
			statMu.Lock()
			if id == 0 {
				name.SetText("Continue latest save")
				info.SetText("")
				if len(saves) > 0 {
					thumb = saves[0].Thumb()
					info.SetText(saves[0].Modified.Format("2006-01-02 15:04"))
				}
				if latestCheck != nil {
					check, known = *latestCheck, true
				}
			} else {
				s := saves[id-1]
				name.SetText(nicks.Display(s))
				info.SetText(s.Modified.Format("2006-01-02 15:04"))
				thumb = s.Thumb()
				check, known = saveChecks[s.Dir]
			}
			statMu.Unlock()
			setIssueIcon(refs.icon, check, known)
			img.File = thumb
			img.Resource = nil
			if thumb == "" {
				img.Resource = theme.FileImageIcon()
			}
			img.Refresh()
		},
	)

	cardH := newSaveItem().MinSize().Height
	saveArea := canvas.NewRectangle(color.Transparent)
	fitSaves := func() {
		n := min(len(saves)+1, 5)
		saveArea.SetMinSize(fyne.NewSize(0, float32(n)*cardH+float32(n-1)*theme.Padding()))
		saveArea.Refresh()
	}

	baseItem, baseParts := newServerItem()
	collapsedH := baseItem.MinSize().Height
	baseParts.details.SetText("a\nb\nc")
	baseParts.details.Show()
	expandedH := baseItem.MinSize().Height

	var serverList *widget.List
	serverList = widget.NewList(
		func() int { return len(servers) },
		func() fyne.CanvasObject {
			o, p := newServerItem()
			itemParts[o] = p
			return o
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			p := itemParts[o]
			if p == nil || id >= len(servers) {
				return
			}
			r := servers[id]
			key := r.key()
			statMu.Lock()
			st, ok := stats[r.server.Addr()]
			check, checked := serverChecks[key]
			statMu.Unlock()
			text := "checking..."
			if ok {
				text = st.String()
			}
			p.name.SetText(r.title())
			p.status.SetText(text)
			p.dot.FillColor = statusColor(st, ok)
			p.dot.Refresh()
			setIssueIcon(p.icon, check, checked)
			p.details.SetText(r.details())
			if expanded[key] {
				p.details.Show()
				p.toggle.SetIcon(theme.MenuDropUpIcon())
			} else {
				p.details.Hide()
				p.toggle.SetIcon(theme.MenuDropDownIcon())
			}
			p.toggle.OnTapped = func() {
				expanded[key] = !expanded[key]
				h := collapsedH
				if expanded[key] {
					h = expandedH
				}
				serverList.SetItemHeight(id, h)
				serverList.RefreshItem(id)
			}
		},
	)

	nickBtn := widget.NewButton("Nickname...", nil)
	nickBtn.Disable()

	detailsOpen := false
	detailsBox := container.NewStack()
	detailsBox.Hide()
	detailsBtn := widget.NewButtonWithIcon("Save details", theme.MenuExpandIcon(), nil)
	detailsBtn.Disable()
	syncDetails := func() {
		if picked < 0 || picked >= len(saves) {
			detailsBtn.Disable()
			detailsBox.Hide()
			return
		}
		detailsBtn.Enable()
		if !detailsOpen {
			detailsBtn.SetIcon(theme.MenuExpandIcon())
			detailsBox.Hide()
			return
		}
		detailsBtn.SetIcon(theme.MenuDropDownIcon())
		scroll := container.NewVScroll(u.saveDetails(saves[picked], func() { saveList.Refresh() }))
		scroll.SetMinSize(fyne.NewSize(0, 260))
		detailsBox.Objects = []fyne.CanvasObject{scroll}
		detailsBox.Show()
		detailsBox.Refresh()
	}
	detailsBtn.OnTapped = func() {
		detailsOpen = !detailsOpen
		syncDetails()
	}

	saveList.HideSeparators = true
	saveList.OnUnselected = func(id widget.ListItemID) {
		selectedSave = -1
		saveList.RefreshItem(id)
	}
	saveList.OnSelected = func(id widget.ListItemID) {
		selectedSave = id
		saveList.RefreshItem(id)
		serverList.UnselectAll()
		t := play.Latest()
		selectedDesc = "Continue latest save"
		targetKey = ""
		picked = -1
		nickBtn.Disable()
		if id > 0 {
			t = play.SaveTarget(saves[id-1])
			selectedDesc = nicks.Display(saves[id-1])
			targetKey = saves[id-1].Dir
			picked = id - 1
			nickBtn.Enable()
		}
		syncDetails()
		target = &t
		showSelection()
	}
	serverList.OnSelected = func(id widget.ListItemID) {
		saveList.UnselectAll()
		picked = -1
		nickBtn.Disable()
		syncDetails()
		r := servers[id]
		account := ""
		if r.hasAcct {
			account = r.account.Username
		}
		t := play.ServerTarget(r.server, account)
		target = &t
		targetKey = r.key()
		selectedDesc = r.title()
		showSelection()
	}

	runSaveChecks := func(list []play.Save) {
		env, err := play.Default()
		if err != nil {
			return
		}
		c := env.NewChecker()
		results := map[string]play.CheckResult{}
		for _, s := range list {
			results[s.Dir] = c.CheckSave(s)
		}
		latest := c.CheckLatest(list)
		statMu.Lock()
		saveChecks = results
		latestCheck = &latest
		statMu.Unlock()
		fyne.Do(func() {
			saveList.Refresh()
			showSelection()
		})
	}

	refresh := func() {
		var err error
		saves, err = play.ListSaves()
		if err != nil {
			status.SetText("Could not read saves: " + err.Error())
		}
		list, serr := play.ListServers()
		names := map[string]int{}
		for _, s := range list {
			names[s.Name] += max(1, len(s.Accounts))
		}
		var rows []serverRow
		for _, s := range list {
			dup := names[s.Name] > 1
			if len(s.Accounts) == 0 {
				rows = append(rows, serverRow{server: s, dupName: dup})
			}
			for _, a := range s.Accounts {
				rows = append(rows, serverRow{server: s, account: a, hasAcct: true, dupName: dup})
			}
		}
		statMu.Lock()
		servers = rows
		saveChecks = map[string]play.CheckResult{}
		latestCheck = nil
		statMu.Unlock()
		for i, r := range rows {
			if expanded[r.key()] {
				serverList.SetItemHeight(i, expandedH)
			} else {
				serverList.SetItemHeight(i, collapsedH)
			}
		}
		if serr != nil {
			status.SetText("Could not read the server list: " + serr.Error())
		}
		target = nil
		launch.Disable()
		saveList.UnselectAll()
		serverList.UnselectAll()
		picked = -1
		syncDetails()
		saveList.Refresh()
		serverList.Refresh()
		if err == nil && serr == nil {
			status.SetText(fmt.Sprintf("%s, %s. Pick one and press Launch.", plural(len(saves), "save", "saves"), plural(len(servers), "saved server account", "saved server accounts")))
		}
		fitSaves()
		go runSaveChecks(append([]play.Save(nil), saves...))
	}

	var startGame func(play.Target)
	doLaunch := func(t play.Target) {
		launch.Disable()
		t.Debug = gamecfg.DebugEnabled()
		beforeGameLaunch()
		start := func() {
			status.SetText("Starting Project Zomboid through Steam...")
			go startGame(t)
		}
		var save *play.Save
		switch t.Kind {
		case play.KindLatest:
			if len(saves) > 0 {
				save = &saves[0]
			}
		case play.KindSave:
			for i := range saves {
				if saves[i].Mode == t.Mode && saves[i].World == t.World {
					save = &saves[i]
				}
			}
		}
		if save == nil {
			start()
			return
		}
		s := *save
		u.runBakOp("Backing up "+nicks.Display(s), func(ctx context.Context, progress func(savebak.Progress)) error {
			return savebak.BeforeLaunch(ctx, s, progress)
		}, func(err error) {
			if err == nil {
				start()
				return
			}
			dialog.ShowConfirm("Backup failed", "The automatic backup before launching failed:\n"+err.Error()+"\n\nLaunch without a backup?", func(ok bool) {
				if ok {
					start()
					return
				}
				launch.Enable()
				status.SetText("Launch cancelled.")
			}, u.win)
		})
	}

	startGame = func(t play.Target) {
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
	}

	launch.OnTapped = func() {
		if target == nil {
			return
		}
		t := *target
		r, known := currentCheck()
		if !known || r.Blocking() {
			showSelection()
			return
		}
		if !r.HasWarnings() {
			doLaunch(t)
			return
		}
		var warn []string
		for _, i := range r.Issues {
			if i.Severity == play.SevWarning {
				warn = append(warn, "- "+i.Text)
			}
		}
		msg := widget.NewLabel(strings.Join(warn, "\n") + "\n\nLaunch anyway?")
		msg.Wrapping = fyne.TextWrapWord
		d := dialog.NewCustomConfirm("Continue anyway?", "Launch", "Cancel", container.NewVScroll(msg), func(ok bool) {
			if ok {
				doLaunch(t)
			}
		}, u.win)
		d.Resize(fyne.NewSize(520, 320))
		d.Show()
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
				selectedDesc = nicks.Display(s)
				showSelection()
			}, u.win)
	}

	pollStatus := func() {
		statMu.Lock()
		rows := append([]serverRow(nil), servers...)
		statMu.Unlock()
		addrs := map[string]play.Server{}
		for _, r := range rows {
			addrs[r.server.Addr()] = r.server
		}
		fresh := map[string]play.ServerStatus{}
		for addr, srv := range addrs {
			fresh[addr] = play.QueryServer(srv.IP, srv.Port, 2*time.Second)
		}
		checks := map[string]play.CheckResult{}
		if env, err := play.Default(); err == nil {
			checker := env.NewChecker()
			for _, r := range rows {
				checks[r.key()] = checker.CheckServer(r.server, r.accountPtr(), fresh[r.server.Addr()])
			}
		}
		statMu.Lock()
		for k, v := range fresh {
			stats[k] = v
		}
		for k, v := range checks {
			serverChecks[k] = v
		}
		statMu.Unlock()
		fyne.Do(func() {
			serverList.Refresh()
			if target != nil && target.Kind == play.KindServer {
				showSelection()
			}
		})
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

	serverArea := canvas.NewRectangle(color.Transparent)
	serverArea.SetMinSize(fyne.NewSize(0, expandedH))
	savesBox := container.NewBorder(widget.NewLabelWithStyle("Single player", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), nil, nil, nil, container.NewStack(saveArea, saveList))
	serversBox := container.NewBorder(widget.NewLabelWithStyle("Servers", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), nil, nil, nil, container.NewStack(serverArea, serverList))
	lists := container.NewBorder(container.NewVBox(savesBox, detailsBox), nil, nil, nil, serversBox)
	bottom := container.NewVBox(statusScroll, container.NewHBox(launch, nickBtn, detailsBtn, refreshBtn))
	return container.NewBorder(nil, bottom, nil, nil, lists)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
