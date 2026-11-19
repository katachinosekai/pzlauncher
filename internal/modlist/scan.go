package modlist

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Roots struct {
	ZomboidDir  string
	WorkshopDir string
	Order       []string
}

var DefaultFolderOrder = []string{"workshop", "steam", "mods"}

type Problem struct {
	Dir string
	Err error
}

type Index struct {
	Game     Version
	Mods     []*Mod
	byID     map[string]*Mod
	Shadowed map[string][]*Mod
	Problems []Problem
	avail    map[string]bool
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

func numericFirst(paths []string) {
	sort.SliceStable(paths, func(i, j int) bool {
		a, ea := strconv.ParseUint(filepath.Base(paths[i]), 10, 64)
		b, eb := strconv.ParseUint(filepath.Base(paths[j]), 10, 64)
		if ea == nil && eb == nil {
			return a < b
		}
		if (ea == nil) != (eb == nil) {
			return ea == nil
		}
		return paths[i] < paths[j]
	})
}

func (r Roots) ModsFolders() []string {
	order := r.Order
	if len(order) == 0 {
		order = DefaultFolderOrder
	}
	var out []string
	for _, s := range order {
		switch s {
		case "workshop":
			if r.ZomboidDir != "" {
				for _, item := range subdirs(filepath.Join(r.ZomboidDir, "Workshop")) {
					if p := filepath.Join(item, "Contents", "mods"); fileExists(p) {
						out = append(out, p)
					}
				}
			}
		case "steam":
			if r.WorkshopDir != "" {
				items := subdirs(r.WorkshopDir)
				numericFirst(items)
				for _, item := range items {
					if p := filepath.Join(item, "mods"); fileExists(p) {
						out = append(out, p)
					}
				}
			}
		case "mods":
			if r.ZomboidDir != "" {
				out = append(out, filepath.Join(r.ZomboidDir, "mods"))
			}
		}
	}
	return out
}

func (r Roots) ModDirs(game Version) []string {
	var out []string
	for _, folder := range r.ModsFolders() {
		for _, dir := range subdirs(folder) {
			if strings.EqualFold(filepath.Base(dir), "examplemod") {
				continue
			}
			vd := VersionDirName(dir, game)
			if !fileExists(filepath.Join(dir, "common", "mod.info")) && !fileExists(filepath.Join(dir, vd, "mod.info")) {
				continue
			}
			out = append(out, dir)
		}
	}
	return out
}

func Scan(r Roots, game Version) *Index {
	x := &Index{Game: game, byID: map[string]*Mod{}, Shadowed: map[string][]*Mod{}}
	for _, dir := range r.ModDirs(game) {
		m, err := ReadModDir(dir, game)
		if err != nil {
			x.Problems = append(x.Problems, Problem{Dir: dir, Err: err})
			continue
		}
		if _, dup := x.byID[m.ID]; dup {
			x.Shadowed[m.ID] = append(x.Shadowed[m.ID], m)
			continue
		}
		x.byID[m.ID] = m
		x.Mods = append(x.Mods, m)
	}
	x.avail = map[string]bool{}
	for _, m := range x.Mods {
		x.avail[m.ID] = x.computeAvailable(m)
	}
	return x
}

func NewIndex(game Version, mods ...*Mod) *Index {
	x := &Index{Game: game, byID: map[string]*Mod{}, Shadowed: map[string][]*Mod{}}
	for _, m := range mods {
		if _, dup := x.byID[m.ID]; dup {
			x.Shadowed[m.ID] = append(x.Shadowed[m.ID], m)
			continue
		}
		x.byID[m.ID] = m
		x.Mods = append(x.Mods, m)
	}
	x.avail = map[string]bool{}
	for _, m := range x.Mods {
		x.avail[m.ID] = x.computeAvailable(m)
	}
	return x
}

func (x *Index) Get(id string) *Mod { return x.byID[id] }

func (x *Index) computeAvailable(m *Mod) bool {
	if !m.AvailableSelf(x.Game) {
		return false
	}
	return x.availableRequired(m, map[string]bool{m.ID: true})
}

func (x *Index) availableRequired(m *Mod, seen map[string]bool) bool {
	for _, raw := range m.Require {
		id := strings.TrimSpace(raw)
		if seen[id] {
			continue
		}
		seen[id] = true
		r := x.byID[id]
		if r == nil || !r.AvailableSelf(x.Game) || !x.availableRequired(r, seen) {
			return false
		}
	}
	return true
}

func (x *Index) Available(id string) bool { return x.avail[id] }

func (x *Index) Unavailable(id string) string {
	m := x.byID[id]
	if m == nil {
		return "not installed"
	}
	if x.avail[id] {
		return ""
	}
	if m.VersionMin != nil && m.VersionMin.Int() > x.Game.Int() {
		return "needs game " + m.VersionMin.String() + " or newer"
	}
	if m.VersionMax != nil && m.VersionMax.Int() < x.Game.Int() {
		return "only for game " + m.VersionMax.String() + " or older"
	}
	for _, r := range m.Requires() {
		rm := x.byID[r]
		if rm == nil {
			return "required mod " + r + " is not installed"
		}
		if !x.avail[r] {
			return "required mod " + r + " is unavailable"
		}
	}
	return "a required mod is unavailable"
}

func (x *Index) AllCopies() []*Mod {
	out := append([]*Mod(nil), x.Mods...)
	for _, ms := range x.Shadowed {
		out = append(out, ms...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out
}
