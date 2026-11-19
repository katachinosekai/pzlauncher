package modlist

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Version struct {
	Major  int
	Minor  int
	Suffix string
}

var versionRe = regexp.MustCompile(`^([0-9]+)\.([0-9]+)(.*)$`)

func javaInt(s string) int {
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return 0
	}
	return int(n)
}

func ParseVersion(s string) (Version, error) {
	m := versionRe.FindStringSubmatch(s)
	if m == nil {
		return Version{}, fmt.Errorf("invalid game version %q", s)
	}
	v := Version{Major: javaInt(m[1]), Minor: javaInt(m[2]), Suffix: m[3]}
	if v.Minor < 0 || v.Minor > 999 {
		return Version{}, fmt.Errorf("invalid game version %q", s)
	}
	return v, nil
}

func (v Version) Int() int       { return v.Major*1000 + v.Minor }
func (v Version) String() string { return fmt.Sprintf("%d.%d%s", v.Major, v.Minor, v.Suffix) }
func (v Version) IsZero() bool   { return v == Version{} }

var MinRequiredVersion = Version{Major: 42, Minor: 0}

type Source string

const (
	SourceWorkshop Source = "Workshop"
	SourceSteam    Source = "Steam"
	SourceMods     Source = "Mods"
)

type Mod struct {
	ID           string
	Name         string
	Description  string
	Author       string
	ModVersion   string
	URL          string
	Category     string
	Icon         string
	Posters      []string
	Require      []string
	Incompatible []string
	LoadAfter    []string
	LoadBefore   []string
	VersionMin   *Version
	VersionMax   *Version
	Dir          string
	CommonDir    string
	VersionDir   string
	InfoPath     string
	Source       Source
	WorkshopID   string
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

func javaSplitComma(s string) []string {
	parts := strings.Split(s, ",")
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return []string{""}
	}
	return parts
}

func blank(s string) bool { return strings.TrimSpace(s) == "" }

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func resolveAsset(versionDir, commonDir, rel string) string {
	p := versionDir + string(filepath.Separator) + rel
	if !fileExists(p) {
		p = commonDir + string(filepath.Separator) + rel
	}
	return p
}

var ErrInvalidModInfo = errors.New("invalid mod.info")

func ParseModInfo(text, versionDir, commonDir string) (*Mod, error) {
	m := &Mod{ID: "undefined_id", Name: "Unnamed Mod", VersionDir: versionDir, CommonDir: commonDir}
	list := func(line, key string) []string {
		return javaSplitComma(strings.ReplaceAll(strings.ReplaceAll(line, key, ""), `\`, ""))
	}
	for _, line := range javaLines(text) {
		switch {
		case strings.Contains(line, "name="):
			if v := strings.ReplaceAll(line, "name=", ""); !blank(v) {
				m.Name = v
			}
		case strings.Contains(line, "poster="):
			if v := strings.ReplaceAll(line, "poster=", ""); !blank(v) {
				m.Posters = append(m.Posters, resolveAsset(versionDir, commonDir, v))
			}
		case strings.Contains(line, "description="):
			m.Description += strings.ReplaceAll(line, "description=", "")
		case strings.Contains(line, "require="):
			m.Require = list(line, "require=")
		case strings.Contains(line, "incompatible="):
			m.Incompatible = list(line, "incompatible=")
		case strings.Contains(line, "loadModAfter="):
			m.LoadAfter = list(line, "loadModAfter=")
		case strings.Contains(line, "loadModBefore="):
			m.LoadBefore = list(line, "loadModBefore=")
		case strings.HasPrefix(line, "id="):
			if v := strings.TrimSpace(strings.ReplaceAll(line, "id=", "")); v != "" {
				m.ID = v
			}
		case strings.Contains(line, "author="):
			m.Author = strings.ReplaceAll(line, "author=", "")
		case strings.Contains(line, "modversion="):
			m.ModVersion = strings.ReplaceAll(line, "modversion=", "")
		case strings.Contains(line, "icon="):
			if v := strings.ReplaceAll(line, "icon=", ""); !blank(v) {
				m.Icon = resolveAsset(versionDir, commonDir, v)
			}
		case strings.Contains(line, "category="):
			m.Category = strings.ReplaceAll(line, "category=", "")
		case strings.Contains(line, "url="):
			m.URL = strings.ReplaceAll(line, "url=", "")
		case strings.Contains(line, "pack="):
			if strings.TrimSpace(strings.ReplaceAll(line, "pack=", "")) == "" {
				return nil, fmt.Errorf("%w: pack= line requires a file name", ErrInvalidModInfo)
			}
		case strings.Contains(line, "tiledef="):
			parts := strings.Fields(strings.TrimSpace(strings.ReplaceAll(line, "tiledef=", "")))
			if len(parts) != 2 {
				return nil, fmt.Errorf("%w: tiledef= line requires file name and file number", ErrInvalidModInfo)
			}
			n, err := strconv.ParseInt(parts[1], 10, 32)
			if err != nil {
				return nil, fmt.Errorf("%w: tiledef= line requires file name and file number", ErrInvalidModInfo)
			}
			if n < 100 || n > 8189 {
				return nil, fmt.Errorf("%w: tiledef=%s %d file number must be from 100 to 8189", ErrInvalidModInfo, parts[0], n)
			}
		case strings.HasPrefix(line, "versionMax="), strings.HasPrefix(line, "versionMin="):
			key := line[:strings.IndexByte(line, '=')+1]
			v := strings.TrimSpace(strings.ReplaceAll(line, key, ""))
			if v == "" {
				continue
			}
			gv, err := ParseVersion(v)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid %s %v", ErrInvalidModInfo, key[:len(key)-1], err)
			}
			if key == "versionMax=" {
				m.VersionMax = &gv
			} else {
				m.VersionMin = &gv
			}
		}
	}
	return m, nil
}

func versionIntFromName(name string) int {
	parts := strings.Split(name, ".")
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	switch {
	case len(parts) == 1:
		return javaInt(parts[0]) * 1000
	case len(parts) >= 2:
		return javaInt(parts[0])*1000 + min(javaInt(parts[1]), 999)
	}
	return 0
}

func VersionDirName(modDir string, game Version) string {
	best := MinRequiredVersion.String()
	bestInt := MinRequiredVersion.Int()
	entries, err := os.ReadDir(modDir)
	if err != nil {
		return best
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, n := range names {
		v := versionIntFromName(n)
		if v < bestInt || (!game.IsZero() && v > game.Int()) {
			continue
		}
		best, bestInt = n, v
	}
	return best
}

func sourceOf(modDir string) (Source, string) {
	root := filepath.Base(filepath.Clean(filepath.Join(modDir, "..", "..")))
	if strings.EqualFold(root, "Contents") {
		return SourceWorkshop, ""
	}
	if isSteamID(root) {
		return SourceSteam, root
	}
	return SourceMods, ""
}

func isSteamID(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}

func ReadModDir(modDir string, game Version) (*Mod, error) {
	modDir, _ = filepath.Abs(modDir)
	common := filepath.Join(modDir, "common")
	version := filepath.Join(modDir, VersionDirName(modDir, game))
	info := filepath.Join(version, "mod.info")
	if !fileExists(info) {
		info = filepath.Join(common, "mod.info")
		if !fileExists(info) {
			return nil, fmt.Errorf("can't find mod.info in mod dir: %q", modDir)
		}
	}
	d, err := os.ReadFile(info)
	if err != nil {
		return nil, err
	}
	m, err := ParseModInfo(string(d), version, common)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", info, err)
	}
	m.Dir, m.InfoPath = modDir, info
	m.Source, m.WorkshopID = sourceOf(modDir)
	return m, nil
}

func (m *Mod) AvailableSelf(game Version) bool {
	if game.IsZero() {
		return true
	}
	if m.VersionMin != nil && m.VersionMin.Int() > game.Int() {
		return false
	}
	return m.VersionMax == nil || !(m.VersionMax.Int() < game.Int())
}

func (m *Mod) VersionRange() string {
	lo, hi := "**", "**"
	if m.VersionMin != nil {
		lo = m.VersionMin.String()
	}
	if m.VersionMax != nil {
		hi = m.VersionMax.String()
	}
	return lo + " - " + hi
}

func (m *Mod) WorkshopURL() string {
	if m.WorkshopID == "" {
		return ""
	}
	return "https://steamcommunity.com/sharedfiles/filedetails/?id=" + m.WorkshopID
}

func cleanIDs(ids []string) []string {
	var out []string
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func (m *Mod) Requires() []string      { return cleanIDs(m.Require) }
func (m *Mod) Incompatibles() []string { return cleanIDs(m.Incompatible) }
func (m *Mod) After() []string         { return cleanIDs(m.LoadAfter) }
func (m *Mod) Before() []string        { return cleanIDs(m.LoadBefore) }
