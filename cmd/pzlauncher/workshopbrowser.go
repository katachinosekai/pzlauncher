package main

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/kata/pzlauncher/internal/install"
	"github.com/kata/pzlauncher/internal/webview"
	"github.com/kata/pzlauncher/internal/workshop"
)

func init() {
	if len(os.Args) > 1 && os.Args[1] == webview.HelperFlag {
		os.Exit(webview.RunHelper(webview.Options{Open: install.DefaultSystem().OpenURL}))
	}
	registerTab(25, "Workshop", (*ui).workshopTab)
}

type wsDeps struct {
	browse   func(ctx context.Context, q workshop.BrowseQuery) (*workshop.BrowseResult, error)
	details  func(ctx context.Context, ids []string) (map[string]workshop.Details, error)
	subs     func() ([]workshop.Sub, error)
	check    func(ctx context.Context, cid string) (*workshop.Result, error)
	thumb    func(ctx context.Context, u string) ([]byte, error)
	external func(u string) error
}

var workshopDeps = wsDeps{
	browse: func(ctx context.Context, q workshop.BrowseQuery) (*workshop.BrowseResult, error) {
		return workshop.Browse(ctx, nil, "", q)
	},
	details: func(ctx context.Context, ids []string) (map[string]workshop.Details, error) {
		return workshop.FetchDetails(ctx, nil, "", ids)
	},
	subs: workshop.LocalSubscriptions,
	check: func(ctx context.Context, cid string) (*workshop.Result, error) {
		return workshop.Check(ctx, workshop.Options{CollectionID: cid})
	},
	external: func(u string) error { return install.DefaultSystem().OpenURL(u) },
}

const wsHint = "Subscribe on Steam's own Workshop page in the built-in browser. Sign in there if Steam asks: the login is Steam's page, the launcher never sees your password. Steam downloads new subscriptions in the Steam client, and downloads may stall while the game is running."

type wsItem struct {
	ID         string
	Title      string
	Info       string
	State      string
	Subscribed bool
	Preview    string
}

type wsBrowser struct {
	u      *ui
	deps   wsDeps
	ctl    *webview.Controller
	thumbs *thumbCache
	engine *widget.Label

	subMu     sync.Mutex
	onSubbed  []func(id string)
	subbedIDs map[string]bool
}

func (u *ui) workshopTab() fyne.CanvasObject {
	b := &wsBrowser{u: u, deps: workshopDeps, subbedIDs: map[string]bool{}}
	b.thumbs = newThumbCache(b.deps.thumb)
	b.engine = widget.NewLabel("Built-in browser: opens when you pick a mod.")
	b.engine.Importance = widget.LowImportance
	b.ctl = &webview.Controller{
		OpenExternal: b.deps.external,
		OnFallback: func(err error) {
			fyne.Do(func() {
				b.engine.SetText("Built-in browser unavailable: pages open in your default web browser.")
				msg := "The built-in browser could not start, so Steam pages open in your default web browser instead."
				if err != nil {
					msg += "\n\nReason: " + err.Error()
				}
				dialog.ShowInformation("Opening pages in your web browser", msg, u.win)
			})
		},
		OnEvent: func(e webview.Event) {
			switch e.Event {
			case webview.EvURL:
				fyne.Do(func() { b.engine.SetText("Built-in browser: " + e.URL) })
			case webview.EvSubscribed:
				if e.Subscribed {
					b.markSubscribed(e.ID)
				}
			}
		},
	}
	signIn := widget.NewButtonWithIcon("Sign in to Steam", theme.LoginIcon(), func() { b.open(workshop.LoginURL) })
	hint := widget.NewLabel(wsHint)
	hint.Wrapping = fyne.TextWrapWord
	hint.Importance = widget.LowImportance
	b.engine.Truncation = fyne.TextTruncateEllipsis
	top := container.NewVBox(container.NewBorder(nil, nil, nil, signIn, b.engine), hint)

	tabs := container.NewAppTabs(
		container.NewTabItem("Search", b.searchTab()),
		container.NewTabItem("Popular / Recent", widget.NewLabel("")),
		container.NewTabItem("My subscriptions", widget.NewLabel("")),
		container.NewTabItem("Pack collection", widget.NewLabel("")),
	)
	builders := []func() (fyne.CanvasObject, func()){nil, b.popularTab, b.subsTab, b.collectionTab}
	loaded := make([]bool, len(builders))
	tabs.OnSelected = func(it *container.TabItem) {
		i := tabs.SelectedIndex()
		if i <= 0 || loaded[i] {
			return
		}
		loaded[i] = true
		content, start := builders[i]()
		it.Content = content
		tabs.Refresh()
		if start != nil {
			start()
		}
	}
	return container.NewBorder(top, nil, nil, nil, tabs)
}

func (b *wsBrowser) open(u string) {
	go func() {
		if err := b.ctl.Open(u); err != nil {
			fyne.Do(func() { dialog.ShowError(err, b.u.win) })
		}
	}()
}

func (b *wsBrowser) markSubscribed(id string) {
	b.subMu.Lock()
	b.subbedIDs[id] = true
	cbs := append([]func(string){}, b.onSubbed...)
	b.subMu.Unlock()
	for _, cb := range cbs {
		cb(id)
	}
}

func (b *wsBrowser) watchSubscribed(cb func(id string)) {
	b.subMu.Lock()
	b.onSubbed = append(b.onSubbed, cb)
	b.subMu.Unlock()
}

type wsRow struct {
	widget.BaseWidget
	img     *canvas.Image
	title   *widget.Label
	info    *widget.Label
	btn     *widget.Button
	preview string
	content fyne.CanvasObject
}

func newWSRow() *wsRow {
	r := &wsRow{}
	r.img = canvas.NewImageFromImage(nil)
	r.img.FillMode = canvas.ImageFillContain
	r.img.SetMinSize(fyne.NewSize(64, 64))
	bg := canvas.NewRectangle(color.NRGBA{128, 128, 128, 40})
	bg.CornerRadius = 4
	r.title = widget.NewLabel("")
	r.title.TextStyle.Bold = true
	r.title.Truncation = fyne.TextTruncateEllipsis
	r.info = widget.NewLabel("")
	r.info.Truncation = fyne.TextTruncateEllipsis
	r.info.Importance = widget.LowImportance
	r.btn = widget.NewButton("Subscribe", nil)
	text := container.New(&tightVBox{}, r.title, r.info)
	r.content = container.NewBorder(nil, nil, container.NewStack(bg, r.img), container.NewCenter(r.btn), text)
	r.ExtendBaseWidget(r)
	return r
}

func (r *wsRow) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(r.content) }

type tightVBox struct{}

func (tightVBox) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var s fyne.Size
	for _, o := range objs {
		m := o.MinSize()
		s.Width = max(s.Width, m.Width)
		s.Height += m.Height - 8
	}
	return s
}

func (tightVBox) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	y := (size.Height - tightVBox{}.MinSize(objs).Height) / 2
	for _, o := range objs {
		h := o.MinSize().Height
		o.Move(fyne.NewPos(0, y-4))
		o.Resize(fyne.NewSize(size.Width, h))
		y += h - 8
	}
}

func (b *wsBrowser) itemList(items *[]wsItem) *widget.List {
	var list *widget.List
	list = widget.NewList(
		func() int { return len(*items) },
		func() fyne.CanvasObject { return newWSRow() },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i >= len(*items) {
				return
			}
			it := (*items)[i]
			r := o.(*wsRow)
			title := it.Title
			if title == "" {
				title = "Workshop item " + it.ID
			}
			r.title.SetText(title)
			info := it.Info
			if it.State != "" {
				info = strings.ToUpper(it.State) + "  ·  " + info
			}
			r.info.SetText(info)
			b.subMu.Lock()
			subbed := it.Subscribed || b.subbedIDs[it.ID]
			b.subMu.Unlock()
			if subbed {
				r.btn.SetText("Open page")
				r.btn.Importance = widget.MediumImportance
			} else {
				r.btn.SetText("Subscribe")
				r.btn.Importance = widget.HighImportance
			}
			r.btn.Refresh()
			id := it.ID
			r.btn.OnTapped = func() { b.open(workshop.ItemURL(id)) }
			r.preview = it.Preview
			preview := it.Preview
			im := b.thumbs.Get(b.u.ctx, preview, func(im image.Image) {
				fyne.Do(func() {
					if r.preview == preview {
						r.img.Image = im
						r.img.Refresh()
					}
				})
			})
			r.img.Image = im
			r.img.Refresh()
		},
	)
	list.OnSelected = func(i widget.ListItemID) {
		if i < len(*items) {
			b.open(workshop.ItemURL((*items)[i].ID))
		}
		list.UnselectAll()
	}
	return list
}

func humanCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 10_000:
		return fmt.Sprintf("%dk", n/1000)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprint(n)
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}

func detailsInfo(d workshop.Details) string {
	var parts []string
	if d.Subscriptions > 0 {
		parts = append(parts, humanCount(d.Subscriptions)+" subscribers")
	}
	if d.Stars > 0 {
		parts = append(parts, fmt.Sprintf("rating %d/5", d.Stars))
	}
	if d.Size > 0 {
		parts = append(parts, humanSize(d.Size))
	}
	if !d.Updated.IsZero() {
		parts = append(parts, "updated "+d.Updated.Format("2006-01-02"))
	}
	if d.Description != "" {
		parts = append(parts, d.Description)
	}
	return strings.Join(parts, "  ·  ")
}

func (b *wsBrowser) browseView(controls fyne.CanvasObject, query func() workshop.BrowseQuery) (fyne.CanvasObject, func()) {
	var items []wsItem
	list := b.itemList(&items)
	status := widget.NewLabel("")
	status.Truncation = fyne.TextTruncateEllipsis
	page := 1
	var current workshop.BrowseQuery
	gen := 0
	more := widget.NewButton("More results", nil)
	more.Hide()
	var load func(reset bool)
	load = func(reset bool) {
		if reset {
			current = query()
			page = 1
		} else {
			page++
		}
		q := current
		q.Page = page
		gen++
		my := gen
		status.SetText("Loading from the Steam Workshop...")
		more.Disable()
		go func() {
			r, err := b.deps.browse(b.u.ctx, q)
			fyne.Do(func() {
				if my != gen {
					return
				}
				more.Enable()
				if err != nil {
					status.SetText("Could not load the list: " + err.Error() + " (use \"Open in browser\")")
					if reset {
						items = nil
						list.Refresh()
					}
					more.Hide()
					return
				}
				if reset {
					items = nil
					list.ScrollToTop()
				}
				for _, d := range r.Items {
					items = append(items, wsItem{ID: d.ID, Title: d.Title, Info: detailsInfo(d), Preview: d.Preview})
				}
				list.Refresh()
				status.SetText(fmt.Sprintf("%s: showing %d of %d items.", q.Sort, len(items), r.Total))
				if r.TotalPages > page && len(r.Items) > 0 {
					more.Show()
				} else {
					more.Hide()
				}
			})
		}()
	}
	more.OnTapped = func() { load(false) }
	openBrowse := widget.NewButtonWithIcon("Open in browser", theme.ComputerIcon(), func() {
		q := query()
		b.open(workshop.BrowseURL("", q))
	})
	top := container.NewVBox(container.NewBorder(nil, nil, nil, openBrowse, controls), status)
	return container.NewBorder(top, container.NewCenter(more), nil, nil, list), func() { load(true) }
}

func (b *wsBrowser) searchTab() fyne.CanvasObject {
	entry := widget.NewEntry()
	entry.SetPlaceHolder("Search Project Zomboid mods...")
	sortSel := widget.NewSelect(workshop.SortNames, nil)
	sortSel.SetSelected(workshop.SortNames[0])
	var load func()
	searchBtn := widget.NewButtonWithIcon("Search", theme.SearchIcon(), func() { load() })
	controls := container.NewBorder(nil, nil, nil, container.NewHBox(sortSel, searchBtn), entry)
	view, l := b.browseView(controls, func() workshop.BrowseQuery {
		return workshop.BrowseQuery{Text: entry.Text, Sort: workshop.SortFromName(sortSel.Selected)}
	})
	load = l
	entry.OnSubmitted = func(string) { load() }
	sortSel.OnChanged = func(string) {
		if entry.Text != "" {
			load()
		}
	}
	return view
}

func (b *wsBrowser) popularTab() (fyne.CanvasObject, func()) {
	opts := []string{"Popular this week", "Most recent"}
	radio := widget.NewRadioGroup(opts, nil)
	radio.Horizontal = true
	radio.Required = true
	radio.SetSelected(opts[0])
	var load func()
	refresh := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), func() { load() })
	view, l := b.browseView(container.NewBorder(nil, nil, nil, refresh, radio), func() workshop.BrowseQuery {
		if radio.Selected == opts[1] {
			return workshop.BrowseQuery{Sort: workshop.SortRecent}
		}
		return workshop.BrowseQuery{Sort: workshop.SortTrendWeek}
	})
	load = l
	radio.OnChanged = func(string) { load() }
	return view, load
}

func subStateLabel(s workshop.SubState) string {
	switch s {
	case workshop.SubInstalled:
		return "Installed"
	case workshop.SubNeedsUpdate:
		return "Update pending"
	}
	return "Downloading"
}

func (b *wsBrowser) subsTab() (fyne.CanvasObject, func()) {
	var items []wsItem
	list := b.itemList(&items)
	summary := widget.NewLabel("")
	summary.Wrapping = fyne.TextWrapWord
	var refresh *widget.Button
	load := func() {
		refresh.Disable()
		summary.SetText("Reading your Steam Workshop subscriptions...")
		go func() {
			subs, err := b.deps.subs()
			var det map[string]workshop.Details
			var derr error
			if err == nil && len(subs) > 0 {
				ids := make([]string, len(subs))
				for i, s := range subs {
					ids[i] = s.ID
				}
				det, derr = b.deps.details(b.u.ctx, ids)
			}
			fyne.Do(func() {
				refresh.Enable()
				if err != nil {
					summary.SetText("Could not read Steam's Workshop file (appworkshop_108600.acf): " + err.Error())
					items = nil
					list.Refresh()
					return
				}
				counts := map[workshop.SubState]int{}
				items = items[:0]
				sort.SliceStable(subs, func(i, j int) bool {
					if subs[i].State != subs[j].State {
						return subs[i].State > subs[j].State
					}
					return strings.ToLower(det[subs[i].ID].Title) < strings.ToLower(det[subs[j].ID].Title)
				})
				for _, s := range subs {
					counts[s.State]++
					d := det[s.ID]
					info := detailsInfo(d)
					if d.Title == "" && s.Size > 0 {
						info = humanSize(s.Size)
					}
					items = append(items, wsItem{ID: s.ID, Title: d.Title, Info: info, State: subStateLabel(s.State), Subscribed: true, Preview: d.Preview})
				}
				list.Refresh()
				msg := fmt.Sprintf("%d subscribed: %d installed, %d update pending, %d downloading or queued.", len(subs), counts[workshop.SubInstalled], counts[workshop.SubNeedsUpdate], counts[workshop.SubDownloading])
				if derr != nil {
					msg += " Titles unavailable: " + derr.Error()
				}
				summary.SetText(msg)
			})
		}()
	}
	refresh = widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), load)
	top := container.NewBorder(nil, nil, nil, refresh, summary)
	return container.NewBorder(top, nil, nil, nil, list), load
}

func (b *wsBrowser) collectionTab() (fyne.CanvasObject, func()) {
	prefs := b.u.app.Preferences()
	colEntry := widget.NewEntry()
	colEntry.SetText(prefs.StringWithFallback(prefCollection, workshop.DefaultCollectionID))
	cid := func() string {
		c := strings.TrimSpace(colEntry.Text)
		if c == "" {
			c = workshop.DefaultCollectionID
		}
		return c
	}
	var items []wsItem
	list := b.itemList(&items)
	summary := widget.NewLabel("")
	summary.Wrapping = fyne.TextWrapWord
	progress := widget.NewLabel("")
	progress.Wrapping = fyne.TextWrapWord

	var missing []wsItem
	queue := []wsItem{}
	pos := -1
	var next, stop, subMissing *widget.Button
	showStep := func() {
		if pos < 0 || pos >= len(queue) {
			if pos >= len(queue) && len(queue) > 0 {
				progress.SetText("Done: every missing item's page was opened. Press Refresh after Steam has downloaded them.")
			} else {
				progress.SetText("")
			}
			pos = -1
			next.Hide()
			stop.Hide()
			subMissing.Show()
			return
		}
		it := queue[pos]
		title := it.Title
		if title == "" {
			title = it.ID
		}
		progress.SetText(fmt.Sprintf("Missing item %d of %d: %s. Press Subscribe on the Steam page; the next one opens by itself once Steam shows it subscribed (or press Next missing).", pos+1, len(queue), title))
		next.Show()
		stop.Show()
		subMissing.Hide()
		b.open(workshop.ItemURL(it.ID))
	}
	var advanceTimer *time.Timer
	b.watchSubscribed(func(id string) {
		fyne.Do(func() {
			list.Refresh()
			if pos >= 0 && pos < len(queue) && queue[pos].ID == id {
				if advanceTimer != nil {
					advanceTimer.Stop()
				}
				want := pos
				advanceTimer = time.AfterFunc(1500*time.Millisecond, func() {
					fyne.Do(func() {
						if pos == want {
							pos++
							showStep()
						}
					})
				})
			}
		})
	})
	next = widget.NewButtonWithIcon("Next missing", theme.NavigateNextIcon(), func() {
		pos++
		showStep()
	})
	stop = widget.NewButton("Stop", func() {
		pos = len(queue)
		showStep()
		progress.SetText("")
	})
	subMissing = widget.NewButtonWithIcon("Subscribe to missing", theme.DownloadIcon(), func() {
		queue = queue[:0]
		for _, m := range missing {
			b.subMu.Lock()
			done := b.subbedIDs[m.ID]
			b.subMu.Unlock()
			if !m.Subscribed && !done {
				queue = append(queue, m)
			}
		}
		if len(queue) == 0 {
			progress.SetText("Nothing to subscribe: every missing item is already subscribed and waits for Steam to download it.")
			return
		}
		pos = 0
		showStep()
	})
	subMissing.Disable()
	next.Hide()
	stop.Hide()

	var refresh *widget.Button
	load := func() {
		c := cid()
		prefs.SetString(prefCollection, c)
		refresh.Disable()
		summary.SetText("Checking collection " + c + "...")
		go func() {
			r, err := b.deps.check(b.u.ctx, c)
			var det map[string]workshop.Details
			var derr error
			if err == nil {
				det, derr = b.deps.details(b.u.ctx, r.IDs)
			}
			fyne.Do(func() {
				refresh.Enable()
				if err != nil {
					summary.SetText("Check failed: " + err.Error())
					items = nil
					list.Refresh()
					return
				}
				miss := map[string]workshop.Item{}
				for _, m := range r.Missing {
					miss[m.ID] = m
				}
				items = items[:0]
				missing = missing[:0]
				var installed []wsItem
				for _, m := range r.Missing {
					d := det[m.ID]
					title := d.Title
					if title == "" {
						title = m.Title
					}
					label := "Missing"
					subbed := false
					switch m.State {
					case workshop.Downloading:
						label, subbed = "Subscribed, downloading", true
					case workshop.NotSubscribed:
						label = "Not subscribed"
					}
					it := wsItem{ID: m.ID, Title: title, Info: detailsInfo(d), State: label, Subscribed: subbed, Preview: d.Preview}
					missing = append(missing, it)
				}
				for _, id := range r.IDs {
					if _, ok := miss[id]; ok {
						continue
					}
					d := det[id]
					installed = append(installed, wsItem{ID: id, Title: d.Title, Info: detailsInfo(d), State: "Installed", Subscribed: true, Preview: d.Preview})
				}
				sort.SliceStable(installed, func(i, j int) bool {
					return strings.ToLower(installed[i].Title) < strings.ToLower(installed[j].Title)
				})
				items = append(append(items, missing...), installed...)
				list.Refresh()
				msg := fmt.Sprintf("%d of %d collection items downloaded.", r.Installed, r.Total)
				if len(r.Missing) == 0 {
					msg += " Everything is there."
				} else {
					var subs, notSubs int
					for _, m := range r.Missing {
						switch m.State {
						case workshop.Downloading:
							subs++
						case workshop.NotSubscribed:
							notSubs++
						}
					}
					msg += fmt.Sprintf(" %d missing", len(r.Missing))
					if r.ACFFound {
						msg += fmt.Sprintf(" (%d subscribed but not downloaded yet, %d not subscribed)", subs, notSubs)
					}
					msg += "."
				}
				if derr != nil {
					msg += " Titles unavailable: " + derr.Error()
				}
				summary.SetText(msg)
				if len(missing) > 0 {
					subMissing.Enable()
				} else {
					subMissing.Disable()
				}
			})
		}()
	}
	refresh = widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), load)
	openCol := widget.NewButtonWithIcon("Open collection page", theme.ComputerIcon(), func() { b.open(workshop.ItemURL(cid())) })
	actions := container.NewHBox(subMissing, next, stop, openCol)
	top := container.NewVBox(
		container.NewBorder(nil, nil, widget.NewLabel("Collection id"), refresh, colEntry),
		summary,
		actions,
		progress,
	)
	return container.NewBorder(top, nil, nil, nil, list), load
}
