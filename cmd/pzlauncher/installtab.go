package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/katachinosekai/pzlauncher/internal/diag"
	"github.com/katachinosekai/pzlauncher/internal/modoptions"
	"github.com/katachinosekai/pzlauncher/internal/packs"
	"github.com/katachinosekai/pzlauncher/internal/paths"
	"github.com/katachinosekai/pzlauncher/internal/play"
	"github.com/katachinosekai/pzlauncher/internal/update"
)

const (
	maxLogLines  = 2000
	prefPackRepo = "packs.lastSource"
	srcLocal     = "Local file"
	srcGitHub    = "GitHub (release file)"
)

func packModRefs() []packs.ModRef {
	idx, err := play.ScanInstalled()
	if err != nil || idx == nil || idx.Index == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []packs.ModRef
	for _, m := range idx.Index.AllCopies() {
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		out = append(out, packs.ModRef{ID: m.ID, Name: m.Name, Requires: m.Requires()})
	}
	return out
}

func (u *ui) installTab() fyne.CanvasObject {
	installed := widget.NewLabel("")
	installedWhen := newMuted("", false)
	showInstalled := func() {
		st, err := update.LoadState()
		switch {
		case err != nil:
			installed.SetText("Unknown (" + err.Error() + ")")
			installedWhen.SetText("")
		case st.PackVersion == "":
			installed.SetText("No pack installed through the launcher yet.")
			installedWhen.SetText("")
		default:
			installed.SetText(st.PackVersion)
			installedWhen.SetText("Installed " + st.InstalledAt.Local().Format("2006-01-02 15:04"))
		}
	}
	showInstalled()
	installedCard := settingsSection("Installed pack", "", installed, tighten(installedWhen.rt))

	var lines []string
	logLabel := widget.NewLabel("")
	logLabel.Wrapping = fyne.TextWrapWord
	logLabel.TextStyle = fyne.TextStyle{Monospace: true}
	logScroll := container.NewVScroll(logLabel)
	logScroll.SetMinSize(fyne.NewSize(0, 220))
	appendLog := func(s string) {
		for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
			lines = append(lines, time.Now().Format("15:04:05 ")+l)
		}
		if len(lines) > maxLogLines {
			lines = lines[len(lines)-maxLogLines:]
		}
		logLabel.SetText(strings.Join(lines, "\n"))
		logScroll.ScrollToBottom()
	}
	logf := func(s string) { fyne.Do(func() { appendLog(s) }) }
	logAcc := widget.NewAccordion(widget.NewAccordionItem("Install log", logScroll))

	status := widget.NewLabel("Choose a pack, then press Install. Quit the game first.")
	status.Wrapping = fyne.TextWrapWord
	bar := widget.NewProgressBarInfinite()
	bar.Stop()
	bar.Hide()

	zipLabel := widget.NewLabel("No file chosen")
	zipLabel.Truncation = fyne.TextTruncateEllipsis
	var zipPath string
	repoEntry := widget.NewEntry()
	repoEntry.SetPlaceHolder("owner/repo, a GitHub release link, or an https link to a .zip")
	repoEntry.SetText(u.app.Preferences().String(prefPackRepo))
	repoNote := newMuted("Uses the .zip attached to the repo's latest release (or the release in the link). Public repos only.", false)

	var pick, install *widget.Button
	pick = widget.NewButtonWithIcon("Choose zip...", theme.FolderOpenIcon(), func() {
		d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
			if err != nil || r == nil {
				return
			}
			r.Close()
			zipPath = r.URI().Path()
			zipLabel.SetText(zipPath)
		}, u.win)
		d.SetFilter(storage.NewExtensionFileFilter([]string{".zip"}))
		if home, err := os.UserHomeDir(); err == nil {
			for _, sub := range []string{"Downloads", "Desktop"} {
				if l, err := storage.ListerForURI(storage.NewFileURI(filepath.Join(home, sub))); err == nil {
					d.SetLocation(l)
					break
				}
			}
		}
		d.Show()
	})
	localRow := container.NewBorder(nil, nil, pick, nil, zipLabel)
	githubRow := container.NewVBox(repoEntry, tighten(repoNote.rt))
	githubRow.Hide()
	srcSel := widget.NewRadioGroup([]string{srcLocal, srcGitHub}, func(s string) {
		if s == srcGitHub {
			localRow.Hide()
			githubRow.Show()
		} else {
			githubRow.Hide()
			localRow.Show()
		}
	})
	srcSel.Horizontal = true
	srcSel.Required = true
	srcSel.SetSelected(srcLocal)
	dryRun := widget.NewCheck("Dry run (only show what would change)", nil)

	busy := func(b bool) {
		if b {
			pick.Disable()
			install.Disable()
			srcSel.Disable()
			bar.Show()
			bar.Start()
			return
		}
		pick.Enable()
		install.Enable()
		srcSel.Enable()
		bar.Stop()
		bar.Hide()
	}

	install = widget.NewButtonWithIcon("Install", theme.DownloadIcon(), func() {
		dry := dryRun.Checked
		fromGitHub := srcSel.Selected == srcGitHub
		var src packs.Source
		if fromGitHub {
			s, err := packs.ParseSource(repoEntry.Text)
			if err != nil {
				dialog.ShowError(err, u.win)
				return
			}
			src = s
			u.app.Preferences().SetString(prefPackRepo, strings.TrimSpace(repoEntry.Text))
		} else if zipPath == "" {
			dialog.ShowInformation("Custom Packs", "Choose a pack zip first.", u.win)
			return
		}
		zp := zipPath
		run := func() {
			defer fyne.Do(func() { busy(false) })
			fail := func(err error) {
				fyne.Do(func() {
					appendLog("error: " + err.Error())
					status.SetText("Failed: " + err.Error())
				})
			}
			label := ""
			if fromGitHub {
				fyne.Do(func() { status.SetText("Downloading from " + src.String() + "...") })
				cache, err := update.CacheDir()
				if err != nil {
					fail(err)
					return
				}
				c := &packs.Client{}
				p, err := c.Fetch(u.ctx, src, filepath.Join(cache, "pack-downloads"), func(done, total int64) {
					if total > 0 {
						fyne.Do(func() { status.SetText(fmt.Sprintf("Downloading from %s... %d%%", src.String(), done*100/total)) })
					}
				})
				if err != nil {
					fail(err)
					return
				}
				zp, label = p, src.String()
				logf("downloaded " + filepath.Base(p) + " from " + src.String())
			}
			fyne.Do(func() { status.SetText("Opening " + filepath.Base(zp) + "...") })
			dir, v, err := update.OpenLocalPack(zp)
			if err != nil {
				fail(err)
				return
			}
			if !dry {
				fyne.Do(func() { status.SetText("Checking what the pack would change...") })
				changes, err := previewInstall(u.ctx, dir)
				if err != nil {
					fail(err)
					return
				}
				answer := make(chan bool, 1)
				fyne.Do(func() { u.confirmPackChanges(changes, func(ok bool) { answer <- ok }) })
				if !<-answer {
					fyne.Do(func() { status.SetText("Cancelled: nothing was changed.") })
					return
				}
			}
			fyne.Do(func() { status.SetText("Installing...") })
			summary, err := runInstall(u.ctx, dir, dry, logf)
			if err != nil {
				fail(err)
				return
			}
			if summary != "" {
				logf(summary)
			}
			if dry {
				fyne.Do(func() { status.SetText("Dry run finished: nothing was changed. See the install log.") })
				return
			}
			name := filepath.Base(zp)
			if label != "" {
				name = label + ": " + name
			}
			if err := update.MarkPackInstalled(name + " (" + v + ")"); err != nil {
				fail(err)
				return
			}
			fyne.Do(func() {
				status.SetText("Installed. " + summary)
				showInstalled()
			})
		}
		start := func() {
			busy(true)
			logAcc.Open(0)
			go run()
		}
		if fromGitHub && !dry {
			dialog.ShowConfirm("Install a pack from GitHub?", "This pack comes from "+src.String()+".\n\nA pack changes the settings it carries (you see every change before anything is written, and your files are backed up first), can add world presets, can pre-approve Java mods in ZombieBuddy (you're asked first) and can switch ZBBatch on. Only install packs from people you trust.", func(ok bool) {
				if ok {
					start()
				}
			}, u.win)
			return
		}
		start()
	})
	install.Importance = widget.HighImportance

	installCard := settingsSection("Install a pack", "Packs carry settings, presets and a ZBBatch cache; the mods themselves come from the Workshop.",
		srcSel, localRow, githubRow,
		container.NewHBox(install, dryRun),
		status, bar, logAcc)

	makeCard := settingsSection("Make a pack from my setup", "Share your settings: pick which mod settings, presets and the ZBBatch cache go in. Personal files (saved builds, outfits, logs, anything with a Steam ID) are never offered.",
		container.NewHBox(widget.NewButtonWithIcon("Make a pack...", theme.ContentAddIcon(), u.makePackDialog)))

	return container.NewVScroll(container.NewVBox(installedCard, installCard, makeCard))
}

func (u *ui) makePackDialog() {
	z, err := paths.Zomboid()
	if err != nil {
		dialog.ShowError(err, u.win)
		return
	}
	home, _ := os.UserHomeDir()
	zbHome := filepath.Join(home, ".zombie_buddy")
	settings := packs.DetectSettings(filepath.Join(z, "Lua"), packModRefs())
	name := widget.NewEntry()
	name.SetText("My-Pack")
	var luaChecks []*widget.Check
	var luaRels []string
	luaBox := container.NewVBox()
	for _, s := range settings {
		if !s.Shareable() {
			continue
		}
		owner := s.Owner
		if owner == "" {
			owner = "unknown mod"
		}
		rel := s.Rel
		if s.Dir {
			rel += "/"
		}
		c := widget.NewCheck(rel+"  ·  "+owner, nil)
		c.SetChecked(s.Owner != "")
		luaChecks = append(luaChecks, c)
		luaRels = append(luaRels, s.Rel)
		luaBox.Add(c)
	}
	var moChecks []*widget.Check
	var moGroups []string
	moBox := container.NewVBox()
	if b, err := os.ReadFile(filepath.Join(z, "Lua", modoptions.FileName)); err == nil {
		enabled := map[string]bool{}
		if t, err := os.ReadFile(filepath.Join(z, "mods", "default.txt")); err == nil {
			for _, id := range diag.EnabledMods(string(t)) {
				enabled[strings.ToLower(id)] = true
			}
		}
		for _, g := range modoptions.Parse(string(b)).Groups() {
			label := fmt.Sprintf("%s  ·  %d option(s)", g.Mod, g.Options)
			if g.KeyBinds > 0 {
				label += fmt.Sprintf(", %d key binding(s) left out", g.KeyBinds)
			}
			c := widget.NewCheck(label, nil)
			if g.Options == 0 {
				c.Disable()
			} else {
				c.SetChecked(enabled[strings.ToLower(g.Mod)])
			}
			moChecks = append(moChecks, c)
			moGroups = append(moGroups, g.Mod)
			moBox.Add(c)
		}
	}
	if len(moChecks) == 0 {
		moBox.Add(widget.NewLabel("No Mod Options saved yet (they appear after you change a mod's options in the game)."))
	}
	modList := widget.NewCheck("Mod list (mods/default.txt)", nil)
	modList.SetChecked(true)
	var presetChecks []*widget.Check
	var presetNames []string
	presetBox := container.NewVBox()
	if ents, err := os.ReadDir(filepath.Join(z, "Sandbox Presets")); err == nil {
		for _, e := range ents {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".cfg") {
				continue
			}
			c := widget.NewCheck(e.Name(), nil)
			presetChecks = append(presetChecks, c)
			presetNames = append(presetNames, e.Name())
			presetBox.Add(c)
		}
	}
	if len(presetChecks) == 0 {
		presetBox.Add(widget.NewLabel("No world presets found."))
	}
	zbb := widget.NewCheck("ZBBatch cache (zbbatch.properties)", nil)
	if _, err := os.Stat(filepath.Join(zbHome, "zbbatch.properties")); err == nil {
		zbb.SetChecked(true)
	} else {
		zbb.Disable()
	}
	head := func(s string) fyne.CanvasObject {
		l := widget.NewLabel(s)
		l.TextStyle = fyne.TextStyle{Bold: true}
		return l
	}
	body := container.NewVBox(
		container.NewBorder(nil, nil, widget.NewLabel("Pack name"), nil, name),
		head("Mod settings (owner detected automatically)"), luaBox,
		head("Mod Options (the game's Options > Mods screen; ticked = mods in your mod list; key bindings stay personal)"), moBox,
		head("Game"), modList,
		head("World presets"), presetBox,
		head("ZBBatch"), zbb,
	)
	sc := container.NewVScroll(body)
	sc.SetMinSize(fyne.NewSize(640, 460))
	dialog.ShowCustomConfirm("Make a pack", "Save pack...", "Cancel", sc, func(ok bool) {
		if !ok {
			return
		}
		o := packs.BuildOptions{Name: name.Text, ZomboidDir: z, ZBHome: zbHome, ModList: modList.Checked, ZBBatch: zbb.Checked && !zbb.Disabled()}
		for i, c := range luaChecks {
			if c.Checked {
				o.Lua = append(o.Lua, luaRels[i])
			}
		}
		for i, c := range moChecks {
			if c.Checked && !c.Disabled() {
				o.ModOptions = append(o.ModOptions, moGroups[i])
			}
		}
		for i, c := range presetChecks {
			if c.Checked {
				o.Presets = append(o.Presets, presetNames[i])
			}
		}
		d := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
			if err != nil || w == nil {
				return
			}
			dest := w.URI().Path()
			w.Close()
			files, err := packs.Build(dest, o)
			if err != nil {
				os.Remove(dest)
				dialog.ShowError(err, u.win)
				return
			}
			if len(files) == 0 {
				dialog.ShowError(errors.New("the pack is empty"), u.win)
				return
			}
			dialog.ShowInformation("Pack saved", fmt.Sprintf("Saved %d files to\n%s\n\nFriends install it with Mods > Custom Packs > Local file, or attach it to a GitHub release and use GitHub.", len(files), dest), u.win)
		}, u.win)
		d.SetFileName(strings.ReplaceAll(strings.TrimSpace(name.Text), " ", "-") + ".zip")
		d.Show()
	}, u.win)
}
