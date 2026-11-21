package main

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/katachinosekai/pzlauncher/internal/update"
	"github.com/katachinosekai/pzlauncher/internal/workshop"
)

func fakeWorkshopDeps() wsDeps {
	now := time.Now()
	ages := map[string]int{"101": 95, "102": 3, "103": 0, "104": 20, "105": 800}
	tags := map[string][]string{"101": {"Build 42", "Interface", "Multiplayer"}, "102": {"Build 41", "Build 42", "Realistic"}, "103": {"Build 42", "Audio"}, "104": {"Build 41", "Weapons"}, "105": {"Build 42", "Vehicles"}}
	mk := func(id, title string, subs int64) workshop.Details {
		return workshop.Details{ID: id, Title: title, Preview: "https://images.steamusercontent.com/ugc/" + id + "/", Description: "A fixture mod description for " + title + ".", Updated: now.AddDate(0, 0, -ages[id]), Size: subs * 1000, Subscriptions: subs, Stars: 4, Tags: tags[id], Found: true}
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
			return []workshop.Sub{{ID: "103", State: workshop.SubDownloading}, {ID: "104", State: workshop.SubNeedsUpdate}, {ID: "101", State: workshop.SubInstalled, Dir: "fixture/101"}, {ID: "102", State: workshop.SubInstalled, Dir: "fixture/102"}, {ID: "900", State: workshop.SubInstalled, Size: 3 << 20, Updated: now.AddDate(-1, -2, 0).Unix(), Dir: "fixture/900"}}, nil
		},
		local: func(dir string) workshop.LocalItem {
			switch dir {
			case "fixture/900":
				return workshop.LocalItem{Title: "Aardvark Private Mod", Description: "Name read from the installed mod.info.", ModIDs: []string{"AardvarkPrivate"}}
			case "fixture/101":
				return workshop.LocalItem{ModIDs: []string{"BetterSorting", "BetterSortingB42"}, Requires: []string{"\\tsarlib"}}
			case "fixture/102":
				return workshop.LocalItem{ModIDs: []string{"CommonSense"}}
			}
			return workshop.LocalItem{}
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
		active:   func() map[string]bool { return map[string]bool{"BetterSorting": true, "CommonSense": true} },
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
	saved := workshopDeps
	workshopDeps = fakeWorkshopDeps()
	defer func() { workshopDeps = saved }()
	shootWorkshop(t, dir, "", 1500*time.Millisecond, []int{0, 1, 2, 3, 4, 5}, nil)
}

func TestScreenshotsWorkshopReal(t *testing.T) {
	dir := os.Getenv("PZL_SHOTS")
	if dir == "" || os.Getenv("PZL_SHOTS_REAL") == "" {
		t.Skip("set PZL_SHOTS=<folder> PZL_SHOTS_REAL=1 to render My subscriptions from the real acf, Steam API and thumbnails")
	}
	wait := 40 * time.Second
	if d, err := time.ParseDuration(os.Getenv("PZL_SHOTS_WAIT")); err == nil {
		wait = d
	}
	shootWorkshop(t, dir, "-Real", wait, []int{0}, func(inner *container.AppTabs, capture func(string)) {
		subs, err := workshopDeps.subs()
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, len(subs))
		for i, s := range subs {
			ids[i] = s.ID
		}
		det, derr := workshopDeps.details(context.Background(), ids)
		local := map[string]workshop.LocalItem{}
		for _, s := range subs {
			local[s.ID] = workshop.ReadLocalItem(s.Dir)
		}
		idx := -1
		for i, it := range subItems(subs, det, local, derr == nil) {
			if _, ok := det[it.ID]; !ok && (idx < 0 || it.Preview != "") {
				idx = i
				if it.Preview != "" {
					break
				}
			}
		}
		if idx < 0 {
			return
		}
		var list *widget.List
		var find func(o fyne.CanvasObject)
		find = func(o fyne.CanvasObject) {
			switch v := o.(type) {
			case *widget.List:
				list = v
			case *fyne.Container:
				for _, ch := range v.Objects {
					find(ch)
				}
			}
		}
		find(inner.Items[0].Content)
		if list == nil {
			t.Fatal("no list")
		}
		list.ScrollTo(idx)
		time.Sleep(wait / 2)
		capture("Workshop-MySubscriptions-Real-NotPublic")
	})
}

func shootWorkshop(t *testing.T, dir, suffix string, wait time.Duration, tabs []int, after func(*container.AppTabs, func(string))) {
	os.MkdirAll(dir, 0o755)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	a := test.NewTempApp(t)
	a.Settings().SetTheme(appTheme{theme.DarkTheme()})
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
	capture := func(name string) {
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		png.Encode(f, w.Canvas().Capture())
		f.Close()
	}
	names := []string{"Workshop-MySubscriptions", "Workshop-RecentlyUpdated", "Workshop-MySubscribedCollection", "Workshop-Search", "Workshop-PopularRecentMods", "Workshop-SearchForCollection"}
	for _, i := range tabs {
		inner.SelectIndex(i)
		time.Sleep(wait)
		capture(names[i] + suffix)
	}
	if after != nil {
		after(inner, capture)
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

func TestSubItems(t *testing.T) {
	det := map[string]workshop.Details{
		"1": {ID: "1", Title: "zeta", Preview: "https://images.steamusercontent.com/ugc/1/"},
		"2": {ID: "2", Title: "Alpha", Preview: "https://images.steamusercontent.com/ugc/2/"},
		"5": {ID: "5", Title: "Queued", Preview: "https://images.steamusercontent.com/ugc/5/"},
	}
	subs := []workshop.Sub{
		{ID: "1", State: workshop.SubInstalled},
		{ID: "3", State: workshop.SubInstalled, Size: 2 << 20, Updated: 1700000000},
		{ID: "4", State: workshop.SubInstalled},
		{ID: "2", State: workshop.SubInstalled},
		{ID: "5", State: workshop.SubDownloading},
	}
	local := map[string]workshop.LocalItem{"3": {Title: "Mine", Image: "/x/poster.png", Description: "From mod.info."}}
	items := subItems(subs, det, local, true)
	var order []string
	for _, it := range items {
		order = append(order, it.ID)
	}
	if strings.Join(order, ",") != "5,2,3,1,4" {
		t.Fatal(order)
	}
	mine := items[2]
	if mine.displayTitle() != "Mine" || mine.Preview != "/x/poster.png" || !strings.HasSuffix(mine.Info, " years ago") || !strings.Contains(mine.Detail, "2.0 MB") || !strings.Contains(mine.Detail, "private or hidden") || mine.Desc != "From mod.info." || strings.Contains(mine.Info, "From mod.info.") {
		t.Fatalf("%+v", mine)
	}
	if items[4].displayTitle() != "Workshop item 4" || items[1].Preview != det["2"].Preview || !items[1].Installed || items[0].Installed {
		t.Fatalf("%+v", items)
	}
	if it := subItems(subs, nil, local, false); strings.Contains(it[2].Detail, "private") {
		t.Fatalf("no private note when the details call failed: %+v", it[2])
	}
}

func TestThumbCacheLocalAndFailures(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var calls int
	var mu sync.Mutex
	c := newThumbCache(func(ctx context.Context, u string) ([]byte, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return []byte("not an image"), nil
	})
	poster := filepath.Join(t.TempDir(), "poster.png")
	src := image.NewRGBA(image.Rect(0, 0, 300, 150))
	f, _ := os.Create(poster)
	png.Encode(f, src)
	f.Close()
	got := make(chan image.Image, 1)
	if c.Get(context.Background(), poster, func(im image.Image) { got <- im }) != nil {
		t.Fatal("first get is async")
	}
	select {
	case im := <-got:
		if b := im.Bounds(); b.Dx() != thumbSide || b.Dy() != thumbSide/2 {
			t.Fatal(b)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("local poster never loaded")
	}
	if c.Get(context.Background(), poster, nil) == nil {
		t.Fatal("not cached")
	}
	bad := "https://images.steamusercontent.com/ugc/9/"
	c.Get(context.Background(), bad, nil)
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.mu.Lock()
		_, failed := c.failed[bad]
		c.mu.Unlock()
		if failed || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	for range 5 {
		c.Get(context.Background(), bad, nil)
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	n := calls
	mu.Unlock()
	if n != 1 {
		t.Fatalf("failed thumbnail fetched %d times, want 1 until the retry delay", n)
	}
}

func TestUpdatedAgo(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	for days, want := range map[int]string{0: "Today", 1: "This week", 6: "This week", 7: "This month", 29: "This month", 30: "1 month ago", 59: "1 month ago", 60: "2 months ago", 364: "12 months ago", 365: "1 year ago", 729: "1 year ago", 800: "2 years ago"} {
		if got := updatedAgo(now.AddDate(0, 0, -days), now); got != want {
			t.Errorf("%d days: got %q want %q", days, got, want)
		}
	}
}

func TestBuildLabel(t *testing.T) {
	for _, c := range []struct {
		tags []string
		want string
	}{
		{[]string{"Build 41", "Map"}, "41"},
		{[]string{"Build 42", "Vehicles"}, "42"},
		{[]string{"Build 42", "Build 41"}, "41/42"},
		{[]string{"Build 40"}, "40"},
		{[]string{"build 41", "Build 42", "Build 43"}, "41/42/43"},
		{[]string{"Build 41", "Build 41"}, "41"},
		{[]string{"Map", "Buildings"}, ""},
		{nil, ""},
	} {
		if got := buildLabel(c.tags); got != c.want {
			t.Errorf("buildLabel(%q) = %q, want %q", c.tags, got, c.want)
		}
	}
}

func TestSearchInfo(t *testing.T) {
	d := workshop.Details{Tags: []string{"Build 41", "Build 42"}, Subscriptions: 12345, Stars: 4, Updated: time.Now()}
	if got, want := searchInfo(d), "41/42  ·  12k subscribers  ·  rating 4/5  ·  Today"; got != want {
		t.Errorf("searchInfo = %q, want %q", got, want)
	}
	if got := searchInfo(workshop.Details{}); got != "" {
		t.Errorf("empty searchInfo = %q", got)
	}
}

func TestRecentItems(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	at := func(h int) int64 { return now.Add(-time.Duration(h) * time.Hour).Unix() }
	all := []wsItem{{ID: "old", updated: at(24 * 40)}, {ID: "day", updated: at(2)}, {ID: "week", updated: at(24 * 3)}, {ID: "none"}, {ID: "month", updated: at(24 * 20)}, {ID: "edge", updated: at(24)}}
	ids := func(items []wsItem) string {
		var out []string
		for _, it := range items {
			out = append(out, it.ID)
		}
		return strings.Join(out, ",")
	}
	for days, want := range map[int]string{1: "day", 7: "day,edge,week", 30: "day,edge,week,month"} {
		if got := ids(recentItems(all, now, days)); got != want {
			t.Errorf("%d days: got %s want %s", days, got, want)
		}
	}
	if got := recentSummary(0, 12, recentRanges[0]); got != "None of your 12 subscriptions were updated today." {
		t.Error(got)
	}
	if got := recentSummary(1, 12, recentRanges[1]); got != "1 of your 12 subscriptions was updated this week." {
		t.Error(got)
	}
	if got := recentSummary(4, 12, recentRanges[2]); got != "4 of your 12 subscriptions were updated this month." {
		t.Error(got)
	}
}
