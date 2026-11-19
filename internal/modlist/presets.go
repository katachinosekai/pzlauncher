package modlist

import (
	"errors"
	"slices"
	"strings"
)

const FavLinePrefix = "!fav!"

type cfgLine struct {
	text string
	term string
}

type Preset struct {
	Name string
	IDs  []string
}

type Presets struct {
	lines   []cfgLine
	nl      string
	Favs    []string
	favLine int
}

func splitTerminated(s string) []cfgLine {
	var out []cfgLine
	for s != "" {
		i := strings.IndexAny(s, "\r\n")
		if i < 0 {
			out = append(out, cfgLine{text: s})
			break
		}
		term := s[i : i+1]
		if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
			term = "\r\n"
		}
		out = append(out, cfgLine{text: s[:i], term: term})
		s = s[i+len(term):]
	}
	return out
}

func splitIDs(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ";") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func ParsePresets(raw string) *Presets {
	p := &Presets{lines: splitTerminated(raw), nl: "\n", favLine: -1}
	if strings.Contains(raw, "\r\n") && !strings.Contains(strings.ReplaceAll(raw, "\r\n", ""), "\n") {
		p.nl = "\r\n"
	}
	if len(p.lines) > 0 && strings.HasPrefix(p.lines[0].text, FavLinePrefix) {
		p.favLine = 0
		if _, after, ok := strings.Cut(p.lines[0].text, ":"); ok {
			p.Favs = splitIDs(after)
		}
	}
	return p
}

func (p *Presets) Render() string {
	var sb strings.Builder
	for _, l := range p.lines {
		sb.WriteString(l.text + l.term)
	}
	return sb.String()
}

func presetName(line string, idx int) (string, string, bool) {
	if idx == 0 && strings.HasPrefix(line, FavLinePrefix) {
		return "", "", false
	}
	name, ids, ok := strings.Cut(line, ":")
	if !ok || name == "" {
		return "", "", false
	}
	return name, ids, true
}

func (p *Presets) List() []Preset {
	byName := map[string]int{}
	var out []Preset
	for i, l := range p.lines {
		name, ids, ok := presetName(l.text, i)
		if !ok {
			continue
		}
		pr := Preset{Name: name, IDs: splitIDs(ids)}
		if j, dup := byName[name]; dup {
			out[j] = pr
			continue
		}
		byName[name] = len(out)
		out = append(out, pr)
	}
	return out
}

func (p *Presets) Get(name string) (Preset, bool) {
	for _, pr := range p.List() {
		if pr.Name == name {
			return pr, true
		}
	}
	return Preset{}, false
}

func ValidPresetName(name string) bool {
	return name != "" && !strings.ContainsAny(name, "/\\:;\".")
}

var ErrInvalidPresetName = errors.New(`invalid preset name (empty, or contains / \ : ; " .)`)

func formatIDs(ids []string) string {
	var sb strings.Builder
	for _, id := range ids {
		sb.WriteString(id + ";")
	}
	return sb.String()
}

func (p *Presets) newLine(text string) cfgLine { return cfgLine{text: text, term: p.nl} }

func (p *Presets) ensureTerminated() {
	if n := len(p.lines); n > 0 && p.lines[n-1].term == "" {
		p.lines[n-1].term = p.nl
	}
}

func (p *Presets) Set(name string, ids []string) error {
	if !ValidPresetName(name) {
		return ErrInvalidPresetName
	}
	line := p.newLine(name + ":" + formatIDs(ids))
	first := -1
	var kept []cfgLine
	for i, l := range p.lines {
		n, _, ok := presetName(l.text, i)
		if ok && n == name {
			if first < 0 {
				first = len(kept)
				line.term = l.term
				if line.term == "" {
					line.term = p.nl
				}
				kept = append(kept, line)
			}
			continue
		}
		kept = append(kept, l)
	}
	if first < 0 {
		p.lines = kept
		if len(p.lines) == 0 {
			p.lines = append(p.lines, p.newLine(FavLinePrefix+":"))
			p.favLine = 0
		}
		p.ensureTerminated()
		p.lines = append(p.lines, line)
		return nil
	}
	p.lines = kept
	return nil
}

func (p *Presets) Delete(name string) bool {
	found := false
	var kept []cfgLine
	for i, l := range p.lines {
		n, _, ok := presetName(l.text, i)
		if ok && n == name {
			found = true
			continue
		}
		kept = append(kept, l)
	}
	p.lines = kept
	return found
}

func (p *Presets) SetFavs(ids []string) {
	p.Favs = slices.Clone(ids)
	line := p.newLine(FavLinePrefix + ":" + formatIDs(ids))
	if p.favLine == 0 {
		line.term = p.lines[0].term
		if line.term == "" {
			line.term = p.nl
		}
		p.lines[0] = line
		return
	}
	p.lines = append([]cfgLine{line}, p.lines...)
	p.favLine = 0
}

func (p *Presets) IsFav(id string) bool { return slices.Contains(p.Favs, id) }
