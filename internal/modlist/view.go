package modlist

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Filter string

const (
	FilterAll       Filter = "All"
	FilterEnabled   Filter = "Enabled"
	FilterDisabled  Filter = "Disabled"
	FilterFavorites Filter = "Favorites"
)

var Filters = []Filter{FilterAll, FilterEnabled, FilterDisabled, FilterFavorites}

type SortMode string

const (
	SortNameAsc      SortMode = "Name A-Z"
	SortNameDesc     SortMode = "Name Z-A"
	SortWorkshopAsc  SortMode = "Workshop ID ascending"
	SortWorkshopDesc SortMode = "Workshop ID descending"
)

var SortModes = []SortMode{SortNameAsc, SortNameDesc, SortWorkshopAsc, SortWorkshopDesc}

var workshopURLRe = regexp.MustCompile(`[?&]id=([0-9]+)`)

func NormalizeQuery(q string) string {
	q = strings.TrimSpace(q)
	if m := workshopURLRe.FindStringSubmatch(q); m != nil {
		return m[1]
	}
	return strings.ToLower(q)
}

func Matches(m *Mod, q string) bool {
	if q == "" {
		return true
	}
	return strings.Contains(strings.ToLower(m.Name), q) ||
		strings.Contains(strings.ToLower(m.ID), q) ||
		(m.WorkshopID != "" && strings.Contains(m.WorkshopID, q))
}

type ViewOptions struct {
	Query    string
	Filter   Filter
	Sort     SortMode
	IsActive func(id string) bool
	IsFav    func(id string) bool
}

func View(mods []*Mod, o ViewOptions) []*Mod {
	q := NormalizeQuery(o.Query)
	var out []*Mod
	for _, m := range mods {
		if !Matches(m, q) {
			continue
		}
		switch o.Filter {
		case FilterEnabled:
			if o.IsActive == nil || !o.IsActive(m.ID) {
				continue
			}
		case FilterDisabled:
			if o.IsActive != nil && o.IsActive(m.ID) {
				continue
			}
		case FilterFavorites:
			if o.IsFav == nil || !o.IsFav(m.ID) {
				continue
			}
		}
		out = append(out, m)
	}
	SortMods(out, o.Sort)
	return out
}

func nameLess(a, b *Mod) bool {
	la, lb := strings.ToLower(a.Name), strings.ToLower(b.Name)
	if la != lb {
		return la < lb
	}
	return a.ID < b.ID
}

func SortMods(mods []*Mod, mode SortMode) {
	switch mode {
	case SortNameDesc:
		sort.SliceStable(mods, func(i, j int) bool { return nameLess(mods[j], mods[i]) })
	case SortWorkshopAsc, SortWorkshopDesc:
		desc := mode == SortWorkshopDesc
		sort.SliceStable(mods, func(i, j int) bool {
			a, b := mods[i], mods[j]
			if (a.WorkshopID == "") != (b.WorkshopID == "") {
				return b.WorkshopID == ""
			}
			if a.WorkshopID == "" {
				return nameLess(a, b)
			}
			na, _ := strconv.ParseUint(a.WorkshopID, 10, 64)
			nb, _ := strconv.ParseUint(b.WorkshopID, 10, 64)
			if na != nb {
				if desc {
					return na > nb
				}
				return na < nb
			}
			return nameLess(a, b)
		})
	default:
		sort.SliceStable(mods, func(i, j int) bool { return nameLess(mods[i], mods[j]) })
	}
}

var tagRe = regexp.MustCompile(`<[^<>]*>`)

func DescriptionText(s string) string {
	s = tagRe.ReplaceAllStringFunc(s, func(t string) string {
		cmd := strings.ToUpper(strings.TrimSpace(t[1 : len(t)-1]))
		if cmd == "LINE" || cmd == "BR" {
			return "\n"
		}
		return " "
	})
	s = strings.ReplaceAll(s, "&lt;", "<")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.Join(strings.Fields(l), " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

type Prefs struct {
	Sort   SortMode `json:"sort"`
	Filter Filter   `json:"filter"`
}

func PrefsPath() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "PZLauncher", "modstab.json"), nil
}

func LoadPrefs(path string) Prefs {
	p := Prefs{Sort: SortNameAsc, Filter: FilterAll}
	d, err := os.ReadFile(path)
	if err != nil {
		return p
	}
	var q Prefs
	if json.Unmarshal(d, &q) != nil {
		return p
	}
	for _, s := range SortModes {
		if q.Sort == s {
			p.Sort = s
		}
	}
	for _, f := range Filters {
		if q.Filter == f {
			p.Filter = f
		}
	}
	return p
}

func SavePrefs(path string, p Prefs) error {
	d, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, d)
}
