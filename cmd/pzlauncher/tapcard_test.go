package main

import (
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestTapCardDoubleTap(t *testing.T) {
	test.NewTempApp(t)
	taps, doubles := 0, 0
	c := newTapCard(widget.NewLabel("save"))
	c.onTap = func() { taps++ }
	c.onDouble = func() { doubles++ }
	test.DoubleTap(c)
	if doubles != 1 || taps != 0 {
		t.Fatalf("double tap: taps=%d doubles=%d, want 0 and 1", taps, doubles)
	}
	c.onDouble = nil
	test.DoubleTap(c)
	if taps != 1 {
		t.Fatalf("double tap without onDouble should act as a tap, taps=%d", taps)
	}
}
