package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"github.com/katachinosekai/pzlauncher/internal/install"
	"github.com/katachinosekai/pzlauncher/internal/paths"
	"github.com/katachinosekai/pzlauncher/internal/play"
	"github.com/katachinosekai/pzlauncher/internal/update"
	"github.com/katachinosekai/pzlauncher/internal/zbsetup"
)

const (
	prefZBUse    = "zombiebuddy.use"
	prefZBCustom = "zombiebuddy.customJar"
)

func zbCustomJar() string {
	a := fyne.CurrentApp()
	if a == nil {
		return ""
	}
	return strings.TrimSpace(a.Preferences().String(prefZBCustom))
}

type zbStatus struct {
	gameDir   string
	libs      string
	installed string
	staged    string
	workshop  string
	launch    []string
	enabled   bool
	rels      []zbsetup.Release
	relNote   string
	custom    string
	customVer string
	err       error
}

func (s zbStatus) latest() *zbsetup.Release { return zbsetup.Latest(s.rels) }

func (s zbStatus) best() string {
	b := s.workshop
	if l := s.latest(); l != nil && zbsetup.Newer(l.Version, b) {
		b = l.Version
	}
	return b
}

func (s zbStatus) needsUpdate() bool {
	if s.custom != "" {
		return s.customVer != "" && s.customVer != s.installed
	}
	return s.installed != "" && zbsetup.Newer(s.best(), s.installed)
}

func loadZBStatus(u *ui, client *zbsetup.Client) zbStatus {
	var s zbStatus
	if s.custom = zbCustomJar(); s.custom != "" {
		if info, err := zbsetup.VerifyJar(s.custom); err == nil {
			s.customVer = info.Version
		}
	}
	root, err := paths.SteamRoot()
	if err != nil {
		s.err = err
		return s
	}
	if s.gameDir, err = paths.GameDir(root); err != nil {
		s.err = err
		return s
	}
	if idx, err := play.ScanInstalled(); err == nil {
		if m := idx.Get(zbsetup.ModID); m != nil {
			s.libs = zbsetup.LibsFromModDir(m.Dir)
		}
	}
	if s.libs == "" {
		if ws, err := paths.WorkshopDir(root); err == nil {
			s.libs = zbsetup.WorkshopLibs(ws)
		}
	}
	s.installed = zbsetup.InstalledVersion(s.gameDir)
	s.staged = zbsetup.StagedVersion(s.gameDir)
	if s.libs != "" {
		s.workshop = zbsetup.JarVersion(filepath.Join(s.libs, zbsetup.JarName))
	}
	for _, f := range install.LaunchFiles(root) {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		vals, err := zbsetup.ReadLaunchOptions(string(b))
		if err != nil {
			continue
		}
		for _, v := range vals {
			s.launch = append(s.launch, v)
			if zbsetup.HasLoader(v, runtime.GOOS) {
				s.enabled = true
			}
		}
	}
	lr, err := client.Releases(u.ctx, false)
	s.rels = lr.Releases
	switch {
	case err != nil:
		s.relNote = " (GitHub unavailable: " + err.Error() + ")"
	case lr.Stale:
		s.relNote = " (saved list; GitHub unavailable)"
	}
	return s
}

func (s zbStatus) text() string {
	if s.err != nil {
		return "Project Zomboid not found: " + s.err.Error()
	}
	or := func(v, d string) string {
		if v == "" {
			return d
		}
		return v
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Installed in the game folder: %s\n", or(s.installed, "not installed"))
	if s.staged != "" {
		fmt.Fprintf(&b, "Waiting to be applied on the next start (ZombieBuddy.jar.new): %s\n", s.staged)
	}
	fmt.Fprintf(&b, "Workshop copy: %s\n", or(s.workshop, "not downloaded (subscribe to ZombieBuddy on the Workshop)"))
	latest := "unknown"
	if l := s.latest(); l != nil {
		latest = l.Tag
	}
	fmt.Fprintf(&b, "Newest GitHub release: %s%s\n", latest, s.relNote)
	if s.custom != "" {
		v := s.customVer
		if v == "" {
			v = "not a valid ZombieBuddy jar"
		}
		fmt.Fprintf(&b, "Custom jar in use: %s (%s). Update and the pack install use it instead of the Workshop or GitHub copies.\n", s.custom, v)
	}
	switch {
	case s.custom != "" && s.needsUpdate():
		fmt.Fprintf(&b, "The game folder has %s; Update installs your custom jar (%s).\n", or(s.installed, "no ZombieBuddy"), s.customVer)
	case s.installed == "":
		b.WriteString("ZombieBuddy is not in the game folder yet.\n")
	case s.needsUpdate():
		fmt.Fprintf(&b, "Update available: %s -> %s\n", s.installed, s.best())
	default:
		b.WriteString("Up to date.\n")
	}
	if len(s.launch) == 0 {
		b.WriteString("Steam launch options: (none)")
	} else {
		b.WriteString("Steam launch options: " + strings.Join(s.launch, "  |  "))
	}
	return b.String()
}

func zbWorkDir() string {
	d, err := update.ConfigDir()
	if err != nil {
		return filepath.Join(".", "zombiebuddy")
	}
	return filepath.Join(d, "zombiebuddy")
}

func (u *ui) zombieBuddyTab() fyne.CanvasObject {
	client := zbsetup.NewClient()
	intro := widget.NewLabel("ZombieBuddy loads Java mods. It is optional: without it, Java mods (and ZBBatch) don't load, everything else works.")
	intro.Wrapping = fyne.TextWrapWord
	ghURL, _ := url.Parse("https://github.com/" + zbsetup.Repo)
	ghLink := widget.NewHyperlink("ZombieBuddy on GitHub (by zed-0xff)", ghURL)
	status := widget.NewLabel("Checking...")
	status.Wrapping = fyne.TextWrapWord
	output := widget.NewLabel("")
	output.Wrapping = fyne.TextWrapWord
	var cur zbStatus
	syncing := false
	use := widget.NewCheck("Use ZombieBuddy (needed for Java mods)", nil)
	picker := widget.NewSelect(nil, nil)
	picker.PlaceHolder = "Choose a version"
	var choices []func() (string, string, error)
	var refresh func()
	updateBtn := widget.NewButton("Update", nil)
	installBtn := widget.NewButton("Install selected version", nil)
	refreshBtn := widget.NewButton("Refresh", nil)
	busy := func(b bool) {
		for _, w := range []fyne.Disableable{use, picker, updateBtn, installBtn, refreshBtn} {
			if b {
				w.Disable()
			} else {
				w.Enable()
			}
		}
	}

	setStatus := func(s zbStatus) {
		cur = s
		status.SetText(s.text())
		syncing = true
		use.SetChecked(s.enabled)
		syncing = false
		if s.enabled {
			u.app.Preferences().SetBool(prefZBUse, true)
		}
		var opts []string
		var vers []string
		choices = nil
		if s.custom != "" && s.customVer != "" {
			opts = append(opts, "Custom jar ("+s.customVer+")")
			vers = append(vers, s.customVer)
			path, ver := s.custom, s.customVer
			choices = append(choices, func() (string, string, error) { return path, ver, nil })
		}
		if s.workshop != "" {
			opts = append(opts, "Workshop copy ("+s.workshop+")")
			vers = append(vers, s.workshop)
			libs := s.libs
			choices = append(choices, func() (string, string, error) {
				f, err := zbsetup.CheckLocal(libs)
				return f.Jar, f.Info.Version, err
			})
		}
		for _, r := range s.rels {
			r := r
			label := r.Tag + " (GitHub, " + r.Published.Format("2006-01-02") + ")"
			if r.Prerelease {
				label += " pre-release"
			}
			opts = append(opts, label)
			vers = append(vers, r.Version)
			choices = append(choices, func() (string, string, error) {
				f, err := client.Fetch(u.ctx, r, func(done, total int64) {
					if total > 0 {
						fyne.Do(func() { output.SetText(fmt.Sprintf("Downloading %s: %d%%", r.Tag, done*100/total)) })
					}
				})
				return f.Jar, f.Info.Version, err
			})
		}
		picker.Options = opts
		picker.ClearSelected()
		for i, v := range vers {
			if s.installed != "" && zbsetup.Compare(v, s.installed) == 0 {
				picker.SetSelectedIndex(i)
				break
			}
		}
		picker.Refresh()
		busy(false)
		if !s.needsUpdate() {
			updateBtn.Disable()
		}
		if s.err != nil {
			use.Disable()
		}
	}

	refresh = func() {
		busy(true)
		go func() {
			s := loadZBStatus(u, client)
			fyne.Do(func() { setStatus(s) })
		}()
	}
	refreshBtn.OnTapped = refresh

	runSetup := func(on, deleteFiles, dry bool) (install.Report, error) {
		return install.ZombieBuddy(u.ctx, install.Options{
			PackDir:                zbWorkDir(),
			DryRun:                 dry,
			SetupZombieBuddy:       on,
			ZombieBuddyJar:         zbCustomJar(),
			RemoveZombieBuddy:      !on,
			RemoveZombieBuddyFiles: deleteFiles,
		})
	}
	finish := func(r install.Report, err error) {
		var b strings.Builder
		if err != nil {
			b.WriteString("Failed: " + err.Error() + "\n")
		}
		if len(r.Changes) == 0 && err == nil {
			b.WriteString("Nothing needed changing.\n")
		}
		for _, c := range r.Changes {
			b.WriteString(c + "\n")
		}
		for _, w := range r.Warnings {
			b.WriteString("Warning: " + w + "\n")
		}
		if r.BackupDir != "" {
			b.WriteString("Originals backed up to " + r.BackupDir + "\n")
		}
		output.SetText(strings.TrimSpace(b.String()))
		refresh()
	}

	use.OnChanged = func(on bool) {
		if syncing {
			return
		}
		revert := func() {
			syncing = true
			use.SetChecked(!on)
			syncing = false
		}
		busy(true)
		go func() {
			preview, err := runSetup(on, false, true)
			fyne.Do(func() {
				busy(false)
				if err != nil {
					revert()
					dialog.ShowError(err, u.win)
					return
				}
				u.app.Preferences().SetBool(prefZBUse, on)
				if len(preview.Changes) == 0 {
					finish(preview, nil)
					return
				}
				text := "These changes will be made:\n\n" + strings.Join(preview.Changes, "\n")
				if len(preview.Warnings) > 0 {
					text += "\n\nWarnings:\n" + strings.Join(preview.Warnings, "\n")
				}
				if install.SteamRunning() {
					text += "\n\nSteam is running. Steam must be closed to change launch options: the launcher closes Steam, makes the change and starts Steam again."
				}
				lbl := widget.NewLabel(text)
				lbl.Wrapping = fyne.TextWrapWord
				del := widget.NewCheck("Also delete ZombieBuddy.jar from the game folder (backed up first)", nil)
				content := container.NewVBox(lbl)
				if !on {
					content.Add(del)
				}
				scroll := container.NewVScroll(content)
				scroll.SetMinSize(fyne.NewSize(620, 280))
				title := "Set up ZombieBuddy"
				if !on {
					title = "Switch ZombieBuddy off"
				}
				dialog.ShowCustomConfirm(title, "Apply", "Cancel", scroll, func(ok bool) {
					if !ok {
						revert()
						u.app.Preferences().SetBool(prefZBUse, !on)
						return
					}
					busy(true)
					output.SetText("Working...")
					go func() {
						r, err := runSetup(on, del.Checked, false)
						fyne.Do(func() { finish(r, err) })
					}()
				}, u.win)
			})
		}()
	}

	installJar := func(get func() (string, string, error)) {
		busy(true)
		output.SetText("Preparing...")
		go func() {
			src, ver, err := get()
			fyne.Do(func() {
				busy(false)
				if err != nil {
					output.SetText("")
					dialog.ShowError(err, u.win)
					return
				}
				if cur.gameDir == "" {
					dialog.ShowError(errors.New("game folder not found"), u.win)
					return
				}
				env := zbsetup.JarEnv{GameDir: cur.gameDir, GOOS: runtime.GOOS, Running: install.GameRunning}
				done := func(r zbsetup.ReplaceResult, err error) {
					if err != nil {
						output.SetText("")
						dialog.ShowError(err, u.win)
						return
					}
					var msg string
					switch {
					case r.Unchanged:
						msg = "ZombieBuddy " + r.NewVersion + " is already installed."
					case r.Staged:
						msg = "ZombieBuddy " + r.NewVersion + " saved as " + r.Jar + "; zbNative swaps it in on the next game start.\n" + zbsetup.ReapproveNote
					default:
						msg = fmt.Sprintf("ZombieBuddy %s -> %s installed in %s.", r.OldVersion, r.NewVersion, cur.gameDir)
						if r.Backup != "" {
							msg += "\nOld jar kept as " + r.Backup
						}
						msg += "\n" + zbsetup.ReapproveNote
					}
					output.SetText(msg)
					refresh()
				}
				if install.GameRunning() {
					if runtime.GOOS != "windows" {
						dialog.ShowError(zbsetup.ErrGameRunning, u.win)
						output.SetText("")
						return
					}
					dialog.ShowConfirm("Project Zomboid is running",
						"ZombieBuddy.jar can't be replaced while the game runs. Save "+ver+" as ZombieBuddy.jar.new instead? ZombieBuddy's zbNative swaps it in on the next start.",
						func(ok bool) {
							if ok {
								done(zbsetup.StageNew(env, src))
							} else {
								output.SetText("")
							}
						}, u.win)
					return
				}
				done(zbsetup.ReplaceJar(env, src))
			})
		}()
	}

	installBtn.OnTapped = func() {
		i := picker.SelectedIndex()
		if i < 0 || i >= len(choices) {
			dialog.ShowInformation("ZombieBuddy", "Choose a version first.", u.win)
			return
		}
		installJar(choices[i])
	}
	updateBtn.OnTapped = func() {
		if len(choices) == 0 {
			return
		}
		if cur.custom != "" {
			if cur.customVer != "" {
				installJar(choices[0])
			}
			return
		}
		best := cur.best()
		if cur.workshop != "" && zbsetup.Compare(cur.workshop, best) >= 0 {
			installJar(choices[0])
			return
		}
		off := 0
		if cur.workshop != "" {
			off = 1
		}
		for i, r := range cur.rels {
			if r.Version == best {
				installJar(choices[off+i])
				return
			}
		}
	}

	customEntry := widget.NewEntry()
	customEntry.SetPlaceHolder("Path to your own ZombieBuddy.jar (leave empty to use the official one)")
	customEntry.SetText(zbCustomJar())
	setCustom := func(p string) {
		u.app.Preferences().SetString(prefZBCustom, p)
		customEntry.SetText(p)
		if p == "" {
			output.SetText("Custom ZombieBuddy jar cleared: the official Workshop or GitHub copy is used again.")
		} else {
			output.SetText("Custom ZombieBuddy jar set: " + p + ". Press Update to install it in the game folder.")
		}
		refresh()
	}
	useCustom := func() {
		p := strings.TrimSpace(customEntry.Text)
		if p == "" {
			setCustom("")
			return
		}
		info, err := zbsetup.VerifyJar(p)
		if err != nil {
			dialog.ShowError(fmt.Errorf("%s: %w", p, err), u.win)
			return
		}
		dialog.ShowConfirm("Use a custom ZombieBuddy jar?",
			"ZombieBuddy "+info.Version+" from\n"+p+"\n\nThis is not an official ZombieBuddy release. ZombieBuddy runs inside the game with full access to your PC: only use builds you trust (your own or from someone you know).\n\nWhile it is set, Update and the pack install use this jar instead of the Workshop or GitHub copies.",
			func(ok bool) {
				if ok {
					setCustom(p)
				}
			}, u.win)
	}
	browse := widget.NewButton("Browse...", func() {
		d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
			if err != nil || r == nil {
				return
			}
			r.Close()
			customEntry.SetText(r.URI().Path())
			useCustom()
		}, u.win)
		d.SetFilter(storage.NewExtensionFileFilter([]string{".jar"}))
		d.Show()
	})
	customEntry.OnSubmitted = func(string) { useCustom() }
	useBtn := widget.NewButton("Use", useCustom)
	clearBtn := widget.NewButton("Clear", func() { setCustom("") })
	customRow := container.NewBorder(nil, nil, widget.NewLabel("Custom jar"), container.NewHBox(browse, useBtn, clearBtn), customEntry)

	refresh()
	top := container.NewVBox(
		container.NewBorder(nil, nil, nil, ghLink, intro),
		use,
		widget.NewSeparator(),
		status,
		container.NewHBox(refreshBtn, updateBtn),
		container.NewBorder(nil, nil, widget.NewLabel("Version"), installBtn, picker),
		customRow,
		widget.NewSeparator(),
	)
	return container.NewBorder(top, nil, nil, nil, container.NewVScroll(output))
}
