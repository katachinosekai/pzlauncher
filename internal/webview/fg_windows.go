//go:build windows

package webview

import (
	"os/exec"

	"golang.org/x/sys/windows"
)

var procAllowSetForegroundWindow = windows.NewLazySystemDLL("user32.dll").NewProc("AllowSetForegroundWindow")

func allowForeground(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil || procAllowSetForegroundWindow.Find() != nil {
		return
	}
	_, _, _ = procAllowSetForegroundWindow.Call(uintptr(cmd.Process.Pid))
}
