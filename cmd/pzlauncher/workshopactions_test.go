package main

import (
	"context"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/katachinosekai/pzlauncher/internal/webview"
	"github.com/katachinosekai/pzlauncher/internal/workshop"
)

func TestActionState(t *testing.T) {
	for _, c := range []struct {
		name              string
		signed, fb        bool
		st                itemState
		up, down, fav, on bool
		note              string
	}{
		{"signed out", false, false, itemState{}, false, false, false, false, "Sign in to Steam to vote, favorite or award."},
		{"no browser", true, true, itemState{vote: "up"}, false, false, false, false, "Voting, favorites and awards need the built-in browser, which could not start."},
		{"reading", true, false, itemState{busy: webview.ActRead}, false, false, false, false, "Checking your vote and favorite on Steam..."},
		{"nothing given", true, false, itemState{vote: "none", fav: "no"}, false, false, false, true, ""},
		{"voted up + favorite", true, false, itemState{vote: "up", fav: "yes"}, true, false, true, true, ""},
		{"voted down", true, false, itemState{vote: "down", fav: "no"}, false, true, false, true, ""},
		{"sending", true, false, itemState{vote: "up", busy: webview.ActVoteDown}, true, false, false, false, "Sending your vote to Steam..."},
		{"error", true, false, itemState{vote: "up", err: "You must own the game"}, true, false, false, true, "Steam: You must own the game"},
	} {
		ui := actionState(c.signed, c.fb, c.st)
		if ui.upHi != c.up || ui.downHi != c.down || ui.favHi != c.fav || ui.steam != c.on || ui.note != c.note {
			t.Errorf("%s: %+v", c.name, ui)
		}
	}
}

type itemCall struct {
	id, act string
	colls   []webview.CollChange
	cb      func(webview.Event)
}

func newActionTestBrowser(t *testing.T) (*wsBrowser, *[]itemCall) {
	t.Helper()
	test.NewTempApp(t)
	b := &wsBrowser{busy: map[string]bool{}, subState: map[string]bool{}, status: newAutoLabel()}
	b.sess = newWSSession(b)
	b.sess.started, b.sess.known, b.sess.signedIn = true, true, true
	calls := &[]itemCall{}
	b.itemSend = func(id, act string, colls []webview.CollChange, cb func(webview.Event)) {
		*calls = append(*calls, itemCall{id, act, colls, cb})
	}
	return b, calls
}

func TestItemActionsFlow(t *testing.T) {
	b, calls := newActionTestBrowser(t)
	redraws := 0
	b.watch(func() { redraws++ })
	answer := func(res, vote, fav, errText string) {
		t.Helper()
		c := (*calls)[len(*calls)-1]
		c.cb(webview.Event{Event: webview.EvItemResult, ID: c.id, Result: res, Vote: vote, Fav: fav, Error: errText})
	}
	last := func() string { return (*calls)[len(*calls)-1].act }

	// opening the row reads vote + favorite once (no reactions dep: nothing fetched)
	if ui, _ := b.actionsFor("1"); ui.steam || ui.note != busyNotes[webview.ActRead] {
		t.Fatalf("%+v", ui)
	}
	b.actionsFor("1")
	if len(*calls) != 1 || last() != webview.ActRead {
		t.Fatalf("%+v", *calls)
	}
	answer(webview.ResultDone, webview.VoteNone, webview.FavNo, "")
	if ui, _ := b.actionsFor("1"); !ui.steam || ui.upHi || ui.downHi || ui.favHi || len(*calls) != 1 || redraws == 0 {
		t.Fatalf("%+v (%d calls, %d redraws)", ui, len(*calls), redraws)
	}

	// vote up lights thumbs up; voting up again sends nothing (Steam cannot remove a vote)
	b.vote("1", webview.ActVoteUp, "Some Mod")
	if last() != webview.ActVoteUp || b.status.Text != "Voting thumbs up on Some Mod..." {
		t.Fatalf("%s %q", last(), b.status.Text)
	}
	answer(webview.ResultDone, webview.VoteUp, webview.FavNo, "")
	if ui, _ := b.actionsFor("1"); !ui.upHi || ui.downHi || b.status.Text != "Voted on Some Mod." {
		t.Fatalf("%+v %q", ui, b.status.Text)
	}
	n := len(*calls)
	b.vote("1", webview.ActVoteUp, "Some Mod")
	if len(*calls) != n {
		t.Fatal("re-voting the same way must not send anything")
	}

	// favorite toggles on and off; the pressed favorite lights up
	b.toggleFav("1", "Some Mod")
	if last() != webview.ActFav {
		t.Fatal(last())
	}
	answer(webview.ResultDone, webview.VoteUp, webview.FavYes, "")
	if ui, _ := b.actionsFor("1"); !ui.favHi || b.status.Text != "Added Some Mod to your favorites." {
		t.Fatalf("%+v %q", ui, b.status.Text)
	}
	b.toggleFav("1", "Some Mod")
	if last() != webview.ActUnfav {
		t.Fatal(last())
	}
	answer(webview.ResultDone, webview.VoteUp, webview.FavNo, "")
	if ui, _ := b.actionsFor("1"); ui.favHi {
		t.Fatalf("%+v", ui)
	}

	// a refused vote keeps the old one and shows Steam's reason; no re-read after an error
	b.vote("1", webview.ActVoteDown, "Some Mod")
	answer(webview.ResultFailed, webview.VoteUp, webview.FavNo, "You must own the game")
	ui, _ := b.actionsFor("1")
	if !ui.upHi || ui.note != "Steam: You must own the game" || b.status.Text != "Steam: You must own the game" {
		t.Fatalf("%+v %q", ui, b.status.Text)
	}
	n = len(*calls)
	b.actionsFor("1")
	if len(*calls) != n {
		t.Fatal("an error must not trigger another read")
	}

	// award: asks Steam's window, and closing it reloads the reactions
	b.award("1", "Some Mod")
	if last() != webview.ActAward || b.status.Text != "Pick an award for Some Mod in the Steam window. Awards spend your Steam Points." {
		t.Fatalf("%s %q", last(), b.status.Text)
	}
	answer(webview.ResultDone, webview.VoteUp, webview.FavNo, "")
	b.state("1").reactState = 2
	b.sess.onEvent(webview.Event{Event: webview.EvAwardClosed, ID: "1"})
	if b.state("1").reactState != 0 {
		t.Fatal("closing the award window must reload the reactions")
	}

	// signing out forgets vote + favorite; signed out, nothing is sent
	b.sess.signedIn = false
	b.sessionChanged(false)
	n = len(*calls)
	if ui, _ := b.actionsFor("1"); ui.steam || ui.upHi || ui.note != "Sign in to Steam to vote, favorite or award." || len(*calls) != n {
		t.Fatalf("%+v", ui)
	}
}

func TestCollectionChanges(t *testing.T) {
	colls := []webview.Coll{{ID: "1", Title: "A", In: true}, {ID: "2", Title: "B"}, {ID: "3", Title: "C", In: true}}
	got := collectionChanges(colls, map[string]bool{"1": true, "2": true, "3": false})
	if len(got) != 2 || got[0] != (webview.CollChange{ID: "2", Title: "B", Add: true}) || got[1] != (webview.CollChange{ID: "3", Title: "C", Add: false}) {
		t.Fatalf("%+v", got)
	}
	if collectionChanges(colls, map[string]bool{"1": true, "3": true}) != nil {
		t.Fatal("unchanged boxes must send nothing")
	}
}

func TestAddToCollectionFlow(t *testing.T) {
	b, calls := newActionTestBrowser(t)
	w := test.NewWindow(nil)
	defer w.Close()
	b.u = &ui{win: w, ctx: context.Background()}
	b.addToCollection("1", "Some Mod")
	c := (*calls)[0]
	if c.act != webview.ActColls {
		t.Fatal(c.act)
	}
	c.cb(webview.Event{Event: webview.EvItemResult, Result: webview.ResultDone, Colls: []webview.Coll{{ID: "8", Title: "Pack", In: true}}})
	if b.state("1").busy != "" || b.status.Text != "" {
		t.Fatalf("%+v %q", b.state("1"), b.status.Text)
	}
	// a failed listing shows Steam's reason instead of a dialog
	b.addToCollection("1", "Some Mod")
	(*calls)[1].cb(webview.Event{Event: webview.EvItemResult, Result: webview.ResultFailed, Error: "Steam did not list your collections"})
	if b.status.Text != "Steam: Steam did not list your collections" {
		t.Fatalf("%q", b.status.Text)
	}
}

func TestSessionItemRouting(t *testing.T) {
	b, _ := newActionTestBrowser(t)
	s := b.sess
	var got []webview.Event
	s.reqs[5] = wsPending{id: "9", item: true, cb: func(e webview.Event) { got = append(got, e) }}
	s.onEvent(webview.Event{Event: webview.EvItemResult, Req: 6, Result: webview.ResultDone})
	s.onEvent(webview.Event{Event: webview.EvItemResult, Req: 5, Result: webview.ResultNotSignedIn})
	if len(got) != 1 || got[0].Result != webview.ResultNotSignedIn || s.signedIn {
		t.Fatalf("%+v signedIn=%v", got, s.signedIn)
	}
	// signed out: answered at once, nothing sent, no sign-in window
	var now []webview.Event
	s.item("9", webview.ActVoteUp, nil, func(e webview.Event) { now = append(now, e) })
	if len(now) != 1 || now[0].Result != webview.ResultNotSignedIn || s.signingIn {
		t.Fatalf("%+v signingIn=%v", now, s.signingIn)
	}
}

// TestActionsOnEveryKindOfRow expands a subscribed mod, a search result and a
// collection in one list (built synchronously: signed out, no browser helper)
// and checks each shows the action row and its reactions.
func TestActionsOnEveryKindOfRow(t *testing.T) {
	test.NewTempApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := test.NewWindow(nil)
	defer w.Close()
	deps := fakeWorkshopDeps()
	b := &wsBrowser{u: &ui{win: w, ctx: ctx}, deps: deps, busy: map[string]bool{}, subState: map[string]bool{}, status: newAutoLabel()}
	b.thumbs = newThumbCache(deps.thumb)
	b.sess = newWSSession(b)
	sub := subItems([]workshop.Sub{{ID: "101", State: workshop.SubInstalled}}, map[string]workshop.Details{"101": {ID: "101", Title: "Subscribed Mod"}}, nil, true)[0]
	d := workshop.Details{ID: "102", Title: "Search Result", Subscriptions: 5, Stars: 4}
	c := workshop.Details{ID: "103", Title: "A Collection", Children: 12}
	items := []wsItem{sub, {ID: d.ID, Title: d.Title, Cols: searchCols(d, false)}, {ID: c.ID, Title: c.Title, Cols: searchCols(c, true), Collection: true}}
	for _, it := range items {
		b.state(it.ID).reactState = 2 // reactions already known: no background fetch in this test
		b.state(it.ID).reacts = []workshop.Reaction{{ID: 34, Count: 2}}
	}
	list := b.itemList(&items, nil)
	w.SetContent(list)
	w.Resize(fyne.NewSize(900, 1200))
	rowFor := func(title string) *wsRow {
		var found *wsRow
		var walk func(o fyne.CanvasObject)
		walk = func(o fyne.CanvasObject) {
			if r, ok := o.(*wsRow); ok && r.title.Text == title {
				found = r
				return
			}
			switch v := o.(type) {
			case *fyne.Container:
				for _, k := range v.Objects {
					walk(k)
				}
			case fyne.Widget:
				for _, k := range test.WidgetRenderer(v).Objects() {
					walk(k)
				}
			}
		}
		walk(list)
		return found
	}
	for _, it := range items {
		r := rowFor(it.Title)
		if r == nil {
			t.Fatalf("%s: row not shown", it.Title)
		}
		test.Tap(r.toggle)
		if r = rowFor(it.Title); r == nil || !r.detBox.Visible() {
			t.Fatalf("%s: row did not expand", it.Title)
		}
		a := r.actions
		if !a.box.Visible() || a.note.Text != "Sign in to Steam to vote, favorite or award." {
			t.Errorf("%s: actions visible=%v note=%q", it.Title, a.box.Visible(), a.note.Text)
		}
		for name, btn := range map[string]bool{"up": a.up.Disabled(), "down": a.down.Disabled(), "favorite": a.fav.Disabled(), "award": a.award.Disabled(), "add to collection": a.addColl.Disabled()} {
			if !btn {
				t.Errorf("%s: %s usable while signed out", it.Title, name)
			}
		}
		if a.share.Disabled() {
			t.Errorf("%s: Share must work signed out", it.Title)
		}
		if !a.reacts.Visible() || len(a.reacts.Objects) != 2 {
			t.Errorf("%s: reactions line %v with %d parts", it.Title, a.reacts.Visible(), len(a.reacts.Objects))
		}
	}
}
