package play

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/katachinosekai/pzlauncher/internal/modlist"
)

type scriptValue struct{ key, value string }

type scriptBlock struct {
	name   string
	values []scriptValue
	blocks []*scriptBlock
}

func (b *scriptBlock) value(key string) (string, bool) {
	for _, v := range b.values {
		if v.key == key {
			return v.value, true
		}
	}
	return "", false
}

func (b *scriptBlock) block(name string) *scriptBlock {
	for _, c := range b.blocks {
		if c.name == name {
			return c
		}
	}
	return nil
}

var blockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)

func parseScript(text string) *scriptBlock {
	text = blockComment.ReplaceAllString(text, "")
	root := &scriptBlock{}
	parseScriptBlock(root, text, 0)
	return root
}

func parseScriptBlock(b *scriptBlock, s string, p int) int {
	start := p
	addValue := func(stmt string) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			return
		}
		k, v, ok := strings.Cut(stmt, "=")
		if !ok {
			k, v = stmt, ""
		}
		b.values = append(b.values, scriptValue{strings.TrimSpace(k), strings.TrimSpace(v)})
	}
	for p < len(s) {
		switch s[p] {
		case '{':
			header := strings.Fields(s[start:p])
			child := &scriptBlock{}
			if len(header) > 0 {
				child.name = header[0]
			}
			b.blocks = append(b.blocks, child)
			p = parseScriptBlock(child, s, p+1)
			start = p
			continue
		case '}':
			addValue(s[start:p])
			return p + 1
		case ',':
			addValue(s[start:p])
			start = p + 1
		}
		p++
	}
	addValue(s[start:])
	return p
}

type ActiveMods struct {
	Mods []string
	Maps []string
}

func ParseActiveMods(text string) ActiveMods {
	root := parseScript(text)
	var am ActiveMods
	v, _ := root.value("VERSION")
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err != nil || n != 1 {
		return am
	}
	if b := root.block("mods"); b != nil {
		for _, val := range b.values {
			if !strings.EqualFold(strings.TrimSpace(val.key), "mod") {
				continue
			}
			id := strings.ReplaceAll(strings.TrimSpace(val.value), `\`, "")
			if strings.TrimSpace(id) != "" {
				am.Mods = append(am.Mods, id)
			}
		}
	}
	if b := root.block("maps"); b != nil {
		for _, val := range b.values {
			if !strings.EqualFold(strings.TrimSpace(val.key), "map") {
				continue
			}
			if m := strings.TrimSpace(val.value); m != "" {
				am.Maps = append(am.Maps, m)
			}
		}
	}
	return am
}

func ReadActiveMods(path string) (ActiveMods, bool) {
	d, err := os.ReadFile(path)
	if err != nil {
		return ActiveMods{}, false
	}
	return ParseActiveMods(string(d)), true
}

type ModIndex struct {
	*modlist.Index
}

func modlistVersion(g GameVersion) modlist.Version {
	return modlist.Version{Major: g.Major, Minor: g.Minor, Suffix: g.Suffix}
}

func (e *Env) ScanMods(game GameVersion) *ModIndex {
	roots := modlist.Roots{ZomboidDir: e.ZomboidDir, WorkshopDir: e.WorkshopDir}
	return &ModIndex{modlist.Scan(roots, modlistVersion(game))}
}

func (x *ModIndex) Len() int { return len(x.Mods) }

func ScanInstalled() (*ModIndex, error) {
	e, err := Default()
	if err != nil {
		return nil, err
	}
	var game GameVersion
	if gi, err := ReadGameInfo(e.GameDir); err == nil {
		game = gi.Version
	}
	return e.ScanMods(game), nil
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func subdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if isDir(p) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func mapDirNames(mapsDir string, challenge bool) []string {
	var out []string
	for _, d := range subdirs(mapsDir) {
		name := filepath.Base(d)
		if strings.EqualFold(name, "challengemaps") {
			if challenge {
				for _, sub := range subdirs(d) {
					if isFile(filepath.Join(sub, "map.info")) {
						out = append(out, name+"/"+filepath.Base(sub))
					}
				}
			}
			continue
		}
		if isFile(filepath.Join(d, "map.info")) {
			out = append(out, name)
		}
	}
	return out
}

func (x *ModIndex) LotDirs(gameDir string, active []string) map[string]bool {
	dirs := map[string]bool{}
	for _, id := range active {
		if !x.Available(id) {
			continue
		}
		m := x.Get(id)
		for _, base := range []string{m.CommonDir, m.VersionDir} {
			if base == "" {
				continue
			}
			for _, n := range mapDirNames(filepath.Join(base, "media", "maps"), false) {
				dirs[n] = true
			}
		}
	}
	if gameDir != "" {
		for _, n := range mapDirNames(filepath.Join(gameDir, "media", "maps"), true) {
			dirs[n] = true
		}
	}
	return dirs
}
