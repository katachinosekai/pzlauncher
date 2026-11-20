package workshop

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/katachinosekai/pzlauncher/internal/modlist"
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

func UploadPreviews(zomboidDir string) map[string]string {
	out := map[string]string{}
	for _, sub := range []string{"Workshop", "workshop-staging"} {
		dirs, _ := filepath.Glob(filepath.Join(zomboidDir, sub, "*"))
		for _, d := range dirs {
			b, err := os.ReadFile(filepath.Join(d, "workshop.txt"))
			if err != nil {
				continue
			}
			id := ""
			for _, line := range strings.Split(string(b), "\n") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(line), "id="); ok {
					id = strings.TrimSpace(v)
					break
				}
			}
			p := filepath.Join(d, "preview.png")
			if st, err := os.Stat(p); id != "" && err == nil && st.Mode().IsRegular() {
				if _, seen := out[id]; !seen {
					out[id] = p
				}
			}
		}
	}
	return out
}
