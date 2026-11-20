package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/katachinosekai/pzlauncher/internal/play"
	"github.com/katachinosekai/pzlauncher/internal/savebak"
)

type bakThumbKey struct {
	path string
	size int64
}

var (
	bakThumbMu    sync.Mutex
	bakThumbCache = map[bakThumbKey]fyne.Resource{}
	bakBusyMu     sync.Mutex
	bakBusy       bool
)

var bakKeepOptions = []string{"1", "2", "3", "4", "5", "6", "7", "8", "10", "15", "20", "30"}

func bakThumb(b savebak.Backup) fyne.Resource {
	k := bakThumbKey{b.Path, b.Size}
	bakThumbMu.Lock()
	r, ok := bakThumbCache[k]
	bakThumbMu.Unlock()
	if ok {
		return r
	}
	data, err := savebak.Thumb(b)
	if err == nil && len(data) > 0 {
		r = fyne.NewStaticResource(filepath.Base(b.Path)+".png", data)
	}
	bakThumbMu.Lock()
	bakThumbCache[k] = r
	bakThumbMu.Unlock()
	return r
}

func bakManager(save play.Save) (*savebak.Manager, savebak.Config, string, error) {
	p, err := savebak.DefaultConfigPath()
	if err != nil {
		return nil, savebak.Config{}, "", err
	}
	cfg, err := savebak.LoadConfig(p)
	if err != nil {
		return nil, cfg, p, err
	}
	m, err := savebak.NewManager(cfg)
	if err != nil {
		return nil, cfg, p, err
	}
	if save.Dir != "" {
		m.SavesDir = filepath.Dir(filepath.Dir(save.Dir))
	}
	return m, cfg, p, nil
}

func (u *ui) openFolder(dir string) {
	link, err := url.Parse(storage.NewFileURI(dir).String())
	if err == nil {
		err = u.app.OpenURL(link)
	}
	if err != nil {
		dialog.ShowError(err, u.win)
	}
}

func bakPhase(p savebak.Progress) string {
	switch p.Phase {
	case "scan":
		return "Scanning save..."
	case "safety":
		return "Safety backup of the current save..."
	case "restore":
		return "Restoring..."
	case "verify":
		return "Verifying..."
	}
	return "Backing up..."
}

func (u *ui) runBakOp(title string, op func(ctx context.Context, progress func(savebak.Progress)) error, done func(error)) {
	bakBusyMu.Lock()
	if bakBusy {
		bakBusyMu.Unlock()
		done(errors.New("another backup or restore is still running"))
		return
	}
	bakBusy = true
	bakBusyMu.Unlock()

	ctx, cancel := context.WithCancel(u.ctx)
	phase := widget.NewLabel("Starting...")
	detail := widget.NewLabel("")
	bar := widget.NewProgressBar()
	var d *dialog.CustomDialog
	cancelBtn := widget.NewButton("Cancel", func() {
		cancel()
		phase.SetText("Cancelling...")
	})
	d = dialog.NewCustomWithoutButtons(title, container.NewVBox(phase, bar, detail, container.NewHBox(layout.NewSpacer(), cancelBtn)), u.win)
	d.Resize(fyne.NewSize(420, 0))
	d.Show()
	go func() {
		err := op(ctx, func(p savebak.Progress) {
			fyne.Do(func() {
				phase.SetText(bakPhase(p))
				if p.TotalBytes > 0 {
					bar.SetValue(float64(p.Bytes) / float64(p.TotalBytes))
				}
				detail.SetText(fmt.Sprintf("%d / %d files, %s / %s", p.Files, p.TotalFiles, savebak.HumanSize(p.Bytes), savebak.HumanSize(p.TotalBytes)))
			})
		})
		cancel()
		bakBusyMu.Lock()
		bakBusy = false
		bakBusyMu.Unlock()
		fyne.Do(func() {
			d.Hide()
			if errors.Is(err, context.Canceled) {
				err = errors.New("cancelled, nothing was changed")
			}
			done(err)
		})
	}()
}

func (u *ui) saveDetails(save play.Save, onChange func()) fyne.CanvasObject {
	box := container.NewVBox()
	var refresh func()
	changed := func() {
		refresh()
		if onChange != nil {
			onChange()
		}
	}
	refresh = func() {
		box.RemoveAll()
		m, cfg, cfgPath, err := bakManager(save)
		if err != nil {
			box.Add(widget.NewLabel("Backups unavailable: " + err.Error()))
			box.Add(widget.NewButton("Backup settings...", func() { u.backupSettings(changed) }))
			return
		}
		g, err := m.Get(save.Mode, save.World)
		if err != nil {
			box.Add(widget.NewLabel("Could not list backups: " + err.Error()))
		}

		summary := widget.NewLabel(fmt.Sprintf("%d backups, %s", len(g.Backups), savebak.HumanSize(g.Size)))
		if !g.Exists {
			summary.SetText(summary.Text + " - save folder deleted, restore a backup to bring it back")
			summary.Importance = widget.WarningImportance
		}
		summary.Wrapping = fyne.TextWrapWord

		label := widget.NewEntry()
		label.SetPlaceHolder("label (optional)")
		backupBtn := widget.NewButtonWithIcon("Back up now", theme.DocumentSaveIcon(), func() {
			lbl := label.Text
			u.runBakOp("Back up "+save.Mode+" / "+save.World, func(ctx context.Context, pr func(savebak.Progress)) error {
				_, err := m.Backup(ctx, save.Mode, save.World, lbl, pr)
				return err
			}, func(err error) {
				if err != nil {
					dialog.ShowError(err, u.win)
				}
				changed()
			})
		})
		if !g.Exists {
			backupBtn.Disable()
			label.Disable()
		}
		pruneBtn := widget.NewButtonWithIcon("Prune automatic", theme.ContentClearIcon(), func() {
			keep := cfg.KeepFor(save.Mode, save.World)
			dialog.ShowConfirm("Prune automatic backups", fmt.Sprintf("Delete all but the newest %d automatic backups of this save? Manual backups are kept.", keep), func(ok bool) {
				if !ok {
					return
				}
				n, freed, err := m.Prune(save.Mode, save.World, keep)
				if err != nil {
					dialog.ShowError(err, u.win)
				} else {
					dialog.ShowInformation("Pruned", fmt.Sprintf("Deleted %d backups, freed %s.", n, savebak.HumanSize(freed)), u.win)
				}
				changed()
			}, u.win)
		})
		folderBtn := widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() {
			u.openFolder(m.BackupDir(save.Mode, save.World))
		})
		if len(g.Backups) == 0 {
			folderBtn.Disable()
		}
		settingsBtn := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() { u.backupSettings(changed) })

		auto := widget.NewCheck("Back up before launching", nil)
		auto.SetChecked(cfg.AutoFor(save.Mode, save.World))
		keep := widget.NewSelect(bakKeepOptions, nil)
		keep.SetSelected(strconv.Itoa(cfg.KeepFor(save.Mode, save.World)))
		if keep.Selected == "" {
			keep.SetSelectedIndex(4)
		}
		persist := func() {
			n, _ := strconv.Atoi(keep.Selected)
			c, err := savebak.LoadConfig(cfgPath)
			if err != nil {
				dialog.ShowError(err, u.win)
				return
			}
			c.SetSave(save.Mode, save.World, auto.Checked, n)
			if err := savebak.SaveConfig(cfgPath, c); err != nil {
				dialog.ShowError(err, u.win)
			}
		}
		auto.OnChanged = func(bool) { persist() }
		keep.OnChanged = func(string) { persist() }

		box.Add(container.NewBorder(nil, nil, nil, container.NewHBox(pruneBtn, folderBtn, settingsBtn), summary))
		box.Add(container.NewBorder(nil, nil, nil, backupBtn, label))
		box.Add(container.NewHBox(auto, widget.NewLabel("keep"), keep, widget.NewLabel("automatic backups")))
		for _, b := range g.Backups {
			box.Add(u.backupRow(m, b, g.Exists, changed))
		}
	}
	refresh()
	return box
}

func (u *ui) backupRow(m *savebak.Manager, b savebak.Backup, saveExists bool, changed func()) fyne.CanvasObject {
	img := canvas.NewImageFromResource(bakThumb(b))
	img.FillMode = canvas.ImageFillContain
	img.CornerRadius = 4
	img.SetMinSize(fyne.NewSize(48, 48))
	name := b.Label
	switch {
	case name == "":
		name = "manual"
	case b.Auto():
		name = "automatic"
	}
	text := widget.NewLabel(fmt.Sprintf("%s  %s  %s", b.Time.Format("2006-01-02 15:04:05"), name, savebak.HumanSize(b.Size)))
	text.Truncation = fyne.TextTruncateEllipsis

	restore := widget.NewButtonWithIcon("", theme.HistoryIcon(), func() {
		msg := fmt.Sprintf("Restore the backup from %s into %s / %s?", b.Time.Format("2006-01-02 15:04:05"), b.Mode, b.World)
		if saveExists {
			msg += "\n\nThe current save is backed up first (label \"before-restore\") and then replaced."
		}
		dialog.ShowConfirm("Restore backup", msg, func(ok bool) {
			if !ok {
				return
			}
			var res savebak.RestoreResult
			u.runBakOp("Restore "+b.Mode+" / "+b.World, func(ctx context.Context, pr func(savebak.Progress)) error {
				var err error
				res, err = m.Restore(ctx, b, pr)
				return err
			}, func(err error) {
				switch {
				case err != nil:
					dialog.ShowError(err, u.win)
				case res.Warning != "":
					dialog.ShowInformation("Restored", res.Warning, u.win)
				default:
					dialog.ShowInformation("Restored", "The save was restored.", u.win)
				}
				changed()
			})
		}, u.win)
	})
	rename := widget.NewButtonWithIcon("", theme.DocumentCreateIcon(), func() {
		e := widget.NewEntry()
		e.SetText(b.Label)
		dialog.ShowForm("Rename backup", "Rename", "Cancel", []*widget.FormItem{
			widget.NewFormItem("Label", e),
			widget.NewFormItem("", widget.NewLabel("Renamed backups count as manual and are never pruned.")),
		}, func(ok bool) {
			if !ok {
				return
			}
			if _, err := m.Rename(b, e.Text); err != nil {
				dialog.ShowError(err, u.win)
			}
			changed()
		}, u.win)
	})
	del := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
		dialog.ShowConfirm("Delete backup", fmt.Sprintf("Delete the backup from %s (%s)? This cannot be undone.", b.Time.Format("2006-01-02 15:04:05"), savebak.HumanSize(b.Size)), func(ok bool) {
			if !ok {
				return
			}
			if err := m.Delete(b); err != nil {
				dialog.ShowError(err, u.win)
			}
			changed()
		}, u.win)
	})
	open := widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() { u.openFolder(filepath.Dir(b.Path)) })
	for _, btn := range []*widget.Button{restore, rename, del, open} {
		btn.Importance = widget.LowImportance
	}
	buttons := container.NewHBox(restore, rename, del, open)
	return container.NewBorder(nil, nil, img, buttons, text)
}

func (u *ui) backupSettings(changed func()) {
	p, err := savebak.DefaultConfigPath()
	if err != nil {
		dialog.ShowError(err, u.win)
		return
	}
	cfg, err := savebak.LoadConfig(p)
	if err != nil {
		dialog.ShowError(err, u.win)
	}
	def, _ := savebak.DefaultRoot()
	root := widget.NewEntry()
	root.SetText(cfg.Root)
	root.SetPlaceHolder(def)
	browse := widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() {
		dialog.ShowFolderOpen(func(l fyne.ListableURI, err error) {
			if err == nil && l != nil {
				root.SetText(l.Path())
			}
		}, u.win)
	})
	auto := widget.NewCheck("Back up a save before launching it", nil)
	auto.SetChecked(cfg.Auto)
	keep := widget.NewSelect(bakKeepOptions, nil)
	keep.SetSelected(strconv.Itoa(cfg.KeepAuto))
	if keep.Selected == "" {
		keep.SetSelectedIndex(4)
	}

	total := widget.NewLabel("Total: ...")
	var m *savebak.Manager
	updateTotal := func() {
		var err error
		m, err = savebak.NewManager(cfg)
		if err != nil {
			total.SetText(err.Error())
			return
		}
		n, err := m.TotalSize()
		if err != nil {
			total.SetText(err.Error())
			return
		}
		total.SetText("All backups: " + savebak.HumanSize(n))
	}
	updateTotal()
	openRoot := widget.NewButtonWithIcon("Open folder", theme.FolderOpenIcon(), func() {
		if m != nil {
			u.openFolder(m.Root)
		}
	})
	pruneAll := widget.NewButtonWithIcon("Prune all automatic", theme.ContentClearIcon(), func() {
		dialog.ShowConfirm("Prune automatic backups", "For every save, delete the automatic backups beyond its keep count? Manual backups are kept.", func(ok bool) {
			if !ok || m == nil {
				return
			}
			n, freed, err := m.PruneAll(cfg)
			if err != nil {
				dialog.ShowError(err, u.win)
			} else {
				dialog.ShowInformation("Pruned", fmt.Sprintf("Deleted %d backups, freed %s.", n, savebak.HumanSize(freed)), u.win)
			}
			updateTotal()
			if changed != nil {
				changed()
			}
		}, u.win)
	})
	items := []*widget.FormItem{
		widget.NewFormItem("Backup folder", container.NewBorder(nil, nil, nil, browse, root)),
		widget.NewFormItem("", widget.NewLabel("Existing backups are not moved when the folder changes.")),
		widget.NewFormItem("Default", auto),
		widget.NewFormItem("Keep automatic", keep),
		widget.NewFormItem("", container.NewHBox(total, layout.NewSpacer(), openRoot, pruneAll)),
	}
	d := dialog.NewForm("Backup settings", "Save", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		c, err := savebak.LoadConfig(p)
		if err != nil {
			dialog.ShowError(err, u.win)
			return
		}
		c.Root = root.Text
		if c.Root != "" {
			if err := savebak.CheckRoot(c.Root, saveDirGuess()); err != nil && saveDirGuess() != "" {
				dialog.ShowError(err, u.win)
				return
			}
		}
		c.Auto = auto.Checked
		c.KeepAuto, _ = strconv.Atoi(keep.Selected)
		if err := savebak.SaveConfig(p, c); err != nil {
			dialog.ShowError(err, u.win)
			return
		}
		if changed != nil {
			changed()
		}
	}, u.win)
	d.Resize(fyne.NewSize(560, 0))
	d.Show()
}

func saveDirGuess() string {
	m, err := savebak.NewManager(savebak.DefaultConfig())
	if err != nil {
		return ""
	}
	return m.SavesDir
}
