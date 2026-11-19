package webview

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const HelperFlag = "--webview"

const ExitUnavailable = 3

type Engine interface {
	Navigate(url string)
	Present()
	Quit()
}

type Options struct {
	DataDir  string
	CacheDir string
	Open     func(string) error
	In       io.Reader
	Out      io.Writer
}

type Helper struct {
	mu   sync.Mutex
	out  io.Writer
	open func(string) error
	cur  string
}

func NewHelper(out io.Writer, open func(string) error) *Helper {
	return &Helper{out: out, open: open}
}

func (h *Helper) Emit(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.out != nil {
		_ = WriteEvent(h.out, e)
	}
}

func (h *Helper) Apply(e Engine, c Command) {
	if !c.Valid() {
		return
	}
	switch c.Op {
	case OpNavigate:
		e.Navigate(c.URL)
		e.Present()
	case OpShow:
		e.Present()
	case OpQuit:
		e.Quit()
	}
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

func (h *Helper) URLChanged(raw string) {
	h.mu.Lock()
	same := raw == h.cur
	h.cur = raw
	h.mu.Unlock()
	if !same {
		h.Emit(Event{Event: EvURL, URL: raw})
	}
}

func (h *Helper) ScriptMessage(s string) {
	if ev, ok := ParseScriptMessage(s); ok {
		h.Emit(ev)
	}
}

func (h *Helper) ReadLoop(in io.Reader, post func(Command)) {
	_ = ReadCommands(in, post)
	post(Command{Op: OpQuit})
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
	_ = os.MkdirAll(o.DataDir, 0o700)
	_ = os.MkdirAll(o.CacheDir, 0o700)
	h := NewHelper(o.Out, o.Open)
	if err := runEngine(h, o); err != nil {
		h.Emit(Event{Event: EvUnavailable, Error: err.Error()})
		return ExitUnavailable
	}
	return 0
}
