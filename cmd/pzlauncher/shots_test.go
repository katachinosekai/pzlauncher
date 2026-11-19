package main

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/kata/pzlauncher/internal/update"
)

func TestScreenshots(t *testing.T) {
	dir := os.Getenv("PZL_SHOTS")
	if dir == "" {
		t.Skip("set PZL_SHOTS=<folder> to render every tab to PNG")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	a := test.NewTempApp(t)
	a.Settings().SetTheme(theme.DarkTheme())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := test.NewWindow(nil)
	defer w.Close()
	u := &ui{app: a, win: w, ctx: ctx, client: update.NewClient()}
	tabs := u.buildTabs()
	w.SetContent(container.NewBorder(nil, u.aboutBar(), nil, nil, tabs))
	w.Resize(fyne.NewSize(900, 820))
	wait := 4 * time.Second
	if v := os.Getenv("PZL_SHOTS_WAIT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			wait = d
		}
	}
	for i, item := range tabs.Items {
		tabs.SelectIndex(i)
		time.Sleep(wait)
		img := w.Canvas().Capture()
		name := strings.NewReplacer(" ", "", "/", "-").Replace(item.Text)
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}
