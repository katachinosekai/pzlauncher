package main

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/kata/pzlauncher/internal/music"
	"github.com/kata/pzlauncher/internal/play"
)

var launcherMusic = music.NewPlayer()

type musicUI struct {
	cfg     music.Config
	path    string
	nowPlay *widget.Label
	refresh func()
}

var musicState = &musicUI{}

func (u *ui) loadMusic() {
	musicState.path, _ = music.ConfigPath()
	musicState.cfg, _ = music.LoadConfig(musicState.path)
	launcherMusic.OnTrack = func(path string) {
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		fyne.Do(func() {
			if musicState.nowPlay != nil {
				musicState.nowPlay.SetText("Now playing: " + name)
			}
		})
	}
	go func() {
		was := false
		for {
			select {
			case <-u.ctx.Done():
				launcherMusic.Stop()
				return
			case <-time.After(3 * time.Second):
			}
			running, err := play.IsRunning()
			if err == nil && running != was {
				was = running
				launcherMusic.SetGamePaused(running)
			}
		}
	}()
	if musicState.cfg.Enabled && musicState.cfg.Folder != "" {
		_ = launcherMusic.Start(musicState.cfg.Folder, musicState.cfg.Shuffle, musicState.cfg.Volume)
	}
}

func (u *ui) saveMusic() {
	if err := music.SaveConfig(musicState.path, musicState.cfg); err != nil {
		dialog.ShowError(err, u.win)
	}
	if musicState.refresh != nil {
		musicState.refresh()
	}
}

func (u *ui) startMusic() error {
	c := musicState.cfg
	if c.Folder == "" {
		return errors.New("choose a music folder first")
	}
	return launcherMusic.Start(c.Folder, c.Shuffle, c.Volume)
}

func (u *ui) chooseMusicFolder(done func(ok bool)) {
	d := dialog.NewFolderOpen(func(dir fyne.ListableURI, err error) {
		if err != nil || dir == nil {
			done(false)
			return
		}
		musicState.cfg.Folder = dir.Path()
		musicState.cfg.Enabled = true
		if err := u.startMusic(); err != nil {
			musicState.cfg.Enabled = false
			u.saveMusic()
			dialog.ShowError(err, u.win)
			done(false)
			return
		}
		u.saveMusic()
		done(true)
	}, u.win)
	d.Resize(fyne.NewSize(700, 500))
	d.Show()
}

func (u *ui) askMusicOnce() {
	if musicState.cfg.Asked {
		return
	}
	msg := widget.NewLabel("Play background music while the launcher is open?\n\nYou pick a folder with your own music (mp3, ogg, wav or flac). It pauses while Project Zomboid is running. You can change this any time under Settings > Launcher music.")
	msg.Wrapping = fyne.TextWrapWord
	d := dialog.NewCustomConfirm("Launcher music", "Choose music folder...", "No thanks", msg, func(yes bool) {
		musicState.cfg.Asked = true
		if !yes {
			musicState.cfg.Enabled = false
			u.saveMusic()
			return
		}
		u.saveMusic()
		u.chooseMusicFolder(func(bool) {})
	}, u.win)
	d.Resize(fyne.NewSize(480, 260))
	d.Show()
}

func (u *ui) musicCard() fyne.CanvasObject {
	folder := widget.NewLabel("")
	folder.Wrapping = fyne.TextWrapBreak
	musicState.nowPlay = widget.NewLabel("")
	musicState.nowPlay.Truncation = fyne.TextTruncateEllipsis
	enable := widget.NewCheck("Play music in the launcher", nil)
	shuffle := widget.NewCheck("Shuffle", nil)
	vol := widget.NewSlider(0, 100)
	vol.Step = 1
	volLabel := widget.NewLabel("")
	pause := widget.NewButton("Pause", nil)
	next := widget.NewButton("Next track", func() { launcherMusic.Next() })
	choose := widget.NewButton("Choose folder...", func() { u.chooseMusicFolder(func(bool) {}) })

	updating := false
	musicState.refresh = func() {
		updating = true
		defer func() { updating = false }()
		c := musicState.cfg
		enable.SetChecked(c.Enabled)
		shuffle.SetChecked(c.Shuffle)
		vol.SetValue(float64(c.Volume))
		volLabel.SetText(strconv.Itoa(c.Volume) + "%")
		if c.Folder == "" {
			folder.SetText("Music folder: none chosen")
		} else {
			folder.SetText("Music folder: " + c.Folder)
		}
		playing := launcherMusic.Playing()
		if playing {
			pause.Enable()
			next.Enable()
		} else {
			pause.Disable()
			next.Disable()
			musicState.nowPlay.SetText("")
		}
		if launcherMusic.Paused() {
			pause.SetText("Resume")
		} else {
			pause.SetText("Pause")
		}
	}
	enable.OnChanged = func(on bool) {
		if updating {
			return
		}
		musicState.cfg.Asked = true
		if !on {
			musicState.cfg.Enabled = false
			launcherMusic.Stop()
			u.saveMusic()
			return
		}
		if musicState.cfg.Folder == "" {
			u.chooseMusicFolder(func(bool) { musicState.refresh() })
			return
		}
		musicState.cfg.Enabled = true
		if err := u.startMusic(); err != nil {
			musicState.cfg.Enabled = false
			dialog.ShowError(err, u.win)
		}
		u.saveMusic()
	}
	shuffle.OnChanged = func(on bool) {
		if updating {
			return
		}
		musicState.cfg.Shuffle = on
		if launcherMusic.Playing() {
			_ = u.startMusic()
		}
		u.saveMusic()
	}
	vol.OnChanged = func(v float64) {
		if updating {
			return
		}
		musicState.cfg.Volume = int(v)
		volLabel.SetText(strconv.Itoa(int(v)) + "%")
		launcherMusic.SetVolume(int(v))
	}
	vol.OnChangeEnded = func(float64) { u.saveMusic() }
	pause.OnTapped = func() {
		launcherMusic.SetPaused(!launcherMusic.Paused())
		musicState.refresh()
	}
	musicState.refresh()

	note := widget.NewLabel("Uses your own music files (mp3, ogg, wav, flac) from the folder you choose, including subfolders. Music pauses while Project Zomboid is running.")
	note.Wrapping = fyne.TextWrapWord
	note.Importance = widget.LowImportance
	volRow := container.NewBorder(nil, nil, widget.NewLabel("Volume"), volLabel, vol)
	return widget.NewCard("Launcher music", "", container.NewVBox(
		container.NewHBox(enable, shuffle),
		container.NewBorder(nil, nil, nil, choose, folder),
		volRow,
		container.NewBorder(nil, nil, nil, container.NewHBox(pause, next), musicState.nowPlay),
		note,
	))
}
