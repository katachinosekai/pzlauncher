package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/katachinosekai/pzlauncher/internal/gamecfg"
	"github.com/katachinosekai/pzlauncher/internal/install"
	"github.com/katachinosekai/pzlauncher/internal/paths"
	"github.com/katachinosekai/pzlauncher/internal/zbsetup"
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

func newMuted(s string, _ bool) *mutedText {
	seg := &widget.TextSegment{Text: s, Style: widget.RichTextStyle{ColorName: theme.ColorNamePlaceHolder}}
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
	return container.New(layout.NewCustomPaddedLayout(-theme.Padding()*2, 0, theme.IconInlineSize()+theme.InnerPadding()*2, 0), o)
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
	switch {
	case gb == gamecfg.DefaultXmxGB && gb == gamecfg.GameDefaultXmxGB:
		s += " (game default)"
	case gb == gamecfg.DefaultXmxGB:
		s += " (default)"
	case gb == gamecfg.GameDefaultXmxGB:
		s += " (game default)"
	}
	return s
}

const (
	softOff        = "Off"
	prefCustomJSON = "settings.customJSON"
)

func softOptions(xmxMB int) []string {
	opts := []string{softOff}
	for gb := 1; gb*1024 < xmxMB; gb++ {
		opts = append(opts, fmt.Sprintf("%d GB", gb))
	}
	return opts
}

func softMB(label string) int {
	var gb int
	if _, err := fmt.Sscanf(label, "%d GB", &gb); err != nil {
		return 0
	}
	return gb * 1024
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
	freeLabel := widget.NewLabel("Free RAM right now: measuring...")
	var samples []int
	sample := func() {
		mb := gamecfg.FreeRAMMB()
		if mb <= 0 {
			freeLabel.SetText("Free RAM right now: unknown")
			return
		}
		samples = append(samples, mb)
		if len(samples) > 12 {
			samples = samples[len(samples)-12:]
		}
		sum := 0
		for _, v := range samples {
			sum += v
		}
		gb := int(float64(sum)/float64(len(samples))/1024 + 0.5)
		freeLabel.SetText(fmt.Sprintf("Free RAM right now: about %d GB (average of the last minute)", gb))
	}
	sample()
	ramTicker := time.NewTicker(5 * time.Second)
	go func() {
		for range ramTicker.C {
			fyne.Do(sample)
		}
	}()
	u.closers = append(u.closers, ramTicker.Stop)
	curLabel := widget.NewLabel("")
	curLabel.Wrapping = fyne.TextWrapWord
	curDetail := newMuted("", true)
	env.JSONFile = u.app.Preferences().String(prefCustomJSON)
	jsonName := widget.NewLabel("")
	jsonPath := newMuted("", true)
	var loadJVM func()
	openJSON := widget.NewButtonWithIcon("Open folder", theme.FolderOpenIcon(), func() { u.openFolder(filepath.Dir(env.JSONPath())) })
	autoJSON := widget.NewButton("Use automatic", func() {
		u.app.Preferences().SetString(prefCustomJSON, "")
		env.JSONFile = ""
		loadJVM()
	})
	customJSON := widget.NewButton("Use a custom file...", func() {
		d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
			if err != nil || r == nil {
				return
			}
			r.Close()
			p := r.URI().Path()
			u.app.Preferences().SetString(prefCustomJSON, p)
			env.JSONFile = p
			loadJVM()
		}, u.win)
		d.SetFilter(storage.NewExtensionFileFilter([]string{".json"}))
		d.Show()
	})
	jsonButtons := container.NewHBox(customJSON, openJSON, autoJSON)
	jsonRow := container.NewBorder(nil, nil, nil, container.NewCenter(jsonButtons), container.NewVBox(jsonName, tighten(jsonPath.rt)))

	var xmxOpts []string
	xmxVal := map[string]int{}
	for gb := lo; gb <= hi; gb++ {
		l := gbLabel(gb)
		xmxOpts = append(xmxOpts, l)
		xmxVal[l] = gb * 1024
	}
	xmxSel := widget.NewSelect(xmxOpts, nil)
	softSel := widget.NewSelect([]string{softOff}, nil)
	softSel.SetSelected(softOff)
	xmxSel.OnChanged = func(l string) {
		mb := xmxVal[l]
		if mb == 0 {
			return
		}
		cur := softSel.Selected
		softSel.Options = softOptions(mb)
		if softMB(cur) >= mb || cur == "" {
			cur = softOff
		}
		softSel.SetSelected(cur)
		softSel.Refresh()
	}
	softNote := newMuted("Soft limit: Java tries to keep the heap below this and gives memory back sooner; the memory limit stays the hard cap.", false)

	flagChecks := map[string]*widget.Check{}
	flagBox := container.NewVBox()
	for _, f := range gamecfg.PackFlags() {
		if strings.HasPrefix(f, softMaxPrefix) {
			continue
		}
		text, raw := f, newMuted(f, true)
		if h := flagHelp[f]; h != "" {
			text = h
		}
		c := widget.NewCheck(text, nil)
		flagChecks[f] = c
		flagBox.Add(container.New(layout.NewCustomPaddedLayout(2, 8, 0, 0), container.NewVBox(c, subLine(raw.rt))))
	}
	// MALLOC_ARENA_MAX=2 lives in Steam's launch options (the launcher starts
	// the game through Steam), so it is Linux-only and saved separately.
	var mallocCheck *widget.Check
	var mallocSub *mutedText
	mallocOn, mallocOK := false, false
	if runtime.GOOS == "linux" {
		mallocCheck = widget.NewCheck("Limit memory pools (about 1 GB less RAM, no FPS cost)", nil)
		mallocSub = newMuted(zbsetup.MallocToken+" (Steam launch options)", true)
		flagBox.Add(container.New(layout.NewCustomPaddedLayout(2, 8, 0, 0), container.NewVBox(mallocCheck, subLine(mallocSub.rt))))
	}
	loadMalloc := func() {
		if mallocCheck == nil {
			return
		}
		mallocOK = false
		var files, vals []string
		if root, err := paths.SteamRoot(); err == nil {
			files = install.LaunchFiles(root)
		}
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			if v, err := zbsetup.ReadLaunchOptions(string(b)); err == nil {
				vals = append(vals, v...)
			}
		}
		if len(files) == 0 {
			mallocCheck.SetChecked(false)
			mallocCheck.Disable()
			mallocSub.SetText(zbsetup.MallocToken + " (Steam's launch options were not found)")
			return
		}
		on, other := zbsetup.MallocStatus(vals)
		if other != "" {
			mallocCheck.SetChecked(true)
			mallocCheck.Disable()
			mallocSub.SetText(zbsetup.MallocVar + "=" + other + " set by you in Steam's launch options; the launcher leaves it alone")
			return
		}
		mallocOn, mallocOK = on, true
		mallocCheck.SetChecked(on)
		mallocCheck.Enable()
		mallocSub.SetText(zbsetup.MallocToken + " (Steam launch options)")
	}
	argsLabel := widget.NewLabel("")
	argsLabel.TextStyle = fyne.TextStyle{Monospace: true}
	argsLabel.Wrapping = fyne.TextWrapBreak

	var info gamecfg.JVMInfo
	jvmOK := false

	loadJVM = func() {
		jvmOK = false
		curDetail.SetText("")
		if env.GameDir == "" && env.JSONFile == "" {
			curLabel.SetText("Project Zomboid install not found.")
			jsonName.SetText("ProjectZomboid64.json not found automatically")
			jsonPath.SetText("")
			customJSON.Show()
			openJSON.Hide()
			autoJSON.Hide()
			argsLabel.SetText("")
			return
		}
		jsonPath.SetText(shortPath(env.JSONPath()))
		if env.JSONFile != "" {
			jsonName.SetText("Custom ProjectZomboid64.json")
			customJSON.Hide()
			openJSON.Show()
			autoJSON.Show()
		} else {
			jsonName.SetText("ProjectZomboid64.json (found automatically)")
			customJSON.Show()
			openJSON.Hide()
			autoJSON.Hide()
		}
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
			curDetail.SetText("Set in ProjectZomboid64.json as " + i.Xmx)
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
		softSel.Options = softOptions(i.XmxMB)
		soft := softOff
		if i.Flags[gamecfg.SoftMaxFlag()] && i.SoftMaxMB > 0 && i.SoftMaxMB%1024 == 0 && i.SoftMaxMB < i.XmxMB {
			soft = fmt.Sprintf("%d GB", i.SoftMaxMB/1024)
		}
		softSel.SetSelected(soft)
		argsLabel.SetText(strings.Join(i.Args, "\n"))
	}

	var applyMalloc func(on bool, msg string)
	// rememberMalloc saves the checkbox as the user's choice and returns true
	// when Steam's launch options have to change (applyMalloc then reports).
	rememberMalloc := func(msg string) bool {
		if mallocCheck == nil || !mallocOK {
			return false
		}
		on := mallocCheck.Checked
		if err := env.SetMallocChoice(on); err != nil {
			msg += "\ncould not remember the memory-pool choice: " + err.Error()
		}
		if on == mallocOn {
			status.SetText(msg)
			return true
		}
		applyMalloc(on, msg)
		return true
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
		if sm := softMB(softSel.Selected); sm > 0 && sm < mb {
			w.Flags[gamecfg.SoftMaxFlag()] = true
			w.SoftMaxMB = sm
		} else {
			w.Flags[gamecfg.SoftMaxFlag()] = false
			w.SoftMaxMB = 0
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
		if rememberMalloc(msg) {
			return
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
		if mallocCheck != nil && mallocOK {
			if on, set := env.MallocChoice(); set && on != mallocOn {
				applyMalloc(on, msg)
				return
			}
		}
		status.SetText(msg)
	})
	applyMalloc = func(on bool, msg string) {
		saveJVM.Disable()
		reapply.Disable()
		status.SetText(msg + "\nChanging Steam's launch options (Steam is closed and started again)...")
		go func() {
			r, err := install.MallocArena(u.ctx, install.Options{
				PackDir:           zbWorkDir(),
				SetMallocArena:    on,
				RemoveMallocArena: !on,
			})
			fyne.Do(func() {
				saveJVM.Enable()
				reapply.Enable()
				loadMalloc()
				switch {
				case err != nil:
					msg += "\nSteam's launch options were not changed: " + err.Error()
				case len(r.Changes) == 0 && len(r.Warnings) == 0:
					msg += "\nSteam's launch options already as chosen."
				case len(r.Changes) > 0:
					word := "added to"
					if !on {
						word = "removed from"
					}
					msg += "\n" + zbsetup.MallocToken + " " + word + " Steam's launch options"
					if r.BackupDir != "" {
						msg += " (backup: " + r.BackupDir + ")"
					}
					msg += "."
				}
				for _, w := range r.Warnings {
					msg += "\nWarning: " + w
				}
				status.SetText(msg)
			})
		}()
	}
	updNote := newMuted("A Steam game update can reset ProjectZomboid64.json. Your last saved memory settings are remembered; use \"Re-apply\" to put them back.", false)

	memCard := settingsSection("Memory", "",
		curLabel, tighten(curDetail.rt),
		container.NewGridWithColumns(2,
			container.NewBorder(nil, nil, widget.NewLabel("Memory limit"), nil, xmxSel),
			container.NewBorder(nil, nil, widget.NewLabel("Soft limit"), nil, softSel)),
		tighten(softNote.rt),
		freeLabel, jsonRow)
	javaSub := "Memory-saving options from the pack. Other entries in ProjectZomboid64.json are never changed."
	if mallocCheck != nil {
		javaSub += " The memory-pool limit is kept in Steam's launch options instead; changing it closes and restarts Steam."
	}
	javaCard := settingsSection("Java options", javaSub,
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
	optName := widget.NewLabel("Where your options.ini file lives")
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
		loadMalloc()
		loadOpts()
		status.SetText("Reloaded.")
	})
	safety := newMuted("Settings can only be saved while the game is closed. Each file is backed up once a day before the first change (<file>.launcher-bak-YYYYMMDD).", false)

	loadJVM()
	loadMalloc()
	loadOpts()
	if len(envErrs) > 0 {
		var msgs []string
		for _, e := range envErrs {
			msgs = append(msgs, e.Error())
		}
		status.SetText(strings.Join(msgs, "\n"))
	}

	body := container.New(layout.NewCustomPaddedLayout(0, 24, 0, 6), container.NewVBox(memCard, javaCard, gameCard, debugCard, safety.rt))
	return container.NewBorder(container.NewBorder(nil, nil, nil, reload, status), nil, nil, nil, container.NewVScroll(body))
}
