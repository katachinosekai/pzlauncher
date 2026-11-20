//go:build windows

package webview

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	pRegisterClassExW    = user32.NewProc("RegisterClassExW")
	pCreateWindowExW     = user32.NewProc("CreateWindowExW")
	pDefWindowProcW      = user32.NewProc("DefWindowProcW")
	pGetMessageW         = user32.NewProc("GetMessageW")
	pTranslateMessage    = user32.NewProc("TranslateMessage")
	pDispatchMessageW    = user32.NewProc("DispatchMessageW")
	pIsDialogMessageW    = user32.NewProc("IsDialogMessageW")
	pPostMessageW        = user32.NewProc("PostMessageW")
	pPostQuitMessage     = user32.NewProc("PostQuitMessage")
	pMoveWindow          = user32.NewProc("MoveWindow")
	pGetClientRect       = user32.NewProc("GetClientRect")
	pSetWindowTextW      = user32.NewProc("SetWindowTextW")
	pGetWindowTextW      = user32.NewProc("GetWindowTextW")
	pShowWindow          = user32.NewProc("ShowWindow")
	pIsIconic            = user32.NewProc("IsIconic")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	pDestroyWindow       = user32.NewProc("DestroyWindow")
	pLoadCursorW         = user32.NewProc("LoadCursorW")
	pGetDpiForWindow     = user32.NewProc("GetDpiForWindow")
	pEnableWindow        = user32.NewProc("EnableWindow")
)

const (
	wmDestroy    = 0x0002
	wmMove       = 0x0003
	wmSize       = 0x0005
	sizeMinimize = 1
	wmClose      = 0x0010
	wmCommand    = 0x0111
	wmApp        = 0x8000
	wsChild      = 0x40000000
	wsVisible    = 0x10000000
	wsTabStop    = 0x00010000
	wsBorder     = 0x00800000
	wsOverlap    = 0x00CF0000
	wsClipCh     = 0x02000000
	esAutoH      = 0x0080
	bsDefPush    = 0x0001
	ssCenterV    = 0x0200
	idOK         = 1
	idBack       = 101
	idFwd        = 102
	idReload     = 103
	idEdit       = 104
	idSecure     = 105
	swRestore    = 9
	swShow       = 5
	swHide       = 0
	cwDefault    = 0x80000000
	ptrSize      = unsafe.Sizeof(uintptr(0))
	iWV2Source   = 4
	iWV2NavSt    = 7
	iWV2SrcChg   = 11
	iWV2FrmNav   = 17
	iWV2Reload   = 31
	iWV2DevTools = 36
	iWV2Back     = 40
	iWV2Fwd      = 41
	iWV2NewWin   = 44
	iWV2TitleC   = 46
	iWV2Title    = 48
	iCtlCoreWV   = 25
)

type wndClassExW struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type winMsg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	X, Y    int32
	Private uint32
}

type winRect struct{ Left, Top, Right, Bottom int32 }

type comHandler struct {
	vtbl *comHandlerVtbl
	fn   func(sender, args unsafe.Pointer) uintptr
}

type comHandlerVtbl struct {
	QueryInterface, AddRef, Release, Invoke uintptr
}

var handlerVtbl = &comHandlerVtbl{
	QueryInterface: windows.NewCallback(func(this *comHandler, riid, out uintptr) uintptr { return 0 }),
	AddRef:         windows.NewCallback(func(this *comHandler) uintptr { return 1 }),
	Release:        windows.NewCallback(func(this *comHandler) uintptr { return 1 }),
	Invoke: windows.NewCallback(func(this *comHandler, sender, args unsafe.Pointer) uintptr {
		return this.fn(sender, args)
	}),
}

func comCall(obj unsafe.Pointer, idx uintptr, args ...uintptr) uintptr {
	vt := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vt, idx*ptrSize))
	r, _, _ := edge.ComProc(fn).Call(append([]uintptr{uintptr(obj)}, args...)...)
	return r
}

func comString(obj unsafe.Pointer, idx uintptr) string {
	var p *uint16
	comCall(obj, idx, uintptr(unsafe.Pointer(&p)))
	if p == nil {
		return ""
	}
	s := windows.UTF16PtrToString(p)
	windows.CoTaskMemFree(unsafe.Pointer(p))
	return s
}

func comBool(obj unsafe.Pointer, idx uintptr) bool {
	var b int32
	comCall(obj, idx, uintptr(unsafe.Pointer(&b)))
	return b != 0
}

func utf16(s string) uintptr {
	p, _ := windows.UTF16PtrFromString(s)
	return uintptr(unsafe.Pointer(p))
}

type winEngine struct {
	h                                          *Helper
	hwnd, host, edit, secure, back, fwd, goBtn uintptr
	reload                                     uintptr
	chromium                                   *edge.Chromium
	wv                                         unsafe.Pointer
	mu                                         sync.Mutex
	queue                                      []func()
	handlers                                   []*comHandler
}

var winEng *winEngine

func (e *winEngine) Load(url string) { e.chromium.Navigate(url) }

func (e *winEngine) Hide() { pShowWindow.Call(e.hwnd, swHide) }

func (e *winEngine) Eval(js string) { e.chromium.Eval(js) }

func (e *winEngine) ClearData(done func()) {
	if e.wv != nil {
		h := &comHandler{vtbl: handlerVtbl}
		h.fn = func(sender, args unsafe.Pointer) uintptr {
			e.Post(done)
			return 0
		}
		e.handlers = append(e.handlers, h)
		comCall(e.wv, iWV2DevTools, utf16("Network.clearBrowserCookies"), utf16("{}"), uintptr(unsafe.Pointer(h)))
		return
	}
	done()
}

func (e *winEngine) Show() {
	if r, _, _ := pIsIconic.Call(e.hwnd); r != 0 {
		pShowWindow.Call(e.hwnd, swRestore)
	}
	pShowWindow.Call(e.hwnd, swShow)
	e.layout()
	e.showView()
	pSetForegroundWindow.Call(e.hwnd)
}

func (e *winEngine) showView() {
	if e.chromium == nil {
		return
	}
	e.chromium.Show()
	e.chromium.Resize()
	e.chromium.NotifyParentWindowPositionChanged()
}

func (e *winEngine) Quit() { pDestroyWindow.Call(e.hwnd) }

func (e *winEngine) Post(fn func()) {
	e.mu.Lock()
	e.queue = append(e.queue, fn)
	e.mu.Unlock()
	pPostMessageW.Call(e.hwnd, wmApp+1, 0, 0)
}

func (e *winEngine) scale(v int32) int32 {
	if pGetDpiForWindow.Find() == nil {
		if d, _, _ := pGetDpiForWindow.Call(e.hwnd); d > 0 {
			return v * int32(d) / 96
		}
	}
	return v
}

func (e *winEngine) layout() {
	var r winRect
	pGetClientRect.Call(e.hwnd, uintptr(unsafe.Pointer(&r)))
	w := r.Right
	bar := e.scale(32)
	pad := e.scale(3)
	bw := e.scale(34)
	sw := e.scale(80)
	gw := e.scale(44)
	x := pad
	for _, b := range []uintptr{e.back, e.fwd, e.reload} {
		w := bw
		if b == e.reload {
			w = e.scale(64)
		}
		pMoveWindow.Call(b, uintptr(x), uintptr(pad), uintptr(w), uintptr(bar-2*pad), 1)
		x += w + pad
	}
	pMoveWindow.Call(e.secure, uintptr(x), uintptr(pad), uintptr(sw), uintptr(bar-2*pad), 1)
	x += sw + pad
	ew := max(w-x-gw-2*pad, 50)
	pMoveWindow.Call(e.edit, uintptr(x), uintptr(pad), uintptr(ew), uintptr(bar-2*pad), 1)
	pMoveWindow.Call(e.goBtn, uintptr(x+ew+pad), uintptr(pad), uintptr(gw), uintptr(bar-2*pad), 1)
	pMoveWindow.Call(e.host, 0, uintptr(bar), uintptr(w), uintptr(max(r.Bottom-bar, 1)), 1)
	if e.chromium != nil {
		e.chromium.Resize()
	}
}

func (e *winEngine) refresh() {
	if e.wv == nil {
		return
	}
	uri := comString(e.wv, iWV2Source)
	pSetWindowTextW.Call(e.edit, utf16(uri))
	label := "Not secure"
	if strings.HasPrefix(uri, "https://") {
		label = "Secure"
	}
	pSetWindowTextW.Call(e.secure, utf16(label))
	b := uintptr(0)
	if comBool(e.wv, 38) {
		b = 1
	}
	pEnableWindow.Call(e.back, b)
	f := uintptr(0)
	if comBool(e.wv, 39) {
		f = 1
	}
	pEnableWindow.Call(e.fwd, f)
}

func (e *winEngine) typed() {
	buf := make([]uint16, 4096)
	pGetWindowTextW.Call(e.edit, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	u := NormalizeTyped(windows.UTF16ToString(buf))
	if u == "" {
		return
	}
	if e.h.Decide(u, true) == Allow {
		e.chromium.Navigate(u)
	} else {
		e.refresh()
	}
}

func wndProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	e := winEng
	if e != nil && hwnd == e.hwnd {
		switch msg {
		case wmSize:
			if wp == sizeMinimize {
				return 0
			}
			e.layout()
			return 0
		case wmMove:
			if e.chromium != nil {
				e.chromium.NotifyParentWindowPositionChanged()
			}
		case wmCommand:
			switch wp & 0xffff {
			case idOK:
				e.typed()
			case idBack:
				if e.wv != nil {
					comCall(e.wv, iWV2Back)
				}
			case idFwd:
				if e.wv != nil {
					comCall(e.wv, iWV2Fwd)
				}
			case idReload:
				if e.wv != nil {
					comCall(e.wv, iWV2Reload)
				}
			}
			return 0
		case wmApp + 1:
			e.mu.Lock()
			q := e.queue
			e.queue = nil
			e.mu.Unlock()
			for _, fn := range q {
				fn()
			}
			return 0
		case wmClose:
			e.h.WindowClosed()
			return 0
		case wmDestroy:
			pPostQuitMessage.Call(0)
			return 0
		}
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

func (e *winEngine) addHandler(idx uintptr, fn func(sender, args unsafe.Pointer) uintptr) {
	h := &comHandler{vtbl: handlerVtbl, fn: fn}
	e.handlers = append(e.handlers, h)
	var token int64
	comCall(e.wv, idx, uintptr(unsafe.Pointer(h)), uintptr(unsafe.Pointer(&token)))
}

func (e *winEngine) child(class, text string, style, id uintptr) uintptr {
	h, _, _ := pCreateWindowExW.Call(0, utf16(class), utf16(text), wsChild|wsVisible|style, 0, 0, 10, 10, e.hwnd, id, 0, 0)
	return h
}

func removeLater(dir string) {
	for i := 0; i < 20; i++ {
		if os.RemoveAll(dir) == nil {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func runEngine(h *Helper, o Options) error {
	runtime.LockOSThread()
	_ = windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)
	e := &winEngine{h: h}
	winEng = e
	var inst windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &inst)
	cursor, _, _ := pLoadCursorW.Call(0, 32512)
	wc := wndClassExW{
		WndProc:    windows.NewCallback(wndProc),
		Instance:   inst,
		Cursor:     windows.Handle(cursor),
		Background: windows.Handle(16),
		ClassName:  windows.StringToUTF16Ptr("PZLauncherBrowser"),
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return err
	}
	hc := wndClassExW{
		WndProc:   pDefWindowProcW.Addr(),
		Instance:  inst,
		Cursor:    windows.Handle(cursor),
		ClassName: windows.StringToUTF16Ptr("PZLauncherHost"),
	}
	hc.Size = uint32(unsafe.Sizeof(hc))
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&hc))); r == 0 {
		return err
	}
	e.hwnd, _, _ = pCreateWindowExW.Call(0, utf16("PZLauncherBrowser"), utf16("Sign in to Steam - PZ Launcher"), wsOverlap|wsClipCh,
		cwDefault, cwDefault, 760, 820, 0, 0, uintptr(inst), 0)
	if e.hwnd == 0 {
		return errors.New("could not create the browser window")
	}
	e.back = e.child("BUTTON", "<", wsTabStop, idBack)
	e.fwd = e.child("BUTTON", ">", wsTabStop, idFwd)
	e.reload = e.child("BUTTON", "Reload", wsTabStop, idReload)
	e.secure = e.child("STATIC", "", ssCenterV, idSecure)
	e.edit = e.child("EDIT", "", wsTabStop|wsBorder|esAutoH, idEdit)
	e.goBtn = e.child("BUTTON", "Go", wsTabStop|bsDefPush, idOK)
	e.host = e.child("PZLauncherHost", "", wsClipCh, 0)
	e.layout()

	c := edge.NewChromium()
	c.DataPath = o.DataDir
	if o.Ephemeral {
		tmp, err := os.MkdirTemp("", "pzlauncher-webview-")
		if err != nil {
			return err
		}
		defer removeLater(tmp)
		c.DataPath = tmp
	}
	c.MessageCallback = h.ScriptMessage
	if !c.Embed(e.host) {
		return errors.New("Microsoft Edge WebView2 Runtime is not available")
	}
	e.chromium = c
	e.showView()
	ctl := c.GetController()
	if ctl == nil {
		return errors.New("WebView2 controller missing")
	}
	comCall(unsafe.Pointer(ctl), iCtlCoreWV, uintptr(unsafe.Pointer(&e.wv)))
	if e.wv == nil {
		return errors.New("WebView2 view missing")
	}
	h.PostJS = "window.chrome.webview.postMessage"
	c.Init(PageScript(h.PostJS))
	nav := func(sender, args unsafe.Pointer) uintptr {
		uri := comString(args, 3)
		if h.Decide(uri, comBool(args, 4)) != Allow {
			comCall(args, 8, 1)
		}
		return 0
	}
	e.addHandler(iWV2NavSt, nav)
	e.addHandler(iWV2FrmNav, nav)
	e.addHandler(iWV2NewWin, func(sender, args unsafe.Pointer) uintptr {
		uri := comString(args, 3)
		comCall(args, 6, 1)
		if h.Decide(uri, true) == Allow {
			c.Navigate(uri)
		}
		return 0
	})
	e.addHandler(iWV2SrcChg, func(sender, args unsafe.Pointer) uintptr {
		e.refresh()
		return 0
	})
	e.addHandler(iWV2TitleC, func(sender, args unsafe.Pointer) uintptr {
		t := comString(e.wv, iWV2Title)
		if t == "" {
			t = "Steam"
		}
		pSetWindowTextW.Call(e.hwnd, utf16(t+" - PZ Launcher"))
		return 0
	})
	c.NavigationCompletedCallback = func(*edge.ICoreWebView2, *edge.ICoreWebView2NavigationCompletedEventArgs) { e.refresh() }

	h.Start(e)
	h.Emit(Event{Event: EvReady})
	go h.ReadLoop(o.In, e.Post)
	var m winMsg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if d, _, _ := pIsDialogMessageW.Call(e.hwnd, uintptr(unsafe.Pointer(&m))); d != 0 {
			continue
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}
