package main

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const searchOpenW = 320

type widthLayout struct{ w float32 }

func (l *widthLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	h := float32(0)
	for _, o := range objs {
		h = max(h, o.MinSize().Height)
	}
	return fyne.NewSize(l.w, h)
}

func (l *widthLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Resize(fyne.NewSize(l.w, size.Height))
		o.Move(fyne.NewPos(0, 0))
		if l.w < 2 {
			o.Hide()
		} else {
			o.Show()
		}
	}
}

type searchEntry struct {
	widget.Entry
	onBlur func()
}

func newSearchEntry() *searchEntry {
	e := &searchEntry{}
	e.ExtendBaseWidget(e)
	return e
}

func (e *searchEntry) FocusLost() {
	e.Entry.FocusLost()
	if e.onBlur != nil {
		e.onBlur()
	}
}

func (e *searchEntry) TypedKey(k *fyne.KeyEvent) {
	if k.Name == fyne.KeyEscape {
		e.SetText("")
		if c := fyne.CurrentApp().Driver().CanvasForObject(e); c != nil {
			c.Unfocus()
		}
		return
	}
	e.Entry.TypedKey(k)
}

func (b *wsBrowser) searchField(onSearch func(string)) fyne.CanvasObject {
	entry := newSearchEntry()
	entry.SetPlaceHolder("Search Workshop mods...")
	wl := &widthLayout{}
	box := container.New(wl, entry)
	var anim *fyne.Animation
	open := false
	animate := func(to float32) {
		if anim != nil {
			anim.Stop()
		}
		from := wl.w
		anim = fyne.NewAnimation(220*time.Millisecond, func(f float32) {
			wl.w = from + (to-from)*f
			box.Refresh()
		})
		anim.Curve = fyne.AnimationEaseOut
		anim.Start()
	}
	var btn *widget.Button
	btn = widget.NewButtonWithIcon("", theme.SearchIcon(), func() {
		if open && entry.Text != "" {
			onSearch(entry.Text)
			return
		}
		if !open {
			open = true
			animate(searchOpenW)
			go func() {
				time.Sleep(240 * time.Millisecond)
				fyne.Do(func() { b.u.win.Canvas().Focus(entry) })
			}()
		}
	})
	btn.Importance = widget.LowImportance
	entry.OnSubmitted = func(s string) {
		if s != "" {
			onSearch(s)
		}
	}
	entry.onBlur = func() {
		if open && entry.Text == "" {
			open = false
			animate(0)
		}
	}
	return container.NewHBox(layout.NewSpacer(), box, btn)
}
