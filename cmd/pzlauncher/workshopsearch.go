package main

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/katachinosekai/pzlauncher/internal/workshop"
)

type growLayout struct{ f float32 }

func (l *growLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	h := float32(0)
	for _, o := range objs {
		h = max(h, o.MinSize().Height)
	}
	return fyne.NewSize(0, h)
}

func (l *growLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Resize(fyne.NewSize(size.Width*l.f, size.Height))
		o.Move(fyne.NewPos(0, 0))
		if l.f < 0.02 {
			o.Hide()
		} else {
			o.Show()
		}
	}
}

func (b *wsBrowser) searchTab() (fyne.CanvasObject, func()) {
	text := ""
	entry := widget.NewEntry()
	entry.SetPlaceHolder("Search Workshop mods...")
	gl := &growLayout{}
	box := container.New(gl, entry)
	sortSel := widget.NewSelect(workshop.SortNames, nil)
	sortSel.SetSelected(workshop.SortNames[0])
	view, load := b.browseView(container.NewBorder(nil, nil, nil, sortSel, box), func() workshop.BrowseQuery {
		return workshop.BrowseQuery{Text: text, Sort: workshop.SortFromName(sortSel.Selected)}
	})
	search := func() {
		text = entry.Text
		if text != "" {
			load()
		}
	}
	entry.OnSubmitted = func(string) { search() }
	sortSel.OnChanged = func(string) { search() }
	var anim *fyne.Animation
	b.searchShown = func() {
		if anim != nil {
			anim.Stop()
		}
		if gl.f < 1 {
			gl.f = 0
			box.Refresh()
			anim = fyne.NewAnimation(260*time.Millisecond, func(f float32) {
				gl.f = f
				box.Refresh()
			})
			anim.Curve = fyne.AnimationEaseOut
			anim.Start()
		}
		go func() {
			time.Sleep(280 * time.Millisecond)
			fyne.Do(func() { b.u.win.Canvas().Focus(entry) })
		}()
	}
	return view, b.searchShown
}
