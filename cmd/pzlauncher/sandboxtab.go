package main

import (
	"cmp"
	"errors"
	"fmt"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/katachinosekai/pzlauncher/internal/install"
	"github.com/katachinosekai/pzlauncher/internal/modlist"
	"github.com/katachinosekai/pzlauncher/internal/paths"
	"github.com/katachinosekai/pzlauncher/internal/play"
	"github.com/katachinosekai/pzlauncher/internal/sandbox"
)

type sandboxTab struct {
	u       *ui
	root    *fyne.Container
	mods    []sandbox.ModOptions
	opts    map[string]sandbox.Option
	targets []sandbox.Target
	file    *sandbox.File
	edits   map[string]string
	sel     int
	list    *widget.List
	form    *fyne.Container
	scroll  *container.Scroll
	status  *widget.Label
	picker  *widget.Select
	saveBtn *widget.Button
}

func (u *ui) sandboxOptionsTab() fyne.CanvasObject {
	t := &sandboxTab{u: u, sel: -1, edits: map[string]string{}}
	t.root = container.NewStack(container.NewCenter(widget.NewLabel("Reading mod sandbox options...")))
	go t.load()
	return t.root
}

func (t *sandboxTab) load() {
	zdir, err := paths.Zomboid()
	if err != nil {
		fyne.Do(func() { t.root.Objects = []fyne.CanvasObject{widget.NewLabel(err.Error())}; t.root.Refresh() })
		return
	}
	var mods []sandbox.ModOptions
	opts := map[string]sandbox.Option{}
	if x, err := play.ScanInstalled(); err == nil {
		if df, err := (&modlist.Store{ZomboidDir: zdir}).LoadDefault(); err == nil {
			for _, id := range df.Mods {
				m := x.Get(id)
				if m == nil {
					continue
				}
				if mo, ok := sandbox.LoadMod(m.ID, m.Name, m.VersionDir, m.CommonDir); ok {
					mods = append(mods, mo)
					for _, o := range mo.Options {
						opts[o.Name] = o
					}
				}
			}
		}
	}
	sort.SliceStable(mods, func(i, j int) bool { return strings.ToLower(mods[i].ModName) < strings.ToLower(mods[j].ModName) })
	targets := sandbox.Targets(zdir)
	fyne.Do(func() {
		t.mods, t.opts, t.targets = mods, opts, targets
		t.build()
	})
}

func (t *sandboxTab) build() {
	note := widget.NewLabel("Settings declared by your enabled mods, as on the game's sandbox screen. Pick a new-game preset or a server's settings to edit. Existing saves keep their settings in a binary file the launcher can't edit yet.")
	note.Wrapping = fyne.TextWrapWord
	if len(t.targets) == 0 {
		msg := widget.NewLabel("No sandbox presets (Zomboid/Sandbox Presets) or server settings (Zomboid/Server/*_SandboxVars.lua) found. Save a preset from the game's sandbox screen or host a server once, then come back.")
		msg.Wrapping = fyne.TextWrapWord
		t.root.Objects = []fyne.CanvasObject{container.NewVBox(note, msg)}
		t.root.Refresh()
		return
	}
	if len(t.mods) == 0 {
		t.root.Objects = []fyne.CanvasObject{container.NewVBox(note, widget.NewLabel("None of your enabled mods has sandbox options."))}
		t.root.Refresh()
		return
	}
	names := make([]string, len(t.targets))
	for i, tg := range t.targets {
		names[i] = tg.String()
	}
	t.status = widget.NewLabel("")
	t.status.Importance = widget.LowImportance
	t.saveBtn = widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), t.save)
	t.saveBtn.Importance = widget.HighImportance
	t.saveBtn.Disable()
	discard := widget.NewButton("Discard changes", func() {
		t.edits = map[string]string{}
		t.changed()
		t.showMod()
	})
	current := ""
	t.picker = widget.NewSelect(names, nil)
	t.picker.OnChanged = func(s string) {
		if s == current {
			return
		}
		pick := func() {
			current = s
			t.edits = map[string]string{}
			t.openTarget(t.targets[t.picker.SelectedIndex()])
		}
		if len(t.edits) == 0 {
			pick()
			return
		}
		dialog.ShowConfirm("Unsaved changes", fmt.Sprintf("Discard %d unsaved change(s) to %s?", len(t.edits), current), func(ok bool) {
			if ok {
				pick()
			} else {
				t.picker.SetSelected(current)
			}
		}, t.u.win)
	}
	t.list = widget.NewList(func() int { return len(t.mods) }, func() fyne.CanvasObject {
		l := widget.NewLabel("")
		l.Truncation = fyne.TextTruncateEllipsis
		return l
	}, func(i widget.ListItemID, o fyne.CanvasObject) {
		mo := t.mods[i]
		n := 0
		for _, op := range mo.Options {
			if _, ok := t.edits[op.Name]; ok {
				n++
			}
		}
		text := fmt.Sprintf("%s (%d)", mo.ModName, len(mo.Options))
		if n > 0 {
			text = fmt.Sprintf("* %s (%d changed)", mo.ModName, n)
		}
		o.(*widget.Label).SetText(text)
	})
	t.list.OnSelected = func(i widget.ListItemID) {
		t.sel = i
		t.showMod()
	}
	t.form = container.NewVBox(widget.NewLabel("Select a mod on the left."))
	t.scroll = container.NewVScroll(t.form)
	split := container.NewHSplit(t.list, t.scroll)
	split.Offset = 0.3
	top := container.NewVBox(note, container.NewBorder(nil, nil, widget.NewLabel("Settings file"), nil, t.picker))
	bottom := container.NewBorder(nil, nil, nil, container.NewHBox(discard, t.saveBtn), t.status)
	t.root.Objects = []fyne.CanvasObject{container.NewBorder(top, bottom, nil, nil, split)}
	t.root.Refresh()
	t.picker.SetSelectedIndex(0)
}

func (t *sandboxTab) openTarget(tg sandbox.Target) {
	f, err := sandbox.Open(tg)
	if err != nil {
		t.file = nil
		t.status.SetText(err.Error())
		return
	}
	t.file = f
	t.changed()
	if t.sel < 0 {
		t.list.Select(0)
		return
	}
	t.showMod()
}

func (t *sandboxTab) value(o sandbox.Option) (string, bool) {
	if v, ok := t.edits[o.Name]; ok {
		return v, true
	}
	if t.file != nil {
		if v, ok := t.file.Value(o.Name); ok {
			return v, true
		}
	}
	return o.Default, false
}

func (t *sandboxTab) set(o sandbox.Option, v string) {
	orig, inFile := "", false
	if t.file != nil {
		orig, inFile = t.file.Value(o.Name)
	}
	if inFile && sameValue(o, orig, v) {
		delete(t.edits, o.Name)
	} else {
		t.edits[o.Name] = v
	}
	t.changed()
}

func sameValue(o sandbox.Option, a, b string) bool {
	if a == b {
		return true
	}
	if o.Type == "double" || o.Type == "integer" {
		var x, y float64
		_, e1 := fmt.Sscan(a, &x)
		_, e2 := fmt.Sscan(b, &y)
		return e1 == nil && e2 == nil && x == y
	}
	return false
}

func (t *sandboxTab) changed() {
	if t.list != nil {
		t.list.Refresh()
	}
	switch n := len(t.edits); {
	case t.file == nil:
		t.saveBtn.Disable()
	case n == 0:
		t.status.SetText(t.file.Path)
		t.saveBtn.Disable()
	default:
		t.status.SetText(fmt.Sprintf("%d unsaved change(s) to %s", n, t.file.Target))
		t.saveBtn.Enable()
	}
}

func (t *sandboxTab) showMod() {
	if t.sel < 0 || t.sel >= len(t.mods) {
		return
	}
	mo := t.mods[t.sel]
	var rows []fyne.CanvasObject
	head := widget.NewLabelWithStyle(mo.ModName, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	rows = append(rows, head)
	page := ""
	for _, o := range mo.Options {
		if o.Page != page {
			page = o.Page
			if p := mo.Text.Page(page); p != "" && p != mo.ModName {
				rows = append(rows, widget.NewSeparator(), widget.NewLabelWithStyle(p, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
			}
		}
		rows = append(rows, t.optionRow(mo, o))
	}
	t.form.Objects = rows
	t.form.Refresh()
	t.scroll.ScrollToTop()
}

func (t *sandboxTab) optionRow(mo sandbox.ModOptions, o sandbox.Option) fyne.CanvasObject {
	label := widget.NewLabel(mo.Text.Label(o))
	label.Wrapping = fyne.TextWrapWord
	cur, inFile := t.value(o)
	info := widget.NewLabel("")
	info.Importance = widget.LowImportance
	info.Wrapping = fyne.TextWrapWord
	var control fyne.CanvasObject
	var setUI func(string)
	defText := o.Default
	switch o.Type {
	case "boolean":
		c := widget.NewCheck("", func(b bool) { t.set(o, fmt.Sprint(b)) })
		c.Checked = cur == "true"
		setUI = func(v string) { c.SetChecked(v == "true") }
		control = c
	case "enum":
		vals := mo.Text.EnumValues(o)
		s := widget.NewSelect(vals, nil)
		idx := func(v string) int {
			var n int
			fmt.Sscan(v, &n)
			return n - 1
		}
		if i := idx(cur); i >= 0 && i < len(vals) {
			s.Selected = vals[i]
		}
		s.OnChanged = func(string) { t.set(o, fmt.Sprint(s.SelectedIndex()+1)) }
		setUI = func(v string) {
			if i := idx(v); i >= 0 && i < len(vals) {
				s.SetSelectedIndex(i)
			}
		}
		if i := idx(o.Default); i >= 0 && i < len(vals) {
			defText = vals[i]
		}
		control = s
	default:
		e := widget.NewEntry()
		e.SetText(cur)
		e.Validator = func(s string) error {
			if !o.Valid(s) {
				return errors.New("not valid")
			}
			return nil
		}
		e.OnChanged = func(s string) {
			if o.Valid(s) {
				t.set(o, s)
			}
		}
		setUI = func(v string) { e.SetText(v) }
		control = e
	}
	parts := []string{}
	if o.Min != "" || o.Max != "" {
		parts = append(parts, fmt.Sprintf("Range %s to %s", cmp.Or(o.Min, "?"), cmp.Or(o.Max, "?")))
	}
	if defText == "" {
		defText = "(empty)"
	}
	parts = append(parts, "Default: "+defText)
	if !inFile {
		parts = append(parts, "not in this file yet: the game uses the default, saving writes it")
	}
	info.SetText(strings.Join(parts, "  ·  "))
	reset := widget.NewButtonWithIcon("", theme.ContentUndoIcon(), func() { setUI(o.Default) })
	var body []fyne.CanvasObject
	body = append(body, label, container.NewBorder(nil, nil, nil, reset, control), info)
	if tip := mo.Text.Tooltip(o); tip != "" {
		tl := widget.NewLabel(tip)
		tl.Wrapping = fyne.TextWrapWord
		tl.Importance = widget.LowImportance
		body = append(body, tl)
	}
	return container.NewVBox(append(body, widget.NewSeparator())...)
}

func (t *sandboxTab) save() {
	if t.file == nil || len(t.edits) == 0 {
		return
	}
	if install.GameRunning() {
		dialog.ShowError(errors.New("Project Zomboid is running: close it first, the game rewrites these files"), t.u.win)
		return
	}
	labels := map[string]string{}
	for _, mo := range t.mods {
		for _, o := range mo.Options {
			labels[o.Name] = mo.ModName + ": " + mo.Text.Label(o)
		}
	}
	names := make([]string, 0, len(t.edits))
	for n := range t.edits {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return labels[names[i]] < labels[names[j]] })
	var b strings.Builder
	for _, n := range names {
		old, ok := t.file.Value(n)
		if !ok {
			old = "(not set)"
		}
		fmt.Fprintf(&b, "%s\n    %s  ->  %s\n", labels[n], old, t.edits[n])
	}
	lbl := widget.NewLabel(b.String())
	lbl.Wrapping = fyne.TextWrapWord
	sc := container.NewVScroll(lbl)
	sc.SetMinSize(fyne.NewSize(560, 300))
	dialog.ShowCustomConfirm("Save to "+t.file.Target.String()+"?", "Save", "Cancel", sc, func(ok bool) {
		if !ok {
			return
		}
		bak, err := t.file.Save(t.edits, t.opts)
		if err != nil {
			dialog.ShowError(err, t.u.win)
			return
		}
		n := len(t.edits)
		t.edits = map[string]string{}
		t.changed()
		t.showMod()
		t.status.SetText(fmt.Sprintf("Saved %d change(s). Backup: %s", n, bak))
	}, t.u.win)
}
