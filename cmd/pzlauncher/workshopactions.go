package main

import (
	"context"
	"fmt"
	"image"
	"sort"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/katachinosekai/pzlauncher/internal/webview"
	"github.com/katachinosekai/pzlauncher/internal/workshop"
)

// Steam-style actions in a Workshop row's expanded box, on every tab:
// vote up / down, Favorite, Award, Share, Add to Collection, and the item's
// reactions (Steam Points awards it received). Votes, favorite and
// collections go through the signed-in hidden browser on the item's Steam page
// (Steam's own buttons and calls); Award shows Steam's own award window;
// Share stays local. The pressed vote and the favorite light up like Steam's
// "toggled" buttons. Steam has no "remove vote": clicking the vote already
// given does nothing.

const maxReactionsShown = 10

// itemState is what is known about one item for the signed-in account.
type itemState struct {
	vote, fav  string // webview.Vote* / Fav* ("" = not read yet)
	busy       string // action running (webview.Act*), "" = idle
	err        string
	reacts     []workshop.Reaction
	reactState int // 0 not fetched, 1 fetching, 2 done
}

// actionUI is what the action row shows for an item.
type actionUI struct {
	upHi, downHi, favHi bool
	steam               bool // vote / favorite / award / add-to-collection usable
	note                string
}

var busyNotes = map[string]string{
	webview.ActRead:     "Checking your vote and favorite on Steam...",
	webview.ActVoteUp:   "Sending your vote to Steam...",
	webview.ActVoteDown: "Sending your vote to Steam...",
	webview.ActFav:      "Adding to your favorites...",
	webview.ActUnfav:    "Removing from your favorites...",
	webview.ActColls:    "Loading your collections...",
	webview.ActSetColls: "Saving your collections...",
	webview.ActAward:    "Opening Steam's award window...",
}

func actionState(signedIn, fallback bool, st itemState) actionUI {
	switch {
	case fallback:
		return actionUI{note: "Voting, favorites and awards need the built-in browser, which could not start."}
	case !signedIn:
		return actionUI{note: "Sign in to Steam to vote, favorite or award."}
	}
	ui := actionUI{upHi: st.vote == webview.VoteUp, downHi: st.vote == webview.VoteDown, favHi: st.fav == webview.FavYes, steam: st.busy == ""}
	switch {
	case st.busy != "":
		ui.note = busyNotes[st.busy]
	case st.err != "":
		ui.note = "Steam: " + st.err
	}
	return ui
}

type actionBar struct {
	box                                  *fyne.Container
	up, down, fav, award, share, addColl *widget.Button
	note                                 *widget.Label
	reacts                               *fyne.Container
}

func newActionBar() *actionBar {
	a := &actionBar{
		up:      widget.NewButtonWithIcon("", iconThumbUp, nil),
		down:    widget.NewButtonWithIcon("", iconThumbDown, nil),
		fav:     widget.NewButtonWithIcon("Favorite", iconStar, nil),
		award:   widget.NewButtonWithIcon("Award", iconAward, nil),
		share:   widget.NewButtonWithIcon("Share", iconShare, nil),
		addColl: widget.NewButtonWithIcon("Add to Collection", iconAddColl, nil),
		note:    widget.NewLabel(""),
		reacts:  container.NewHBox(),
	}
	a.note.Importance = widget.LowImportance
	a.note.Truncation = fyne.TextTruncateEllipsis
	buttons := container.NewHBox(a.up, a.down, a.fav, a.award, a.share, a.addColl)
	a.box = container.NewVBox(container.NewBorder(nil, nil, buttons, nil, a.note), a.reacts)
	a.box.Hide()
	return a
}

// actionCalls are what the buttons do for the item on this row.
type actionCalls struct {
	up, down, fav, award, share, addColl func()
}

func (a *actionBar) show(ui actionUI, do actionCalls) {
	set := func(b *widget.Button, hi, on bool, tap func()) {
		b.Importance = widget.MediumImportance
		if hi {
			b.Importance = widget.HighImportance
		}
		b.OnTapped = tap
		if on {
			b.Enable()
		} else {
			b.Disable()
		}
		b.Refresh()
	}
	set(a.up, ui.upHi, ui.steam, do.up)
	set(a.down, ui.downHi, ui.steam, do.down)
	set(a.fav, ui.favHi, ui.steam, do.fav)
	set(a.award, false, ui.steam, do.award)
	set(a.addColl, false, ui.steam, do.addColl)
	set(a.share, false, true, do.share)
	a.note.SetText(ui.note)
	a.box.Show()
}

// showReactions fills the reactions line: icon + count, most given first.
func (a *actionBar) showReactions(rs []workshop.Reaction, icon func(url string, set func(image.Image)) image.Image) {
	a.reacts.Objects = nil
	if len(rs) == 0 {
		a.reacts.Hide()
		a.reacts.Refresh()
		return
	}
	a.reacts.Add(widget.NewLabel("Reactions:"))
	total := 0
	for _, r := range rs {
		total += r.Count
	}
	for i, r := range rs {
		if i == maxReactionsShown {
			a.reacts.Add(widget.NewLabel(fmt.Sprintf("+%d more", len(rs)-i)))
			break
		}
		img := canvas.NewImageFromImage(nil)
		img.FillMode = canvas.ImageFillContain
		img.SetMinSize(fyne.NewSize(22, 22))
		img.Image = icon(workshop.ReactionIconURL(r.ID), func(im image.Image) { img.Image = im; img.Refresh() })
		lbl := widget.NewLabel(fmt.Sprint(r.Count))
		a.reacts.Add(container.NewHBox(container.NewCenter(img), lbl))
	}
	a.reacts.Show()
	a.reacts.Refresh()
}

func (b *wsBrowser) ctx() context.Context {
	if b.u != nil && b.u.ctx != nil {
		return b.u.ctx
	}
	return context.Background()
}

// reactionIcon loads a reaction icon through the thumbnail cache.
func (b *wsBrowser) reactionIcon(u string, set func(image.Image)) image.Image {
	if b.thumbs == nil {
		return nil
	}
	return b.thumbs.Get(b.ctx(), u, func(im image.Image) { fyne.Do(func() { set(im) }) })
}

func (b *wsBrowser) signedIn() (signed, fallback bool) {
	if b.sess == nil {
		return false, false
	}
	return b.sess.known && b.sess.signedIn && !b.sess.fallback, b.sess.fallback
}

func (b *wsBrowser) state(id string) *itemState {
	if b.itemStates == nil {
		b.itemStates = map[string]*itemState{}
	}
	st := b.itemStates[id]
	if st == nil {
		st = &itemState{}
		b.itemStates[id] = st
	}
	return st
}

// actionsFor is the action row state for an item. The first time an
// expanded row is shown it reads the vote / favorite (signed in) and the
// reactions; it is called while a list row is bound, so it does not redraw.
func (b *wsBrowser) actionsFor(id string) (actionUI, []workshop.Reaction) {
	st := b.state(id)
	signed, fallback := b.signedIn()
	if signed && st.vote == "" && st.fav == "" && st.busy == "" && st.err == "" {
		b.runItem(id, webview.ActRead, nil, "")
	}
	if st.reactState == 0 && b.deps.reactions != nil {
		st.reactState = 1
		ctx := b.ctx()
		go func() {
			rs, err := b.deps.reactions(ctx, id)
			fyne.Do(func() {
				st := b.state(id)
				st.reactState = 2
				if err == nil {
					st.reacts = rs
				}
				b.notify()
			})
		}()
	}
	return actionState(signed, fallback, *st), st.reacts
}

func (b *wsBrowser) vote(id, want, title string) {
	st := b.state(id)
	if st.busy != "" || (want == webview.ActVoteUp && st.vote == webview.VoteUp) || (want == webview.ActVoteDown && st.vote == webview.VoteDown) {
		return // Steam ignores re-voting the same way
	}
	word := "thumbs up"
	if want == webview.ActVoteDown {
		word = "thumbs down"
	}
	b.status.SetText("Voting " + word + " on " + title + "...")
	b.runItem(id, want, nil, title)
	b.notify()
}

func (b *wsBrowser) toggleFav(id, title string) {
	st := b.state(id)
	if st.busy != "" {
		return
	}
	act := webview.ActFav
	b.status.SetText("Adding " + title + " to your favorites...")
	if st.fav == webview.FavYes {
		act = webview.ActUnfav
		b.status.SetText("Removing " + title + " from your favorites...")
	}
	b.runItem(id, act, nil, title)
	b.notify()
}

func (b *wsBrowser) award(id, title string) {
	if b.state(id).busy != "" {
		return
	}
	b.status.SetText("Pick an award for " + title + " in the Steam window. Awards spend your Steam Points.")
	b.runItem(id, webview.ActAward, nil, title)
	b.notify()
}

// awardClosed: Steam's award window was closed; reload the item's reactions.
func (b *wsBrowser) awardClosed(id string) {
	st := b.state(id)
	st.reactState = 0
	b.status.SetText("Steam award window closed.")
	b.notify()
}

func (b *wsBrowser) addToCollection(id, title string) {
	if b.state(id).busy != "" {
		return
	}
	b.status.SetText("Loading your collections...")
	b.runItemThen(id, webview.ActColls, nil, title, func(e webview.Event) {
		if e.Result != webview.ResultDone {
			return
		}
		b.status.SetText("")
		b.collectionsDialog(id, title, e.Colls)
	})
	b.notify()
}

// collectionChanges is what to send for the ticked boxes: only differences.
func collectionChanges(colls []webview.Coll, ticked map[string]bool) []webview.CollChange {
	var out []webview.CollChange
	for _, c := range colls {
		if ticked[c.ID] != c.In {
			out = append(out, webview.CollChange{ID: c.ID, Title: c.Title, Add: ticked[c.ID]})
		}
	}
	return out
}

func (b *wsBrowser) collectionsDialog(id, title string, colls []webview.Coll) {
	if len(colls) == 0 {
		dialog.ShowInformation("Add to Collection", "You have no collections that can hold this item. Create a collection on Steam first.", b.u.win)
		return
	}
	sort.SliceStable(colls, func(i, j int) bool { return colls[i].Title < colls[j].Title })
	ticked := map[string]bool{}
	box := container.NewVBox()
	for _, c := range colls {
		c := c
		ticked[c.ID] = c.In
		name := c.Title
		if name == "" {
			name = "Collection " + c.ID
		}
		chk := widget.NewCheck(name, func(on bool) { ticked[c.ID] = on })
		chk.SetChecked(c.In)
		box.Add(chk)
	}
	scroll := container.NewVScroll(box)
	scroll.SetMinSize(fyne.NewSize(360, min(float32(len(colls))*40+8, 320)))
	content := container.NewBorder(widget.NewLabel("Your collections holding \""+title+"\":"), nil, nil, nil, scroll)
	d := dialog.NewCustomConfirm("Add to Collection", "Save", "Cancel", content, func(ok bool) {
		if !ok {
			return
		}
		changes := collectionChanges(colls, ticked)
		if len(changes) == 0 {
			return
		}
		b.status.SetText("Saving your collections...")
		b.runItemThen(id, webview.ActSetColls, changes, title, func(e webview.Event) {
			if e.Result == webview.ResultDone {
				b.status.SetText(fmt.Sprintf("Updated %d of your collections for %s.", len(changes), title))
			}
		})
		b.notify()
	}, b.u.win)
	d.Show()
}

func (b *wsBrowser) share(id, title string) {
	link := workshop.ItemURL(id)
	entry := widget.NewEntry()
	entry.SetText(link)
	entry.OnChanged = func(s string) {
		if s != link {
			entry.SetText(link)
		}
	}
	copyBtn := widget.NewButtonWithIcon("Copy link", iconShare, func() {
		b.u.app.Clipboard().SetContent(link)
		b.status.SetText("Copied the link to " + title + ".")
	})
	content := container.NewVBox(
		widget.NewLabel("Link to \""+title+"\":"),
		entry,
		container.NewHBox(copyBtn,
			widget.NewButton("Open in browser", func() { b.openExternal(link) }),
			widget.NewButton("Open in Steam", func() { b.openExternal("steam://url/CommunityFilePage/" + id) })),
	)
	d := dialog.NewCustom("Share", "Close", content, b.u.win)
	d.Resize(fyne.NewSize(520, 0))
	d.Show()
}

func (b *wsBrowser) runItem(id, act string, colls []webview.CollChange, title string) {
	b.runItemThen(id, act, colls, title, nil)
}

// runItemThen runs an item action and updates the item's state; then (if
// set) gets the result too.
func (b *wsBrowser) runItemThen(id, act string, colls []webview.CollChange, title string, then func(webview.Event)) {
	st := b.state(id)
	st.busy = act
	st.err = ""
	// an answer given at once (signed out, no browser) arrives while a list row
	// may still be binding: the caller shows the new state, no redraw then
	inCall := true
	defer func() { inCall = false }()
	send := b.itemSend
	if send == nil {
		send = b.sess.item
	}
	send(id, act, colls, func(e webview.Event) {
		st := b.state(id) // may have been reset by a sign-out meanwhile
		st.busy = ""
		switch e.Result {
		case webview.ResultDone, webview.ResultAlready:
			if e.Vote != "" {
				st.vote = e.Vote
			}
			if e.Fav != "" {
				st.fav = e.Fav
			}
			switch act {
			case webview.ActVoteUp, webview.ActVoteDown:
				b.status.SetText("Voted on " + title + ".")
			case webview.ActFav:
				b.status.SetText("Added " + title + " to your favorites.")
			case webview.ActUnfav:
				b.status.SetText("Removed " + title + " from your favorites.")
			}
		case webview.ResultNotSignedIn:
			if act != webview.ActRead {
				b.status.SetText("Sign in to Steam to vote, favorite or award.")
			}
		default:
			msg := e.Error
			if msg == "" {
				msg = "it did not go through"
			}
			st.err = msg
			if act != webview.ActRead {
				b.status.SetText("Steam: " + msg)
			}
		}
		if then != nil {
			then(e)
		}
		if !inCall {
			b.notify()
		}
	})
}

// sessionChanged forgets votes and favorites when signed out (they belong to
// that account; reactions are public and stay) and redraws the lists.
func (b *wsBrowser) sessionChanged(signedIn bool) {
	if !signedIn {
		for _, st := range b.itemStates {
			st.vote, st.fav, st.err = "", "", ""
		}
	}
	b.notify()
}
