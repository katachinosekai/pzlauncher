package workshop

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kata/pzlauncher/internal/modlist"
)

type LocalItem struct {
	Title       string
	Image       string
	Description string
}

func ReadLocalItem(itemDir string) LocalItem {
	var out LocalItem
	if itemDir == "" {
		return out
	}
	ents, err := os.ReadDir(filepath.Join(itemDir, "mods"))
	if err != nil {
		return out
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		m, err := modlist.ReadModDir(filepath.Join(itemDir, "mods", e.Name()), modlist.Version{})
		if err != nil {
			continue
		}
		name := strings.TrimSpace(m.Name)
		if name == "" || name == "Unnamed Mod" {
			name = e.Name()
		}
		names = append(names, name)
		if out.Image == "" {
			for _, p := range append(append([]string{}, m.Posters...), m.Icon) {
				if st, err := os.Stat(p); p != "" && err == nil && st.Mode().IsRegular() {
					out.Image = p
					break
				}
			}
		}
		if out.Description == "" {
			out.Description = shortText(m.Description, 200)
		}
	}
	sort.Strings(names)
	out.Title = strings.Join(names, ", ")
	return out
}
