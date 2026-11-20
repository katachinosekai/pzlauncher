package main

import (
	"encoding/json"
	"os"
	"path/filepath"

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
