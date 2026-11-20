package webview

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const HelperFlag = "--webview"

const EphemeralFlag = "--ephemeral"

const ExitUnavailable = 3

const JobTimeout = 30 * time.Second

type Engine interface {
	Load(url string)
	Show()
	Hide()
	Eval(js string)
	ClearData(done func())
	Quit()
	Post(fn func())
}

type Options struct {
	DataDir   string
	CacheDir  string
	Open      func(string) error
	Ephemeral bool
	In        io.Reader
	Out       io.Writer
}

type signMode int

const (
	signNone signMode = iota
	signWindow
	signQR
)

type Helper struct {
	mu   sync.Mutex
	out  io.Writer
	open func(string) error

	PostJS    string
	eng       Engine
	sign      signMode
	known     bool
	signedIn  bool
	name      string
	jobs      []Command
	cur       *Command
	gen       int
	timeout   time.Duration
	afterFunc func(time.Duration, func()) func()
	stopTimer func()
	lastQR    string
	lastChal  string
	clearing  bool
}

func NewHelper(out io.Writer, open func(string) error) *Helper {
	return &Helper{out: out, open: open, timeout: JobTimeout, afterFunc: func(d time.Duration, f func()) func() {
		t := time.AfterFunc(d, f)
		return func() { t.Stop() }
	}}
}

func (h *Helper) Emit(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.out != nil {
		_ = WriteEvent(h.out, e)
	}
}

func (h *Helper) Start(e Engine) {
	h.eng = e
	trace("helper started, loading %s", StatusURL)
	e.Load(StatusURL)
}

func (h *Helper) Apply(c Command) {
	if !c.Valid() || h.eng == nil {
		return
	}
	e := h.eng
	trace("command %s mode=%s", c.Op, c.Mode)
	switch c.Op {
	case OpSignIn:
		h.requeueCurrent()
		h.lastQR, h.lastChal = "", ""
		if c.Mode == ModeWindow {
			h.sign = signWindow
			e.Load(LoginURL)
			e.Show()
		} else {
			h.sign = signQR
			e.Hide()
			e.Load(LoginURL)
		}
	case OpCancelSignIn:
		if h.sign != signNone {
			h.sign = signNone
			e.Hide()
			e.Load(StatusURL)
			h.next()
		}
	case OpSignOut:
		h.requeueCurrent()
		h.jobs = nil
		h.clearing = true
		e.ClearData(func() {
			h.clearing = false
			h.known, h.signedIn, h.name = true, false, ""
			h.Emit(Event{Event: EvSession})
			e.Load(StatusURL)
		})
	case OpState:
		if h.sign == signNone && h.cur == nil && !h.clearing {
			e.Load(StatusURL)
		} else {
			h.emitSession()
		}
	case OpSubscribe:
		h.jobs = append(h.jobs, c)
		h.next()
	case OpQuit:
		e.Quit()
	}
}

func (h *Helper) emitSession() {
	trace("session known=%v signedIn=%v", h.known, h.signedIn)
	if h.known {
		h.Emit(Event{Event: EvSession, SignedIn: h.signedIn, Name: h.name})
	}
}

func (h *Helper) next() {
	if h.cur != nil || h.sign != signNone || h.clearing || len(h.jobs) == 0 {
		return
	}
	c := h.jobs[0]
	h.jobs = h.jobs[1:]
	h.cur = &c
	h.gen++
	gen := h.gen
	h.stopTimer = h.afterFunc(h.timeout, func() {
		h.eng.Post(func() {
			if h.cur != nil && h.gen == gen {
				h.finish(ResultFailed, "Steam did not answer in time")
			}
		})
	})
	h.eng.Load(ItemURL(c.ID))
}

func (h *Helper) stop() {
	if h.stopTimer != nil {
		h.stopTimer()
		h.stopTimer = nil
	}
}

func (h *Helper) finish(result, errText string) {
	c := h.cur
	if c == nil {
		return
	}
	h.stop()
	h.cur = nil
	h.Emit(Event{Event: EvSubResult, ID: c.ID, Req: c.Req, Result: result, Error: errText})
	if result == ResultNotSignedIn && (h.signedIn || !h.known) {
		h.known, h.signedIn, h.name = true, false, ""
		h.emitSession()
	}
	h.next()
}

func (h *Helper) requeueCurrent() {
	if h.cur == nil {
		return
	}
	h.jobs = append([]Command{*h.cur}, h.jobs...)
	h.cur = nil
	h.stop()
	h.gen++
}

func (h *Helper) WindowClosed() {
	h.eng.Hide()
	if h.sign == signNone {
		return
	}
	h.sign = signNone
	h.Emit(Event{Event: EvSignInClosed})
	h.eng.Load(StatusURL)
	h.next()
}

func (h *Helper) Decide(raw string, userGesture bool) Action {
	a := Decide(raw, userGesture)
	if a == External && h.open != nil {
		if err := h.open(raw); err == nil {
			h.Emit(Event{Event: EvExternal, URL: raw})
		}
	}
	return a
}

func (h *Helper) ScriptMessage(s string) {
	m, ok := ParseScriptMessage(s)
	if !ok || h.eng == nil {
		trace("script message ignored (parsed=%v engine=%v, %d bytes)", ok, h.eng != nil, len(s))
		return
	}
	switch m.Kind {
	case MsgPage:
		h.page(m)
	case MsgQR:
		if h.sign == signQR && (m.Image != h.lastQR || m.Challenge != h.lastChal) {
			h.lastQR, h.lastChal = m.Image, m.Challenge
			h.Emit(Event{Event: EvQR, Image: m.Image, Challenge: m.Challenge})
		}
	case MsgResult:
		if h.cur != nil && h.cur.ID == m.ID {
			h.finish(m.Result, m.Error)
		}
	}
}

func (h *Helper) page(m ScriptMsg) {
	trace("page %s signedIn=%v named=%v signing=%v %s", stripQuery(m.URL), m.SignedIn, m.Name != "", h.sign != signNone, m.Diag)
	changed := !h.known || h.signedIn != m.SignedIn || (m.SignedIn && m.Name != "" && m.Name != h.name)
	h.known, h.signedIn = true, m.SignedIn
	if m.SignedIn {
		if m.Name != "" {
			h.name = m.Name
		}
	} else {
		h.name = ""
	}
	if m.SignedIn && h.sign != signNone {
		h.sign = signNone
		h.eng.Hide()
		changed = true
	}
	if changed {
		h.emitSession()
	}
	if h.sign == signNone && h.cur != nil && m.ItemID == h.cur.ID {
		h.eng.Eval(SubscribeScript(h.cur.ID, h.cur.Unsubscribe, h.cur.Deps, h.PostJS))
	}
	h.next()
}

func (h *Helper) ReadLoop(in io.Reader, post func(func())) {
	_ = ReadCommands(in, func(c Command) { post(func() { h.Apply(c) }) })
	post(func() { h.Apply(Command{Op: OpQuit}) })
}

var ErrUnavailable = errors.New("no built-in browser engine")

func DataDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "PZLauncher", "webview")
}

func CacheDir() string {
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "PZLauncher", "webview")
}

func RunHelper(o Options) int {
	if o.In == nil {
		o.In = os.Stdin
	}
	if o.Out == nil {
		o.Out = os.Stdout
	}
	if o.DataDir == "" {
		o.DataDir = DataDir()
	}
	if o.CacheDir == "" {
		o.CacheDir = CacheDir()
	}
	if !o.Ephemeral {
		_ = os.MkdirAll(o.DataDir, 0o700)
		_ = os.MkdirAll(o.CacheDir, 0o700)
	}
	h := NewHelper(o.Out, o.Open)
	if err := runEngine(h, o); err != nil {
		h.Emit(Event{Event: EvUnavailable, Error: err.Error()})
		return ExitUnavailable
	}
	return 0
}

func trace(format string, a ...any) {
	fmt.Fprintf(os.Stderr, time.Now().Format("15:04:05.000 ")+format+"\n", a...)
}

func stripQuery(u string) string {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		return u[:i]
	}
	return u
}
