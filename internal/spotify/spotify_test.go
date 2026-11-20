package spotify

import "testing"

func TestParseWindowTitle(t *testing.T) {
	for in, want := range map[string]Status{
		"Spotify Premium":                    {Running: true},
		"Spotify":                            {Running: true},
		"Daft Punk - Harder, Better, Faster": {Running: true, Playing: true, Artist: "Daft Punk", Title: "Harder, Better, Faster"},
		"A - B - C":                          {Running: true, Playing: true, Artist: "A", Title: "B - C"},
		"Podcast episode":                    {Running: true, Playing: true, Title: "Podcast episode"},
	} {
		got, ok := ParseWindowTitle(in)
		if !ok || got != want {
			t.Errorf("%q: got %+v", in, got)
		}
	}
	if _, ok := ParseWindowTitle(""); ok {
		t.Error("empty title accepted")
	}
	if (Status{Artist: "A", Title: "B"}).Label() != "A - B" {
		t.Error("label")
	}
}
