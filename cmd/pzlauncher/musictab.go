package main

import (
	"image/color"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/katachinosekai/pzlauncher/internal/music"
	"github.com/katachinosekai/pzlauncher/internal/spotify"
)

func init() { registerTab(45, "Music", (*ui).musicTab) }

func trackName(p string) string { return strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)) }

func (u *ui) musicTab() fyne.CanvasObject {
	var all, shown []string
	cur := ""
	search := widget.NewEntry()
	search.SetPlaceHolder("Search your music...")
	count := newMuted("", false)
	list := widget.NewList(
		func() int { return len(shown) },
		func() fyne.CanvasObject {
			title := widget.NewLabel("")
			title.Truncation = fyne.TextTruncateEllipsis
			folder := widget.NewLabel("")
			folder.Importance = widget.LowImportance
			folder.Truncation = fyne.TextTruncateEllipsis
			icon := widget.NewIcon(theme.MediaPlayIcon())
			return container.NewBorder(nil, nil, icon, nil, container.NewGridWithColumns(2, title, folder))
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i >= len(shown) {
				return
			}
			p := shown[i]
			row := o.(*fyne.Container)
			grid := row.Objects[0].(*fyne.Container)
			icon := row.Objects[1].(*widget.Icon)
			title := grid.Objects[0].(*widget.Label)
			folder := grid.Objects[1].(*widget.Label)
			title.SetText(trackName(p))
			rel := filepath.Dir(p)
			if base := musicState.cfg.Folder; base != "" {
				if r, err := filepath.Rel(base, rel); err == nil {
					rel = r
				}
			}
			if rel == "." {
				rel = ""
			}
			folder.SetText(rel)
			if p == cur {
				title.TextStyle = fyne.TextStyle{Bold: true}
				title.Importance = widget.HighImportance
				icon.SetResource(theme.VolumeUpIcon())
			} else {
				title.TextStyle = fyne.TextStyle{}
				title.Importance = widget.MediumImportance
				icon.SetResource(theme.MediaPlayIcon())
			}
			title.Refresh()
		},
	)
	filter := func() {
		q := strings.ToLower(strings.TrimSpace(search.Text))
		shown = shown[:0]
		for _, p := range all {
			if q == "" || strings.Contains(strings.ToLower(p), q) {
				shown = append(shown, p)
			}
		}
		if len(all) == 0 {
			count.SetText("Turn music on and choose a folder to see your tracks here.")
		} else {
			count.SetText(strings.TrimSpace(strings.Join([]string{itoa(len(shown)), "of", itoa(len(all)), "tracks. Click one to play it."}, " ")))
		}
		list.Refresh()
	}
	search.OnChanged = func(string) { filter() }
	list.OnSelected = func(i widget.ListItemID) {
		list.UnselectAll()
		if i < len(shown) {
			launcherMusic.PlayTrack(shown[i])
		}
	}
	reload := func() {
		all = launcherMusic.Tracks()
		cur = launcherMusic.Current()
		filter()
	}
	u.onMusicChange(func() {
		if len(launcherMusic.Tracks()) != len(all) {
			reload()
			return
		}
		cur = launcherMusic.Current()
		list.Refresh()
	})
	reload()
	listArea := canvas.NewRectangle(color.Transparent)
	listArea.SetMinSize(fyne.NewSize(0, 360))
	library := settingsSection("Library", "", search, count.rt, container.NewStack(listArea, list))
	extra := u.extraMusicCards()
	folderCard := u.musicCard()
	showFolder := func() {
		if musicSource() == music.SourceSpotify {
			folderCard.Hide()
			library.Hide()
		} else {
			folderCard.Show()
			library.Show()
		}
	}
	showFolder()
	u.onMusicChange(showFolder)
	cards := []fyne.CanvasObject{extra[0], folderCard, library}
	cards = append(cards, extra[1:]...)
	return container.NewVScroll(container.NewVBox(cards...))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

var musicWatchers []func()

func (u *ui) onMusicChange(fn func()) { musicWatchers = append(musicWatchers, fn) }

func musicChanged() {
	for _, fn := range musicWatchers {
		fn()
	}
}

type nowPlaying struct {
	on, paused bool
	title      string
}

func currentPlaying() nowPlaying {
	if musicSource() == music.SourceSpotify {
		st := spotify.Get()
		return nowPlaying{on: st.Running && st.Label() != "", paused: !st.Playing, title: st.Label()}
	}
	if !launcherMusic.Playing() {
		return nowPlaying{}
	}
	return nowPlaying{on: true, paused: launcherMusic.Paused(), title: trackName(launcherMusic.Current())}
}

func marquee(s string, pos, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	loop := append(append([]rune{}, r...), []rune("   •   ")...)
	pos %= len(loop)
	out := append(loop[pos:], loop[:pos]...)
	return string(out[:width])
}

func (u *ui) miniPlayer() fyne.CanvasObject {
	const titleChars = 30
	prev := widget.NewButtonWithIcon("", theme.MediaSkipPreviousIcon(), nil)
	toggle := widget.NewButtonWithIcon("", theme.MediaPauseIcon(), nil)
	next := widget.NewButtonWithIcon("", theme.MediaSkipNextIcon(), nil)
	for _, b := range []*widget.Button{prev, toggle, next} {
		b.Importance = widget.LowImportance
	}
	name := widget.NewLabel("")
	name.Truncation = fyne.TextTruncateClip
	nameBox := container.NewGridWrap(fyne.NewSize(230, name.MinSize().Height), name)
	var np nowPlaying
	pos := 0
	sync := func() {
		if np.paused {
			pos = 0
			toggle.SetIcon(theme.MediaPlayIcon())
		} else {
			toggle.SetIcon(theme.MediaPauseIcon())
		}
		name.SetText(marquee(np.title, pos, titleChars))
	}
	prev.OnTapped = func() {
		if musicSource() == music.SourceSpotify {
			_ = spotify.Prev()
		} else {
			launcherMusic.Prev()
		}
	}
	next.OnTapped = func() {
		if musicSource() == music.SourceSpotify {
			_ = spotify.Next()
		} else {
			launcherMusic.Next()
		}
	}
	toggle.OnTapped = func() {
		if musicSource() == music.SourceSpotify {
			_ = spotify.PlayPause()
		} else {
			launcherMusic.SetPaused(!launcherMusic.Paused())
		}
		np = currentPlaying()
		sync()
		musicChanged()
	}
	controls := container.NewHBox(nameBox, prev, toggle, next)
	cover := canvas.NewRectangle(theme.Color(theme.ColorNameBackground))
	box := container.NewStack(controls, cover)
	box.Hide()
	var anim *fyne.Animation
	fadeIn := func() {
		if anim != nil {
			anim.Stop()
		}
		bg := theme.Color(theme.ColorNameBackground)
		r, g, b, _ := bg.RGBA()
		cover.FillColor = bg
		cover.Show()
		box.Show()
		anim = fyne.NewAnimation(600*time.Millisecond, func(f float32) {
			cover.FillColor = color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(255 * (1 - f))}
			cover.Refresh()
			if f >= 1 {
				cover.Hide()
			}
		})
		anim.Curve = fyne.AnimationEaseOut
		anim.Start()
	}
	shown := false
	lastTitle := ""
	check := func() {
		np = currentPlaying()
		if np.title != lastTitle {
			lastTitle, pos = np.title, 0
			if shown && np.on {
				fadeIn()
			}
		}
		if np.on && !shown {
			shown = true
			fadeIn()
		} else if !np.on && shown {
			shown = false
			box.Hide()
		}
		sync()
	}
	u.onMusicChange(check)
	slow := time.NewTicker(1500 * time.Millisecond)
	fast := time.NewTicker(250 * time.Millisecond)
	u.closers = append(u.closers, slow.Stop, fast.Stop)
	go func() {
		for range slow.C {
			fyne.Do(check)
		}
	}()
	go func() {
		for range fast.C {
			fyne.Do(func() {
				if shown && !np.paused && len([]rune(np.title)) > titleChars {
					pos++
					name.SetText(marquee(np.title, pos, titleChars))
				}
			})
		}
	}()
	return container.NewHBox(box, layout.NewSpacer())
}
