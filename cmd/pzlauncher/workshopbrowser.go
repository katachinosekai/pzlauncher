package main

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/katachinosekai/pzlauncher/internal/install"
	"github.com/katachinosekai/pzlauncher/internal/webview"
	"github.com/katachinosekai/pzlauncher/internal/workshop"
)

func init() {
	if len(os.Args) > 1 && os.Args[1] == webview.HelperFlag {
		os.Exit(webview.RunHelper(webview.Options{Open: install.DefaultSystem().OpenURL, Ephemeral: slices.Contains(os.Args[2:], webview.EphemeralFlag)}))
	}
	registerTab(25, "Workshop", (*ui).workshopTab)
}

type wsDeps struct {
	browse   func(ctx context.Context, q workshop.BrowseQuery) (*workshop.BrowseResult, error)
	details  func(ctx context.Context, ids []string) (map[string]workshop.Details, error)
	subs     func() ([]workshop.Sub, error)
	check    func(ctx context.Context, cid string) (*workshop.Result, error)
	thumb    func(ctx context.Context, u string) ([]byte, error)
	local    func(itemDir string) workshop.LocalItem
	external func(u string) error
}

var workshopDeps = wsDeps{
	browse: func(ctx context.Context, q workshop.BrowseQuery) (*workshop.BrowseResult, error) {
		return workshop.Browse(ctx, nil, "", q)
	},
	details: func(ctx context.Context, ids []string) (map[string]workshop.Details, error) {
		return workshop.FetchDetails(ctx, nil, "", ids)
	},
	subs:  workshop.LocalSubscriptions,
	local: workshop.ReadLocalItem,
	check: func(ctx context.Context, cid string) (*workshop.Result, error) {
		return workshop.Check(ctx, workshop.Options{CollectionID: cid})
	},
	external: func(u string) error { return install.DefaultSystem().OpenURL(u) },
}

const wsHint = "Subscribe and Unsubscribe run in the background on Steam's own Workshop page in a hidden built-in browser. Sign in once with Steam's QR code or login page; the launcher never sees your password. Steam downloads new subscriptions in the Steam client, and downloads may stall while the game is running."

type wsItem struct {
	ID         string
	Title      string
	Info       string
	State      string
	Subscribed bool
	Installed  bool
	Preview    string
	sub        workshop.SubState
}

func (it wsItem) displayTitle() string {
	if it.Title == "" {
		return "Workshop item " + it.ID
	}
	return it.Title
}

type wsBrowser struct {
	u      *ui
	deps   wsDeps
	ctl    *webview.Controller
	thumbs *thumbCache
	status *widget.Label
	sess   *wsSession

	busy     map[string]bool
	subState map[string]bool
	watchers []func()
}

func (u *ui) workshopTab() fyne.CanvasObject {
	b := &wsBrowser{u: u, deps: workshopDeps, busy: map[string]bool{}, subState: map[string]bool{}}
	b.thumbs = newThumbCache(b.deps.thumb)
	b.status = widget.NewLabel("")
	b.status.Importance = widget.LowImportance
	b.status.Truncation = fyne.TextTruncateEllipsis
	b.ctl = &webview.Controller{
		OnFallback: func(err error) { fyne.Do(func() { b.sess.onFallback(err) }) },
		OnEvent:    func(e webview.Event) { fyne.Do(func() { b.sess.onEvent(e) }) },
	}
	b.sess = newWSSession(b)
	u.closers = append(u.closers, b.ctl.Close)
	u.onTab("Workshop", b.sess.ensure)
	hint := widget.NewLabel(wsHint)
	hint.Wrapping = fyne.TextWrapWord
	hint.Importance = widget.LowImportance
	top := container.NewVBox(container.NewBorder(nil, nil, nil, container.NewHBox(b.sess.signInBtn, b.sess.signOutBtn), b.sess.label), b.status, hint)

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

func (b *wsBrowser) openExternal(u string) {
	if err := b.deps.external(u); err != nil {
		dialog.ShowError(err, b.u.win)
	}
}

func (b *wsBrowser) subscribed(it wsItem) bool {
	if v, ok := b.subState[it.ID]; ok {
		return v
	}
	return it.Subscribed
}

func (b *wsBrowser) notify() {
	for _, w := range b.watchers {
		w()
	}
}

func (b *wsBrowser) watch(fn func()) { b.watchers = append(b.watchers, fn) }

func (b *wsBrowser) setSub(id string, unsub bool, cb func(webview.Event)) {
	b.busy[id] = true
	b.notify()
	b.sess.subscribe(id, unsub, func(e webview.Event) {
		delete(b.busy, id)
		switch e.Result {
		case webview.ResultDone, webview.ResultAlready:
			b.subState[id] = !unsub
		}
		b.notify()
		cb(e)
	})
}

func (b *wsBrowser) toggle(it wsItem) {
	unsub := b.subscribed(it)
	title := it.Title
	if title == "" {
		title = "item " + it.ID
	}
	verb := "Subscribing to"
	if unsub {
		verb = "Unsubscribing from"
	}
	b.status.SetText(verb + " " + title + "...")
	b.setSub(it.ID, unsub, func(e webview.Event) {
		switch e.Result {
		case webview.ResultDone:
			if unsub {
				b.status.SetText("Unsubscribed from " + title + ".")
			} else {
				b.status.SetText("Subscribed to " + title + ". Steam downloads it in the Steam client.")
			}
		case webview.ResultAlready:
			if unsub {
				b.status.SetText(title + " was not subscribed.")
			} else {
				b.status.SetText(title + " was already subscribed.")
			}
		case resOpened:
			b.status.SetText("Opened " + title + " in your default browser: press Subscribe there.")
		case webview.ResultNotSignedIn:
			b.status.SetText("Not signed in to Steam (" + e.Error + "): nothing changed for " + title + ".")
		default:
			b.status.SetText("Could not change " + title + ".")
			dialog.ShowError(fmt.Errorf("%s %s failed: %s", verb, title, e.Error), b.u.win)
		}
	})
}

type wsRow struct {
	widget.BaseWidget
	card    *tapCard
	img     *canvas.Image
	title   *widget.Label
	info    *widget.Label
	dot     *canvas.Circle
	btn     *widget.Button
	ext     *widget.Button
	preview string
	content fyne.CanvasObject
}

func newWSRow() *wsRow {
	r := &wsRow{
		img:   canvas.NewImageFromImage(nil),
		title: widget.NewLabel(""),
		info:  widget.NewLabel(""),
		dot:   canvas.NewCircle(theme.Color(theme.ColorNameDisabled)),
		btn:   widget.NewButton("Subscribe", nil),
		ext:   widget.NewButtonWithIcon("", theme.ComputerIcon(), nil),
	}
	bg := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	bg.CornerRadius = 10
	thumbBg := canvas.NewRectangle(color.NRGBA{128, 128, 128, 40})
	thumbBg.CornerRadius = 8
	r.img.FillMode = canvas.ImageFillContain
	r.img.CornerRadius = 8
	r.img.SetMinSize(fyne.NewSize(64, 64))
	r.title.Truncation = fyne.TextTruncateEllipsis
	r.info.Truncation = fyne.TextTruncateEllipsis
	r.info.Alignment = fyne.TextAlignTrailing
	r.info.Importance = widget.LowImportance
	r.info.SizeName = theme.SizeNameCaptionText
	dotBox := container.New(layout.NewCustomPaddedLayout(0, 0, 6, 8), container.NewCenter(container.NewGridWrap(fyne.NewSize(12, 12), r.dot)))
	text := container.NewGridWithColumns(2,
		container.NewVBox(layout.NewSpacer(), r.title, layout.NewSpacer()),
		container.NewBorder(nil, nil, nil, dotBox, container.NewVBox(layout.NewSpacer(), r.info, layout.NewSpacer())),
	)
	right := container.NewCenter(container.NewHBox(r.btn, r.ext))
	inner := container.New(layout.NewCustomPaddedLayout(4, 4, 4, 4), container.NewBorder(nil, nil, container.NewStack(thumbBg, r.img), right, text))
	r.card = newTapCard(container.NewStack(bg, inner))
	r.content = container.New(layout.NewCustomPaddedLayout(3, 3, 0, 0), r.card)
	r.ExtendBaseWidget(r)
	return r
}

func (r *wsRow) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(r.content) }

func wsDotColor(busy, subscribed, installed bool) color.Color {
	switch {
	case busy:
		return theme.Color(theme.ColorNameWarning)
	case !subscribed:
		return theme.Color(theme.ColorNameDisabled)
	case installed:
		return theme.Color(theme.ColorNameSuccess)
	default:
		return theme.Color(theme.ColorNameWarning)
	}
}

func (b *wsBrowser) showMenu(it wsItem, e *fyne.PointEvent) {
	label := "Subscribe"
	if b.subscribed(it) {
		label = "Unsubscribe"
	}
	sub := fyne.NewMenuItem(label, func() { b.toggle(it) })
	sub.Disabled = b.busy[it.ID]
	link := workshop.ItemURL(it.ID)
	menu := fyne.NewMenu("",
		sub,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Open in browser", func() { b.openExternal(link) }),
		fyne.NewMenuItem("Copy link", func() {
			b.u.app.Clipboard().SetContent(link)
			b.status.SetText("Copied the link to " + it.displayTitle() + ".")
		}),
	)
	widget.ShowPopUpMenuAtPosition(menu, b.u.win.Canvas(), e.AbsolutePosition)
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
			r.title.SetText(it.displayTitle())
			info := it.Info
			if it.State != "" {
				info = strings.ToUpper(it.State) + "  ·  " + info
			}
			r.info.SetText(info)
			busy := b.busy[it.ID]
			sub := b.subscribed(it)
			r.dot.FillColor = wsDotColor(busy, sub, it.Installed)
			r.dot.Refresh()
			switch {
			case busy:
				r.btn.SetText("Working...")
				r.btn.Importance = widget.MediumImportance
				r.btn.Disable()
			case sub:
				r.btn.SetText("Unsubscribe")
				r.btn.Importance = widget.MediumImportance
				r.btn.Enable()
			default:
				r.btn.SetText("Subscribe")
				r.btn.Importance = widget.HighImportance
				r.btn.Enable()
			}
			r.btn.Refresh()
			r.btn.OnTapped = func() { b.toggle(it) }
			id := it.ID
			r.ext.OnTapped = func() { b.openExternal(workshop.ItemURL(id)) }
			r.card.onMenu = func(e *fyne.PointEvent) { b.showMenu(it, e) }
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
	list.HideSeparators = true
	list.OnSelected = func(widget.ListItemID) { list.UnselectAll() }
	b.watch(list.Refresh)
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
		b.openExternal(workshop.BrowseURL("", query()))
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

func subItems(subs []workshop.Sub, det map[string]workshop.Details, local map[string]workshop.LocalItem, detailsOK bool) []wsItem {
	items := make([]wsItem, 0, len(subs))
	for _, s := range subs {
		d := det[s.ID]
		l := local[s.ID]
		_, public := det[s.ID]
		info := detailsInfo(d)
		if !public {
			var parts []string
			if s.Size > 0 {
				parts = append(parts, humanSize(s.Size))
			}
			if s.Updated > 0 {
				parts = append(parts, "updated "+time.Unix(s.Updated, 0).Format("2006-01-02"))
			}
			if detailsOK {
				parts = append(parts, "private or hidden on the Workshop")
			}
			if l.Description != "" {
				parts = append(parts, l.Description)
			}
			info = strings.Join(parts, "  ·  ")
		}
		title := d.Title
		if title == "" {
			title = l.Title
		}
		preview := d.Preview
		if preview == "" {
			preview = l.Image
		}
		items = append(items, wsItem{ID: s.ID, Title: title, Info: info, State: subStateLabel(s.State), Subscribed: true, Installed: s.State == workshop.SubInstalled, Preview: preview, sub: s.State})
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.sub != b.sub {
			return a.sub > b.sub
		}
		if (a.Title == "") != (b.Title == "") {
			return a.Title != ""
		}
		return strings.ToLower(a.Title) < strings.ToLower(b.Title)
	})
	return items
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
			local := map[string]workshop.LocalItem{}
			for _, s := range subs {
				if d := det[s.ID]; d.Title == "" || d.Preview == "" {
					local[s.ID] = b.deps.local(s.Dir)
				}
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
				for _, s := range subs {
					counts[s.State]++
				}
				items = subItems(subs, det, local, derr == nil)
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
	var q *webview.SubQueue
	stopReq := false
	var stop, subMissing *widget.Button
	var step func()
	step = func() {
		if q == nil {
			return
		}
		if stopReq && !q.Finished() {
			q.Stop("stopped by you")
		}
		it, ok := q.Current()
		if !ok {
			progress.SetText(q.Summary() + " Press Refresh after Steam has downloaded them.")
			q = nil
			stop.Hide()
			subMissing.Show()
			return
		}
		progress.SetText(q.Progress())
		b.setSub(it.ID, false, func(e webview.Event) {
			if q == nil {
				return
			}
			if e.Result == resOpened {
				q.Record(webview.ResultFailed, "opened in your default browser")
				q.Stop("built-in browser unavailable: press Subscribe on the page that opened, then run this again")
			} else {
				q.Record(e.Result, e.Error)
			}
			step()
		})
	}
	stop = widget.NewButtonWithIcon("Stop", theme.MediaStopIcon(), func() {
		stopReq = true
		stop.Disable()
		if q != nil {
			progress.SetText(q.Progress() + " (stopping after this one)")
		}
	})
	subMissing = widget.NewButtonWithIcon("Subscribe to missing", theme.DownloadIcon(), func() {
		var todo []webview.QueueItem
		for _, m := range missing {
			if !b.subscribed(m) {
				todo = append(todo, webview.QueueItem{ID: m.ID, Title: m.Title})
			}
		}
		if len(todo) == 0 {
			progress.SetText("Nothing to subscribe: every missing item is already subscribed and waits for Steam to download it.")
			return
		}
		q = webview.NewSubQueue(todo)
		stopReq = false
		stop.Enable()
		stop.Show()
		subMissing.Hide()
		step()
	})
	subMissing.Disable()
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
					installed = append(installed, wsItem{ID: id, Title: d.Title, Info: detailsInfo(d), State: "Installed", Subscribed: true, Installed: true, Preview: d.Preview})
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
	openCol := widget.NewButtonWithIcon("Open in browser", theme.ComputerIcon(), func() { b.openExternal(workshop.ItemURL(cid())) })
	actions := container.NewHBox(subMissing, stop, openCol)
	top := container.NewVBox(
		container.NewBorder(nil, nil, widget.NewLabel("Collection id"), refresh, colEntry),
		summary,
		actions,
		progress,
	)
	return container.NewBorder(top, nil, nil, nil, list), load
}
