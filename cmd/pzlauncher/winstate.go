package main

import (
	"encoding/json"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"fyne.io/fyne/v2"

	"github.com/katachinosekai/pzlauncher/internal/update"
)

type winState struct {
	Width     float32    `json:"width"`
	Height    float32    `json:"height"`
	Native    *winRect32 `json:"native,omitempty"`
	Maximized bool       `json:"maximized,omitempty"`
}

type winRect32 struct{ Left, Top, Right, Bottom int32 }

const minWinW, minWinH = 600, 500

func winStatePath() string {
	d, err := update.ConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "window.json")
}

func loadWinState() (winState, bool) {
	var s winState
	p := winStatePath()
	if p == "" {
		return s, false
	}
	b, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(b, &s) != nil || s.Width < minWinW || s.Height < minWinH {
		return s, false
	}
	return s, true
}

func saveWinState(s winState) {
	p := winStatePath()
	if p == "" || s.Width < minWinW || s.Height < minWinH {
		return
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(p), 0o755)
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, p)
	}
}

func (u *ui) restoreWindow(def fyne.Size) {
	s, ok := loadWinState()
	if !ok {
		u.win.Resize(def)
		return
	}
	u.win.Resize(fyne.NewSize(s.Width, s.Height))
	u.winSaved = s
}

func (u *ui) rememberWindow() {
	s := winState{}
	sz := u.win.Canvas().Size()
	s.Width, s.Height = sz.Width, sz.Height
	nativeSave(u.win, &s)
	saveWinState(s)
}

func (u *ui) watchWindow() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		done := make(chan struct{})
		fyne.Do(func() {
			u.rememberWindow()
			close(done)
		})
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		os.Exit(0)
	}()
	t := time.NewTicker(5 * time.Second)
	u.closers = append(u.closers, t.Stop)
	var last winState
	go func() {
		for range t.C {
			fyne.Do(func() {
				s := winState{}
				sz := u.win.Canvas().Size()
				s.Width, s.Height = sz.Width, sz.Height
				nativeSave(u.win, &s)
				if s.Width == last.Width && s.Height == last.Height && s.Maximized == last.Maximized && sameRect(s.Native, last.Native) {
					return
				}
				last = s
				saveWinState(s)
			})
		}
	}()
}

func sameRect(a, b *winRect32) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
