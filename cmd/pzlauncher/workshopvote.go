package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/katachinosekai/pzlauncher/internal/webview"
)

// Steam Workshop thumbs up / down for subscribed mods. The vote is read from
// and sent through the signed-in hidden browser (Steam's own vote buttons on
// the mod page). Steam has no "remove vote": the vote already given stays
// highlighted and clicking it again does nothing.

// voteUI is what the vote bar shows for one mod.
type voteUI struct {
	upHi, downHi bool // the vote already given
	enabled      bool
	note         string
}

// voteState decides the vote bar for a mod from the session and what is known
// about the vote (vote: "", up, down, none).
func voteState(signedIn, fallback, busy bool, vote, errText string) voteUI {
	switch {
	case fallback:
		return voteUI{note: "Voting needs the built-in browser, which could not start."}
	case !signedIn:
		return voteUI{note: "Sign in to Steam to vote."}
	case busy && vote == "":
		return voteUI{note: "Checking your vote on Steam..."}
	case busy:
		return voteUI{upHi: vote == webview.VoteUp, downHi: vote == webview.VoteDown, note: "Sending your vote to Steam..."}
	}
	ui := voteUI{upHi: vote == webview.VoteUp, downHi: vote == webview.VoteDown, enabled: true}
	switch {
	case errText != "":
		ui.note = "Steam: " + errText
	case vote == webview.VoteUp:
		ui.note = "You voted thumbs up."
	case vote == webview.VoteDown:
		ui.note = "You voted thumbs down."
	case vote == webview.VoteNone:
		ui.note = "You have not voted on this mod yet."
	}
	return ui
}

type voteBar struct {
	box      *fyne.Container
	up, down *widget.Button
	note     *widget.Label
}

func newVoteBar() *voteBar {
	v := &voteBar{
		up:   widget.NewButton("Thumbs up", nil),
		down: widget.NewButton("Thumbs down", nil),
		note: widget.NewLabel(""),
	}
	v.note.Importance = widget.LowImportance
	v.note.Truncation = fyne.TextTruncateEllipsis
	lbl := widget.NewLabel("Your vote:")
	v.box = container.NewBorder(nil, nil, container.NewHBox(lbl, v.up, v.down), nil, v.note)
	v.box.Hide()
	return v
}

func (v *voteBar) show(ui voteUI, onUp, onDown func()) {
	set := func(b *widget.Button, hi bool, tap func()) {
		b.Importance = widget.MediumImportance
		if hi {
			b.Importance = widget.HighImportance
		}
		b.OnTapped = tap
		if ui.enabled {
			b.Enable()
		} else {
			b.Disable()
		}
		b.Refresh()
	}
	set(v.up, ui.upHi, onUp)
	set(v.down, ui.downHi, onDown)
	v.note.SetText(ui.note)
	v.box.Show()
}

func (b *wsBrowser) signedIn() (signed, fallback bool) {
	if b.sess == nil {
		return false, false
	}
	return b.sess.known && b.sess.signedIn && !b.sess.fallback, b.sess.fallback
}

func (b *wsBrowser) voteMaps() {
	if b.votes == nil {
		b.votes, b.voteBusy, b.voteErr = map[string]string{}, map[string]bool{}, map[string]string{}
	}
}

// voteFor is the vote bar state for a mod; the first time an expanded row is
// shown while signed in it asks Steam for the current vote (called while a
// list row is bound, so it does not redraw the list itself).
func (b *wsBrowser) voteFor(id string) voteUI {
	b.voteMaps()
	signed, fallback := b.signedIn()
	if signed && b.votes[id] == "" && !b.voteBusy[id] && b.voteErr[id] == "" {
		b.runVote(id, "", "")
	}
	return voteState(signed, fallback, b.voteBusy[id], b.votes[id], b.voteErr[id])
}

// castVote sends a thumbs up / down; the vote already given is left alone.
func (b *wsBrowser) castVote(id, want, title string) {
	b.voteMaps()
	if b.votes[id] == want || b.voteBusy[id] {
		return
	}
	word := "thumbs up"
	if want == webview.VoteDown {
		word = "thumbs down"
	}
	b.status.SetText("Voting " + word + " on " + title + "...")
	b.runVote(id, want, title)
	b.notify()
}

func (b *wsBrowser) runVote(id, want, title string) {
	b.voteBusy[id] = true
	delete(b.voteErr, id)
	// an answer given at once (signed out, no browser) arrives while a list row
	// may still be binding: the caller shows the new state, no redraw then
	inCall := true
	defer func() { inCall = false }()
	send := b.voteSend
	if send == nil {
		send = b.sess.vote
	}
	send(id, want, func(e webview.Event) {
		b.voteMaps() // may have been cleared by a sign-out meanwhile
		delete(b.voteBusy, id)
		switch e.Result {
		case webview.ResultDone, webview.ResultAlready:
			if e.Vote != "" {
				b.votes[id] = e.Vote
			}
			if want != "" {
				b.status.SetText("Voted on " + title + ".")
			}
		case webview.ResultNotSignedIn:
			if want != "" {
				b.status.SetText("Sign in to Steam to vote.")
			}
		default:
			msg := e.Error
			if msg == "" {
				msg = "the vote did not go through"
			}
			b.voteErr[id] = msg
			if want != "" {
				b.status.SetText("Could not vote on " + title + ": " + msg)
			}
		}
		if !inCall {
			b.notify()
		}
	})
}

// sessionChanged forgets votes when signed out (they belong to that account)
// and redraws the lists so vote bars follow the session.
func (b *wsBrowser) sessionChanged(signedIn bool) {
	if !signedIn {
		b.votes, b.voteBusy, b.voteErr = nil, nil, nil
	}
	b.notify()
}
