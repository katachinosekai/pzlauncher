package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/kata/pzlauncher/internal/gamecfg"
)

var flagHelp = map[string]string{
	"-XX:+UseCompactObjectHeaders":      "Smaller Java objects",
	"-XX:+UseStringDeduplication":       "Share identical text in memory",
	"-XX:+UnlockExperimentalVMOptions":  "Allow experimental options (needed by the Shenandoah delay below)",
	"-XX:ShenandoahUncommitDelay=30000": "Give unused memory back faster (Shenandoah)",
	"-XX:ZUncommitDelay=30":             "Give unused memory back faster (ZGC)",
}

const softMaxPrefix = "-XX:SoftMaxHeapSize="

type mutedText struct {
	rt  *widget.RichText
	seg *widget.TextSegment
}

func newMuted(s string, small bool) *mutedText {
	seg := &widget.TextSegment{Text: s, Style: widget.RichTextStyle{ColorName: theme.ColorNamePlaceHolder}}
	if small {
		seg.Style.SizeName = theme.SizeNameCaptionText
	}
	rt := widget.NewRichText(seg)
	rt.Wrapping = fyne.TextWrapWord
	return &mutedText{rt: rt, seg: seg}
}

func (m *mutedText) SetText(s string) {
	m.seg.Text = s
	m.rt.Refresh()
}

func settingsSection(title, sub string, objs ...fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	bg.CornerRadius = 10
	head := widget.NewLabel(title)
	head.TextStyle = fyne.TextStyle{Bold: true}
	head.SizeName = theme.SizeNameSubHeadingText
	items := []fyne.CanvasObject{head}
	if sub != "" {
		items = append(items, tighten(newMuted(sub, false).rt))
	}
	items = append(items, objs...)
	inner := container.New(layout.NewCustomPaddedLayout(4, 10, 8, 8), container.NewVBox(items...))
	return container.New(layout.NewCustomPaddedLayout(8, 8, 0, 0), container.NewStack(bg, inner))
}

func tighten(o fyne.CanvasObject) fyne.CanvasObject {
	return container.New(layout.NewCustomPaddedLayout(-theme.Padding()*2, 0, 0, 0), o)
}

func subLine(o fyne.CanvasObject) fyne.CanvasObject {
	return container.New(layout.NewCustomPaddedLayout(-theme.Padding()*3, 0, theme.IconInlineSize()+theme.InnerPadding(), 0), o)
}

func shortPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			p = filepath.Join("~", rel)
		}
	}
	r := []rune(p)
	if len(r) > 80 {
		p = string(r[:24]) + "…" + string(r[len(r)-55:])
	}
	return p
}

func memText(mb int) string {
	if mb%1024 == 0 {
		return fmt.Sprintf("%d GB", mb/1024)
	}
	return fmt.Sprintf("%.1f GB", float64(mb)/1024)
}

func gbLabel(gb int) string {
	s := fmt.Sprintf("%d GB", gb)
	if gb == gamecfg.RecommendedXmxGB {
		s += " (recommended for big modpacks)"
	}
	return s
}

func (u *ui) settingsTab() fyne.CanvasObject {
	env, envErrs := gamecfg.DefaultEnv()
	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord

	showErr := func(err error) {
		if errors.Is(err, gamecfg.ErrGameRunning) {
			dialog.ShowInformation("Game is running", err.Error(), u.win)
			return
		}
		dialog.ShowError(err, u.win)
	}

	ramMB := gamecfg.TotalRAMMB()
	lo, hi := gamecfg.XmxRangeGB(ramMB)
	ramText := "System RAM: unknown"
	if ramMB > 0 {
		ramText = fmt.Sprintf("System RAM: %.1f GB", float64(ramMB)/1024)
	}
	ramLabel := newMuted(ramText+fmt.Sprintf(" · choose %d-%d GB · 8 GB (the pack's 8192m) is recommended for big modpacks", lo, hi), false)
	curLabel := widget.NewLabel("")
	curLabel.TextStyle = fyne.TextStyle{Bold: true}
	curLabel.Wrapping = fyne.TextWrapWord
	curDetail := newMuted("", true)
	jsonName := widget.NewLabel("Game file: ProjectZomboid64.json")
	jsonPath := newMuted("", true)
	openJSON := widget.NewButtonWithIcon("Open folder", theme.FolderOpenIcon(), func() { u.openFolder(filepath.Dir(env.JSONPath())) })
	jsonRow := container.NewBorder(nil, nil, nil, container.NewCenter(openJSON), container.NewVBox(jsonName, tighten(jsonPath.rt)))

	var xmxOpts []string
	xmxVal := map[string]int{}
	for gb := lo; gb <= hi; gb++ {
		l := gbLabel(gb)
		xmxOpts = append(xmxOpts, l)
		xmxVal[l] = gb * 1024
	}
	xmxSel := widget.NewSelect(xmxOpts, nil)

	softLabel := newMuted(strings.TrimSuffix(softMaxPrefix, "="), true)
	flagChecks := map[string]*widget.Check{}
	flagBox := container.NewVBox()
	for _, f := range gamecfg.PackFlags() {
		text, raw := f, newMuted(f, true)
		if strings.HasPrefix(f, softMaxPrefix) {
			text, raw = "Keep the heap small (soft limit, set below the memory limit)", softLabel
		} else if h := flagHelp[f]; h != "" {
			text = h
		}
		c := widget.NewCheck(text, nil)
		flagChecks[f] = c
		flagBox.Add(container.NewVBox(c, subLine(raw.rt)))
	}
	argsLabel := widget.NewLabel("")
	argsLabel.TextStyle = fyne.TextStyle{Monospace: true}
	argsLabel.Wrapping = fyne.TextWrapBreak

	var info gamecfg.JVMInfo
	jvmOK := false

	loadJVM := func() {
		jvmOK = false
		curDetail.SetText("")
		if env.GameDir == "" {
			curLabel.SetText("Project Zomboid install not found.")
			jsonRow.Hide()
			argsLabel.SetText("")
			return
		}
		jsonPath.SetText(shortPath(env.JSONPath()))
		jsonRow.Show()
		i, err := env.LoadJVM()
		if err != nil {
			curLabel.SetText("Could not read ProjectZomboid64.json")
			curDetail.SetText(err.Error())
			argsLabel.SetText("")
			return
		}
		info, jvmOK = i, true
		cur := "Java default"
		if i.Xmx != "" {
			cur = memText(i.XmxMB)
			curDetail.SetText("Set in the game file as " + i.Xmx)
		}
		curLabel.SetText("Memory: " + cur + "  ·  Garbage collector: " + i.GC)
		sel := ""
		if i.XmxMB > 0 && i.XmxMB%1024 == 0 {
			if l := gbLabel(i.XmxMB / 1024); xmxVal[l] != 0 {
				sel = l
			}
		}
		if sel == "" && i.XmxMB > 0 {
			sel = fmt.Sprintf("current: %s", i.Xmx)
			if _, ok := xmxVal[sel]; !ok {
				xmxSel.Options = append([]string{sel}, xmxOpts...)
				xmxVal[sel] = i.XmxMB
			}
		}
		xmxSel.SetSelected(sel)
		for f, c := range flagChecks {
			c.SetChecked(i.Flags[f])
		}
		if i.SoftMax != "" {
			softLabel.SetText(i.SoftMax)
		} else {
			softLabel.SetText(strings.TrimSuffix(softMaxPrefix, "="))
		}
		argsLabel.SetText(strings.Join(i.Args, "\n"))
	}

	saveJVM := widget.NewButton("Save memory & Java options", func() {
		if !jvmOK {
			showErr(errors.New("ProjectZomboid64.json could not be read"))
			return
		}
		mb := xmxVal[xmxSel.Selected]
		if mb == 0 {
			showErr(errors.New("pick a memory size first"))
			return
		}
		w := info.Want()
		w.XmxMB = mb
		for f, c := range flagChecks {
			w.Flags[f] = c.Checked
		}
		changed, notes, err := env.SaveJVM(w)
		if err != nil {
			showErr(err)
			return
		}
		loadJVM()
		msg := "Memory settings already as chosen (remembered for re-apply)."
		if changed {
			msg = "Saved ProjectZomboid64.json (backup: " + gamecfg.BackupPath(env.JSONPath(), env.Now()) + ")."
		}
		if len(notes) > 0 {
			msg += "\n" + strings.Join(notes, "\n")
		}
		status.SetText(msg)
	})
	saveJVM.Importance = widget.HighImportance

	reapply := widget.NewButton("Re-apply my memory settings", func() {
		changed, notes, err := env.Reapply()
		if err != nil {
			showErr(err)
			return
		}
		loadJVM()
		msg := "Remembered memory settings are already in place."
		if changed {
			msg = "Re-applied your remembered memory settings."
		}
		if len(notes) > 0 {
			msg += "\n" + strings.Join(notes, "\n")
		}
		status.SetText(msg)
	})
	updNote := newMuted("A Steam game update can reset ProjectZomboid64.json. Your last saved memory settings are remembered; use \"Re-apply\" to put them back.", false)

	memCard := settingsSection("Memory", "",
		curLabel, tighten(curDetail.rt),
		container.NewBorder(nil, nil, widget.NewLabel("Memory limit"), nil, xmxSel),
		ramLabel.rt, jsonRow)
	javaCard := settingsSection("Java options", "Memory-saving options from the pack. Other entries in the game file are never changed.",
		flagBox, widget.NewAccordion(widget.NewAccordionItem("All Java options (read-only)", argsLabel)),
		container.NewHBox(saveJVM, reapply), updNote.rt)

	modeSel := widget.NewSelect([]string{gamecfg.ModeFullscreen, gamecfg.ModeBorderless, gamecfg.ModeWindowed}, nil)
	widthE := widget.NewEntry()
	heightE := widget.NewEntry()
	vsync := widget.NewCheck("VSync", nil)
	const uncapped = "Uncapped"
	fpsSel := widget.NewSelect(nil, nil)
	fontSel := widget.NewSelect(gamecfg.FontSizes, nil)
	volSliders := map[string]*widget.Slider{}
	volBox := container.NewVBox()
	volRows := map[string]fyne.CanvasObject{}
	for _, v := range gamecfg.Volumes {
		s := widget.NewSlider(0, 10)
		s.Step = 1
		val := widget.NewLabel("")
		s.OnChanged = func(f float64) { val.SetText(strconv.Itoa(int(f))) }
		volSliders[v.Key] = s
		row := container.NewBorder(nil, nil, widget.NewLabel(v.Label), val, s)
		volRows[v.Key] = row
		volBox.Add(row)
	}
	modeRow := widget.NewFormItem("Display mode", modeSel)
	resRow := widget.NewFormItem("Resolution", container.NewGridWithColumns(2, container.NewBorder(nil, nil, nil, widget.NewLabel("x"), widthE), heightE))
	fpsRow := widget.NewFormItem("Frame-rate limit", fpsSel)
	fontRow := widget.NewFormItem("UI font size", fontSel)
	form := widget.NewForm(modeRow, resRow, fpsRow, fontRow)
	optPath := newMuted("", true)
	optErr := newMuted("", false)
	optName := widget.NewLabel("Game file: options.ini")
	openOpts := widget.NewButtonWithIcon("Open folder", theme.FolderOpenIcon(), func() { u.openFolder(filepath.Dir(env.OptionsPath())) })
	optRow := container.NewBorder(nil, nil, nil, container.NewCenter(openOpts), container.NewVBox(optName, tighten(optPath.rt)))

	var gopts gamecfg.GameOptions
	optsOK := false
	loadOpts := func() {
		optsOK = false
		optErr.SetText("")
		optErr.rt.Hide()
		if env.ZomboidDir == "" {
			optErr.SetText("Zomboid folder not found.")
			optErr.rt.Show()
			optRow.Hide()
			return
		}
		optRow.Show()
		optPath.SetText(shortPath(env.OptionsPath()))
		_, o, err := env.LoadOptions()
		if err != nil {
			optErr.SetText("Could not read options.ini: " + err.Error() + " (start the game once to create it)")
			optErr.rt.Show()
			return
		}
		gopts, optsOK = o, true
		modeSel.SetSelected(o.Mode)
		widthE.SetText(strconv.Itoa(o.Width))
		heightE.SetText(strconv.Itoa(o.Height))
		vsync.SetChecked(o.VSync)
		var fps []string
		if o.Present[gamecfg.KeyUncappedFPS] {
			fps = append(fps, uncapped)
		}
		found := false
		for _, r := range gamecfg.FrameRates {
			fps = append(fps, strconv.Itoa(r))
			found = found || r == o.FrameRate
		}
		if !found && o.FrameRate > 0 {
			fps = append(fps, strconv.Itoa(o.FrameRate))
		}
		fpsSel.Options = fps
		if o.Uncapped {
			fpsSel.SetSelected(uncapped)
		} else {
			fpsSel.SetSelected(strconv.Itoa(o.FrameRate))
		}
		if o.FontSize >= 1 && o.FontSize <= len(gamecfg.FontSizes) {
			fontSel.SetSelected(gamecfg.FontSizes[o.FontSize-1])
		}
		show := func(w fyne.CanvasObject, on bool) {
			if on {
				w.Show()
			} else {
				w.Hide()
			}
		}
		show(vsync, o.Present[gamecfg.KeyVSync])
		for k, s := range volSliders {
			s.SetValue(float64(o.Volumes[k]))
			s.OnChanged(s.Value)
			show(volRows[k], o.Present[k])
		}
	}

	saveOpts := widget.NewButton("Save game options", func() {
		if !optsOK {
			showErr(errors.New("options.ini could not be read"))
			return
		}
		o := gopts
		o.Volumes = map[string]int{}
		for k, v := range gopts.Volumes {
			o.Volumes[k] = v
		}
		o.Mode = modeSel.Selected
		var err error
		if o.Width, err = strconv.Atoi(strings.TrimSpace(widthE.Text)); err != nil {
			showErr(errors.New("width must be a number"))
			return
		}
		if o.Height, err = strconv.Atoi(strings.TrimSpace(heightE.Text)); err != nil {
			showErr(errors.New("height must be a number"))
			return
		}
		o.VSync = vsync.Checked
		if fpsSel.Selected == uncapped {
			o.Uncapped = true
		} else if n, err := strconv.Atoi(fpsSel.Selected); err == nil {
			o.Uncapped = false
			o.FrameRate = n
		}
		for i, f := range gamecfg.FontSizes {
			if fontSel.Selected == f {
				o.FontSize = i + 1
			}
		}
		for k, s := range volSliders {
			o.Volumes[k] = int(s.Value)
		}
		n, err := env.SaveOptions(o)
		if err != nil {
			showErr(err)
			return
		}
		loadOpts()
		if n == 0 {
			status.SetText("Game options unchanged.")
		} else {
			status.SetText(fmt.Sprintf("Saved %d game option(s) to options.ini (backup: %s).", n, gamecfg.BackupPath(env.OptionsPath(), env.Now())))
		}
	})
	saveOpts.Importance = widget.HighImportance
	gameCard := settingsSection("Game options", "Read from the game's options.ini. Every other line in the file is kept as is.",
		form, vsync, volBox, optErr.rt, optRow, container.NewHBox(saveOpts))

	debugCheck := widget.NewCheck("Launch with debug mode", nil)
	if on, err := env.Debug(); err == nil {
		debugCheck.SetChecked(on)
	}
	debugCheck.OnChanged = func(on bool) {
		if err := env.SetDebug(on); err != nil {
			showErr(err)
			return
		}
		if on {
			status.SetText("Debug mode on for the next launch.")
		} else {
			status.SetText("Debug mode off.")
		}
	}
	debugNote := newMuted("Needs ZombieBuddy and the launcher's helper mod.", true)
	debugCard := settingsSection("Debug", "", debugCheck, subLine(debugNote.rt))

	reload := widget.NewButton("Reload", func() {
		loadJVM()
		loadOpts()
		status.SetText("Reloaded.")
	})
	safety := newMuted("Settings can only be saved while the game is closed. Each file is backed up once a day before the first change (<file>.launcher-bak-YYYYMMDD).", false)

	loadJVM()
	loadOpts()
	if len(envErrs) > 0 {
		var msgs []string
		for _, e := range envErrs {
			msgs = append(msgs, e.Error())
		}
		status.SetText(strings.Join(msgs, "\n"))
	}

	body := container.New(layout.NewCustomPaddedLayout(0, 24, 0, 6), container.NewVBox(memCard, javaCard, gameCard, debugCard, u.musicCard(), safety.rt))
	return container.NewBorder(container.NewBorder(nil, nil, nil, reload, status), nil, nil, nil, container.NewVScroll(body))
}
