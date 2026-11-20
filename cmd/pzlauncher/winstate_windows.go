//go:build windows

package main

import (
	"unsafe"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"golang.org/x/sys/windows"
)

var (
	wsUser32            = windows.NewLazySystemDLL("user32.dll")
	pGetWindowPlacement = wsUser32.NewProc("GetWindowPlacement")
	pSetWindowPlacement = wsUser32.NewProc("SetWindowPlacement")
	pMonitorFromRect    = wsUser32.NewProc("MonitorFromRect")
	pMonitorFromWindow  = wsUser32.NewProc("MonitorFromWindow")
	pGetMonitorInfoW    = wsUser32.NewProc("GetMonitorInfoW")
	pGetWindowRect      = wsUser32.NewProc("GetWindowRect")
	pSetWindowPos       = wsUser32.NewProc("SetWindowPos")
)

type windowPlacement struct {
	Length, Flags, ShowCmd uint32
	MinPos, MaxPos         [2]int32
	Normal                 winRect32
	Device                 winRect32
}

const (
	swShowNormal    = 1
	swShowMaximized = 3
	swShowMinimized = 2
)

func withHWND(w fyne.Window, fn func(h uintptr)) {
	nw, ok := w.(driver.NativeWindow)
	if !ok {
		return
	}
	nw.RunNative(func(ctx any) {
		if c, ok := ctx.(driver.WindowsWindowContext); ok && c.HWND != 0 {
			fn(c.HWND)
		}
	})
}

func nativeSave(w fyne.Window, s *winState) {
	withHWND(w, func(h uintptr) {
		var p windowPlacement
		p.Length = uint32(unsafe.Sizeof(p))
		if r, _, _ := pGetWindowPlacement.Call(h, uintptr(unsafe.Pointer(&p))); r == 0 {
			return
		}
		n := p.Normal
		s.Native = &n
		s.Maximized = p.ShowCmd == swShowMaximized
	})
}

type monitorInfo struct {
	Size    uint32
	Monitor winRect32
	Work    winRect32
	Flags   uint32
}

func fitToWorkArea(h uintptr) {
	m, _, _ := pMonitorFromWindow.Call(h, 2)
	if m == 0 {
		return
	}
	mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
	if r, _, _ := pGetMonitorInfoW.Call(m, uintptr(unsafe.Pointer(&mi))); r == 0 {
		return
	}
	var wr winRect32
	if r, _, _ := pGetWindowRect.Call(h, uintptr(unsafe.Pointer(&wr))); r == 0 {
		return
	}
	work := mi.Work
	ww, wh := wr.Right-wr.Left, wr.Bottom-wr.Top
	aw, ah := work.Right-work.Left, work.Bottom-work.Top
	if ww <= aw && wh <= ah && wr.Left >= work.Left && wr.Top >= work.Top && wr.Right <= work.Right && wr.Bottom <= work.Bottom {
		return
	}
	nw, nh := min(ww, aw), min(wh, ah)
	x := work.Left + (aw-nw)/2
	y := work.Top + (ah-nh)/2
	const swpNoZOrder, swpNoActivate = 0x0004, 0x0010
	pSetWindowPos.Call(h, 0, uintptr(x), uintptr(y), uintptr(nw), uintptr(nh), swpNoZOrder|swpNoActivate)
}

func (u *ui) nativeRestore() {
	s := u.winSaved
	defer withHWND(u.win, fitToWorkArea)
	if s.Native == nil || s.Native.Right <= s.Native.Left || s.Native.Bottom <= s.Native.Top {
		return
	}
	if m, _, _ := pMonitorFromRect.Call(uintptr(unsafe.Pointer(s.Native)), 0); m == 0 {
		return
	}
	withHWND(u.win, func(h uintptr) {
		var p windowPlacement
		p.Length = uint32(unsafe.Sizeof(p))
		pGetWindowPlacement.Call(h, uintptr(unsafe.Pointer(&p)))
		p.Normal = *s.Native
		p.ShowCmd = swShowNormal
		if s.Maximized {
			p.ShowCmd = swShowMaximized
		}
		pSetWindowPlacement.Call(h, uintptr(unsafe.Pointer(&p)))
	})
}
