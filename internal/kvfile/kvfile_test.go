package kvfile

import (
	"strings"
	"testing"
)

func TestSimple(t *testing.T) {
	for _, c := range []struct {
		in string
		ok bool
	}{
		{"a=1\nb = 2\n", true},
		{"# comment\n[Main]\nspeed=3\r\n; note\n", true},
		{"", false},
		{"just text\n", false},
		{"Options = {\n  a = 1,\n}\n", false},
		{"a=1\nfree text line\n", false},
		{`{"a": 1}`, false},
	} {
		if Simple(c.in) != c.ok {
			t.Errorf("%q: want %v", c.in, c.ok)
		}
	}
}

func TestMerge(t *testing.T) {
	dst := "# my settings\r\nvolume = 5\r\nkeep=mine\r\n[Graphics]\r\nshadows=off\r\n\r\n"
	src := "volume=8\nnewtop=1\n[Graphics]\nshadows=on\nbloom=low\n[Audio]\nmusic=0\n"
	out, ch := Merge(dst, src)
	want := "# my settings\r\nvolume = 8\r\nkeep=mine\r\nnewtop=1\r\n[Graphics]\r\nshadows=on\r\nbloom=low\r\n[Audio]\r\nmusic=0\r\n\r\n"
	if out != want {
		t.Fatalf("got %q\nwant %q", out, want)
	}
	var names []string
	for _, c := range ch {
		s := c.Name() + ":" + c.Old + ">" + c.New
		if c.Added {
			s += "+"
		}
		names = append(names, s)
	}
	if strings.Join(names, ",") != "volume:5>8,[Graphics] shadows:off>on,newtop:>1+,[Graphics] bloom:>low+,[Audio] music:>0+" {
		t.Fatal(names)
	}
	again, ch2 := Merge(out, src)
	if again != out || len(ch2) != 0 {
		t.Fatalf("second merge changed things: %v", ch2)
	}
}

func TestMergeTopLevelBeforeFirstSection(t *testing.T) {
	out, _ := Merge("; head\n[S]\nx=1\n", "top=2\n")
	if out != "; head\ntop=2\n[S]\nx=1\n" {
		t.Fatalf("%q", out)
	}
}

func TestApplyKeepsFormatting(t *testing.T) {
	lines, n := Parse("a =  1\r\nb=2\r\n")
	if n != 2 {
		t.Fatal(n)
	}
	got := Apply(lines, func(l *Line) (string, bool) { return "9", l.Key == "a" })
	if got != "a =  9\r\nb=2\r\n" {
		t.Fatalf("%q", got)
	}
}

func TestApplyKeepsEverythingButEditedValues(t *testing.T) {
	in := "; comment\r\n[General]\r\nvolume = 5\r\nname=Someone\r\n\r\n[Keys]\r\njump=Space\r\nraw line without equals\r\n"
	lines, n := Parse(in)
	if n != 3 {
		t.Fatalf("keys = %d", n)
	}
	if Apply(lines, func(*Line) (string, bool) { return "", false }) != in {
		t.Fatal("round trip changed the file")
	}
	out := Apply(lines, func(l *Line) (string, bool) { return "7", l.Section == "General" && l.Key == "volume" })
	if out != strings.Replace(in, "volume = 5", "volume = 7", 1) {
		t.Fatalf("got %q", out)
	}
}
