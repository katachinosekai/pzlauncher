package spotify

import "strings"

type Status struct {
	Running bool
	Playing bool
	Title   string
	Artist  string
}

func (s Status) Label() string {
	switch {
	case s.Title != "" && s.Artist != "":
		return s.Artist + " - " + s.Title
	case s.Title != "":
		return s.Title
	}
	return ""
}

func ParseWindowTitle(t string) (Status, bool) {
	t = strings.TrimSpace(t)
	switch t {
	case "":
		return Status{}, false
	case "Spotify", "Spotify Free", "Spotify Premium":
		return Status{Running: true}, true
	}
	artist, title, ok := strings.Cut(t, " - ")
	if !ok {
		return Status{Running: true, Playing: true, Title: t}, true
	}
	return Status{Running: true, Playing: true, Artist: strings.TrimSpace(artist), Title: strings.TrimSpace(title)}, true
}
