package main

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
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
	"github.com/katachinosekai/pzlauncher/internal/paths"
	"github.com/katachinosekai/pzlauncher/internal/webview"
	"github.com/katachinosekai/pzlauncher/internal/workshop"
)

func init() {
	if len(os.Args) > 1 && os.Args[1] == webview.HelperFlag {
		os.Exit(webview.RunHelper(webview.Options{Open: install.DefaultSystem().OpenURL, Ephemeral: slices.Contains(os.Args[2:], webview.EphemeralFlag)}))
	}
	registerTab(35, "Workshop", (*ui).workshopTab)
}

type wsDeps struct {
	browse   func(ctx context.Context, q workshop.BrowseQuery) (*workshop.BrowseResult, error)
	details  func(ctx context.Context, ids []string) (map[string]workshop.Details, error)
	subs     func() ([]workshop.Sub, error)
	check    func(ctx context.Context, cid string) (*workshop.Result, error)
	thumb    func(ctx context.Context, u string) ([]byte, error)
	local    func(itemDir string) workshop.LocalItem
	uploads  func() map[string]string
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
	uploads: func() map[string]string {
		z, err := paths.Zomboid()
		if err != nil {
			return nil
		}
		return workshop.UploadPreviews(z)
	},
	check: func(ctx context.Context, cid string) (*workshop.Result, error) {
		return workshop.Check(ctx, workshop.Options{CollectionID: cid})
	},
	external: func(u string) error { return install.DefaultSystem().OpenURL(u) },
}

type wsItem struct {
	ID         string
	Title      string
	Info       string
	Desc       string
	State      string
	Subscribed bool
	Installed  bool
	Preview    string
	Collection bool
	subs       int64
	stars      int
	size       int64
	updated    int64
	seq        int
	sub        workshop.SubState
}

func (it wsItem) displayTitle() string {
	if it.Title == "" {
		return "Workshop item " + it.ID
	}
	return it.Title
}

type wsBrowser struct {
	openCollection func(id, title string)
	searchShown    func()
	u              *ui
	deps           wsDeps
	ctl            *webview.Controller
	thumbs         *thumbCache
	status         *autoLabel
	sess           *wsSession

	busy     map[string]bool
	subState map[string]bool
	watchers []func()
	fixes    map[*widget.List]func()
}

func (b *wsBrowser) refreshList(l *widget.List) {
	l.Refresh()
	if fix := b.fixes[l]; fix != nil {
		fix()
	}
}

func (u *ui) workshopTab() fyne.CanvasObject {
	b := &wsBrowser{u: u, deps: workshopDeps, busy: map[string]bool{}, subState: map[string]bool{}}
	b.thumbs = newThumbCache(b.deps.thumb)
	b.status = newAutoLabel()
	b.status.Importance = widget.LowImportance
	b.status.Truncation = fyne.TextTruncateEllipsis
	b.ctl = &webview.Controller{
		OnFallback: func(err error) { fyne.Do(func() { b.sess.onFallback(err) }) },
		OnEvent:    func(e webview.Event) { fyne.Do(func() { b.sess.onEvent(e) }) },
	}
	b.sess = newWSSession(b)
	u.closers = append(u.closers, b.ctl.Close)
	top := container.NewVBox(b.sess.label, b.status)

	tabs := container.NewAppTabs(
		container.NewTabItem("My subscriptions", widget.NewLabel("")),
		container.NewTabItem("My Subscribed Collection", widget.NewLabel("")),
		container.NewTabItemWithIcon("", theme.SearchIcon(), widget.NewLabel("")),
		container.NewTabItem("Popular / Recent Mods", widget.NewLabel("")),
		container.NewTabItem("Search for Collection", widget.NewLabel("")),
	)
	const searchIdx = 2
	builders := []func() (fyne.CanvasObject, func()){b.subsTab, b.collectionTab, b.searchTab, b.popularTab, b.collectionsTab}
	var viewing *container.TabItem
	b.openCollection = func(id, title string) {
		view, load := b.collectionView(id)
		if viewing == nil {
			viewing = container.NewTabItem("", view)
			tabs.Append(viewing)
		}
		viewing.Text = "Collection: " + title
		viewing.Content = view
		tabs.Select(viewing)
		tabs.Refresh()
		load()
	}
	loaded := make([]bool, len(builders))
	open := func(it *container.TabItem) {
		i := tabs.SelectedIndex()
		if i == searchIdx && loaded[i] && b.searchShown != nil {
			b.searchShown()
		}
		if i < 0 || i >= len(builders) || loaded[i] {
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
	tabs.OnSelected = open
	ensure := b.sess.ensure
	u.onTab("Workshop", func() {
		ensure()
		open(tabs.Selected())
	})
	u.signButtons = container.NewHBox(b.sess.signInBtn)
	u.signCheck = b.sess.ensure
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
	toggle  *widget.Button
	details *widget.Label
	detBox  fyne.CanvasObject
	body    *fyne.Container
	preview string
	content fyne.CanvasObject
}

const wsDetailsH = 120

func newWSRow() *wsRow {
	r := &wsRow{
		img:     canvas.NewImageFromImage(nil),
		title:   widget.NewLabel(""),
		info:    widget.NewLabel(""),
		dot:     canvas.NewCircle(theme.Color(theme.ColorNameDisabled)),
		btn:     widget.NewButton("Subscribe", nil),
		ext:     widget.NewButtonWithIcon("", theme.ComputerIcon(), nil),
		toggle:  widget.NewButtonWithIcon("", theme.MenuDropDownIcon(), nil),
		details: widget.NewLabel(""),
	}
	r.toggle.Importance = widget.LowImportance
	r.details.Wrapping = fyne.TextWrapWord
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
	dotBox := container.New(layout.NewCustomPaddedLayout(0, 0, 6, 8), container.NewCenter(container.NewGridWrap(fyne.NewSize(12, 12), r.dot)))
	text := container.NewGridWithColumns(2,
		container.NewVBox(layout.NewSpacer(), r.title, layout.NewSpacer()),
		container.NewBorder(nil, nil, nil, dotBox, container.NewVBox(layout.NewSpacer(), r.info, layout.NewSpacer())),
	)
	right := container.NewCenter(container.NewHBox(r.btn, r.ext, r.toggle))
	inner := container.New(layout.NewCustomPaddedLayout(4, 4, 4, 4), container.NewBorder(nil, nil, container.NewStack(thumbBg, r.img), right, text))
	r.card = newTapCard(container.NewStack(bg, inner))
	detScroll := container.NewVScroll(r.details)
	detScroll.SetMinSize(fyne.NewSize(0, wsDetailsH))
	r.detBox = container.New(layout.NewCustomPaddedLayout(0, 0, 16, 0), detScroll)
	r.detBox.Hide()
	r.body = container.NewVBox(r.card, r.detBox)
	r.content = container.New(layout.NewCustomPaddedLayout(3, 3, 0, 0), r.body)
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
	if it.Collection {
		sub = fyne.NewMenuItem("View items", func() { b.openCollection(it.ID, it.displayTitle()) })
	}
	link := workshop.ItemURL(it.ID)
	menu := fyne.NewMenu("",
		sub,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Open in Steam", func() { b.openExternal("steam://url/CommunityFilePage/" + it.ID) }),
		fyne.NewMenuItem("Open in browser", func() { b.openExternal(link) }),
		fyne.NewMenuItem("Copy link", func() {
			b.u.app.Clipboard().SetContent(link)
			b.status.SetText("Copied the link to " + it.displayTitle() + ".")
		}),
	)
	widget.ShowPopUpMenuAtPosition(menu, b.u.win.Canvas(), e.AbsolutePosition)
}

func wsDetailsText(it wsItem) string {
	var parts []string
	if it.State != "" {
		parts = append(parts, it.State)
	}
	if it.Info != "" {
		parts = append(parts, it.Info)
	}
	if it.Desc != "" {
		parts = append(parts, it.Desc)
	}
	parts = append(parts, "Workshop item "+it.ID)
	return strings.Join(parts, "\n\n")
}

func (b *wsBrowser) itemList(items *[]wsItem, nearEnd func()) *widget.List {
	var list *widget.List
	expanded := map[string]bool{}
	heights := map[int]float32{}
	collapsedH := newWSRow().MinSize().Height
	expandedH := collapsedH + wsDetailsH + theme.Padding()
	fix := func() {
		for i, it := range *items {
			want := collapsedH
			if expanded[it.ID] {
				want = expandedH
			}
			if h, ok := heights[i]; ok && h == want || !ok && want == collapsedH {
				continue
			}
			heights[i] = want
			list.SetItemHeight(i, want)
		}
		for i := range heights {
			if i >= len(*items) {
				delete(heights, i)
				list.SetItemHeight(i, collapsedH)
			}
		}
	}
	list = widget.NewList(
		func() int { return len(*items) },
		func() fyne.CanvasObject { return newWSRow() },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i >= len(*items) {
				return
			}
			if nearEnd != nil && i >= len(*items)-4 {
				nearEnd()
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
			open := expanded[it.ID]
			r.toggle.OnTapped = func() {
				expanded[it.ID] = !open
				fix()
				list.RefreshItem(i)
			}
			if open {
				r.toggle.SetIcon(theme.MenuDropUpIcon())
				r.details.SetText(wsDetailsText(it))
				r.detBox.Show()
			} else {
				r.toggle.SetIcon(theme.MenuDropDownIcon())
				r.detBox.Hide()
			}
			r.body.Refresh()
			if it.Collection {
				r.dot.FillColor = color.Transparent
				r.dot.Refresh()
				r.btn.SetText("View items")
				r.btn.Importance = widget.HighImportance
				r.btn.Enable()
				r.btn.Refresh()
				r.btn.OnTapped = func() { b.openCollection(it.ID, it.displayTitle()) }
			}
			r.btn.Show()
			switch {
			case it.Collection:
			case busy:
				r.btn.SetText("Working...")
				r.btn.Importance = widget.MediumImportance
				r.btn.Disable()
			case sub:
				r.btn.Hide()
			default:
				r.btn.SetText("Subscribe")
				r.btn.Importance = widget.HighImportance
				r.btn.Enable()
			}
			if !it.Collection {
				r.btn.Refresh()
				r.btn.OnTapped = func() { b.toggle(it) }
			}
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
	if b.fixes == nil {
		b.fixes = map[*widget.List]func(){}
	}
	b.fixes[list] = fix
	b.watch(func() { b.refreshList(list) })
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
	if d.Collection && d.Children > 0 {
		parts = append(parts, fmt.Sprintf("%d items", d.Children))
	}
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
	return strings.Join(parts, "  ·  ")
}

func (b *wsBrowser) browseView(controls fyne.CanvasObject, query func() workshop.BrowseQuery) (fyne.CanvasObject, func()) {
	view, load, _ := b.browseViewSorted(controls, query, nil)
	return view, load
}

func (b *wsBrowser) browseViewSorted(controls fyne.CanvasObject, query func() workshop.BrowseQuery, order func([]wsItem)) (fyne.CanvasObject, func(), func()) {
	var items []wsItem
	page := 1
	var current workshop.BrowseQuery
	gen := 0
	loading, hasMore := false, false
	var load func(reset bool)
	list := b.itemList(&items, func() {
		if !loading && hasMore {
			load(false)
		}
	})
	status := widget.NewLabel("")
	status.Truncation = fyne.TextTruncateEllipsis
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
		loading = true
		if reset {
			status.SetText("Loading from the Steam Workshop...")
		} else {
			status.SetText(fmt.Sprintf("Loading more... (%d so far)", len(items)))
		}
		go func() {
			r, err := b.deps.browse(b.u.ctx, q)
			fyne.Do(func() {
				if my != gen {
					return
				}
				loading = false
				if err != nil {
					hasMore = false
					status.SetText("Could not load the list: " + err.Error() + " (use \"Open in browser\")")
					if reset {
						items = nil
						b.refreshList(list)
					}
					return
				}
				if reset {
					items = nil
					list.ScrollToTop()
				}
				for _, d := range r.Items {
					items = append(items, wsItem{ID: d.ID, Title: d.Title, Info: detailsInfo(d), Desc: d.Description, Preview: d.Preview, Collection: d.Collection || q.Collections, subs: d.Subscriptions, stars: d.Stars, size: d.Size, updated: d.Updated.Unix(), seq: len(items)})
				}
				hasMore = r.TotalPages > page && len(r.Items) > 0
				if order != nil {
					order(items)
				}
				b.refreshList(list)
				kind := "items"
				if q.Collections {
					kind = "collections"
				}
				status.SetText(fmt.Sprintf("%s: showing %d of %d %s.", q.Sort, len(items), r.Total, kind))
			})
		}()
	}
	openBrowse := widget.NewButtonWithIcon("Open in browser", theme.ComputerIcon(), func() {
		b.openExternal(workshop.BrowseURL("", query()))
	})
	top := container.NewVBox(container.NewBorder(nil, nil, nil, openBrowse, controls), status)
	resort := func() {
		if order != nil {
			order(items)
			b.refreshList(list)
		}
	}
	return container.NewBorder(top, nil, nil, nil, list), func() { load(true) }, resort
}

func (b *wsBrowser) collectionsTab() (fyne.CanvasObject, func()) {
	opts := []string{"Popular this week", "Most popular", "Most recent"}
	radio := widget.NewRadioGroup(opts, nil)
	radio.Horizontal = true
	radio.Required = true
	radio.SetSelected(opts[0])
	entry := widget.NewEntry()
	entry.SetPlaceHolder("Search collections...")
	var load func()
	refresh := widget.NewButtonWithIcon("", theme.SearchIcon(), func() { load() })
	view, l := b.browseView(container.NewBorder(nil, nil, radio, refresh, entry), func() workshop.BrowseQuery {
		q := workshop.BrowseQuery{Text: entry.Text, Collections: true, Sort: workshop.SortTrendWeek}
		switch radio.Selected {
		case opts[1]:
			q.Sort = workshop.SortPopular
		case opts[2]:
			q.Sort = workshop.SortRecent
		}
		return q
	})
	load = l
	radio.OnChanged = func(string) { load() }
	entry.OnSubmitted = func(string) { load() }
	return view, load
}

func (b *wsBrowser) popularTab() (fyne.CanvasObject, func()) {
	opts := []string{"Popular this week", "Most recent"}
	radio := widget.NewRadioGroup(opts, nil)
	radio.Horizontal = true
	radio.Required = true
	radio.SetSelected(opts[0])
	sortNames := []string{"Steam order", "Name", "Subscribers", "Rating", "Size", "Last updated"}
	sortSel := widget.NewSelect(sortNames, nil)
	sortSel.SetSelected(sortNames[0])
	order := func(items []wsItem) {
		var less func(a, b wsItem) bool
		switch sortSel.Selected {
		case "Name":
			less = func(a, b wsItem) bool { return strings.ToLower(a.displayTitle()) < strings.ToLower(b.displayTitle()) }
		case "Subscribers":
			less = func(a, b wsItem) bool { return a.subs > b.subs }
		case "Rating":
			less = func(a, b wsItem) bool { return a.stars > b.stars }
		case "Size":
			less = func(a, b wsItem) bool { return a.size > b.size }
		case "Last updated":
			less = func(a, b wsItem) bool { return a.updated > b.updated }
		default:
			less = func(a, b wsItem) bool { return a.seq < b.seq }
		}
		sort.SliceStable(items, func(i, j int) bool { return less(items[i], items[j]) })
	}
	var load func()
	refresh := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), func() { load() })
	controls := container.NewBorder(nil, nil, nil, container.NewHBox(widget.NewLabel("Sort by"), sortSel, refresh), radio)
	view, l, resort := b.browseViewSorted(controls, func() workshop.BrowseQuery {
		if radio.Selected == opts[1] {
			return workshop.BrowseQuery{Sort: workshop.SortRecent}
		}
		return workshop.BrowseQuery{Sort: workshop.SortTrendWeek}
	}, order)
	load = l
	radio.OnChanged = func(string) { load() }
	sortSel.OnChanged = func(string) { resort() }
	return view, load
}

func byPopular(items []wsItem, det map[string]workshop.Details) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := det[items[i].ID].Subscriptions, det[items[j].ID].Subscriptions
		if a != b {
			return a > b
		}
		return strings.ToLower(items[i].Title) < strings.ToLower(items[j].Title)
	})
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
		desc := d.Description
		if desc == "" {
			desc = l.Description
		}
		items = append(items, wsItem{ID: s.ID, Title: title, Info: info, Desc: desc, State: subStateLabel(s.State), Subscribed: true, Installed: s.State == workshop.SubInstalled, Preview: preview, sub: s.State})
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
	list := b.itemList(&items, nil)
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
			var uploads map[string]string
			if b.deps.uploads != nil {
				uploads = b.deps.uploads()
			}
			for _, s := range subs {
				if d := det[s.ID]; d.Title == "" || d.Preview == "" {
					l := b.deps.local(s.Dir)
					if l.Image == "" {
						l.Image = uploads[s.ID]
					}
					local[s.ID] = l
				}
			}
			fyne.Do(func() {
				refresh.Enable()
				if err != nil {
					summary.SetText("Could not read Steam's Workshop file (appworkshop_108600.acf): " + err.Error())
					items = nil
					b.refreshList(list)
					return
				}
				counts := map[workshop.SubState]int{}
				for _, s := range subs {
					counts[s.State]++
				}
				items = subItems(subs, det, local, derr == nil)
				b.refreshList(list)
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

func (b *wsBrowser) collectionTab() (fyne.CanvasObject, func()) { return b.collectionView("") }

func (b *wsBrowser) collectionView(fixed string) (fyne.CanvasObject, func()) {
	prefs := b.u.app.Preferences()
	colEntry := widget.NewEntry()
	colEntry.SetText(prefs.StringWithFallback(prefCollection, workshop.DefaultCollectionID))
	if fixed != "" {
		colEntry.SetText(fixed)
	}
	cid := func() string {
		c := strings.TrimSpace(colEntry.Text)
		if c == "" {
			c = workshop.DefaultCollectionID
		}
		return c
	}
	var items []wsItem
	list := b.itemList(&items, nil)
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
		if fixed == "" {
			prefs.SetString(prefCollection, c)
		}
		refresh.Disable()
		summary.SetText("Checking collection " + c + "...")
		go func() {
			r, err := b.deps.check(b.u.ctx, c)
			var det map[string]workshop.Details
			var derr error
			local := map[string]workshop.LocalItem{}
			var uploads map[string]string
			if b.deps.uploads != nil {
				uploads = b.deps.uploads()
			}
			if err == nil {
				det, derr = b.deps.details(b.u.ctx, r.IDs)
				for _, id := range r.IDs {
					if d := det[id]; (d.Title == "" || d.Preview == "") && r.ContentDir != "" {
						local[id] = b.deps.local(filepath.Join(r.ContentDir, id))
					}
				}
			}
			title := func(id, fallback string) string {
				if t := det[id].Title; t != "" {
					return t
				}
				if t := local[id].Title; t != "" {
					return t
				}
				return fallback
			}
			preview := func(id string) string {
				if p := det[id].Preview; p != "" {
					return p
				}
				if p := local[id].Image; p != "" {
					return p
				}
				return uploads[id]
			}
			fyne.Do(func() {
				refresh.Enable()
				if err != nil {
					summary.SetText("Check failed: " + err.Error())
					items = nil
					b.refreshList(list)
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
					label := "Missing"
					subbed := false
					switch m.State {
					case workshop.Downloading:
						label, subbed = "Subscribed, downloading", true
					case workshop.NotSubscribed:
						label = "Not subscribed"
					}
					it := wsItem{ID: m.ID, Title: title(m.ID, m.Title), Info: detailsInfo(d), Desc: d.Description, State: label, Subscribed: subbed, Preview: preview(m.ID)}
					missing = append(missing, it)
				}
				for _, id := range r.IDs {
					if _, ok := miss[id]; ok {
						continue
					}
					d := det[id]
					installed = append(installed, wsItem{ID: id, Title: title(id, ""), Info: detailsInfo(d), Desc: d.Description, State: "Installed", Subscribed: true, Installed: true, Preview: preview(id)})
				}
				items = append(append(items, missing...), installed...)
				byPopular(items, det)
				b.refreshList(list)
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
	idRow := container.NewBorder(nil, nil, widget.NewLabel("Collection id"), refresh, colEntry)
	if fixed != "" {
		idRow = container.NewBorder(nil, nil, nil, refresh, summary)
	}
	top := container.NewVBox(
		idRow,
		summary,
		actions,
		progress,
	)
	return container.NewBorder(top, nil, nil, nil, list), load
}
