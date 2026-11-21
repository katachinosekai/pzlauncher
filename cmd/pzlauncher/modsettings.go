package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/katachinosekai/pzlauncher/internal/install"
	"github.com/katachinosekai/pzlauncher/internal/kvfile"
	"github.com/katachinosekai/pzlauncher/internal/packs"
	"github.com/katachinosekai/pzlauncher/internal/paths"
)

const maxEditBytes = 1 << 20

func (u *ui) modSettingsButton(modID, modName string) *widget.Button {
	return widget.NewButtonWithIcon("Mod settings", theme.SettingsIcon(), func() { u.modSettingsDialog(modID, modName) })
}

func (u *ui) modSettingsDialog(modID, modName string) {
	z, err := paths.Zomboid()
	if err != nil {
		dialog.ShowError(err, u.win)
		return
	}
	lua := filepath.Join(z, "Lua")
	all := packs.DetectSettings(lua, packModRefs())
	mine := packs.SettingsFor(all, modID)
	if len(mine) == 0 {
		dialog.ShowInformation(modName, "No settings files for this mod were found in Zomboid/Lua.\n\nMany mods keep their settings in the game's Mod Options menu (Options > Mods) instead, or only create the file after you change a setting in game.", u.win)
		return
	}
	list := container.NewVBox()
	for _, s := range mine {
		s := s
		p := filepath.Join(lua, s.Rel)
		label := widget.NewLabel(s.Rel)
		var actions *fyne.Container
		switch {
		case s.Blocked != "":
			actions = container.NewHBox(widget.NewLabel("(" + s.Blocked + ")"))
		case s.Dir:
			actions = container.NewHBox(widget.NewButtonWithIcon("Open folder", theme.FolderOpenIcon(), func() { u.openFolder(p) }))
		default:
			actions = container.NewHBox(widget.NewButtonWithIcon("Edit", theme.DocumentCreateIcon(), func() { u.editSettingsFile(p, modName) }))
		}
		list.Add(container.NewBorder(nil, nil, nil, actions, label))
	}
	note := newMuted("Files in Zomboid/Lua that belong to this mod (detected by name). Quit the game before editing; the mod may overwrite changes made while it runs.", false)
	body := container.NewVBox(note.rt, list)
	d := dialog.NewCustom(modName+": settings", "Close", container.NewVScroll(body), u.win)
	d.Resize(fyne.NewSize(620, 380))
	d.Show()
}

func (u *ui) editSettingsFile(p, modName string) {
	if install.GameRunning() {
		dialog.ShowInformation("Game is running", "Quit Project Zomboid first: the mod would overwrite your changes when it saves its settings.", u.win)
		return
	}
	st, err := os.Stat(p)
	if err != nil {
		dialog.ShowError(err, u.win)
		return
	}
	if st.Size() > maxEditBytes {
		dialog.ShowError(errors.New("this file is too large to edit here; open its folder instead"), u.win)
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		dialog.ShowError(err, u.win)
		return
	}
	text := string(b)
	lines, n := kvfile.Parse(text)
	entries := map[*kvfile.Line]*widget.Entry{}
	var content fyne.CanvasObject
	var collect func() string
	if n == 0 {
		e := widget.NewMultiLineEntry()
		e.SetText(text)
		e.Wrapping = fyne.TextWrapOff
		e.TextStyle = fyne.TextStyle{Monospace: true}
		content = e
		collect = func() string { return e.Text }
	} else {
		box := container.NewVBox()
		lastSection := "\x00"
		for _, l := range lines {
			if l.Key == "" {
				continue
			}
			if l.Section != lastSection {
				lastSection = l.Section
				if l.Section != "" {
					h := widget.NewLabel(l.Section)
					h.TextStyle = fyne.TextStyle{Bold: true}
					box.Add(h)
				}
			}
			e := widget.NewEntry()
			e.SetText(l.Val)
			entries[l] = e
			box.Add(container.NewGridWithColumns(2, widget.NewLabel(l.Key), e))
		}
		content = box
		collect = func() string {
			return kvfile.Apply(lines, func(l *kvfile.Line) (string, bool) {
				e := entries[l]
				if e == nil {
					return "", false
				}
				return e.Text, e.Text != l.Val
			})
		}
	}
	sc := container.NewVScroll(content)
	sc.SetMinSize(fyne.NewSize(640, 420))
	dialog.ShowCustomConfirm(modName+": "+filepath.Base(p), "Save", "Cancel", sc, func(ok bool) {
		if !ok {
			return
		}
		nt := collect()
		if nt == text {
			return
		}
		bak := p + ".bak-" + time.Now().Format("20060102-150405")
		if err := os.WriteFile(bak, b, 0o644); err != nil {
			dialog.ShowError(err, u.win)
			return
		}
		tmp := p + ".tmp"
		if err := os.WriteFile(tmp, []byte(nt), st.Mode().Perm()); err != nil {
			dialog.ShowError(err, u.win)
			return
		}
		if err := os.Rename(tmp, p); err != nil {
			dialog.ShowError(err, u.win)
			return
		}
		dialog.ShowInformation("Saved", fmt.Sprintf("Saved %s.\nThe old version is kept as %s.", filepath.Base(p), filepath.Base(bak)), u.win)
	}, u.win)
}
