//go:build !windows

package webview

import "os/exec"

func allowForeground(cmd *exec.Cmd) {}
