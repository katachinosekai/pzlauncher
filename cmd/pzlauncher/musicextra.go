package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/katachinosekai/pzlauncher/internal/gamecfg"
	"github.com/katachinosekai/pzlauncher/internal/install"
	"github.com/katachinosekai/pzlauncher/internal/music"
	"github.com/katachinosekai/pzlauncher/internal/paths"
	"github.com/katachinosekai/pzlauncher/internal/play"
	"github.com/katachinosekai/pzlauncher/internal/spotify"
)

func musicSource() string {
	if musicState.cfg.Source == music.SourceSpotify {
		return music.SourceSpotify
	}
	return music.SourceFolder
}

func optionsINI() string {
	z, err := paths.Zomboid()
	if err != nil {
		return ""
	}
	return filepath.Join(z, "options.ini")
}

func gameMusicOn() (bool, error) {
	b, err := os.ReadFile(optionsINI())
	if err != nil {
		return true, err
	}
	v, ok := gamecfg.ParseINI(b).Get("musicVolume")
	return !ok || strings.TrimSpace(v) != "0", nil
}

func setGameMusic(on bool) error {
	p := optionsINI()
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	ini := gamecfg.ParseINI(b)
	cur, _ := ini.Get("musicVolume")
	want := "0"
	if on {
		want = musicState.cfg.GameVol
		if want == "" || want == "0" {
			want = "5"
		}
	} else if strings.TrimSpace(cur) != "0" {
		musicState.cfg.GameVol = strings.TrimSpace(cur)
	}
	if _, err := ini.Set("musicVolume", want); err != nil {
		return err
	}
	if err := os.WriteFile(p+".bak-"+time.Now().Format("20060102-150405"), b, 0o644); err != nil {
		return err
	}
	return os.WriteFile(p, ini.Bytes(), 0o644)
}

func knoxInstalled() bool {
	idx, err := play.ScanInstalled()
	if err != nil || idx == nil || idx.Index == nil {
		return false
	}
	for _, m := range idx.Index.AllCopies() {
		if strings.HasPrefix(m.ID, "KnoxInventory") && !strings.Contains(m.ID, "Fix") {
			return true
		}
	}
	return false
}

func (u *ui) extraMusicCards() []fyne.CanvasObject {
	src := widget.NewRadioGroup([]string{"My music folder", "Spotify"}, nil)
	src.Horizontal = true
	src.Required = true
	spotNote := newMuted("", false)
	syncSrc := func() {
		if musicSource() == music.SourceSpotify {
			src.SetSelected("Spotify")
			st := spotify.Get()
			switch {
			case !st.Running:
				spotNote.SetText("Spotify isn't running. Start it and play something; the controls at the top follow it.")
			case st.Label() != "":
				spotNote.SetText("Spotify: " + st.Label())
			default:
				spotNote.SetText("Spotify is running.")
			}
			spotNote.rt.Show()
		} else {
			src.SetSelected("My music folder")
			spotNote.rt.Hide()
		}
	}
	src.OnChanged = func(s string) {
		want := music.SourceFolder
		if s == "Spotify" {
			want = music.SourceSpotify
		}
		if want == musicSource() {
			return
		}
		musicState.cfg.Source = want
		if want == music.SourceSpotify {
			launcherMusic.Stop()
		} else if musicState.cfg.Enabled && musicState.cfg.Folder != "" {
			_ = u.startMusic()
		}
		u.saveMusic()
		syncSrc()
	}
	syncSrc()
	u.onMusicChange(syncSrc)
	sourceCard := settingsSection("Music source", "Your own files play inside the launcher. With Spotify the launcher only shows and controls what the Spotify app plays.", src, spotNote.rt)

	gameMusic := widget.NewCheck("Allow Project Zomboid's own music (in game and in its menus)", nil)
	if on, err := gameMusicOn(); err == nil {
		gameMusic.SetChecked(on)
	} else {
		gameMusic.Disable()
	}
	gameMusic.OnChanged = func(on bool) {
		if install.GameRunning() {
			dialog.ShowInformation("Game is running", "Quit Project Zomboid first: it rewrites options.ini when it closes.", u.win)
			return
		}
		if err := setGameMusic(on); err != nil {
			dialog.ShowError(err, u.win)
			return
		}
		u.saveMusic()
	}
	gameNote := newMuted("Off sets the game's music volume to 0 in options.ini (the old value is remembered and comes back when you tick it again).", false)
	cards := []fyne.CanvasObject{sourceCard, settingsSection("Game music", "", gameMusic, gameNote.rt)}

	if knoxInstalled() {
		open := widget.NewButtonWithIcon("Open Knox Inventory music folder", theme.FolderOpenIcon(), func() {
			z, err := paths.Zomboid()
			if err != nil {
				dialog.ShowError(err, u.win)
				return
			}
			dir := filepath.Join(z, "knox_music")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				dialog.ShowError(err, u.win)
				return
			}
			u.openFolder(dir)
		})
		knoxNote := newMuted("Knox Inventory plays the files in Zomboid/knox_music on its main-menu radio; each subfolder is a playlist.", false)
		cards = append(cards, settingsSection("Knox Inventory", "", container.NewHBox(open), knoxNote.rt))
	}
	return cards
}
