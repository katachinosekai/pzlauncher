//go:build !linux && !windows

package webview

func runEngine(h *Helper, o Options) error { return ErrUnavailable }
