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

type tapCard struct {
	widget.BaseWidget
	content fyne.CanvasObject
	onTap   func()
	onMenu  func(*fyne.PointEvent)
}

func newTapCard(c fyne.CanvasObject) *tapCard {
	t := &tapCard{content: c}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tapCard) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(t.content) }

func (t *tapCard) Tapped(*fyne.PointEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}

func (t *tapCard) TappedSecondary(e *fyne.PointEvent) {
	if t.onMenu != nil {
		t.onMenu(e)
	}
}

type saveParts struct {
	card       *tapCard
	bg         *canvas.Rectangle
	img        *canvas.Image
	name, info *widget.Label
	icon       *widget.Icon
	toggle     *widget.Button
	details    *fyne.Container
}

func newSaveItem() (fyne.CanvasObject, *saveParts) {
	p := &saveParts{
		bg:      canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground)),
		img:     canvas.NewImageFromResource(nil),
		name:    widget.NewLabel(""),
		info:    widget.NewLabel(""),
		icon:    widget.NewIcon(nil),
		toggle:  widget.NewButtonWithIcon("", theme.MenuDropDownIcon(), nil),
		details: container.NewStack(),
	}
	p.img.FillMode = canvas.ImageFillContain
	p.img.CornerRadius = 8
	p.img.SetMinSize(fyne.NewSize(64, 64))
	p.name.Truncation = fyne.TextTruncateEllipsis
	p.icon.Hide()
	p.toggle.Importance = widget.LowImportance
	p.bg.CornerRadius = 10
	p.details.Hide()
	mid := container.NewVBox(layout.NewSpacer(), p.name, layout.NewSpacer())
	right := container.NewHBox(container.NewCenter(p.icon), container.NewCenter(p.info), container.NewCenter(p.toggle))
	inner := container.New(layout.NewCustomPaddedLayout(4, 4, 4, 4), container.NewBorder(nil, nil, p.img, right, mid))
	p.card = newTapCard(container.NewStack(p.bg, inner))
	body := container.NewVBox(p.card, container.New(layout.NewCustomPaddedLayout(0, 0, 16, 0), p.details))
	return container.New(layout.NewCustomPaddedLayout(3, 3, 0, 0), body), p
}

func (u *ui) playTab() fyne.CanvasObject {
	var (
		saves        []play.Save
		servers      []serverRow
		target       *play.Target
		targetKey    string
		selectedDesc string
		selectedSave = -1
		statMu       sync.Mutex
		stats        = map[string]play.ServerStatus{}
		saveChecks   = map[string]play.CheckResult{}
		latestCheck  *play.CheckResult
		serverChecks = map[string]play.CheckResult{}
		expanded     = map[string]bool{}
		itemParts    = map[fyne.CanvasObject]*serverParts{}
		saveParts    = map[fyne.CanvasObject]*saveParts{}
		saveOpen     = map[string]bool{}
		savePanels   = map[string]fyne.CanvasObject{}
	)
	const detailsH = 280
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

	cardH := func() float32 { o, _ := newSaveItem(); return o.MinSize().Height }()
	var saveList *widget.List
	var fitSaves func()
	var nicknameFor func(play.Save)
	panelFor := func(sv play.Save) fyne.CanvasObject {
		if p, ok := savePanels[sv.Dir]; ok {
			return p
		}
		scroll := container.NewVScroll(u.saveDetails(sv, func() { saveList.Refresh() }))
		scroll.SetMinSize(fyne.NewSize(0, detailsH))
		savePanels[sv.Dir] = scroll
		return scroll
	}
	toggleDetails := func(id widget.ListItemID) {
		if id < 1 || id > len(saves) {
			return
		}
		key := saves[id-1].Dir
		saveOpen[key] = !saveOpen[key]
		h := cardH
		if saveOpen[key] {
			h += detailsH + theme.Padding()
		}
		saveList.SetItemHeight(id, h)
		saveList.RefreshItem(id)
		fitSaves()
	}
	saveList = widget.NewList(
		func() int { return len(saves) + 1 },
		func() fyne.CanvasObject {
			o, p := newSaveItem()
			saveParts[o] = p
			return o
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			p := saveParts[o]
			if p == nil || id > len(saves) {
				return
			}
			p.bg.FillColor = theme.Color(theme.ColorNameInputBackground)
			if id == selectedSave {
				p.bg.FillColor = theme.Color(theme.ColorNameSelection)
			}
			p.bg.Refresh()
			thumb := ""
			var check play.CheckResult
			known := false
			statMu.Lock()
			if id == 0 {
				p.name.SetText("Continue latest save")
				p.info.SetText("")
				if len(saves) > 0 {
					thumb = saves[0].Thumb()
					p.info.SetText(saves[0].Modified.Format("2006-01-02 15:04"))
				}
				if latestCheck != nil {
					check, known = *latestCheck, true
				}
			} else {
				sv := saves[id-1]
				p.name.SetText(nicks.Display(sv))
				p.info.SetText(sv.Modified.Format("2006-01-02 15:04"))
				thumb = sv.Thumb()
				check, known = saveChecks[sv.Dir]
			}
			statMu.Unlock()
			setIssueIcon(p.icon, check, known)
			p.img.File = thumb
			p.img.Resource = nil
			if thumb == "" {
				p.img.Resource = theme.FileImageIcon()
			}
			p.img.Refresh()
			p.card.onTap = func() { saveList.Select(id) }
			if id == 0 {
				p.toggle.Hide()
				p.details.Hide()
				p.card.onMenu = nil
				return
			}
			sv := saves[id-1]
			p.toggle.Show()
			p.toggle.OnTapped = func() { toggleDetails(id) }
			if saveOpen[sv.Dir] {
				p.toggle.SetIcon(theme.MenuDropUpIcon())
				p.details.Objects = []fyne.CanvasObject{panelFor(sv)}
				p.details.Show()
			} else {
				p.toggle.SetIcon(theme.MenuDropDownIcon())
				p.details.Objects = nil
				p.details.Hide()
			}
			p.details.Refresh()
			p.card.onMenu = func(e *fyne.PointEvent) {
				saveList.Select(id)
				label := "Show details && backups"
				if saveOpen[sv.Dir] {
					label = "Hide details && backups"
				}
				menu := fyne.NewMenu("",
					fyne.NewMenuItem("Nickname...", func() { nicknameFor(sv) }),
					fyne.NewMenuItem(strings.ReplaceAll(label, "&&", "&"), func() { toggleDetails(id) }),
					fyne.NewMenuItem("Open save folder", func() { u.openFolder(sv.Dir) }),
				)
				widget.ShowPopUpMenuAtPosition(menu, u.win.Canvas(), e.AbsolutePosition)
			}
		},
	)

	saveArea := canvas.NewRectangle(color.Transparent)
	fitSaves = func() {
		n := min(len(saves)+1, 5)
		h := float32(n)*cardH + float32(n-1)*theme.Padding()
		for i := 0; i < n-1 && i < len(saves); i++ {
			if saveOpen[saves[i].Dir] {
				h += detailsH + theme.Padding()
				break
			}
		}
		saveArea.SetMinSize(fyne.NewSize(0, h))
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
		if id > 0 {
			t = play.SaveTarget(saves[id-1])
			selectedDesc = nicks.Display(saves[id-1])
			targetKey = saves[id-1].Dir
		}
		target = &t
		showSelection()
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
		for k := range savePanels {
			delete(savePanels, k)
		}
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

	nicknameFor = func(s play.Save) {
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
	lists := container.NewBorder(savesBox, nil, nil, nil, serversBox)
	bottom := container.NewVBox(statusScroll, container.NewHBox(launch, refreshBtn))
	return container.NewBorder(nil, bottom, nil, nil, lists)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
