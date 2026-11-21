package main

import (
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/katachinosekai/pzlauncher/internal/webview"
)

func TestVoteState(t *testing.T) {
	for _, c := range []struct {
		name             string
		signed, fb, busy bool
		vote, err        string
		up, down, on     bool
		note             string
	}{
		{"signed out", false, false, false, "", "", false, false, false, "Sign in to Steam to vote."},
		{"no browser", true, true, false, "up", "", false, false, false, "Voting needs the built-in browser, which could not start."},
		{"reading", true, false, true, "", "", false, false, false, "Checking your vote on Steam..."},
		{"not voted", true, false, false, "none", "", false, false, true, "You have not voted on this mod yet."},
		{"voted up", true, false, false, "up", "", true, false, true, "You voted thumbs up."},
		{"voted down", true, false, false, "down", "", false, true, true, "You voted thumbs down."},
		{"sending", true, false, true, "up", "", true, false, false, "Sending your vote to Steam..."},
		{"error", true, false, false, "up", "You must own the game", true, false, true, "Steam: You must own the game"},
	} {
		ui := voteState(c.signed, c.fb, c.busy, c.vote, c.err)
		if ui.upHi != c.up || ui.downHi != c.down || ui.enabled != c.on || ui.note != c.note {
			t.Errorf("%s: %+v", c.name, ui)
		}
	}
}

type voteCall struct {
	id, want string
	cb       func(webview.Event)
}

func TestVoteFlow(t *testing.T) {
	test.NewTempApp(t)
	b := &wsBrowser{busy: map[string]bool{}, subState: map[string]bool{}, status: newAutoLabel()}
	b.sess = newWSSession(b)
	b.sess.started, b.sess.known, b.sess.signedIn = true, true, true
	var calls []voteCall
	b.voteSend = func(id, want string, cb func(webview.Event)) { calls = append(calls, voteCall{id, want, cb}) }
	redraws := 0
	b.watch(func() { redraws++ })
	answer := func(res, vote, errText string) {
		t.Helper()
		c := calls[len(calls)-1]
		c.cb(webview.Event{Event: webview.EvVoteResult, ID: c.id, Result: res, Vote: vote, Error: errText})
	}

	// opening the row reads the vote once
	if ui := b.voteFor("1"); ui.enabled || ui.note != "Checking your vote on Steam..." {
		t.Fatalf("%+v", ui)
	}
	b.voteFor("1")
	if len(calls) != 1 || calls[0].want != "" {
		t.Fatalf("%+v", calls)
	}
	answer(webview.ResultDone, webview.VoteNone, "")
	if ui := b.voteFor("1"); !ui.enabled || ui.upHi || ui.downHi || len(calls) != 1 {
		t.Fatalf("%+v (%d calls)", ui, len(calls))
	}
	if redraws == 0 {
		t.Fatal("the answer must redraw the list")
	}

	// vote up, then clicking up again sends nothing (Steam cannot remove a vote)
	b.castVote("1", webview.VoteUp, "Some Mod")
	if len(calls) != 2 || calls[1].want != webview.VoteUp || b.status.Text != "Voting thumbs up on Some Mod..." {
		t.Fatalf("%+v %q", calls, b.status.Text)
	}
	if ui := b.voteFor("1"); ui.enabled || ui.note != "Sending your vote to Steam..." {
		t.Fatalf("%+v", ui)
	}
	answer(webview.ResultDone, webview.VoteUp, "")
	if ui := b.voteFor("1"); !ui.upHi || ui.downHi || b.status.Text != "Voted on Some Mod." {
		t.Fatalf("%+v %q", ui, b.status.Text)
	}
	b.castVote("1", webview.VoteUp, "Some Mod")
	if len(calls) != 2 {
		t.Fatal("re-voting the same way must not send anything")
	}

	// a refused vote keeps the old one and shows Steam's reason
	b.castVote("1", webview.VoteDown, "Some Mod")
	answer(webview.ResultFailed, webview.VoteUp, "You must own the game")
	ui := b.voteFor("1")
	if !ui.upHi || ui.note != "Steam: You must own the game" || b.status.Text != "Could not vote on Some Mod: You must own the game" {
		t.Fatalf("%+v %q", ui, b.status.Text)
	}
	if len(calls) != 3 {
		t.Fatal("an error must not trigger another read")
	}

	// signing out forgets the votes; signed out, nothing is sent
	b.sess.signedIn = false
	b.sessionChanged(false)
	if ui := b.voteFor("1"); ui.enabled || ui.note != "Sign in to Steam to vote." || len(calls) != 3 {
		t.Fatalf("%+v (%d calls)", ui, len(calls))
	}

	// an answer arriving after sign-out does not crash
	b.sess.signedIn = true
	b.voteFor("2")
	b.sessionChanged(false)
	answer(webview.ResultDone, webview.VoteDown, "")
}

func TestSessionVoteRouting(t *testing.T) {
	test.NewTempApp(t)
	b := &wsBrowser{busy: map[string]bool{}, subState: map[string]bool{}, status: newAutoLabel()}
	s := newWSSession(b)
	b.sess = s
	s.known, s.signedIn = true, true
	var got []webview.Event
	s.reqs[5] = wsPending{id: "9", vote: true, cb: func(e webview.Event) { got = append(got, e) }}
	s.onEvent(webview.Event{Event: webview.EvVoteResult, Req: 6, Result: webview.ResultDone})
	s.onEvent(webview.Event{Event: webview.EvVoteResult, Req: 5, Result: webview.ResultNotSignedIn})
	if len(got) != 1 || got[0].Result != webview.ResultNotSignedIn || s.signedIn {
		t.Fatalf("%+v signedIn=%v", got, s.signedIn)
	}
	if _, ok := s.reqs[5]; ok {
		t.Fatal("request not cleared")
	}
	// signed out: answered at once, nothing sent, no sign-in window
	s.started = true
	var now []webview.Event
	s.vote("9", webview.VoteUp, func(e webview.Event) { now = append(now, e) })
	if len(now) != 1 || now[0].Result != webview.ResultNotSignedIn || s.signingIn {
		t.Fatalf("%+v signingIn=%v", now, s.signingIn)
	}
}
