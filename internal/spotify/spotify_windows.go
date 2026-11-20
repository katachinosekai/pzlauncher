//go:build windows

package spotify

import (
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	pEnumWindows     = user32.NewProc("EnumWindows")
	pGetWindowTextW  = user32.NewProc("GetWindowTextW")
	pGetWindowThread = user32.NewProc("GetWindowThreadProcessId")
	pIsWindowVisible = user32.NewProc("IsWindowVisible")
	pKeybdEvent      = user32.NewProc("keybd_event")
)

const (
	vkNext      = 0xB0
	vkPrev      = 0xB1
	vkPlayPause = 0xB3
	keyUp       = 0x0002
)

func spotifyPIDs() map[uint32]bool {
	pids := map[uint32]bool{}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return pids
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), "Spotify.exe") {
			pids[e.ProcessID] = true
		}
	}
	return pids
}

func Get() Status {
	pids := spotifyPIDs()
	if len(pids) == 0 {
		return Status{}
	}
	best := Status{Running: true}
	cb := syscall.NewCallback(func(h uintptr, _ uintptr) uintptr {
		var pid uint32
		pGetWindowThread.Call(h, uintptr(unsafe.Pointer(&pid)))
		if !pids[pid] {
			return 1
		}
		if v, _, _ := pIsWindowVisible.Call(h); v == 0 {
			return 1
		}
		buf := make([]uint16, 512)
		n, _, _ := pGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if n == 0 {
			return 1
		}
		if s, ok := ParseWindowTitle(windows.UTF16ToString(buf[:n])); ok && (s.Title != "" || !best.Playing) {
			best = s
		}
		return 1
	})
	pEnumWindows.Call(cb, 0)
	return best
}

func key(vk uintptr) error {
	pKeybdEvent.Call(vk, 0, 0, 0)
	pKeybdEvent.Call(vk, 0, keyUp, 0)
	return nil
}

func PlayPause() error { return key(vkPlayPause) }
func Next() error      { return key(vkNext) }
func Prev() error      { return key(vkPrev) }
