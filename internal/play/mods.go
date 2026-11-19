package play

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
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

type ModInfo struct {
	ID         string
	Dir        string
	CommonDir  string
	VersionDir string
	Require    []string
	VersionMin *GameVersion
	VersionMax *GameVersion
}

func javaLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func javaSplit(s, sep string) []string {
	if !strings.Contains(s, sep) {
		return []string{s}
	}
	parts := strings.Split(s, sep)
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func parseModInfo(text string) (*ModInfo, bool) {
	m := &ModInfo{ID: "undefined_id"}
	splitList := func(line, key string) []string {
		v := strings.ReplaceAll(strings.ReplaceAll(line, key, ""), `\`, "")
		return javaSplit(v, ",")
	}
	for _, line := range javaLines(text) {
		switch {
		case strings.Contains(line, "name="), strings.Contains(line, "poster="), strings.Contains(line, "description="):
		case strings.Contains(line, "require="):
			m.Require = splitList(line, "require=")
		case strings.Contains(line, "incompatible="), strings.Contains(line, "loadModAfter="), strings.Contains(line, "loadModBefore="):
		case strings.HasPrefix(line, "id="):
			if id := strings.TrimSpace(strings.ReplaceAll(line, "id=", "")); id != "" {
				m.ID = id
			}
		case strings.Contains(line, "author="), strings.Contains(line, "modversion="), strings.Contains(line, "icon="),
			strings.Contains(line, "category="), strings.Contains(line, "url="), strings.Contains(line, "pack="), strings.Contains(line, "tiledef="):
		case strings.HasPrefix(line, "versionMax="), strings.HasPrefix(line, "versionMin="):
			key := line[:strings.IndexByte(line, '=')+1]
			v := strings.TrimSpace(strings.ReplaceAll(line, key, ""))
			if v == "" {
				continue
			}
			gv, err := ParseGameVersion(v)
			if err != nil {
				return nil, false
			}
			if key == "versionMax=" {
				m.VersionMax = &gv
			} else {
				m.VersionMin = &gv
			}
		}
	}
	return m, true
}

type ModIndex struct {
	Game  GameVersion
	byID  map[string]*ModInfo
	order []*ModInfo
}

var minRequiredVersion = GameVersion{Major: 42, Minor: 0}

func modVersionDirName(modDir string, game GameVersion) string {
	best := minRequiredVersion.String()
	bestInt := minRequiredVersion.Int()
	entries, err := os.ReadDir(modDir)
	if err != nil {
		return best
	}
	for _, e := range entries {
		v := versionIntFromName(e.Name())
		if v >= bestInt && v <= game.Int() {
			best, bestInt = e.Name(), v
		}
	}
	return best
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func readModDir(modDir string, game GameVersion) *ModInfo {
	common := filepath.Join(modDir, "common")
	version := filepath.Join(modDir, modVersionDirName(modDir, game))
	info := filepath.Join(version, "mod.info")
	if !isFile(info) {
		info = filepath.Join(common, "mod.info")
		if !isFile(info) {
			return nil
		}
	}
	d, err := os.ReadFile(info)
	if err != nil {
		return nil
	}
	m, ok := parseModInfo(string(d))
	if !ok {
		return nil
	}
	m.Dir, m.CommonDir, m.VersionDir = modDir, common, version
	return m
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

func (e *Env) modFolders() []string {
	var out []string
	if e.ZomboidDir != "" {
		for _, item := range subdirs(filepath.Join(e.ZomboidDir, "Workshop")) {
			if p := filepath.Join(item, "Contents", "mods"); isDir(p) {
				out = append(out, p)
			}
		}
	}
	if e.WorkshopDir != "" {
		for _, item := range subdirs(e.WorkshopDir) {
			if p := filepath.Join(item, "mods"); isDir(p) {
				out = append(out, p)
			}
		}
	}
	if e.ZomboidDir != "" {
		out = append(out, filepath.Join(e.ZomboidDir, "mods"))
	}
	return out
}

func (e *Env) ScanMods(game GameVersion) *ModIndex {
	idx := &ModIndex{Game: game, byID: map[string]*ModInfo{}}
	for _, folder := range e.modFolders() {
		for _, modDir := range subdirs(folder) {
			if strings.EqualFold(filepath.Base(modDir), "examplemod") {
				continue
			}
			m := readModDir(modDir, game)
			if m == nil {
				continue
			}
			idx.order = append(idx.order, m)
			if _, dup := idx.byID[m.ID]; !dup {
				idx.byID[m.ID] = m
			}
		}
	}
	return idx
}

func (x *ModIndex) Get(id string) *ModInfo { return x.byID[id] }

func (x *ModIndex) Len() int { return len(x.byID) }

func (x *ModIndex) availableSelf(m *ModInfo) bool {
	if m.VersionMin != nil && m.VersionMin.Int() > x.Game.Int() {
		return false
	}
	if m.VersionMax != nil && m.VersionMax.Int() < x.Game.Int() {
		return false
	}
	return true
}

func (x *ModIndex) availableRequired(m *ModInfo, seen map[string]bool) bool {
	for _, r := range m.Require {
		r = strings.TrimSpace(r)
		if seen[r] {
			continue
		}
		seen[r] = true
		dep := x.byID[r]
		if dep == nil || !x.availableSelf(dep) || !x.availableRequired(dep, seen) {
			return false
		}
	}
	return true
}

func (x *ModIndex) Available(id string) bool {
	m := x.byID[id]
	if m == nil || !x.availableSelf(m) {
		return false
	}
	return x.availableRequired(m, map[string]bool{m.ID: true})
}

func (x *ModIndex) Unavailable(id string) string {
	m := x.byID[id]
	switch {
	case m == nil:
		return "not installed"
	case m.VersionMin != nil && m.VersionMin.Int() > x.Game.Int():
		return "needs game " + m.VersionMin.String() + " or newer"
	case m.VersionMax != nil && m.VersionMax.Int() < x.Game.Int():
		return "only for game " + m.VersionMax.String() + " or older"
	case !x.availableRequired(m, map[string]bool{m.ID: true}):
		return "a required mod is missing or unavailable"
	}
	return ""
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
		m := x.byID[id]
		for _, base := range []string{m.CommonDir, m.VersionDir} {
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
