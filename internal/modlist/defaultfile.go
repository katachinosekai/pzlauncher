package modlist

import (
	"runtime"
	"slices"
	"strconv"
	"strings"
)

type span struct{ start, end int }

type blockInfo struct {
	found  bool
	body   span
	values []entry
	indent string
}

type entry struct {
	key, value string
	raw        string
}

type DefaultFile struct {
	raw     string
	NL      string
	Version int
	Mods    []string
	Maps    []string
	modRaw  map[string]string
	mods    blockInfo
	maps    blockInfo
	origMod []string
	origMap []string
}

func platformNL() string {
	if runtime.GOOS == "windows" {
		return "\r\n"
	}
	return "\n"
}

func skipComment(s string, i int) int {
	if strings.HasPrefix(s[i:], "/*") {
		if e := strings.Index(s[i+2:], "*/"); e >= 0 {
			return i + 2 + e + 2
		}
		return len(s)
	}
	if strings.HasPrefix(s[i:], "//") {
		if e := strings.IndexAny(s[i:], "\r\n"); e >= 0 {
			return i + e
		}
		return len(s)
	}
	return i
}

func stripComments(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if j := skipComment(s, i); j != i {
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func matchBrace(s string, open int) int {
	depth := 0
	for i := open; i < len(s); {
		if j := skipComment(s, i); j != i {
			i = j
			continue
		}
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
		i++
	}
	return -1
}

func parseValues(body string) []entry {
	var out []entry
	clean := stripComments(body)
	for _, part := range strings.Split(clean, ",") {
		t := strings.TrimSpace(part)
		if t == "" {
			continue
		}
		k, v, ok := strings.Cut(t, "=")
		if !ok {
			out = append(out, entry{raw: t})
			continue
		}
		out = append(out, entry{key: strings.TrimSpace(k), value: strings.TrimSpace(v), raw: t})
	}
	return out
}

func detectIndent(body, key string) string {
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r", ""), "\n") {
		t := strings.TrimLeft(line, " \t")
		if t != "" && strings.HasPrefix(strings.ToLower(t), key) {
			return line[:len(line)-len(t)]
		}
	}
	return "    "
}

func ParseDefault(raw string) *DefaultFile {
	f := &DefaultFile{raw: raw, NL: platformNL(), Version: -1, modRaw: map[string]string{}}
	if strings.Contains(raw, "\r\n") {
		f.NL = "\r\n"
	} else if strings.Contains(raw, "\n") {
		f.NL = "\n"
	}
	i := 0
	for i < len(raw) {
		if j := skipComment(raw, i); j != i {
			i = j
			continue
		}
		rest := raw[i:]
		k := strings.IndexAny(rest, "{=,}")
		if k < 0 {
			break
		}
		name := strings.TrimSpace(stripComments(rest[:k]))
		switch rest[k] {
		case '=':
			end := strings.IndexByte(rest[k:], ',')
			val := ""
			if end < 0 {
				val = rest[k+1:]
				i = len(raw)
			} else {
				val = rest[k+1 : k+end]
				i += k + end + 1
			}
			if strings.EqualFold(name, "VERSION") {
				if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
					f.Version = n
				}
			}
		case '{':
			open := i + k
			closeAt := matchBrace(raw, open)
			if closeAt < 0 {
				i = len(raw)
				break
			}
			b := blockInfo{found: true, body: span{open + 1, closeAt}}
			body := raw[open+1 : closeAt]
			b.values = parseValues(body)
			lower := strings.ToLower(name)
			switch {
			case lower == "mods" && !f.mods.found:
				b.indent = detectIndent(body, "mod")
				f.mods = b
			case lower == "maps" && !f.maps.found:
				b.indent = detectIndent(body, "map")
				f.maps = b
			}
			i = closeAt + 1
		default:
			i += k + 1
		}
	}
	if f.Version == 1 {
		for _, e := range f.mods.values {
			if !strings.EqualFold(e.key, "mod") {
				continue
			}
			id := strings.TrimSpace(strings.ReplaceAll(e.value, `\`, ""))
			if id == "" {
				continue
			}
			if _, dup := f.modRaw[id]; !dup {
				f.modRaw[id] = e.value
			}
			f.Mods = append(f.Mods, id)
		}
		for _, e := range f.maps.values {
			if strings.EqualFold(e.key, "map") && e.value != "" {
				f.Maps = append(f.Maps, e.value)
			}
		}
	}
	f.origMod = slices.Clone(f.Mods)
	f.origMap = slices.Clone(f.Maps)
	return f
}

func NewDefault() *DefaultFile {
	return &DefaultFile{NL: platformNL(), Version: 1, modRaw: map[string]string{}}
}

func (f *DefaultFile) Raw() string { return f.raw }

func (f *DefaultFile) Changed() bool {
	return !slices.Equal(f.Mods, f.origMod) || !slices.Equal(f.Maps, f.origMap)
}

func (f *DefaultFile) blockBody(b blockInfo, key string, items []string, rawOf func(string) string) string {
	nl := f.NL
	indent := b.indent
	if indent == "" {
		indent = "    "
	}
	var sb strings.Builder
	sb.WriteString(nl)
	for _, it := range items {
		sb.WriteString(indent + key + " = " + rawOf(it) + "," + nl)
	}
	for _, e := range b.values {
		if !strings.EqualFold(e.key, key) {
			sb.WriteString(indent + e.raw + "," + nl)
		}
	}
	return sb.String()
}

func (f *DefaultFile) Render() string {
	if f.raw != "" && !f.Changed() {
		return f.raw
	}
	modRaw := func(id string) string {
		if r, ok := f.modRaw[id]; ok {
			return r
		}
		return id
	}
	same := func(s string) string { return s }
	nl := f.NL
	if f.raw == "" {
		var sb strings.Builder
		sb.WriteString("VERSION = 1," + nl + nl + "mods" + nl + "{")
		sb.WriteString(f.blockBody(blockInfo{}, "mod", f.Mods, modRaw))
		sb.WriteString("}" + nl + nl + "maps" + nl + "{")
		sb.WriteString(f.blockBody(blockInfo{}, "map", f.Maps, same))
		sb.WriteString("}" + nl)
		return sb.String()
	}
	type repl struct {
		s    span
		text string
	}
	var reps []repl
	if !slices.Equal(f.Mods, f.origMod) {
		if f.mods.found {
			reps = append(reps, repl{f.mods.body, f.blockBody(f.mods, "mod", f.Mods, modRaw)})
		}
	}
	if !slices.Equal(f.Maps, f.origMap) && f.maps.found {
		reps = append(reps, repl{f.maps.body, f.blockBody(f.maps, "map", f.Maps, same)})
	}
	out := f.raw
	slices.SortFunc(reps, func(a, b repl) int { return b.s.start - a.s.start })
	for _, r := range reps {
		out = out[:r.s.start] + r.text + out[r.s.end:]
	}
	if !f.mods.found && !slices.Equal(f.Mods, f.origMod) {
		if !strings.HasSuffix(out, "\n") {
			out += nl
		}
		out += nl + "mods" + nl + "{" + f.blockBody(blockInfo{}, "mod", f.Mods, modRaw) + "}" + nl
	}
	return out
}

func (f *DefaultFile) Rebase() {
	f2 := ParseDefault(f.Render())
	*f = *f2
}
