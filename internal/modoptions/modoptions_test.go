package modoptions

import (
	"strings"
	"testing"
)

const mine = "tickbox|ModA|enabled|true\r\nslider|ModA|volume|40\r\nkeybind|ModA|open|24\r\ncombobox|ModB|mode|2\r\nslider|ModC|speed|30|w11\r\n"

func TestParseGroupsExtract(t *testing.T) {
	f := Parse(mine)
	if l, ok := f.Get("ModC", "speed"); !ok || l.Value != "30" || l.Rest != "|w11" || l.String() != "slider|ModC|speed|30|w11" {
		t.Fatalf("%+v", l)
	}
	g := f.Groups()
	if len(g) != 3 || g[0].Mod != "ModA" || g[0].Options != 2 || g[0].KeyBinds != 1 {
		t.Fatalf("%+v", g)
	}
	got := Extract(f, map[string]bool{"ModA": true, "ModC": true}, false)
	if got != "tickbox|ModA|enabled|true\r\nslider|ModA|volume|40\r\nslider|ModC|speed|30|w11\r\n" {
		t.Fatalf("%q", got)
	}
	if !strings.Contains(Extract(f, map[string]bool{"ModA": true}, true), "keybind|ModA|open|24") {
		t.Fatal("keybinds missing when asked for")
	}
}

func TestMerge(t *testing.T) {
	dst := Parse(mine)
	src := Parse("tickbox|ModA|enabled|false\nslider|ModA|volume|40\nslider|ModC|speed|55\ncolorpicker|ModD|tint|1 0 0 1\n")
	out, ch := Merge(dst, src)
	want := "tickbox|ModA|enabled|false\r\nslider|ModA|volume|40\r\nkeybind|ModA|open|24\r\ncombobox|ModB|mode|2\r\nslider|ModC|speed|55|w11\r\ncolorpicker|ModD|tint|1 0 0 1\r\n"
	if out != want {
		t.Fatalf("got %q", out)
	}
	if len(ch) != 3 || ch[0].ID != "enabled" || ch[0].Old != "true" || ch[1].New != "55" || !ch[2].Added {
		t.Fatalf("%+v", ch)
	}
	again, ch2 := Merge(Parse(out), src)
	if again != out || len(ch2) != 0 {
		t.Fatalf("second merge changed things: %+v", ch2)
	}
	if got, _ := Merge(Parse(mine), Parse("")); got != mine {
		t.Fatal("empty pack changed the file")
	}
	if got, _ := Merge(Parse(""), Parse("tickbox|M|x|true\r\n")); got != "tickbox|M|x|true\r\n" {
		t.Fatalf("new file: %q", got)
	}
}
