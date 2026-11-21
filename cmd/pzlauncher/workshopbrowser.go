package main

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
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
	"github.com/katachinosekai/pzlauncher/internal/modlist"
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
	active   func() map[string]bool
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
	active: func() map[string]bool {
		z, err := paths.Zomboid()
		if err != nil {
			return nil
		}
		f, err := (&modlist.Store{ZomboidDir: z}).LoadDefault()
		if err != nil {
			return nil
		}
		on := map[string]bool{}
		for _, id := range f.Mods {
			on[id] = true
		}
		return on
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
	Detail     string
	Tags       []string
	ModIDs     []string
	Requires   []string
	State      string
	Subscribed bool
	Installed  bool
	Preview    string
	Collection bool
	Cols       []string // search tabs: column values (see wsColsLayout)
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
	active   map[string]bool
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
		container.NewTabItem("Recently Updated", widget.NewLabel("")),
		container.NewTabItem("My Subscribed Collection", widget.NewLabel("")),
		container.NewTabItemWithIcon("", theme.SearchIcon(), widget.NewLabel("")),
		container.NewTabItem("Popular / Recent Mods", widget.NewLabel("")),
		container.NewTabItem("Search for Collection", widget.NewLabel("")),
	)
	const searchIdx = 3
	builders := []func() (fyne.CanvasObject, func()){b.subsTab, b.recentTab, b.collectionTab, b.searchTab, b.popularTab, b.collectionsTab}
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
	bg, thumbBg *canvas.Rectangle
	rightBox    *fyne.Container
	inner       *fyne.Container // the card's padded content
	card        *tapCard
	img         *canvas.Image
	title       *widget.Label
	info        *widget.Label
	cols        [4]*widget.Label // search tabs: aligned columns instead of info
	colBox      *fyne.Container
	colArea     fyne.CanvasObject // colBox inside its tight-padding theme override
	dot         *canvas.Circle
	btn         *widget.Button
	ext         *widget.Button
	toggle      *widget.Button
	details     *widget.Label
	detBox      fyne.CanvasObject
	body        *fyne.Container
	text        *fyne.Container
	preview     string
	content     fyne.CanvasObject
}

// wsTitleMinW is the narrowest the title gets before the info side gives way.
const wsTitleMinW = 120

// textWant is the width a label needs to show s uncut: text + the label's
// inner padding on both sides + 1px against rounding (one pixel short and the
// label already cuts "Today" to "To…").
func textWant(s string, style fyne.TextStyle) float32 {
	return fyne.MeasureText(s, theme.TextSize(), style).Width + 2*theme.InnerPadding() + 1
}

// Column cells use a tight inner padding (theme override on the column area)
// and a fixed gap, so the title keeps more of the row.
const (
	wsColPad = 2
	wsColGap = 10
)

// tightTheme is the app theme with a small inner padding, for the column cells.
type tightTheme struct{ fyne.Theme }

func (t tightTheme) Size(n fyne.ThemeSizeName) float32 {
	if n == theme.SizeNameInnerPadding {
		return wsColPad
	}
	return t.Theme.Size(n)
}

// colWant is the width a column cell needs to show s uncut.
func colWant(s string, style fyne.TextStyle) float32 {
	return fyne.MeasureText(s, theme.TextSize(), style).Width + 2*wsColPad + 1
}

// Column headers and samples (longest expected values) for the search tabs, so
// every row and the header get the same column widths and line up.
var (
	wsModHead  = []string{"Build", "Subs", "Rating", "Updated"}
	wsCollHead = []string{"Build", "Rating", "Items", "Updated"}
	wsModCols  = [4][]string{{"B41/B42"}, {"999k subs", "12.3M subs"}, {"★ 5/5"}, {"12 months ago", "10 years ago"}}
	wsCollCols = [4][]string{{"B41/B42"}, {"★ 5/5"}, {"9999 items"}, {"12 months ago", "10 years ago"}}
)

// wsColsLayout places the 4 column labels right-aligned in fixed widths:
// mods = build | subs | rating | age, collections = build | rating | items | age.
// The build column (index 0) is hidden when the row is too narrow.
type wsColsLayout struct {
	collection bool
	showBuild  bool
}

func (l *wsColsLayout) widths() [4]float32 {
	samples, head := wsModCols, wsModHead
	if l.collection {
		samples, head = wsCollCols, wsCollHead
	}
	var w [4]float32
	for i, ss := range samples {
		w[i] = colWant(head[i], fyne.TextStyle{Bold: true})
		for _, s := range ss {
			w[i] = max(w[i], colWant(s, fyne.TextStyle{}))
		}
	}
	return w
}

func (l *wsColsLayout) width(showBuild bool) float32 {
	w := l.widths()
	total := w[1] + w[2] + w[3] + 2*wsColGap
	if showBuild {
		total += w[0] + wsColGap
	}
	return total
}

func (l *wsColsLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	w := l.widths()
	x := size.Width
	for i := 3; i >= 0; i-- {
		o := objs[i]
		if i == 0 && !l.showBuild {
			o.Hide()
			continue
		}
		o.Show()
		h := o.MinSize().Height
		x -= w[i]
		o.Move(fyne.NewPos(x, (size.Height-h)/2))
		o.Resize(fyne.NewSize(w[i], h))
		x -= wsColGap
	}
}

func (l *wsColsLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	h := float32(0)
	for _, o := range objs {
		h = max(h, o.MinSize().Height)
	}
	return fyne.NewSize(l.width(false), h)
}

// titleInfoLayout lays out [title | info+dot]: the info side gets the width it
// needs, the title the rest (shortened with "…" first, down to minTitle).
// In column mode the build column goes next; only then is anything cut.
type titleInfoLayout struct {
	info     *widget.Label
	cols     *wsColsLayout
	colBox   *fyne.Container
	colsOn   bool
	extra    fyne.CanvasObject // the status dot, to the right of the info
	minTitle float32
}

func (l *titleInfoLayout) extraW() float32 {
	if l.extra == nil {
		return 0
	}
	return l.extra.MinSize().Width + theme.Padding() // dot + the Border gap before it
}

func (l *titleInfoLayout) infoWant() float32 {
	if l.colsOn {
		return l.cols.width(l.cols.showBuild) + l.extraW()
	}
	return textWant(l.info.Text, l.info.TextStyle) + l.extraW()
}

func (l *titleInfoLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if l.colsOn {
		show := l.cols.width(true)+l.extraW() <= size.Width-l.minTitle
		if show != l.cols.showBuild {
			l.cols.showBuild = show
			l.colBox.Refresh()
		}
	}
	iw := min(l.infoWant(), max(size.Width-l.minTitle, objs[1].MinSize().Width))
	iw = min(iw, size.Width)
	tw := max(size.Width-iw-theme.Padding(), 0)
	objs[0].Move(fyne.NewPos(0, 0))
	objs[0].Resize(fyne.NewSize(tw, size.Height))
	objs[1].Move(fyne.NewPos(size.Width-iw, 0))
	objs[1].Resize(fyne.NewSize(iw, size.Height))
}

func (l *titleInfoLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	a, b := objs[0].MinSize(), objs[1].MinSize()
	return fyne.NewSize(a.Width+b.Width+theme.Padding(), max(a.Height, b.Height))
}

// setInfo shows a single info line (subscription tabs).
func (r *wsRow) setInfo(s string) {
	l := r.text.Layout.(*titleInfoLayout)
	l.colsOn = false
	r.colArea.Hide()
	r.info.Show()
	r.info.SetText(s)
	r.text.Refresh()
}

// setCols shows aligned columns (search tabs).
func (r *wsRow) setCols(cols []string, collection bool) {
	l := r.text.Layout.(*titleInfoLayout)
	l.colsOn = true
	l.cols.collection = collection
	for i, c := range r.cols {
		t := ""
		if i < len(cols) {
			t = cols[i]
		}
		c.SetText(t)
	}
	r.info.Hide()
	r.colArea.Show()
	r.text.Refresh()
}

const wsDetailsH = 170

// wsBtnSize fits the widest text the row's main button shows.
func wsBtnSize() fyne.Size {
	var sz fyne.Size
	for _, t := range []string{"Subscribe", "Subscribed", "View items", "Working..."} {
		b := widget.NewButton(t, nil)
		b.Importance = widget.HighImportance
		sz = sz.Max(b.MinSize())
	}
	return sz
}

// newWSHeader is the column header for a search tab's list: a row with the
// same layout and blank stand-ins for the thumbnail, buttons and dot, so each
// heading sits over its column and hides the build heading with the rows.
func newWSHeader(collection bool) *wsRow {
	h := newWSRow()
	h.bg.FillColor = color.Transparent
	h.thumbBg.FillColor = color.Transparent
	h.img.SetMinSize(fyne.NewSize(64, 0))
	rs := h.rightBox.MinSize()
	pad := canvas.NewRectangle(color.Transparent)
	pad.SetMinSize(fyne.NewSize(rs.Width, 0))
	h.rightBox.Objects = []fyne.CanvasObject{pad}
	h.dot.FillColor = color.Transparent
	// no card padding above/below (sides unchanged, so the headings stay aligned)
	h.inner.Layout = layout.NewCustomPaddedLayout(0, 0, 4, 4)
	h.content.(*fyne.Container).Layout = layout.NewCustomPaddedLayout(0, 0, 0, 0)
	h.title.TextStyle = fyne.TextStyle{Bold: true}
	h.title.Importance = widget.LowImportance
	h.title.SetText("Name")
	for _, c := range h.cols {
		c.TextStyle = fyne.TextStyle{Bold: true}
	}
	h.setHeader(collection)
	return h
}

func (r *wsRow) setHeader(collection bool) {
	head := wsModHead
	if collection {
		head = wsCollHead
	}
	r.setCols(head, collection)
}

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
	r.bg, r.thumbBg = bg, thumbBg
	r.img.FillMode = canvas.ImageFillContain
	r.img.CornerRadius = 8
	r.img.SetMinSize(fyne.NewSize(64, 64))
	r.title.Truncation = fyne.TextTruncateEllipsis
	r.info.Truncation = fyne.TextTruncateEllipsis
	r.info.Alignment = fyne.TextAlignTrailing
	r.info.Importance = widget.LowImportance
	colObjs := make([]fyne.CanvasObject, 4)
	for i := range r.cols {
		c := widget.NewLabel("")
		c.Truncation = fyne.TextTruncateEllipsis
		c.Alignment = fyne.TextAlignTrailing
		c.Importance = widget.LowImportance
		r.cols[i], colObjs[i] = c, c
	}
	cl := &wsColsLayout{showBuild: true}
	r.colBox = container.New(cl, colObjs...)
	colArea := container.NewThemeOverride(r.colBox, tightTheme{fyne.CurrentApp().Settings().Theme()})
	colArea.Hide()
	r.colArea = colArea
	dotBox := container.New(layout.NewCustomPaddedLayout(0, 0, 4, 4), container.NewCenter(container.NewGridWrap(fyne.NewSize(12, 12), r.dot)))
	r.text = container.New(&titleInfoLayout{info: r.info, cols: cl, colBox: r.colBox, extra: dotBox, minTitle: wsTitleMinW},
		container.NewVBox(layout.NewSpacer(), r.title, layout.NewSpacer()),
		container.NewBorder(nil, nil, nil, dotBox, container.NewStack(container.NewVBox(layout.NewSpacer(), r.info, layout.NewSpacer()), colArea)),
	)
	// fixed-width main button, so "Subscribe" / "Subscribed" / "View items"
	// rows keep their columns at the same place (and under the header)
	r.rightBox = container.NewHBox(container.NewGridWrap(wsBtnSize(), r.btn), r.ext, r.toggle)
	right := container.NewCenter(r.rightBox)
	inner := container.New(layout.NewCustomPaddedLayout(4, 4, 4, 4), container.NewBorder(nil, nil, container.NewStack(thumbBg, r.img), right, r.text))
	r.inner = inner
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

func wsDetailsText(it wsItem, active map[string]bool) string {
	var facts []string
	if it.Detail != "" {
		facts = append(facts, it.Detail)
	}
	if len(it.Tags) > 0 {
		facts = append(facts, "Tags: "+strings.Join(it.Tags, "  ·  "))
	}
	if len(it.ModIDs) > 0 {
		facts = append(facts, "Mod IDs: "+strings.Join(it.ModIDs, ", "))
	}
	if len(it.Requires) > 0 {
		req := make([]string, len(it.Requires))
		for i, r := range it.Requires {
			req[i] = strings.TrimLeft(r, "\\")
		}
		facts = append(facts, "Requires: "+strings.Join(req, ", "))
	}
	if len(it.ModIDs) > 0 && active != nil {
		if len(it.ModIDs) == 1 {
			state := "no"
			if active[it.ModIDs[0]] {
				state = "yes"
			}
			facts = append(facts, "Enabled in mod list: "+state)
		} else {
			var on []string
			for _, id := range it.ModIDs {
				state := " (off)"
				if active[id] {
					state = " (on)"
				}
				on = append(on, id+state)
			}
			facts = append(facts, "Enabled in mod list: "+strings.Join(on, ", "))
		}
	}
	var parts []string
	if len(facts) > 0 {
		parts = append(parts, strings.Join(facts, "\n"))
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
			if it.Cols != nil {
				r.setCols(it.Cols, it.Collection)
			} else {
				r.setInfo(info)
			}
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
				r.details.SetText(wsDetailsText(it, b.active))
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
				r.btn.SetText("Subscribed")
				r.btn.Importance = widget.MediumImportance
				r.btn.Enable()
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
	if !d.Updated.IsZero() {
		parts = append(parts, updatedAgo(d.Updated, time.Now()))
	}
	return strings.Join(parts, "  ·  ")
}

func popularity(d workshop.Details) []string {
	var parts []string
	if d.Subscriptions > 0 {
		parts = append(parts, humanCount(d.Subscriptions)+" subs")
	}
	if d.Stars > 0 {
		parts = append(parts, fmt.Sprintf("★ %d/5", d.Stars))
	}
	return parts
}

func sizeAndDate(size int64, updated time.Time) []string {
	var parts []string
	if size > 0 {
		parts = append(parts, humanSize(size))
	}
	if !updated.IsZero() {
		parts = append(parts, "updated "+updated.Format("2006-01-02"))
	}
	return parts
}

var buildTag = regexp.MustCompile(`(?i)^\s*build\s*(\d+(?:\.\d+)*)\s*$`)

// buildLabel turns the Workshop's "Build NN" tags into a short label:
// "B41", "B42", "B41/B42" when both, any other build as B<number>.
func buildLabel(tags []string) string {
	seen := map[string]bool{}
	var nums []string
	for _, t := range tags {
		if m := buildTag.FindStringSubmatch(t); m != nil && !seen[m[1]] {
			seen[m[1]] = true
			nums = append(nums, m[1])
		}
	}
	sort.Slice(nums, func(i, j int) bool {
		a, _ := strconv.ParseFloat(nums[i], 64)
		c, _ := strconv.ParseFloat(nums[j], 64)
		if a != c {
			return a < c
		}
		return nums[i] < nums[j]
	})
	for i, n := range nums {
		nums[i] = "B" + n
	}
	return strings.Join(nums, "/")
}

// searchCols are the column values for search results (Popular / Recent,
// Search for Collection, the magnifier search):
// mods = build | subs | rating | age, collections = build | rating | items | age.
func searchCols(d workshop.Details, collection bool) []string {
	var subs, rating, items, age string
	if d.Subscriptions > 0 {
		subs = humanCount(d.Subscriptions) + " subs"
	}
	if d.Stars > 0 {
		rating = fmt.Sprintf("★ %d/5", d.Stars)
	}
	if d.Children > 0 {
		items = fmt.Sprintf("%d items", d.Children)
	}
	if !d.Updated.IsZero() {
		age = updatedAgo(d.Updated, time.Now())
	}
	if collection {
		return []string{buildLabel(d.Tags), rating, items, age}
	}
	return []string{buildLabel(d.Tags), subs, rating, age}
}

func detailsDetail(d workshop.Details) string {
	return strings.Join(append(popularity(d), sizeAndDate(d.Size, d.Updated)...), "  ·  ")
}

func updatedAgo(t, now time.Time) string {
	days := int(now.Sub(t).Hours() / 24)
	switch {
	case days < 1:
		return "Today"
	case days < 7:
		return "Week"
	case days < 30:
		return "Month"
	case days < 365:
		if n := days / 30; n > 1 {
			return fmt.Sprintf("%d months ago", n)
		}
		return "1 month ago"
	}
	if n := days / 365; n > 1 {
		return fmt.Sprintf("%d years ago", n)
	}
	return "1 year ago"
}

// wsBar is a search tab's control bar: left (filter options), center (grows,
// e.g. a search box; at least centerMin wide) and right (buttons). "Open in
// browser" is appended on the right. It wraps to two lines when too narrow.
type wsBar struct {
	left, center fyne.CanvasObject
	centerMin    float32
	right        []fyne.CanvasObject
}

// wrapBarLayout lays out [left | center | right] on one line, or left on the
// first line and [center | right] on the second when one line doesn't fit.
// Fyne asks MinSize without a width, so the parent (browseLayout) asks
// heightFor(width) instead; MinSize's height follows the last layout.
type wrapBarLayout struct {
	centerMin float32
	two       bool
}

func (l *wrapBarLayout) wraps(objs []fyne.CanvasObject, width float32) bool {
	_, _, _, hasLeft := l.parts(objs)
	return hasLeft && l.oneLineW(objs) > width
}

// heightFor is the bar height at the given width.
func (l *wrapBarLayout) heightFor(objs []fyne.CanvasObject, width float32) float32 {
	lw, cw, rw, _ := l.parts(objs)
	if l.wraps(objs, width) {
		return lw.Height + theme.Padding() + max(cw.Height, rw.Height)
	}
	return max(lw.Height, cw.Height, rw.Height)
}

func (l *wrapBarLayout) parts(objs []fyne.CanvasObject) (lw, cw, rw fyne.Size, hasLeft bool) {
	lw, cw, rw = objs[0].MinSize(), objs[1].MinSize(), objs[2].MinSize()
	cw.Width = max(cw.Width, l.centerMin)
	return lw, cw, rw, lw.Width > 0
}

func (l *wrapBarLayout) oneLineW(objs []fyne.CanvasObject) float32 {
	lw, cw, rw, hasLeft := l.parts(objs)
	w := cw.Width + rw.Width + theme.Padding()
	if hasLeft {
		w += lw.Width + theme.Padding()
	}
	return w
}

func (l *wrapBarLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	lw, cw, rw, _ := l.parts(objs)
	w := max(lw.Width, cw.Width+rw.Width+theme.Padding()) // the two-line width, so the window can shrink
	if l.two {
		return fyne.NewSize(w, lw.Height+theme.Padding()+max(cw.Height, rw.Height))
	}
	return fyne.NewSize(w, max(lw.Height, cw.Height, rw.Height))
}

func (l *wrapBarLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	lw, _, rw, hasLeft := l.parts(objs)
	two := l.wraps(objs, size.Width)
	l.two = two
	pad := theme.Padding()
	y, h := float32(0), size.Height
	cx := float32(0)
	if two {
		objs[0].Move(fyne.NewPos(0, 0))
		objs[0].Resize(fyne.NewSize(min(lw.Width, size.Width), lw.Height))
		y = lw.Height + pad
		h = max(size.Height-y, 0)
	} else if hasLeft {
		objs[0].Move(fyne.NewPos(0, 0))
		objs[0].Resize(fyne.NewSize(lw.Width, h))
		cx = lw.Width + pad
	}
	objs[2].Move(fyne.NewPos(size.Width-rw.Width, y))
	objs[2].Resize(fyne.NewSize(rw.Width, h))
	objs[1].Move(fyne.NewPos(cx, y))
	objs[1].Resize(fyne.NewSize(max(size.Width-rw.Width-pad-cx, 0), h))
}

// browseLayout stacks [bar, status, header, list]: the bar gets its height for
// the current width (one or two lines), then the status line and the column
// header at their own height, the list the rest.
type browseLayout struct{ bar *wrapBarLayout }

func (l *browseLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	bar, status, header, list := objs[0], objs[1], objs[2], objs[3]
	pad := theme.Padding()
	bh := l.bar.heightFor(bar.(*fyne.Container).Objects, size.Width)
	bar.Move(fyne.NewPos(0, 0))
	bar.Resize(fyne.NewSize(size.Width, bh))
	y := bh + pad
	if status.Visible() {
		sh := status.MinSize().Height
		status.Move(fyne.NewPos(0, y))
		status.Resize(fyne.NewSize(size.Width, sh))
		y += sh
		if !header.Visible() {
			y += pad
		}
	}
	if header.Visible() {
		hh := header.MinSize().Height
		header.Move(fyne.NewPos(0, y))
		header.Resize(fyne.NewSize(size.Width, hh))
		y += hh
	}
	list.Move(fyne.NewPos(0, y))
	list.Resize(fyne.NewSize(size.Width, max(size.Height-y, 0)))
}

func (l *browseLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	b, s, h, li := objs[0].MinSize(), objs[1].MinSize(), objs[2].MinSize(), objs[3].MinSize()
	return fyne.NewSize(max(b.Width, s.Width, h.Width, li.Width), b.Height+s.Height+h.Height+li.Height+2*theme.Padding())
}

func (b *wsBrowser) browseView(bar wsBar, query func() workshop.BrowseQuery) (fyne.CanvasObject, func()) {
	view, load, _ := b.browseViewSorted(bar, query, nil)
	return view, load
}

func (b *wsBrowser) browseViewSorted(bar wsBar, query func() workshop.BrowseQuery, order func([]wsItem)) (fyne.CanvasObject, func(), func()) {
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
	header := newWSHeader(false)
	load = func(reset bool) {
		if reset {
			current = query()
			header.setHeader(current.Collections)
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
					coll := d.Collection || q.Collections
					items = append(items, wsItem{ID: d.ID, Title: d.Title, Cols: searchCols(d, coll), Detail: strings.Join(sizeAndDate(d.Size, d.Updated), "  ·  "), Tags: d.Tags, Desc: d.Description, Preview: d.Preview, Collection: coll, subs: d.Subscriptions, stars: d.Stars, size: d.Size, updated: d.Updated.Unix(), seq: len(items)})
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
	left, center := bar.left, bar.center
	if left == nil {
		left = layout.NewSpacer()
	}
	if center == nil {
		center = layout.NewSpacer()
	}
	barLayout := &wrapBarLayout{centerMin: bar.centerMin}
	barBox := container.New(barLayout, left, center, container.NewHBox(append(bar.right, openBrowse)...))
	resort := func() {
		if order != nil {
			order(items)
			b.refreshList(list)
		}
	}
	view := container.New(&browseLayout{bar: barLayout}, barBox, status, header, list)
	return view, func() { load(true) }, resort
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
	view, l := b.browseView(wsBar{left: radio, center: entry, centerMin: 200, right: []fyne.CanvasObject{refresh}}, func() workshop.BrowseQuery {
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
	bar := wsBar{left: radio, right: []fyne.CanvasObject{widget.NewLabel("Sort by"), sortSel, refresh}}
	view, l, resort := b.browseViewSorted(bar, func() workshop.BrowseQuery {
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
		updated, size := d.Updated, d.Size
		if !public {
			size = s.Size
			updated = time.Time{}
			if s.Updated > 0 {
				updated = time.Unix(s.Updated, 0)
			}
		}
		var info string
		if !updated.IsZero() {
			info = updatedAgo(updated, time.Now())
		}
		detail := append(popularity(d), sizeAndDate(size, updated)...)
		if !public && detailsOK {
			detail = append(detail, "private or hidden on the Workshop")
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
		items = append(items, wsItem{ID: s.ID, Title: title, Info: info, Detail: strings.Join(detail, "  ·  "), Tags: d.Tags, ModIDs: l.ModIDs, Requires: l.Requires, Desc: desc, State: subStateLabel(s.State), Subscribed: true, Installed: s.State == workshop.SubInstalled, Preview: preview, sub: s.State})
		if !updated.IsZero() {
			items[len(items)-1].updated = updated.Unix()
		}
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

type subsData struct {
	subs   []workshop.Sub
	err    error
	items  []wsItem
	derr   error
	active map[string]bool
}

func (b *wsBrowser) readSubs() subsData {
	var out subsData
	out.subs, out.err = b.deps.subs()
	var det map[string]workshop.Details
	if out.err == nil && len(out.subs) > 0 {
		ids := make([]string, len(out.subs))
		for i, s := range out.subs {
			ids[i] = s.ID
		}
		det, out.derr = b.deps.details(b.u.ctx, ids)
	}
	local := map[string]workshop.LocalItem{}
	var uploads map[string]string
	if b.deps.uploads != nil {
		uploads = b.deps.uploads()
	}
	for _, s := range out.subs {
		l := b.deps.local(s.Dir)
		if l.Image == "" {
			l.Image = uploads[s.ID]
		}
		local[s.ID] = l
	}
	if b.deps.active != nil {
		out.active = b.deps.active()
	}
	if out.err == nil {
		out.items = subItems(out.subs, det, local, out.derr == nil)
	}
	return out
}

const subsReadError = "Could not read Steam's Workshop file (appworkshop_108600.acf): "

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
			d := b.readSubs()
			fyne.Do(func() {
				b.active = d.active
				refresh.Enable()
				if d.err != nil {
					summary.SetText(subsReadError + d.err.Error())
					items = nil
					b.refreshList(list)
					return
				}
				counts := map[workshop.SubState]int{}
				for _, s := range d.subs {
					counts[s.State]++
				}
				items = d.items
				b.refreshList(list)
				msg := fmt.Sprintf("%d subscribed: %d installed, %d update pending, %d downloading or queued.", len(d.subs), counts[workshop.SubInstalled], counts[workshop.SubNeedsUpdate], counts[workshop.SubDownloading])
				if d.derr != nil {
					msg += " Titles unavailable: " + d.derr.Error()
				}
				summary.SetText(msg)
			})
		}()
	}
	refresh = widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), load)
	top := container.NewBorder(nil, nil, nil, refresh, summary)
	return container.NewBorder(top, nil, nil, nil, list), load
}

type recentRange struct {
	label string
	days  int
	words string
}

var recentRanges = []recentRange{{"Today", 1, "today"}, {"This Week", 7, "this week"}, {"This Month", 30, "this month"}}

func recentItems(all []wsItem, now time.Time, days int) []wsItem {
	cut := now.Add(-time.Duration(days) * 24 * time.Hour).Unix()
	var out []wsItem
	for _, it := range all {
		if it.updated > cut {
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].updated > out[j].updated })
	return out
}

func recentSummary(n, total int, r recentRange) string {
	if n == 0 {
		return fmt.Sprintf("None of your %d subscriptions were updated %s.", total, r.words)
	}
	verb := "were"
	if n == 1 {
		verb = "was"
	}
	return fmt.Sprintf("%d of your %d subscriptions %s updated %s.", n, total, verb, r.words)
}

func (b *wsBrowser) recentTab() (fyne.CanvasObject, func()) {
	var items, all []wsItem
	total := -1
	list := b.itemList(&items, nil)
	summary := widget.NewLabel("")
	summary.Wrapping = fyne.TextWrapWord
	labels := make([]string, len(recentRanges))
	for i, r := range recentRanges {
		labels[i] = r.label
	}
	prefs := b.u.app.Preferences()
	radio := widget.NewRadioGroup(labels, nil)
	radio.Horizontal = true
	radio.Required = true
	radio.SetSelected(prefs.StringWithFallback(prefRecentRange, recentRanges[1].label))
	if radio.Selected == "" {
		radio.SetSelected(recentRanges[1].label)
	}
	show := func() {
		if total < 0 {
			return
		}
		r := recentRanges[1]
		for _, c := range recentRanges {
			if c.label == radio.Selected {
				r = c
			}
		}
		items = recentItems(all, time.Now(), r.days)
		b.refreshList(list)
		summary.SetText(recentSummary(len(items), total, r))
	}
	radio.OnChanged = func(string) {
		prefs.SetString(prefRecentRange, radio.Selected)
		show()
	}
	var refresh *widget.Button
	load := func() {
		refresh.Disable()
		summary.SetText("Reading your Steam Workshop subscriptions...")
		go func() {
			d := b.readSubs()
			fyne.Do(func() {
				b.active = d.active
				refresh.Enable()
				if d.err != nil {
					summary.SetText(subsReadError + d.err.Error())
					all, items, total = nil, nil, -1
					b.refreshList(list)
					return
				}
				all, total = d.items, len(d.subs)
				show()
				if d.derr != nil {
					summary.SetText(summary.Text + " Steam details unavailable (" + d.derr.Error() + "): only private mods' local dates are known.")
				}
			})
		}()
	}
	refresh = widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), load)
	top := container.NewVBox(container.NewBorder(nil, nil, nil, refresh, radio), summary)
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
					if r.ContentDir != "" {
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
					it := wsItem{ID: m.ID, Title: title(m.ID, m.Title), Info: detailsInfo(d), Detail: detailsDetail(d), Tags: d.Tags, ModIDs: local[m.ID].ModIDs, Requires: local[m.ID].Requires, Desc: d.Description, State: label, Subscribed: subbed, Preview: preview(m.ID)}
					missing = append(missing, it)
				}
				for _, id := range r.IDs {
					if _, ok := miss[id]; ok {
						continue
					}
					d := det[id]
					installed = append(installed, wsItem{ID: id, Title: title(id, ""), Info: detailsInfo(d), Detail: detailsDetail(d), Tags: d.Tags, ModIDs: local[id].ModIDs, Requires: local[id].Requires, Desc: d.Description, State: "Installed", Subscribed: true, Installed: true, Preview: preview(id)})
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
