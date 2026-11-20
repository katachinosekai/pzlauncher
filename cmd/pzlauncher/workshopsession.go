package main

import (
	"image/color"
	"os"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/katachinosekai/pzlauncher/internal/webview"
)

const (
	resOpened = "opened"
)

type wsPending struct {
	id    string
	unsub bool
	cb    func(webview.Event)
}

type wsSession struct {
	b          *wsBrowser
	label      *widget.Label
	signInBtn  *widget.Button
	signOutBtn *widget.Button

	started   bool
	ephemeral bool
	known     bool
	signedIn  bool
	name      string
	fallback  bool
	signingIn bool
	nextReq   int
	reqs      map[int]wsPending
	waiting   []wsPending

	qrDlg  *dialog.CustomDialog
	qrImg  *canvas.Image
	qrNote *widget.Label
}

func newWSSession(b *wsBrowser) *wsSession {
	s := &wsSession{b: b, reqs: map[int]wsPending{}}
	s.label = widget.NewLabel("Steam sign-in: not checked yet.")
	s.label.Truncation = fyne.TextTruncateEllipsis
	s.signInBtn = widget.NewButtonWithIcon("Sign in to Steam", theme.LoginIcon(), func() { s.askSignIn() })
	s.signOutBtn = widget.NewButtonWithIcon("Sign out", theme.LogoutIcon(), func() { s.signOut() })
	s.signOutBtn.Hide()
	return s
}

func (s *wsSession) args() []string {
	if s.ephemeral {
		return []string{webview.HelperFlag, webview.EphemeralFlag}
	}
	return []string{webview.HelperFlag}
}

func (s *wsSession) ensure() {
	if s.started || s.fallback {
		return
	}
	s.started = true
	s.ephemeral = false
	s.b.ctl.Args = s.args()
	s.label.SetText("Steam sign-in: checking...")
	s.send(webview.Command{Op: webview.OpState})
}

func (s *wsSession) send(c webview.Command) bool {
	if err := s.b.ctl.Send(c); err != nil {
		return false
	}
	return true
}

func (s *wsSession) refresh() {
	switch {
	case s.fallback:
		s.label.SetText("Built-in browser unavailable: Subscribe opens Steam pages in your default browser.")
	case !s.known:
		s.label.SetText("Steam sign-in: checking...")
	case s.signedIn:
		who := "Signed in to Steam"
		if s.name != "" {
			who += " as " + s.name
		}
		s.label.SetText(who)
	default:
		msg := "Not signed in to Steam (only needed to Subscribe/Unsubscribe here)."
		s.label.SetText(msg)
	}
	if s.signedIn && !s.fallback {
		s.signInBtn.Hide()
		s.signOutBtn.Show()
	} else {
		s.signOutBtn.Hide()
		s.signInBtn.Show()
	}
}

func (s *wsSession) onEvent(e webview.Event) {
	switch e.Event {
	case webview.EvSession:
		s.known, s.signedIn, s.name = true, e.SignedIn, e.Name
		if e.SignedIn {
			s.signingIn = false
			if s.qrDlg != nil {
				s.qrDlg.Hide()
				s.qrDlg = nil
			}
			w := s.waiting
			s.waiting = nil
			for _, p := range w {
				s.subscribe(p.id, p.unsub, p.cb)
			}
		}
		s.refresh()
	case webview.EvQR:
		if s.qrImg == nil || e.Image == "" {
			return
		}
		if img, err := webview.DecodeQR(e.Image); err == nil {
			s.qrImg.Image = img
			s.qrImg.Refresh()
			s.qrNote.SetText("Scan with the Steam Mobile app and approve. The code refreshes by itself.")
		}
	case webview.EvSignInClosed:
		s.signingIn = false
		s.failWaiting("sign-in cancelled")
	case webview.EvSubResult:
		p, ok := s.reqs[e.Req]
		if !ok {
			return
		}
		delete(s.reqs, e.Req)
		if e.Result == webview.ResultNotSignedIn {
			s.known, s.signedIn = true, false
			s.refresh()
			s.waiting = append(s.waiting, p)
			if !s.signingIn {
				s.askSignIn()
			}
			return
		}
		p.cb(e)
	}
}

func (s *wsSession) onFallback(err error) {
	s.fallback = true
	s.refresh()
	msg := "The built-in browser could not start, so Subscribe opens the mod's Steam page in your default web browser instead; press Subscribe there."
	if err != nil {
		msg += "\n\nReason: " + err.Error()
	}
	dialog.ShowInformation("Built-in browser unavailable", msg, s.b.u.win)
	pend := s.reqs
	s.reqs = map[int]wsPending{}
	for _, p := range pend {
		s.openExternal(p)
	}
	w := s.waiting
	s.waiting = nil
	for _, p := range w {
		s.openExternal(p)
	}
}

func (s *wsSession) openExternal(p wsPending) {
	_ = s.b.deps.external(webview.ItemURL(p.id))
	p.cb(webview.Event{Event: webview.EvSubResult, ID: p.id, Result: resOpened})
}

func (s *wsSession) subscribe(id string, unsub bool, cb func(webview.Event)) {
	p := wsPending{id: id, unsub: unsub, cb: cb}
	s.ensure()
	if s.fallback {
		s.openExternal(p)
		return
	}
	s.nextReq++
	req := s.nextReq
	s.reqs[req] = p
	if !s.send(webview.Command{Op: webview.OpSubscribe, ID: id, Req: req, Unsubscribe: unsub}) {
		delete(s.reqs, req)
		s.openExternal(p)
	}
}

func (s *wsSession) failWaiting(reason string) {
	w := s.waiting
	s.waiting = nil
	for _, p := range w {
		p.cb(webview.Event{Event: webview.EvSubResult, ID: p.id, Result: webview.ResultNotSignedIn, Error: reason})
	}
}

func (s *wsSession) askSignIn() {
	s.ensure()
	if s.fallback {
		dialog.ShowInformation("Sign in to Steam", "The built-in browser is unavailable, so the launcher cannot sign in to Steam. Subscribe opens Steam pages in your default browser instead.", s.b.u.win)
		s.failWaiting("built-in browser unavailable")
		return
	}
	s.startSignIn(webview.ModeWindow)
}

func (s *wsSession) startSignIn(mode string) {
	s.signingIn = true
	if !s.send(webview.Command{Op: webview.OpSignIn, Mode: mode}) {
		return
	}
	if mode == webview.ModeWindow {
		s.label.SetText("Finish signing in in the Steam window. It closes by itself when you are signed in.")
		return
	}
	s.qrImg = canvas.NewImageFromImage(nil)
	s.qrImg.FillMode = canvas.ImageFillContain
	s.qrImg.ScaleMode = canvas.ImageScalePixels
	s.qrImg.SetMinSize(fyne.NewSize(260, 260))
	s.qrNote = widget.NewLabel("Loading Steam's sign-in code...")
	s.qrNote.Wrapping = fyne.TextWrapWord
	s.qrNote.Alignment = fyne.TextAlignCenter
	other := widget.NewButton("Use Steam's login page instead", nil)
	var d *dialog.CustomDialog
	switching := false
	other.OnTapped = func() {
		switching = true
		d.Hide()
		s.startSignIn(webview.ModeWindow)
	}
	quiet := canvas.NewRectangle(color.White)
	quiet.SetMinSize(fyne.NewSize(300, 300))
	content := container.NewVBox(container.NewCenter(container.NewStack(quiet, container.NewCenter(s.qrImg))), s.qrNote, container.NewCenter(other))
	d = dialog.NewCustom("Sign in with the Steam Mobile app", "Cancel", content, s.b.u.win)
	d.SetOnClosed(func() {
		if s.qrDlg == d {
			s.qrDlg, s.qrImg = nil, nil
		}
		if switching || !s.signingIn {
			return
		}
		s.signingIn = false
		s.send(webview.Command{Op: webview.OpCancelSignIn})
		s.failWaiting("sign-in cancelled")
	})
	d.Resize(fyne.NewSize(420, 0))
	s.qrDlg = d
	d.Show()
}

func (s *wsSession) signOut() {
	if !s.started || s.fallback {
		return
	}
	persistent := !s.ephemeral
	s.label.SetText("Signing out...")
	s.signOutBtn.Disable()
	go func() {
		s.b.ctl.CloseWait(5 * time.Second)
		if persistent {
			_ = os.RemoveAll(webview.DataDir())
			_ = os.RemoveAll(webview.CacheDir())
		}
		fyne.Do(func() {
			s.signOutBtn.Enable()
			s.known, s.signedIn, s.name = false, false, ""
			s.refresh()
			s.send(webview.Command{Op: webview.OpState})
		})
	}()
}
