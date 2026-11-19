package main

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/kata/pzlauncher/internal/update"
	"github.com/kata/pzlauncher/internal/workshop"
)

func fakeWorkshopDeps() wsDeps {
	day := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	mk := func(id, title string, subs int64) workshop.Details {
		return workshop.Details{ID: id, Title: title, Preview: "https://images.steamusercontent.com/ugc/" + id + "/", Description: "A fixture mod description for " + title + ".", Updated: day, Size: subs * 1000, Subscriptions: subs, Stars: 4, Found: true}
	}
	all := map[string]workshop.Details{}
	for _, d := range []workshop.Details{mk("101", "Better Sorting", 512000), mk("102", "Common Sense", 1200000), mk("103", "True Music", 88000), mk("104", "Brita's Weapon Pack", 340000), mk("105", "Autotsar Trailers", 95000)} {
		all[d.ID] = d
	}
	return wsDeps{
		browse: func(ctx context.Context, q workshop.BrowseQuery) (*workshop.BrowseResult, error) {
			r := &workshop.BrowseResult{Page: 1, TotalPages: 3, Total: 74255}
			for _, id := range []string{"101", "102", "103", "104", "105"} {
				r.Items = append(r.Items, all[id])
			}
			return r, nil
		},
		details: func(ctx context.Context, ids []string) (map[string]workshop.Details, error) { return all, nil },
		subs: func() ([]workshop.Sub, error) {
			return []workshop.Sub{{ID: "103", State: workshop.SubDownloading}, {ID: "104", State: workshop.SubNeedsUpdate}, {ID: "101", State: workshop.SubInstalled}, {ID: "102", State: workshop.SubInstalled}}, nil
		},
		check: func(ctx context.Context, cid string) (*workshop.Result, error) {
			return &workshop.Result{CollectionID: cid, ACFFound: true, IDs: []string{"101", "102", "103", "104", "105"}, Total: 5, Installed: 3,
				Missing: []workshop.Item{{ID: "105", State: workshop.NotSubscribed}, {ID: "103", State: workshop.Downloading}}}, nil
		},
		thumb: func(ctx context.Context, u string) ([]byte, error) {
			img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
			c := color.NRGBA{uint8(len(u) * 37), 120, 90, 255}
			for i := range img.Pix {
				if i%4 == 3 {
					img.Pix[i] = 255
				} else if i%4 == 0 {
					img.Pix[i] = c.R
				} else {
					img.Pix[i] = 100
				}
			}
			var buf bytesBuffer
			png.Encode(&buf, img)
			return buf.b, nil
		},
		external: func(string) error { return errors.New("test: no browser") },
	}
}

type bytesBuffer struct{ b []byte }

func (w *bytesBuffer) Write(p []byte) (int, error) { w.b = append(w.b, p...); return len(p), nil }

func TestScreenshotsWorkshop(t *testing.T) {
	dir := os.Getenv("PZL_SHOTS")
	if dir == "" {
		t.Skip("set PZL_SHOTS=<folder> to render the Workshop sub-tabs to PNG")
	}
	os.MkdirAll(dir, 0o755)
	saved := workshopDeps
	workshopDeps = fakeWorkshopDeps()
	defer func() { workshopDeps = saved }()
	os.Setenv("XDG_CACHE_HOME", t.TempDir())
	a := test.NewTempApp(t)
	a.Settings().SetTheme(theme.DarkTheme())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := test.NewWindow(nil)
	defer w.Close()
	u := &ui{app: a, win: w, ctx: ctx, client: update.NewClient()}
	content := u.workshopTab()
	w.SetContent(content)
	w.Resize(fyne.NewSize(900, 820))
	var inner *container.AppTabs
	var find func(o fyne.CanvasObject)
	find = func(o fyne.CanvasObject) {
		if at, ok := o.(*container.AppTabs); ok && inner == nil {
			inner = at
			return
		}
		if c, ok := o.(*fyne.Container); ok {
			for _, ch := range c.Objects {
				find(ch)
			}
		}
	}
	find(content)
	if inner == nil {
		t.Fatal("no inner tabs")
	}
	names := []string{"Workshop-Search", "Workshop-PopularRecent", "Workshop-MySubscriptions", "Workshop-PackCollection"}
	for i := range inner.Items {
		inner.SelectIndex(i)
		time.Sleep(1500 * time.Millisecond)
		f, err := os.Create(filepath.Join(dir, names[i]+".png"))
		if err != nil {
			t.Fatal(err)
		}
		png.Encode(f, w.Canvas().Capture())
		f.Close()
	}
}

func TestThumbURL(t *testing.T) {
	if got := thumbURL("https://images.steamusercontent.com/ugc/1/ABC/"); got != "https://images.steamusercontent.com/ugc/1/ABC/?imw=128&imh=128&ima=fit&impolicy=Letterbox&imcolor=%23000000&letterbox=false" {
		t.Fatal(got)
	}
	for _, u := range []string{"https://images.steamusercontent.com/ugc/1/?x=1", "https://i.imgur.com/a.png"} {
		if thumbURL(u) != u {
			t.Fatal(u)
		}
	}
	if humanCount(1234567) != "1.2M" || humanSize(3<<20) != "3.0 MB" {
		t.Fatal(humanCount(1234567), humanSize(3<<20))
	}
}
