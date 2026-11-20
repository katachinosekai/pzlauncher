package main

import "testing"

func TestApplyKVKeepsEverythingButEditedValues(t *testing.T) {
	in := "; comment\r\n[General]\r\nvolume = 5\r\nname=Kata\r\n\r\n[Keys]\r\njump=Space\r\nraw line without equals\r\n"
	lines, n := parseKV(in)
	if n != 3 {
		t.Fatalf("keys = %d", n)
	}
	unchanged := applyKV(lines, func(*kvLine) (string, bool) { return "", false })
	if unchanged != in {
		t.Fatalf("round trip changed the file:\n%q\n%q", in, unchanged)
	}
	out := applyKV(lines, func(l *kvLine) (string, bool) {
		if l.section == "General" && l.key == "volume" {
			return "7", true
		}
		return "", false
	})
	want := "; comment\r\n[General]\r\nvolume = 7\r\nname=Kata\r\n\r\n[Keys]\r\njump=Space\r\nraw line without equals\r\n"
	if out != want {
		t.Fatalf("got %q\nwant %q", out, want)
	}
}
