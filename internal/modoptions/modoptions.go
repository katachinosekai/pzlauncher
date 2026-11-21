package modoptions

import (
	"sort"
	"strings"
)

const FileName = "ModOptions.ini"

type Line struct {
	Type, Mod, ID, Value string
	Rest                 string
}

func (l Line) key() string { return l.Mod + "|" + l.ID }

func parseLine(s string) (Line, bool) {
	p := strings.SplitN(strings.TrimSuffix(s, "\r"), "|", 5)
	if len(p) < 4 || p[0] == "" || p[1] == "" || p[2] == "" {
		return Line{}, false
	}
	l := Line{Type: p[0], Mod: p[1], ID: p[2], Value: p[3]}
	if len(p) == 5 {
		l.Rest = "|" + p[4]
	}
	return l, true
}

func (l Line) String() string { return l.Type + "|" + l.Mod + "|" + l.ID + "|" + l.Value + l.Rest }

type File struct {
	raw  []string
	nl   string
	at   map[string]int
	line map[int]Line
}

func Parse(text string) *File {
	f := &File{nl: "\n", at: map[string]int{}, line: map[int]Line{}}
	if strings.Contains(text, "\r\n") {
		f.nl = "\r\n"
	}
	if text == "" {
		f.nl = "\r\n"
		return f
	}
	f.raw = strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, s := range f.raw {
		if l, ok := parseLine(s); ok {
			if _, dup := f.at[l.key()]; !dup {
				f.at[l.key()] = i
			}
			f.line[i] = l
		}
	}
	return f
}

func (f *File) Lines() []Line {
	idx := make([]int, 0, len(f.line))
	for i := range f.line {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	out := make([]Line, len(idx))
	for j, i := range idx {
		out[j] = f.line[i]
	}
	return out
}

func (f *File) Get(mod, id string) (Line, bool) {
	i, ok := f.at[mod+"|"+id]
	if !ok {
		return Line{}, false
	}
	return f.line[i], true
}

type Group struct {
	Mod      string
	Options  int
	KeyBinds int
}

func (f *File) Groups() []Group {
	var out []Group
	pos := map[string]int{}
	for _, l := range f.Lines() {
		i, ok := pos[l.Mod]
		if !ok {
			i = len(out)
			pos[l.Mod] = i
			out = append(out, Group{Mod: l.Mod})
		}
		if l.Type == "keybind" {
			out[i].KeyBinds++
		} else {
			out[i].Options++
		}
	}
	return out
}

func Extract(f *File, mods map[string]bool, keybinds bool) string {
	var b strings.Builder
	for _, l := range f.Lines() {
		if !mods[l.Mod] || (l.Type == "keybind" && !keybinds) {
			continue
		}
		b.WriteString(l.String())
		b.WriteString("\r\n")
	}
	return b.String()
}

type Change struct {
	Mod, ID, Old, New string
	Added             bool
}

func Merge(dst *File, src *File) (string, []Change) {
	out := append([]string(nil), dst.raw...)
	var changes []Change
	var added []string
	for _, l := range src.Lines() {
		if i, ok := dst.at[l.key()]; ok {
			cur := dst.line[i]
			if cur.Value == l.Value {
				continue
			}
			changes = append(changes, Change{Mod: l.Mod, ID: l.ID, Old: cur.Value, New: l.Value})
			cur.Value = l.Value
			out[i] = cur.String()
			continue
		}
		changes = append(changes, Change{Mod: l.Mod, ID: l.ID, New: l.Value, Added: true})
		added = append(added, Line{Type: l.Type, Mod: l.Mod, ID: l.ID, Value: l.Value}.String())
	}
	end := len(out)
	for end > 0 && out[end-1] == "" {
		end--
	}
	out = append(out[:end], append(added, out[end:]...)...)
	if len(added) > 0 && end == len(out)-len(added) {
		out = append(out, "")
	}
	return strings.Join(out, dst.nl), changes
}
