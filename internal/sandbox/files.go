package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Kind int

const (
	Preset Kind = iota
	Server
)

type Target struct {
	Kind Kind
	Name string
	Path string
}

func (t Target) String() string {
	if t.Kind == Server {
		return "Server: " + t.Name
	}
	return "Preset: " + t.Name
}

func Targets(zomboidDir string) []Target {
	var out []Target
	if m, err := filepath.Glob(filepath.Join(zomboidDir, "Sandbox Presets", "*.cfg")); err == nil {
		sort.Strings(m)
		for _, p := range m {
			out = append(out, Target{Preset, strings.TrimSuffix(filepath.Base(p), ".cfg"), p})
		}
	}
	if m, err := filepath.Glob(filepath.Join(zomboidDir, "Server", "*_SandboxVars.lua")); err == nil {
		sort.Strings(m)
		for _, p := range m {
			out = append(out, Target{Server, strings.TrimSuffix(filepath.Base(p), "_SandboxVars.lua"), p})
		}
	}
	return out
}

var (
	reCfgLine  = regexp.MustCompile(`^([A-Za-z0-9_][A-Za-z0-9_.]*)=(.*)$`)
	reLuaOpen  = regexp.MustCompile(`^\s*([A-Za-z0-9_]+)\s*=\s*\{\s*$`)
	reLuaValue = regexp.MustCompile(`^(\s*)([A-Za-z0-9_]+)\s*=\s*(.*?)\s*,?\s*$`)
)

type File struct {
	Target
	lines  []string
	nl     string
	values map[string]string
	where  map[string]int
	close  map[string]int
	end    int
}

func Open(t Target) (*File, error) {
	b, err := os.ReadFile(t.Path)
	if err != nil {
		return nil, err
	}
	return parseFile(t, string(b)), nil
}

func parseFile(t Target, s string) *File {
	f := &File{Target: t, nl: "\n", values: map[string]string{}, where: map[string]int{}, close: map[string]int{}, end: -1}
	if strings.Contains(s, "\r\n") {
		f.nl = "\r\n"
	}
	f.lines = strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if t.Kind == Preset {
		for i, l := range f.lines {
			if m := reCfgLine.FindStringSubmatch(l); m != nil {
				f.values[m[1]] = m[2]
				f.where[m[1]] = i
			}
		}
		return f
	}
	var stack []string
	for i, l := range f.lines {
		tr := strings.TrimSpace(l)
		if m := reLuaOpen.FindStringSubmatch(l); m != nil {
			stack = append(stack, m[1])
			continue
		}
		if tr == "}," || tr == "}" {
			if len(stack) == 2 {
				f.close[stack[1]] = i
			} else if len(stack) == 1 {
				f.end = i
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		if len(stack) < 1 || len(stack) > 2 || strings.HasPrefix(tr, "--") {
			continue
		}
		if m := reLuaValue.FindStringSubmatch(l); m != nil {
			name := m[2]
			if len(stack) == 2 {
				name = stack[1] + "." + m[2]
			}
			f.values[name] = luaValue(m[3])
			f.where[name] = i
		}
	}
	return f
}

func luaValue(v string) string {
	if strings.HasPrefix(v, `"`) {
		if u, err := strconv.Unquote(v); err == nil {
			return u
		}
		return strings.Trim(v, `"`)
	}
	return v
}

func luaQuote(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(v) + `"`
}

func (f *File) Value(name string) (string, bool) {
	v, ok := f.values[name]
	return v, ok
}

func (f *File) format(o Option, v string) string {
	if f.Kind == Server && o.Type == "string" {
		return luaQuote(v)
	}
	return v
}

func (f *File) Render(changes map[string]string, opts map[string]Option) (string, error) {
	lines := append([]string(nil), f.lines...)
	type ins struct {
		at   int
		text []string
	}
	var inserts []ins
	names := make([]string, 0, len(changes))
	for n := range changes {
		names = append(names, n)
	}
	sort.Strings(names)
	newGroups := map[string][]string{}
	var groupOrder []string
	for _, n := range names {
		o, ok := opts[n]
		if !ok {
			return "", fmt.Errorf("unknown option %s", n)
		}
		v := changes[n]
		if !o.Valid(v) {
			return "", fmt.Errorf("%s: %q is not a valid value", n, v)
		}
		val := f.format(o, v)
		if i, ok := f.where[n]; ok {
			if f.Kind == Preset {
				lines[i] = n + "=" + val
			} else {
				m := reLuaValue.FindStringSubmatch(lines[i])
				lines[i] = m[1] + m[2] + " = " + val + ","
			}
			continue
		}
		if f.Kind == Preset {
			inserts = append(inserts, ins{len(lines), []string{"", n + "=" + val}})
			continue
		}
		if o.Group() == "" {
			if f.end < 0 {
				return "", fmt.Errorf("%s: no closing } found", f.Path)
			}
			inserts = append(inserts, ins{f.end, []string{"    " + o.Key() + " = " + val + ","}})
			continue
		}
		line := "        " + o.Key() + " = " + val + ","
		if c, ok := f.close[o.Group()]; ok {
			inserts = append(inserts, ins{c, []string{line}})
			continue
		}
		if f.end < 0 {
			return "", fmt.Errorf("%s: no closing } found", f.Path)
		}
		if _, ok := newGroups[o.Group()]; !ok {
			groupOrder = append(groupOrder, o.Group())
		}
		newGroups[o.Group()] = append(newGroups[o.Group()], line)
	}
	for _, g := range groupOrder {
		block := append([]string{"    " + g + " = {"}, newGroups[g]...)
		inserts = append(inserts, ins{f.end, append(block, "    },")})
	}
	if f.Kind == Preset && len(inserts) > 0 && len(lines) > 0 && lines[len(lines)-1] == "" {
		for i := range inserts {
			inserts[i].at = len(lines) - 1
		}
	}
	sort.SliceStable(inserts, func(i, j int) bool { return inserts[i].at > inserts[j].at })
	for _, in := range inserts {
		lines = append(lines[:in.at], append(append([]string(nil), in.text...), lines[in.at:]...)...)
	}
	return strings.Join(lines, f.nl), nil
}

func (f *File) Save(changes map[string]string, opts map[string]Option) (backup string, err error) {
	text, err := f.Render(changes, opts)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(f.Path)
	if err != nil {
		return "", err
	}
	old, err := os.ReadFile(f.Path)
	if err != nil {
		return "", err
	}
	backup = f.Path + ".bak-" + time.Now().Format("20060102-150405")
	if err := os.WriteFile(backup, old, st.Mode().Perm()); err != nil {
		return "", err
	}
	tmp := f.Path + ".tmp"
	if err := os.WriteFile(tmp, []byte(text), st.Mode().Perm()); err != nil {
		return backup, err
	}
	if err := os.Rename(tmp, f.Path); err != nil {
		os.Remove(tmp)
		return backup, err
	}
	nf := parseFile(f.Target, text)
	*f = *nf
	return backup, nil
}
