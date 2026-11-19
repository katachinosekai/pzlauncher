//go:build linux

package webview

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/ebitengine/purego"
)

type gtkAPI struct {
	gIdleAdd           func(fn, data uintptr) uint32
	gSignalConnectData func(inst uintptr, sig string, cb, data, destroy uintptr, flags uint32) uint64

	gtkInitCheck          func(argc, argv uintptr) int32
	gtkMain               func()
	gtkMainQuit           func()
	gtkWindowNew          func(kind int32) uintptr
	gtkWindowSetTitle     func(w uintptr, title string)
	gtkWindowSetDefSize   func(w uintptr, width, height int32)
	gtkWindowPresent      func(w uintptr)
	gtkBoxNew             func(orient, spacing int32) uintptr
	gtkBoxPackStart       func(box, child uintptr, expand, fill int32, pad uint32)
	gtkButtonNewFromIcon  func(name string, size int32) uintptr
	gtkWidgetSetTooltip   func(w uintptr, text string)
	gtkWidgetSetSensitive func(w uintptr, on int32)
	gtkWidgetShowAll      func(w uintptr)
	gtkWidgetDestroy      func(w uintptr)
	gtkContainerAdd       func(c, w uintptr)
	gtkEntryNew           func() uintptr
	gtkEntrySetText       func(e uintptr, s string)
	gtkEntryGetText       func(e uintptr) string
	gtkEntrySetIcon       func(e uintptr, pos int32, name string)
	gtkEntrySetIconTip    func(e uintptr, pos int32, tip string)
	gtkWidgetHide         func(w uintptr)

	wkDataManagerNew     func(k1, v1, k2, v2 string, end uintptr) uintptr
	wkDataManagerNewEph  func() uintptr
	wkContextNewWithDM   func(dm uintptr) uintptr
	wkDMCookieManager    func(dm uintptr) uintptr
	wkCookieSetStorage   func(cm uintptr, path string, kind int32)
	wkCookieSetPolicy    func(cm uintptr, policy int32)
	wkViewNewWithContext func(ctx uintptr) uintptr
	wkViewLoadURI        func(v uintptr, uri string)
	wkViewGetURI         func(v uintptr) string
	wkViewGetTitle       func(v uintptr) string
	wkViewGoBack         func(v uintptr)
	wkViewGoForward      func(v uintptr)
	wkViewReload         func(v uintptr)
	wkViewCanGoBack      func(v uintptr) int32
	wkViewCanGoForward   func(v uintptr) int32
	wkViewGetTLSInfo     func(v uintptr, cert *uintptr, errs *uint32) int32
	wkViewGetUCM         func(v uintptr) uintptr
	wkUserScriptNew      func(src string, frames, at int32, allow, block uintptr) uintptr
	wkUCMAddScript       func(ucm, script uintptr)
	wkUCMRegisterHandler func(ucm uintptr, name string) int32
	wkJSResultGetValue   func(r uintptr) uintptr
	jscValueToString     func(v uintptr) string
	wkNavDecisionAction  func(d uintptr) uintptr
	wkNavActionRequest   func(a uintptr) uintptr
	wkNavActionGesture   func(a uintptr) int32
	wkURIRequestGetURI   func(r uintptr) string
	wkPolicyIgnore       func(d uintptr)
	wkEvalJS             func(v uintptr, js string, length int, world, source, cancel, cb, data uintptr)
	wkRunJS              func(v uintptr, js string, cancel, cb, data uintptr)
	wkDMClear            func(dm uintptr, types uint32, span int64, cancel, cb, data uintptr)
}

func openLib(names ...string) (uintptr, error) {
	var errs []string
	for _, n := range names {
		h, err := purego.Dlopen(n, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err == nil {
			return h, nil
		}
		errs = append(errs, err.Error())
	}
	return 0, errors.New(strings.Join(errs, "; "))
}

func loadGTK() (api *gtkAPI, err error) {
	defer func() {
		if r := recover(); r != nil {
			api, err = nil, fmt.Errorf("WebKitGTK symbol missing: %v", r)
		}
	}()
	glib, err := openLib("libglib-2.0.so.0")
	if err != nil {
		return nil, err
	}
	gobj, err := openLib("libgobject-2.0.so.0")
	if err != nil {
		return nil, err
	}
	gtk, err := openLib("libgtk-3.so.0")
	if err != nil {
		return nil, err
	}
	wk, err := openLib("libwebkit2gtk-4.1.so.0", "libwebkit2gtk-4.0.so.37")
	if err != nil {
		return nil, err
	}
	jsc, err := openLib("libjavascriptcoregtk-4.1.so.0", "libjavascriptcoregtk-4.0.so.18")
	if err != nil {
		return nil, err
	}
	a := &gtkAPI{}
	reg := purego.RegisterLibFunc
	reg(&a.gIdleAdd, glib, "g_idle_add")
	reg(&a.gSignalConnectData, gobj, "g_signal_connect_data")
	reg(&a.gtkInitCheck, gtk, "gtk_init_check")
	reg(&a.gtkMain, gtk, "gtk_main")
	reg(&a.gtkMainQuit, gtk, "gtk_main_quit")
	reg(&a.gtkWindowNew, gtk, "gtk_window_new")
	reg(&a.gtkWindowSetTitle, gtk, "gtk_window_set_title")
	reg(&a.gtkWindowSetDefSize, gtk, "gtk_window_set_default_size")
	reg(&a.gtkWindowPresent, gtk, "gtk_window_present")
	reg(&a.gtkBoxNew, gtk, "gtk_box_new")
	reg(&a.gtkBoxPackStart, gtk, "gtk_box_pack_start")
	reg(&a.gtkButtonNewFromIcon, gtk, "gtk_button_new_from_icon_name")
	reg(&a.gtkWidgetSetTooltip, gtk, "gtk_widget_set_tooltip_text")
	reg(&a.gtkWidgetSetSensitive, gtk, "gtk_widget_set_sensitive")
	reg(&a.gtkWidgetShowAll, gtk, "gtk_widget_show_all")
	reg(&a.gtkWidgetDestroy, gtk, "gtk_widget_destroy")
	reg(&a.gtkContainerAdd, gtk, "gtk_container_add")
	reg(&a.gtkEntryNew, gtk, "gtk_entry_new")
	reg(&a.gtkEntrySetText, gtk, "gtk_entry_set_text")
	reg(&a.gtkEntryGetText, gtk, "gtk_entry_get_text")
	reg(&a.gtkEntrySetIcon, gtk, "gtk_entry_set_icon_from_icon_name")
	reg(&a.gtkEntrySetIconTip, gtk, "gtk_entry_set_icon_tooltip_text")
	reg(&a.gtkWidgetHide, gtk, "gtk_widget_hide")
	reg(&a.wkDMClear, wk, "webkit_website_data_manager_clear")
	reg(&a.wkDataManagerNewEph, wk, "webkit_website_data_manager_new_ephemeral")
	if _, err := purego.Dlsym(wk, "webkit_web_view_evaluate_javascript"); err == nil {
		reg(&a.wkEvalJS, wk, "webkit_web_view_evaluate_javascript")
	} else {
		reg(&a.wkRunJS, wk, "webkit_web_view_run_javascript")
	}
	reg(&a.wkDataManagerNew, wk, "webkit_website_data_manager_new")
	reg(&a.wkContextNewWithDM, wk, "webkit_web_context_new_with_website_data_manager")
	reg(&a.wkDMCookieManager, wk, "webkit_website_data_manager_get_cookie_manager")
	reg(&a.wkCookieSetStorage, wk, "webkit_cookie_manager_set_persistent_storage")
	reg(&a.wkCookieSetPolicy, wk, "webkit_cookie_manager_set_accept_policy")
	reg(&a.wkViewNewWithContext, wk, "webkit_web_view_new_with_context")
	reg(&a.wkViewLoadURI, wk, "webkit_web_view_load_uri")
	reg(&a.wkViewGetURI, wk, "webkit_web_view_get_uri")
	reg(&a.wkViewGetTitle, wk, "webkit_web_view_get_title")
	reg(&a.wkViewGoBack, wk, "webkit_web_view_go_back")
	reg(&a.wkViewGoForward, wk, "webkit_web_view_go_forward")
	reg(&a.wkViewReload, wk, "webkit_web_view_reload")
	reg(&a.wkViewCanGoBack, wk, "webkit_web_view_can_go_back")
	reg(&a.wkViewCanGoForward, wk, "webkit_web_view_can_go_forward")
	reg(&a.wkViewGetTLSInfo, wk, "webkit_web_view_get_tls_info")
	reg(&a.wkViewGetUCM, wk, "webkit_web_view_get_user_content_manager")
	reg(&a.wkUserScriptNew, wk, "webkit_user_script_new")
	reg(&a.wkUCMAddScript, wk, "webkit_user_content_manager_add_script")
	reg(&a.wkUCMRegisterHandler, wk, "webkit_user_content_manager_register_script_message_handler")
	reg(&a.wkJSResultGetValue, wk, "webkit_javascript_result_get_js_value")
	reg(&a.jscValueToString, jsc, "jsc_value_to_string")
	reg(&a.wkNavDecisionAction, wk, "webkit_navigation_policy_decision_get_navigation_action")
	reg(&a.wkNavActionRequest, wk, "webkit_navigation_action_get_request")
	reg(&a.wkNavActionGesture, wk, "webkit_navigation_action_is_user_gesture")
	reg(&a.wkURIRequestGetURI, wk, "webkit_uri_request_get_uri")
	reg(&a.wkPolicyIgnore, wk, "webkit_policy_decision_ignore")
	return a, nil
}

type gtkEngine struct {
	api                         *gtkAPI
	h                           *Helper
	win, view, entry, back, fwd uintptr
	dm                          uintptr
	mu                          sync.Mutex
	queue                       []func()
	idleCB, clearCB             uintptr
	clearDone                   func()
}

var linuxEngine *gtkEngine

const wkDataCookiesAndStorage = 1<<8 | 1<<3 | 1<<4 | 1<<6 | 1<<0 | 1<<1

func (e *gtkEngine) Load(url string) { e.api.wkViewLoadURI(e.view, url) }
func (e *gtkEngine) Quit()           { e.api.gtkMainQuit() }
func (e *gtkEngine) Hide()           { e.api.gtkWidgetHide(e.win) }

func (e *gtkEngine) Show() {
	e.api.gtkWidgetShowAll(e.win)
	e.api.gtkWindowPresent(e.win)
}

func (e *gtkEngine) Eval(js string) {
	if e.api.wkEvalJS != nil {
		e.api.wkEvalJS(e.view, js, -1, 0, 0, 0, 0, 0)
		return
	}
	e.api.wkRunJS(e.view, js, 0, 0, 0)
}

func (e *gtkEngine) ClearData(done func()) {
	e.clearDone = done
	e.api.wkDMClear(e.dm, wkDataCookiesAndStorage, 0, 0, e.clearCB, 0)
}

func (e *gtkEngine) Post(fn func()) {
	e.mu.Lock()
	e.queue = append(e.queue, fn)
	e.mu.Unlock()
	e.api.gIdleAdd(e.idleCB, 0)
}

func (e *gtkEngine) refresh() {
	a := e.api
	uri := a.wkViewGetURI(e.view)
	a.gtkEntrySetText(e.entry, uri)
	a.gtkWidgetSetSensitive(e.back, a.wkViewCanGoBack(e.view))
	a.gtkWidgetSetSensitive(e.fwd, a.wkViewCanGoForward(e.view))
	icon, tip := "channel-insecure-symbolic", "Not a secure connection"
	if strings.HasPrefix(uri, "https://") {
		var cert uintptr
		var errs uint32
		if a.wkViewGetTLSInfo(e.view, &cert, &errs) != 0 && errs == 0 {
			icon, tip = "channel-secure-symbolic", "Secure connection (https)"
		}
	}
	a.gtkEntrySetIcon(e.entry, 0, icon)
	a.gtkEntrySetIconTip(e.entry, 0, tip)
}

func runEngine(h *Helper, o Options) error {
	runtime.LockOSThread()
	a, err := loadGTK()
	if err != nil {
		return err
	}
	if a.gtkInitCheck(0, 0) == 0 {
		return errors.New("GTK could not open the display")
	}
	e := &gtkEngine{api: a, h: h}
	linuxEngine = e

	e.idleCB = purego.NewCallback(func(data uintptr) uintptr {
		e.mu.Lock()
		q := e.queue
		e.queue = nil
		e.mu.Unlock()
		for _, fn := range q {
			fn()
		}
		return 0
	})
	e.clearCB = purego.NewCallback(func(src, res, data uintptr) {
		if d := e.clearDone; d != nil {
			e.clearDone = nil
			d()
		}
	})
	deleteEv := purego.NewCallback(func(w, ev, data uintptr) uintptr {
		h.WindowClosed()
		return 1
	})
	clicked := purego.NewCallback(func(w, data uintptr) {
		switch data {
		case 1:
			a.wkViewGoBack(e.view)
		case 2:
			a.wkViewGoForward(e.view)
		case 3:
			a.wkViewReload(e.view)
		}
	})
	activate := purego.NewCallback(func(w, data uintptr) {
		u := NormalizeTyped(a.gtkEntryGetText(e.entry))
		if u == "" {
			return
		}
		if h.Decide(u, true) == Allow {
			a.wkViewLoadURI(e.view, u)
		} else {
			e.refresh()
		}
	})
	destroy := purego.NewCallback(func(w, data uintptr) { a.gtkMainQuit() })
	decide := purego.NewCallback(func(view, decision, kind, data uintptr) uintptr {
		kind &= 0xffffffff
		if kind != 0 && kind != 1 {
			return 0
		}
		action := a.wkNavDecisionAction(decision)
		if action == 0 {
			return 0
		}
		uri := a.wkURIRequestGetURI(a.wkNavActionRequest(action))
		gesture := a.wkNavActionGesture(action) != 0
		act := h.Decide(uri, gesture || kind == 1)
		if act == Allow && kind == 0 {
			return 0
		}
		a.wkPolicyIgnore(decision)
		if act == Allow {
			a.wkViewLoadURI(e.view, uri)
		}
		return 1
	})
	notify := purego.NewCallback(func(obj, pspec, data uintptr) {
		if data == 2 {
			t := a.wkViewGetTitle(e.view)
			if t == "" {
				t = "Steam"
			}
			a.gtkWindowSetTitle(e.win, t+" - PZ Launcher")
			return
		}
		e.refresh()
	})
	loadChanged := purego.NewCallback(func(view, ev, data uintptr) { e.refresh() })
	scriptMsg := purego.NewCallback(func(ucm, result, data uintptr) {
		if v := a.wkJSResultGetValue(result); v != 0 {
			h.ScriptMessage(a.jscValueToString(v))
		}
	})

	var dm uintptr
	if o.Ephemeral {
		dm = a.wkDataManagerNewEph()
	} else {
		dm = a.wkDataManagerNew("base-data-directory", o.DataDir, "base-cache-directory", o.CacheDir, 0)
	}
	if dm == 0 {
		return errors.New("WebKit data manager could not be created")
	}
	e.dm = dm
	cm := a.wkDMCookieManager(dm)
	if !o.Ephemeral {
		a.wkCookieSetStorage(cm, filepath.Join(o.DataDir, "cookies.sqlite"), 1)
	}
	a.wkCookieSetPolicy(cm, 0)
	ctx := a.wkContextNewWithDM(dm)
	e.view = a.wkViewNewWithContext(ctx)
	if e.view == 0 {
		return errors.New("WebKit view could not be created")
	}
	ucm := a.wkViewGetUCM(e.view)
	if a.wkUCMRegisterHandler(ucm, "pzl") != 0 {
		a.gSignalConnectData(ucm, "script-message-received::pzl", scriptMsg, 0, 0, 0)
		h.PostJS = "window.webkit.messageHandlers.pzl.postMessage"
		a.wkUCMAddScript(ucm, a.wkUserScriptNew(PageScript(h.PostJS), 1, 0, 0, 0))
	}

	e.win = a.gtkWindowNew(0)
	a.gtkWindowSetTitle(e.win, "Sign in to Steam - PZ Launcher")
	a.gtkWindowSetDefSize(e.win, 760, 820)
	vbox := a.gtkBoxNew(1, 0)
	bar := a.gtkBoxNew(0, 4)
	e.back = a.gtkButtonNewFromIcon("go-previous", 2)
	e.fwd = a.gtkButtonNewFromIcon("go-next", 2)
	reload := a.gtkButtonNewFromIcon("view-refresh", 2)
	a.gtkWidgetSetTooltip(e.back, "Back")
	a.gtkWidgetSetTooltip(e.fwd, "Forward")
	a.gtkWidgetSetTooltip(reload, "Reload")
	e.entry = a.gtkEntryNew()
	for i, b := range []uintptr{e.back, e.fwd, reload} {
		a.gSignalConnectData(b, "clicked", clicked, uintptr(i+1), 0, 0)
		a.gtkBoxPackStart(bar, b, 0, 0, 0)
	}
	a.gtkBoxPackStart(bar, e.entry, 1, 1, 0)
	a.gtkBoxPackStart(vbox, bar, 0, 0, 2)
	a.gtkBoxPackStart(vbox, e.view, 1, 1, 0)
	a.gtkContainerAdd(e.win, vbox)
	a.gSignalConnectData(e.entry, "activate", activate, 0, 0, 0)
	a.gSignalConnectData(e.win, "destroy", destroy, 0, 0, 0)
	a.gSignalConnectData(e.win, "delete-event", deleteEv, 0, 0, 0)
	a.gSignalConnectData(e.view, "decide-policy", decide, 0, 0, 0)
	a.gSignalConnectData(e.view, "notify::uri", notify, 1, 0, 0)
	a.gSignalConnectData(e.view, "notify::title", notify, 2, 0, 0)
	a.gSignalConnectData(e.view, "load-changed", loadChanged, 0, 0, 0)
	e.refresh()

	h.Start(e)
	h.Emit(Event{Event: EvReady})
	go h.ReadLoop(o.In, e.Post)
	a.gtkMain()
	return nil
}
