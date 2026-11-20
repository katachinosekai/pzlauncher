package main

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/katachinosekai/pzlauncher/internal/update"
	"github.com/katachinosekai/pzlauncher/internal/workshop"
)

func TestBrowseLoadsNextPageNearBottom(t *testing.T) {
	a := test.NewTempApp(t)
	w := test.NewWindow(nil)
	defer w.Close()
	var calls atomic.Int32
	deps := fakeWorkshopDeps()
	deps.browse = func(ctx context.Context, q workshop.BrowseQuery) (*workshop.BrowseResult, error) {
		calls.Add(1)
		r := &workshop.BrowseResult{Page: q.Page, TotalPages: 3, Total: 90}
		for i := 0; i < 30; i++ {
			r.Items = append(r.Items, workshop.Details{ID: fmt.Sprintf("%d", q.Page*100+i), Title: fmt.Sprintf("Mod %d-%d", q.Page, i), Subscriptions: int64(i)})
		}
		return r, nil
	}
	u := &ui{app: a, win: w, ctx: context.Background(), client: update.NewClient()}
	b := &wsBrowser{u: u, deps: deps, busy: map[string]bool{}, subState: map[string]bool{}}
	b.thumbs = newThumbCache(deps.thumb)
	view, load := b.popularTab()
	w.SetContent(view)
	w.Resize(fyne.NewSize(900, 700))
	load()
	time.Sleep(500 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("first load: %d calls", calls.Load())
	}
	var list *widget.List
	var find func(o fyne.CanvasObject)
	find = func(o fyne.CanvasObject) {
		if l, ok := o.(*widget.List); ok {
			list = l
		}
		if c, ok := o.(*fyne.Container); ok {
			for _, ch := range c.Objects {
				find(ch)
			}
		}
	}
	find(view)
	fyne.Do(list.ScrollToBottom)
	time.Sleep(800 * time.Millisecond)
	if calls.Load() < 2 {
		t.Fatalf("scrolling to the bottom did not load page 2 (calls=%d)", calls.Load())
	}
}
