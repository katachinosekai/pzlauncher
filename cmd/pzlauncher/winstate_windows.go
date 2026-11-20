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

func (u *ui) nativeRestore() {
	s := u.winSaved
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
