package play

import (
	"path/filepath"
	"testing"
)

func TestNicknames(t *testing.T) {
	p := filepath.Join(t.TempDir(), "PZLauncher", "nicknames.json")
	n, err := LoadNicknames(p)
	if err != nil {
		t.Fatal(err)
	}
	s := Save{Mode: "Sandbox", World: "2026-11-17_18-54-25"}
	if got := n.Display(s); got != "Sandbox / 2026-11-17_18-54-25" {
		t.Fatalf("display without nickname: %q", got)
	}
	if err := n.Set(s, "  Riverside run  "); err != nil {
		t.Fatal(err)
	}
	again, err := LoadNicknames(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Get(s); got != "Riverside run" {
		t.Fatalf("reloaded nickname: %q", got)
	}
	if got := again.Display(s); got != "Riverside run  (Sandbox / 2026-11-17_18-54-25)" {
		t.Fatalf("display with nickname: %q", got)
	}
	if err := again.Set(s, " "); err != nil {
		t.Fatal(err)
	}
	cleared, _ := LoadNicknames(p)
	if cleared.Get(s) != "" {
		t.Fatal("blank nickname should clear it")
	}
}
