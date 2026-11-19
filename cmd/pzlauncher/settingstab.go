package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/kata/pzlauncher/internal/gamecfg"
)

var flagHelp = map[string]string{
	"-XX:+UseCompactObjectHeaders":      "smaller Java objects",
	"-XX:+UseStringDeduplication":       "shares identical text in memory",
	"-XX:+UnlockExperimentalVMOptions":  "needed by the Shenandoah delay below",
	"-XX:ShenandoahUncommitDelay=30000": "gives memory back faster (Shenandoah GC)",
	"-XX:ZUncommitDelay=30":             "gives memory back faster (ZGC)",
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
	ramLabel := widget.NewLabel(ramText + fmt.Sprintf(". Choose %d-%d GB; 8 GB (the pack's 8192m) is recommended for big modpacks.", lo, hi))
	ramLabel.Wrapping = fyne.TextWrapWord
	curLabel := widget.NewLabel("")
	curLabel.Wrapping = fyne.TextWrapWord

	var xmxOpts []string
	xmxVal := map[string]int{}
	for gb := lo; gb <= hi; gb++ {
		l := gbLabel(gb)
		xmxOpts = append(xmxOpts, l)
		xmxVal[l] = gb * 1024
	}
	xmxSel := widget.NewSelect(xmxOpts, nil)

	softLabel := widget.NewLabel("")
	flagChecks := map[string]*widget.Check{}
	flagBox := container.NewVBox()
	for _, f := range gamecfg.PackFlags() {
		text := f
		if strings.HasPrefix(f, "-XX:SoftMaxHeapSize=") {
			text = "-XX:SoftMaxHeapSize (keeps the heap small; set below the memory limit)"
		} else if h := flagHelp[f]; h != "" {
			text += " - " + h
		}
		c := widget.NewCheck(text, nil)
		flagChecks[f] = c
		flagBox.Add(c)
	}
	argsLabel := widget.NewLabel("")
	argsLabel.TextStyle = fyne.TextStyle{Monospace: true}
	argsLabel.Wrapping = fyne.TextWrapBreak

	var info gamecfg.JVMInfo
	jvmOK := false

	loadJVM := func() {
		jvmOK = false
		if env.GameDir == "" {
			curLabel.SetText("Project Zomboid install not found.")
			argsLabel.SetText("")
			return
		}
		i, err := env.LoadJVM()
		if err != nil {
			curLabel.SetText("Could not read " + env.JSONPath() + ": " + err.Error())
			argsLabel.SetText("")
			return
		}
		info, jvmOK = i, true
		cur := "none (Java default)"
		if i.Xmx != "" {
			cur = fmt.Sprintf("%s (%.1f GB)", i.Xmx, float64(i.XmxMB)/1024)
		}
		curLabel.SetText("Current memory: " + cur + "  |  GC: " + i.GC + "\n" + env.JSONPath())
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
			softLabel.SetText("SoftMaxHeapSize now: " + strings.TrimPrefix(i.SoftMax, "-XX:SoftMaxHeapSize="))
		} else {
			softLabel.SetText("")
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
	updNote := widget.NewLabel("A Steam game update can reset ProjectZomboid64.json. Your last saved memory settings are remembered; use the button to put them back.")
	updNote.Wrapping = fyne.TextWrapWord
	updNote.Importance = widget.LowImportance

	memCard := widget.NewCard("Memory", "", container.NewVBox(ramLabel, curLabel, xmxSel))
	javaCard := widget.NewCard("Java options", "Memory-saving flags from the pack; other entries are never changed",
		container.NewVBox(flagBox, softLabel, widget.NewAccordion(widget.NewAccordionItem("All Java options (read-only)", argsLabel)),
			container.NewHBox(saveJVM, reapply), updNote))

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
	resRow := widget.NewFormItem("Resolution", container.NewGridWithColumns(3, widthE, widget.NewLabel("x"), heightE))
	fpsRow := widget.NewFormItem("Frame-rate limit", fpsSel)
	fontRow := widget.NewFormItem("UI font size", fontSel)
	form := widget.NewForm(modeRow, resRow, fpsRow, fontRow)
	optPath := widget.NewLabel("")
	optPath.Importance = widget.LowImportance

	var gopts gamecfg.GameOptions
	optsOK := false
	loadOpts := func() {
		optsOK = false
		if env.ZomboidDir == "" {
			optPath.SetText("Zomboid folder not found.")
			return
		}
		_, o, err := env.LoadOptions()
		if err != nil {
			optPath.SetText("Could not read " + env.OptionsPath() + ": " + err.Error() + " (start the game once to create it)")
			return
		}
		gopts, optsOK = o, true
		optPath.SetText(env.OptionsPath())
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
	gameCard := widget.NewCard("Game options", "From options.ini; every other line is kept as is",
		container.NewVBox(form, vsync, volBox, optPath, saveOpts))

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
	debugNote := widget.NewLabel("Needs ZombieBuddy and the launcher's helper mod")
	debugNote.Importance = widget.LowImportance
	debugCard := widget.NewCard("Debug", "", container.NewVBox(debugCheck, debugNote))

	reload := widget.NewButton("Reload", func() {
		loadJVM()
		loadOpts()
		status.SetText("Reloaded.")
	})
	safety := widget.NewLabel("Settings can only be saved while the game is closed. Each file is backed up once a day before the first change (<file>.launcher-bak-YYYYMMDD).")
	safety.Wrapping = fyne.TextWrapWord
	safety.Importance = widget.LowImportance

	loadJVM()
	loadOpts()
	if len(envErrs) > 0 {
		var msgs []string
		for _, e := range envErrs {
			msgs = append(msgs, e.Error())
		}
		status.SetText(strings.Join(msgs, "\n"))
	}

	body := container.NewVBox(memCard, javaCard, gameCard, debugCard, u.musicCard(), safety)
	return container.NewBorder(container.NewBorder(nil, nil, nil, reload, status), nil, nil, nil, container.NewVScroll(body))
}
