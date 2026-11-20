package main

import (
	"errors"
	"fmt"
	"image"
	"net/url"
	"slices"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/katachinosekai/pzlauncher/internal/modlist"
	"github.com/katachinosekai/pzlauncher/internal/paths"
	"github.com/katachinosekai/pzlauncher/internal/play"
)

const (
	presetChoose     = "(choose preset)"
	presetDisableAll = "Disable all"
	presetEnableAll  = "Enable all"
	autoLoadModID    = "AutoLoadSave"
)

type modsTab struct {
	u         *ui
	store     *modlist.Store
	index     *modlist.Index
	model     *modlist.Model
	file      *modlist.DefaultFile
	presets   *modlist.Presets
	prefs     modlist.Prefs
	prefsPath string
	gameNote  string

	view     []*modlist.Mod
	selected *modlist.Mod
	orderSel int
	mapSel   int

	icons   *modlist.ImageCache
	posters *modlist.ImageCache

	root        *fyne.Container
	list        *widget.List
	search      *widget.Entry
	filterSel   *widget.Select
	sortSel     *widget.Select
	count       *widget.Label
	presetSel   *widget.Select
	presetGuard bool
	saveBtn     *widget.Button
	revertBtn   *widget.Button
	status      *widget.Label

	details    *fyne.Container
	orderList  *widget.List
	orderIssue *widget.Label
	issues     []modlist.OrderIssue
	mapList    *widget.List
}

func (u *ui) modsTab() fyne.CanvasObject {
	t := &modsTab{u: u, orderSel: -1, mapSel: -1,
		icons:   modlist.NewImageCache(48, 4),
		posters: modlist.NewImageCache(640, 2),
	}
	t.prefsPath, _ = modlist.PrefsPath()
	t.prefs = modlist.LoadPrefs(t.prefsPath)
	t.root = container.NewStack(container.NewCenter(widget.NewLabel("Scanning installed mods...")))
	go t.load()
	return t.root
}

func (t *modsTab) load() {
	zdir, err := paths.Zomboid()
	if err != nil {
		fyne.Do(func() { t.showError(err) })
		return
	}
	roots := modlist.Roots{ZomboidDir: zdir}
	var game modlist.Version
	note := ""
	if root, err := paths.SteamRoot(); err == nil {
		roots.WorkshopDir, _ = paths.WorkshopDir(root)
		if gd, err := paths.GameDir(root); err == nil {
			if gi, err := play.ReadGameInfo(gd); err == nil {
				game = modlist.Version{Major: gi.Version.Major, Minor: gi.Version.Minor, Suffix: gi.Version.Suffix}
			} else {
				note = "Game version unknown (" + err.Error() + "): version limits are not checked."
			}
		} else {
			note = "Game not found: version limits are not checked."
		}
	} else {
		note = "Steam not found: only local mods are listed and version limits are not checked."
	}
	store := &modlist.Store{ZomboidDir: zdir, Running: play.IsRunning}
	idx := modlist.Scan(roots, game)
	f, err := store.LoadDefault()
	if err != nil {
		fyne.Do(func() { t.showError(err) })
		return
	}
	p, err := store.LoadPresets()
	if err != nil {
		fyne.Do(func() { t.showError(err) })
		return
	}
	fyne.Do(func() {
		t.store, t.index, t.file, t.presets, t.gameNote = store, idx, f, p, note
		t.model = modlist.NewModel(idx, f.Mods, f.Maps)
		if t.list == nil {
			t.build()
		}
		t.refreshAll()
		msg := fmt.Sprintf("%d mods installed, %d enabled.", len(idx.Mods), len(t.model.Active))
		if !game.IsZero() {
			msg += " Game " + game.String() + "."
		}
		if f.Raw() != "" && f.Version != 1 {
			msg += " default.txt has no VERSION = 1 header: the game ignores it and the launcher will not save it."
		}
		t.setStatus(msg)
	})
}

func (t *modsTab) showError(err error) {
	l := widget.NewLabel("Could not load the mod list: " + err.Error())
	l.Wrapping = fyne.TextWrapWord
	t.root.Objects = []fyne.CanvasObject{container.NewCenter(l)}
	t.root.Refresh()
}

func (t *modsTab) setStatus(s string) {
	if t.gameNote != "" {
		s += " " + t.gameNote
	}
	t.status.SetText(s)
}

func (t *modsTab) savePrefs() {
	if t.prefsPath != "" {
		_ = modlist.SavePrefs(t.prefsPath, t.prefs)
	}
}

func (t *modsTab) build() {
	t.search = widget.NewEntry()
	t.search.SetPlaceHolder("Search name, ID, Workshop ID or link")
	t.search.OnChanged = func(string) { t.refreshList() }

	var filters, sorts []string
	for _, f := range modlist.Filters {
		filters = append(filters, string(f))
	}
	for _, s := range modlist.SortModes {
		sorts = append(sorts, string(s))
	}
	t.filterSel = widget.NewSelect(filters, nil)
	t.filterSel.SetSelected(string(t.prefs.Filter))
	t.filterSel.OnChanged = func(s string) {
		t.prefs.Filter = modlist.Filter(s)
		t.savePrefs()
		t.refreshList()
	}
	t.sortSel = widget.NewSelect(sorts, nil)
	t.sortSel.SetSelected(string(t.prefs.Sort))
	t.sortSel.OnChanged = func(s string) {
		t.prefs.Sort = modlist.SortMode(s)
		t.savePrefs()
		t.refreshList()
	}
	t.count = widget.NewLabel("")

	t.list = widget.NewList(func() int { return len(t.view) }, t.createRow, t.updateRow)
	t.list.OnSelected = func(id widget.ListItemID) {
		if id >= 0 && id < len(t.view) {
			t.selected = t.view[id]
			t.showDetails()
		}
	}

	t.search.Scroll = container.ScrollNone
	filterRow := container.NewGridWithColumns(2, t.filterSel, t.sortSel)
	left := container.NewBorder(container.NewVBox(t.search, filterRow, t.count), nil, nil, nil, t.list)

	t.details = container.NewVBox(widget.NewLabel("Select a mod to see its details."))
	detailsScroll := container.NewVScroll(t.details)

	t.orderList = widget.NewList(func() int { return len(t.model.Active) }, func() fyne.CanvasObject {
		l := widget.NewLabel("")
		l.Truncation = fyne.TextTruncateEllipsis
		return l
	}, t.updateOrderRow)
	t.orderList.OnSelected = func(id widget.ListItemID) { t.orderSel = id }
	t.orderIssue = widget.NewLabel("")
	t.orderIssue.Wrapping = fyne.TextWrapWord
	up := widget.NewButtonWithIcon("Up", theme.MoveUpIcon(), func() { t.moveOrder(-1) })
	down := widget.NewButtonWithIcon("Down", theme.MoveDownIcon(), func() { t.moveOrder(1) })
	auto := widget.NewButton("Auto-sort", func() {
		t.model.AutoSort()
		t.orderSel = -1
		t.orderList.UnselectAll()
		t.changed("Load order auto-sorted (the game's own Auto button rules).")
	})
	rmMissing := widget.NewButton("Remove missing", func() {
		miss := t.model.RemoveMissing()
		if len(miss) == 0 {
			t.setStatus("No missing mods in the list.")
			return
		}
		t.changed("Removed from the list (not installed): " + strings.Join(miss, ", "))
	})
	orderPane := container.NewBorder(nil, container.NewVBox(container.NewHBox(up, down, layout.NewSpacer(), rmMissing, auto), t.orderIssue), nil, nil, t.orderList)

	t.mapList = widget.NewList(func() int { return len(t.model.Maps) }, func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id < len(t.model.Maps) {
				o.(*widget.Label).SetText(fmt.Sprintf("%d. %s", id+1, t.model.Maps[id]))
			}
		})
	t.mapList.OnSelected = func(id widget.ListItemID) { t.mapSel = id }
	mapUp := widget.NewButtonWithIcon("Up", theme.MoveUpIcon(), func() { t.moveMap(-1) })
	mapDown := widget.NewButtonWithIcon("Down", theme.MoveDownIcon(), func() { t.moveMap(1) })
	mapNote := widget.NewLabel("Map load order from default.txt (maps { }). Only reordering is offered; the game adds and removes entries itself.")
	mapNote.Wrapping = fyne.TextWrapWord
	mapPane := container.NewBorder(nil, container.NewVBox(container.NewHBox(mapUp, mapDown), mapNote), nil, nil, t.mapList)

	right := container.NewAppTabs(
		container.NewTabItem("Details", detailsScroll),
		container.NewTabItem("Load order", orderPane),
		container.NewTabItem("Maps", mapPane),
	)
	split := container.NewHSplit(left, right)
	split.Offset = 0.5

	t.presetSel = widget.NewSelect(nil, t.choosePreset)
	t.presetSel.PlaceHolder = presetChoose
	savePreset := widget.NewButtonWithIcon("Save preset...", theme.DocumentSaveIcon(), t.savePresetDialog)
	delPreset := widget.NewButtonWithIcon("Delete preset", theme.DeleteIcon(), t.deletePreset)
	reload := widget.NewButtonWithIcon("Reload", theme.ViewRefreshIcon(), func() {
		if t.dirty() {
			dialog.ShowConfirm("Reload", "Discard unsaved changes and reload from disk?", func(ok bool) {
				if ok {
					go t.load()
				}
			}, t.u.win)
			return
		}
		go t.load()
	})
	top := container.NewBorder(nil, nil, widget.NewLabel("Presets:"), container.NewHBox(savePreset, delPreset, reload), t.presetSel)

	t.status = widget.NewLabel("")
	t.status.Wrapping = fyne.TextWrapWord
	t.saveBtn = widget.NewButtonWithIcon("Save mod list", theme.DocumentSaveIcon(), t.save)
	t.saveBtn.Importance = widget.HighImportance
	t.revertBtn = widget.NewButton("Revert", func() {
		t.model = modlist.NewModel(t.index, t.file.Mods, t.file.Maps)
		t.refreshAll()
		t.setStatus("Reverted to the saved mod list.")
	})
	bottom := container.NewBorder(nil, nil, nil, container.NewHBox(t.revertBtn, t.saveBtn), t.status)

	t.root.Objects = []fyne.CanvasObject{container.NewBorder(top, bottom, nil, nil, split)}
	t.root.Refresh()
}

func (t *modsTab) dirty() bool {
	if t.model == nil {
		return false
	}
	return !slices.Equal(t.model.Active, t.file.Mods) || !slices.Equal(t.model.Maps, t.file.Maps)
}

func (t *modsTab) refreshAll() {
	t.refreshPresets()
	t.refreshList()
	t.orderList.Refresh()
	t.mapList.Refresh()
	t.refreshOrderIssues()
	t.showDetails()
	t.refreshButtons()
}

func (t *modsTab) refreshButtons() {
	if t.dirty() {
		t.saveBtn.Enable()
		t.revertBtn.Enable()
	} else {
		t.saveBtn.Disable()
		t.revertBtn.Disable()
	}
}

func (t *modsTab) changed(msg string) {
	t.refreshList()
	t.orderList.Refresh()
	t.mapList.Refresh()
	t.refreshOrderIssues()
	t.showDetails()
	t.refreshButtons()
	if msg != "" {
		t.setStatus(msg)
	}
}

func (t *modsTab) refreshPresets() {
	opts := []string{presetDisableAll, presetEnableAll}
	for _, p := range t.presets.List() {
		opts = append(opts, p.Name)
	}
	t.presetGuard = true
	cur := t.presetSel.Selected
	t.presetSel.Options = opts
	if !slices.Contains(opts, cur) || cur == presetDisableAll || cur == presetEnableAll {
		t.presetSel.ClearSelected()
	}
	t.presetSel.Refresh()
	t.presetGuard = false
}

func (t *modsTab) refreshList() {
	if t.model == nil {
		return
	}
	t.view = modlist.View(t.index.Mods, modlist.ViewOptions{
		Query:    t.search.Text,
		Filter:   modlist.Filter(t.filterSel.Selected),
		Sort:     modlist.SortMode(t.sortSel.Selected),
		IsActive: t.model.IsActive,
		IsFav:    t.presets.IsFav,
	})
	t.count.SetText(fmt.Sprintf("Showing %d of %d installed mods, %d enabled", len(t.view), len(t.index.Mods), len(t.model.Active)))
	t.list.UnselectAll()
	t.list.Refresh()
}

type modRow struct {
	check *widget.Check
	icon  *canvas.Image
	name  *widget.Label
	info  *widget.Label
}

func (t *modsTab) createRow() fyne.CanvasObject {
	check := widget.NewCheck("", nil)
	icon := canvas.NewImageFromResource(theme.FileImageIcon())
	icon.FillMode = canvas.ImageFillContain
	icon.SetMinSize(fyne.NewSize(28, 28))
	name := widget.NewLabel("")
	name.Truncation = fyne.TextTruncateEllipsis
	info := widget.NewLabel("")
	info.Importance = widget.LowImportance
	info.Truncation = fyne.TextTruncateEllipsis
	text := container.New(layout.NewCustomPaddedVBoxLayout(-12), name, info)
	return container.NewBorder(nil, nil, container.NewHBox(check, icon), nil, text)
}

func rowParts(o fyne.CanvasObject) modRow {
	c := o.(*fyne.Container)
	var r modRow
	for _, obj := range c.Objects {
		v, ok := obj.(*fyne.Container)
		if !ok {
			continue
		}
		if chk, ok := v.Objects[0].(*widget.Check); ok {
			r.check = chk
			r.icon = v.Objects[1].(*canvas.Image)
			continue
		}
		r.name = v.Objects[0].(*widget.Label)
		r.info = v.Objects[1].(*widget.Label)
	}
	return r
}

func setImage(img *canvas.Image, src image.Image) {
	if src == nil {
		img.Image = nil
		img.Resource = theme.FileImageIcon()
	} else {
		img.Resource = nil
		img.Image = src
	}
	img.Refresh()
}

func (t *modsTab) sourceText(m *modlist.Mod) string {
	switch m.Source {
	case modlist.SourceSteam:
		return m.ID + " | WS " + m.WorkshopID
	case modlist.SourceWorkshop:
		return m.ID + " | Workshop upload"
	}
	return m.ID + " | local"
}

func (t *modsTab) updateRow(id widget.ListItemID, o fyne.CanvasObject) {
	if id >= len(t.view) {
		return
	}
	m := t.view[id]
	r := rowParts(o)
	active := t.model.IsActive(m.ID)
	available := t.index.Available(m.ID)
	r.check.OnChanged = nil
	r.check.SetChecked(active)
	if !active && !available {
		r.check.Disable()
	} else {
		r.check.Enable()
	}
	modID := m.ID
	r.check.OnChanged = func(on bool) { t.toggle(modID, on) }

	name := m.Name
	if t.presets.IsFav(m.ID) {
		name = "* " + name
	}
	switch {
	case !available:
		r.name.Importance = widget.LowImportance
		name += " (unavailable)"
	case active:
		r.name.Importance = widget.SuccessImportance
	case t.model.IsIncompatible(m.ID):
		r.name.Importance = widget.WarningImportance
		name += " (incompatible)"
	default:
		r.name.Importance = widget.MediumImportance
	}
	r.name.SetText(name)
	r.info.SetText(t.sourceText(m))

	path := m.Icon
	if path == "" && len(m.Posters) > 0 {
		path = m.Posters[0]
	}
	img, ok := t.icons.Get(path, func(image.Image) {
		fyne.Do(func() { t.list.RefreshItem(id) })
	})
	if ok {
		setImage(r.icon, img)
	} else {
		setImage(r.icon, nil)
	}
}

func (t *modsTab) toggle(id string, on bool) {
	c := t.model.ForceActivate(id, on)
	var parts []string
	if len(c.Enabled) > 0 {
		parts = append(parts, "Enabled: "+strings.Join(c.Enabled, ", "))
	}
	if len(c.Disabled) > 0 {
		parts = append(parts, "Disabled: "+strings.Join(c.Disabled, ", "))
	}
	if len(c.Skipped) > 0 {
		var sk []string
		for k, err := range c.Skipped {
			sk = append(sk, k+": "+err.Error())
		}
		sort.Strings(sk)
		dialog.ShowInformation("Not enabled", strings.Join(sk, "\n"), t.u.win)
		parts = append(parts, "Not enabled: "+strings.Join(sk, "; "))
	}
	if len(c.Disabled) > 0 && slices.Contains(c.Disabled, autoLoadModID) {
		parts = append(parts, "Note: "+autoLoadModID+" is the launcher's helper for auto-loading saves from the Play tab.")
	}
	t.changed(strings.Join(parts, ". "))
}

func (t *modsTab) moveOrder(d int) {
	if t.model.Move(t.orderSel, d) {
		t.orderSel += d
		t.orderList.Select(t.orderSel)
		t.changed("")
	}
}

func (t *modsTab) moveMap(d int) {
	if t.model.MoveMap(t.mapSel, d) {
		t.mapSel += d
		t.mapList.Select(t.mapSel)
		t.changed("")
	}
}

func (t *modsTab) updateOrderRow(id widget.ListItemID, o fyne.CanvasObject) {
	if id >= len(t.model.Active) {
		return
	}
	l := o.(*widget.Label)
	mid := t.model.Active[id]
	m := t.index.Get(mid)
	text := fmt.Sprintf("%d. ", id+1)
	l.Importance = widget.MediumImportance
	switch {
	case m == nil:
		text += mid + " (not installed)"
		l.Importance = widget.DangerImportance
	case !t.index.Available(mid):
		text += m.Name + " [" + mid + "] (unavailable)"
		l.Importance = widget.LowImportance
	default:
		text += m.Name + " [" + mid + "]"
	}
	for _, is := range t.issues {
		if is.ID == mid {
			text += " - " + is.Reason
			if l.Importance == widget.MediumImportance {
				l.Importance = widget.WarningImportance
			}
			break
		}
	}
	l.SetText(text)
}

func (t *modsTab) refreshOrderIssues() {
	t.issues = t.model.OrderIssues()
	if len(t.issues) == 0 {
		t.orderIssue.SetText("Load order OK.")
		return
	}
	var lines []string
	for i, is := range t.issues {
		if i == 5 {
			lines = append(lines, fmt.Sprintf("... and %d more", len(t.issues)-5))
			break
		}
		lines = append(lines, is.ID+": "+is.Reason)
	}
	t.orderIssue.SetText(strings.Join(lines, "\n"))
	t.orderList.Refresh()
}

func listText(ids []string, state func(string) string) string {
	if len(ids) == 0 {
		return "-"
	}
	var out []string
	for _, id := range ids {
		out = append(out, id+state(id))
	}
	return strings.Join(out, ", ")
}

func (t *modsTab) showDetails() {
	m := t.selected
	if m == nil || t.index.Get(m.ID) != m {
		t.details.Objects = []fyne.CanvasObject{widget.NewLabel("Select a mod to see its details.")}
		t.details.Refresh()
		return
	}
	poster := canvas.NewImageFromResource(theme.FileImageIcon())
	poster.FillMode = canvas.ImageFillContain
	poster.SetMinSize(fyne.NewSize(256, 256))
	showPoster := func(path string) {
		img, ok := t.posters.Get(path, func(img image.Image) {
			fyne.Do(func() {
				if t.selected == m {
					setImage(poster, img)
				}
			})
		})
		if ok {
			setImage(poster, img)
		} else {
			setImage(poster, nil)
		}
	}
	if len(m.Posters) > 0 {
		showPoster(m.Posters[0])
	} else {
		setImage(poster, nil)
	}
	thumbs := container.NewHBox()
	if len(m.Posters) > 1 {
		for i, p := range m.Posters {
			path := p
			thumbs.Add(widget.NewButton(fmt.Sprintf("Poster %d", i+1), func() { showPoster(path) }))
		}
	}

	title := widget.NewLabel(m.Name)
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Wrapping = fyne.TextWrapWord
	title.SizeName = theme.SizeNameSubHeadingText

	form := widget.NewForm()
	add := func(label, value string) *widget.Label {
		l := widget.NewLabel(value)
		l.Wrapping = fyne.TextWrapWord
		l.Selectable = true
		form.Append(label, l)
		return l
	}
	status := "Disabled"
	if t.model.IsActive(m.ID) {
		status = "Enabled"
	}
	if why := t.index.Unavailable(m.ID); why != "" {
		status += " - unavailable: " + why
	}
	if inc := t.model.ActiveIncompatible(m.ID); len(inc) > 0 {
		status += " - incompatible with enabled: " + strings.Join(inc, ", ")
	}
	add("Status", status)
	add("Mod ID", m.ID)
	if m.WorkshopID != "" {
		add("Workshop ID", m.WorkshopID)
	}
	add("Source", string(m.Source))
	if m.Author != "" {
		add("Author", m.Author)
	}
	if m.ModVersion != "" {
		add("Version", m.ModVersion)
	}
	if m.URL != "" {
		add("URL", m.URL)
	}
	gv := add("Zomboid version", m.VersionRange())
	if !m.AvailableSelf(t.index.Game) {
		gv.Importance = widget.DangerImportance
	}
	reqState := func(id string) string {
		switch {
		case t.index.Get(id) == nil:
			return " (missing)"
		case t.model.IsActive(id):
			return " (enabled)"
		case !t.index.Available(id):
			return " (unavailable)"
		}
		return ""
	}
	add("Requires", listText(m.Requires(), reqState))
	add("Incompatible", listText(t.model.IncompatibleWith(m.ID), reqState))
	if len(m.After()) > 0 {
		add("Load after", strings.Join(m.After(), ", "))
	}
	if len(m.Before()) > 0 {
		add("Load before", strings.Join(m.Before(), ", "))
	}
	if n := t.model.NeededFor(m.ID); len(n) > 0 {
		add("Needed by", strings.Join(n, ", "))
	}
	add("Path", m.Dir)
	for _, s := range t.index.Shadowed[m.ID] {
		add("Also at (ignored)", s.Dir)
	}

	buttons := container.NewHBox()
	fav := t.presets.IsFav(m.ID)
	favText := "Add to favorites"
	if fav {
		favText = "Remove from favorites"
	}
	buttons.Add(widget.NewButton(favText, func() { t.setFavorite(m.ID, !fav) }))
	if u := m.WorkshopURL(); u != "" {
		buttons.Add(widget.NewButton("Open Workshop page", func() {
			if pu, err := url.Parse(u); err == nil {
				_ = t.u.app.OpenURL(pu)
			}
		}))
	}

	desc := widget.NewLabel(modlist.DescriptionText(m.Description))
	desc.Wrapping = fyne.TextWrapWord
	desc.Selectable = true

	t.details.Objects = []fyne.CanvasObject{title, poster, thumbs, buttons, form, widget.NewSeparator(), desc}
	t.details.Refresh()
}

func (t *modsTab) setFavorite(id string, fav bool) {
	go func() {
		p, res, err := t.store.SetFavorite(id, fav)
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(err, t.u.win)
				return
			}
			t.presets = p
			t.refreshList()
			t.showDetails()
			t.setStatus(backupNote("Favorites saved.", res))
		})
	}()
}

func backupNote(msg string, res modlist.SaveResult) string {
	if res.Backup != "" {
		msg += " Backup: " + res.Backup
	}
	return msg
}

func (t *modsTab) choosePreset(name string) {
	if t.presetGuard || name == "" {
		return
	}
	switch name {
	case presetDisableAll:
		t.model.DisableAll()
		t.changed("All mods disabled (not saved yet).")
		return
	case presetEnableAll:
		c := t.model.EnableAll()
		t.changed(fmt.Sprintf("Enabled %d mods (not saved yet).", len(c.Enabled)))
		return
	}
	p, ok := t.presets.Get(name)
	if !ok {
		return
	}
	res := t.model.LoadPreset(p.IDs)
	msg := fmt.Sprintf("Preset %q loaded: %d mods enabled (not saved yet).", name, len(t.model.Active))
	t.changed(msg)
	if len(res.Missing) > 0 {
		var lines []string
		for _, id := range res.Missing {
			lines = append(lines, strings.SplitN(id, `\`, 2)[0])
		}
		l := widget.NewLabel(strings.Join(lines, "\n"))
		l.Selectable = true
		dialog.ShowCustom("Missing mods", "OK", container.NewVBox(
			widget.NewLabel(fmt.Sprintf("%d mods of preset %q are not installed and were skipped:", len(lines), name)),
			container.NewGridWrap(fyne.NewSize(420, 240), container.NewVScroll(l)),
		), t.u.win)
	}
}

func (t *modsTab) savePresetDialog() {
	def := "New"
	if s := t.presetSel.Selected; s != "" && s != presetDisableAll && s != presetEnableAll {
		def = s
	}
	entry := widget.NewEntry()
	entry.SetText(def)
	entry.Validator = func(s string) error {
		if !modlist.ValidPresetName(s) {
			return modlist.ErrInvalidPresetName
		}
		return nil
	}
	dialog.ShowForm("Save preset", "Save", "Cancel", []*widget.FormItem{
		widget.NewFormItem("Name", entry),
		widget.NewFormItem("", widget.NewLabel(fmt.Sprintf("Saves the %d enabled mods, in load order.", len(t.model.Active)))),
	}, func(ok bool) {
		if !ok {
			return
		}
		name := entry.Text
		if _, exists := t.presets.Get(name); exists {
			dialog.ShowConfirm("Overwrite preset", fmt.Sprintf("Preset %q already exists. Overwrite it with the current %d enabled mods?", name, len(t.model.Active)), func(yes bool) {
				if yes {
					t.writePreset(name)
				}
			}, t.u.win)
			return
		}
		t.writePreset(name)
	}, t.u.win)
}

func (t *modsTab) writePreset(name string) {
	ids := slices.Clone(t.model.Active)
	go func() {
		p, res, err := t.store.SavePreset(name, ids)
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(err, t.u.win)
				return
			}
			t.presets = p
			t.refreshPresets()
			t.presetGuard = true
			t.presetSel.SetSelected(name)
			t.presetGuard = false
			t.refreshList()
			t.setStatus(backupNote(fmt.Sprintf("Preset %q saved (%d mods).", name, len(ids)), res))
		})
	}()
}

func (t *modsTab) deletePreset() {
	name := t.presetSel.Selected
	if _, ok := t.presets.Get(name); !ok {
		dialog.ShowInformation("Delete preset", "Choose a saved preset first.", t.u.win)
		return
	}
	dialog.ShowConfirm("Delete preset", fmt.Sprintf("Delete preset %q? This changes pz_modlist_settings.cfg (a backup is made first).", name), func(ok bool) {
		if !ok {
			return
		}
		go func() {
			p, res, err := t.store.DeletePreset(name)
			fyne.Do(func() {
				if err != nil {
					dialog.ShowError(err, t.u.win)
					return
				}
				t.presets = p
				t.refreshPresets()
				t.setStatus(backupNote(fmt.Sprintf("Preset %q deleted.", name), res))
			})
		}()
	}, t.u.win)
}

func (t *modsTab) save() {
	do := func() {
		t.file.Mods = slices.Clone(t.model.Active)
		t.file.Maps = slices.Clone(t.model.Maps)
		f := t.file
		go func() {
			res, err := t.store.SaveDefault(f)
			fyne.Do(func() {
				if err != nil {
					if errors.Is(err, modlist.ErrChangedOnDisk) {
						err = fmt.Errorf("%w (default.txt was changed outside the launcher, e.g. by the game). Use Reload; your unsaved changes would be lost", err)
					}
					t.file.Mods, t.file.Maps = t.lastSaved()
					dialog.ShowError(err, t.u.win)
					return
				}
				t.refreshButtons()
				msg := "Mod list saved."
				if !res.Written {
					msg = "Mod list unchanged."
				}
				t.setStatus(backupNote(msg, res))
			})
		}()
	}
	var warn []string
	if slices.Contains(t.file.Mods, autoLoadModID) && !t.model.IsActive(autoLoadModID) {
		warn = append(warn, autoLoadModID+" is being disabled. It is the launcher's helper: without it the Play tab cannot auto-load saves or auto-connect.")
	}
	if fatal := t.fatalIssues(); fatal != "" {
		warn = append(warn, "Load order problems:\n"+fatal)
	}
	if len(warn) == 0 {
		do()
		return
	}
	dialog.ShowConfirm("Save mod list", strings.Join(warn, "\n\n")+"\n\nSave anyway?", func(ok bool) {
		if ok {
			do()
		}
	}, t.u.win)
}

func (t *modsTab) lastSaved() ([]string, []string) {
	g := modlist.ParseDefault(t.file.Raw())
	if t.file.Raw() == "" {
		return nil, nil
	}
	return g.Mods, g.Maps
}

func (t *modsTab) fatalIssues() string {
	var out []string
	for _, is := range t.issues {
		if is.Fatal {
			out = append(out, is.ID+": "+is.Reason)
		}
	}
	if len(out) > 5 {
		out = append(out[:5], fmt.Sprintf("... and %d more", len(out)-5))
	}
	return strings.Join(out, "\n")
}
