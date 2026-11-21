package main

import (
	"context"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func tapButton(t *testing.T, w fyne.Window, text string) {
	t.Helper()
	top := w.Canvas().Overlays().Top()
	if top == nil {
		t.Fatal("no dialog shown")
	}
	for _, o := range test.LaidOutObjects(top) {
		if b, ok := o.(*widget.Button); ok && b.Text == text {
			test.Tap(b)
			return
		}
	}
	t.Fatalf("no %q button", text)
}

func overlayText(w fyne.Window) string {
	var b strings.Builder
	for _, o := range test.LaidOutObjects(w.Canvas().Overlays().Top()) {
		if l, ok := o.(*widget.Label); ok {
			b.WriteString(l.Text + "\n")
		}
	}
	return b.String()
}

func TestConfirmPackChanges(t *testing.T) {
	test.NewTempApp(t)
	w := test.NewWindow(widget.NewLabel(""))
	w.Resize(fyne.NewSize(900, 700))
	u := &ui{win: w, ctx: context.Background()}

	got := -1
	u.confirmPackChanges(nil, func(ok bool) { got = map[bool]int{false: 0, true: 1}[ok] })
	if got != 1 || w.Canvas().Overlays().Top() != nil {
		t.Fatal("no changes should go ahead without a dialog")
	}

	changes := []string{"Lua/ModOptions.ini: ModA enabled: true -> false", "Lua/SampleMod.ini: [Main] fresh: (new) -> on"}
	got = -1
	u.confirmPackChanges(changes, func(ok bool) { got = map[bool]int{false: 0, true: 1}[ok] })
	txt := overlayText(w)
	if !strings.Contains(txt, "2 change(s)") || !strings.Contains(txt, "ModA enabled: true -> false") {
		t.Fatalf("dialog text:\n%s", txt)
	}
	tapButton(t, w, "Cancel")
	if got != 0 {
		t.Fatal("Cancel did not cancel")
	}
	u.confirmPackChanges(changes, func(ok bool) { got = map[bool]int{false: 0, true: 1}[ok] })
	tapButton(t, w, "Install")
	if got != 1 {
		t.Fatal("Install did not confirm")
	}
}
