package main

import "testing"

func TestMarquee(t *testing.T) {
	if marquee("Short", 5, 30) != "Short" {
		t.Fatal("short titles must not scroll")
	}
	long := "Saferoom Theme (Resident Evil 7 OST) - extended"
	a, b := marquee(long, 0, 20), marquee(long, 1, 20)
	if len([]rune(a)) != 20 || a == b || []rune(a)[1] != []rune(b)[0] {
		t.Fatalf("marquee step wrong: %q -> %q", a, b)
	}
	if marquee(long, len([]rune(long))+7, 20) != a {
		t.Fatal("marquee should loop")
	}
}
