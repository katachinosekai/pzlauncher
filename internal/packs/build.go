package packs

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/katachinosekai/pzlauncher/internal/modoptions"
)

type BuildOptions struct {
	Name       string
	ZomboidDir string
	ZBHome     string
	Lua        []string
	ModList    bool
	Presets    []string
	ZBBatch    bool
	ModOptions []string
	Now        time.Time
}

func safePackName(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		if r == '-' || r == '_' || r == '.' || r == ' ' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" || out == "." || out == ".." {
		out = "My-Pack"
	}
	return out
}

func Build(dest string, o BuildOptions) ([]string, error) {
	if o.ZomboidDir == "" {
		return nil, errors.New("Zomboid folder not found")
	}
	if len(o.Lua) == 0 && !o.ModList && len(o.Presets) == 0 && !o.ZBBatch && len(o.ModOptions) == 0 {
		return nil, errors.New("nothing selected for the pack")
	}
	top := safePackName(o.Name)
	luaDir := filepath.Join(o.ZomboidDir, "Lua")
	type item struct {
		src, arc string
		text     *string
	}
	var items []item
	if len(o.ModOptions) > 0 {
		b, err := os.ReadFile(filepath.Join(luaDir, modoptions.FileName))
		if err != nil {
			return nil, err
		}
		want := map[string]bool{}
		for _, m := range o.ModOptions {
			want[m] = true
		}
		text := modoptions.Extract(modoptions.Parse(string(b)), want, false)
		if text == "" {
			return nil, errors.New("none of the chosen Mod Options groups has options to share")
		}
		if reSteamID.MatchString(text) {
			return nil, errors.New("the chosen Mod Options contain a Steam ID")
		}
		items = append(items, item{arc: top + "/settings/Lua/" + modoptions.FileName, text: &text})
	}
	for _, rel := range o.Lua {
		if strings.Contains(rel, "..") || filepath.IsAbs(rel) {
			return nil, fmt.Errorf("bad settings path %q", rel)
		}
		if filepath.ToSlash(rel) == modoptions.FileName {
			return nil, fmt.Errorf("%s goes in through the Mod Options choice, not as a whole file", rel)
		}
		if r := blockReason(filepath.Base(rel)); r != "" {
			return nil, fmt.Errorf("%s can't go into a pack: %s", rel, r)
		}
		src := filepath.Join(luaDir, rel)
		st, err := os.Lstat(src)
		if err != nil {
			return nil, err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%s is a link; only real files can go into a pack", rel)
		}
		if st.IsDir() {
			if dirHasSteamID(src) {
				return nil, fmt.Errorf("%s contains a Steam ID", rel)
			}
			err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				if d.Type()&os.ModeSymlink != 0 {
					return nil
				}
				r, _ := filepath.Rel(luaDir, p)
				items = append(items, item{src: p, arc: top + "/settings/Lua/" + filepath.ToSlash(r)})
				return nil
			})
			if err != nil {
				return nil, err
			}
			continue
		}
		if hasSteamID(src) {
			return nil, fmt.Errorf("%s contains a Steam ID", rel)
		}
		items = append(items, item{src: src, arc: top + "/settings/Lua/" + filepath.ToSlash(rel)})
	}
	if o.ModList {
		items = append(items, item{src: filepath.Join(o.ZomboidDir, "mods", "default.txt"), arc: top + "/settings/mods/default.txt"})
	}
	for _, p := range o.Presets {
		if filepath.Base(p) != p || !strings.HasSuffix(strings.ToLower(p), ".cfg") {
			return nil, fmt.Errorf("bad preset name %q", p)
		}
		items = append(items, item{src: filepath.Join(o.ZomboidDir, "Sandbox Presets", p), arc: top + "/presets/" + p})
	}
	if o.ZBBatch {
		items = append(items, item{src: filepath.Join(o.ZBHome, "zbbatch.properties"), arc: top + "/zbbatch/zbbatch.properties"})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].arc < items[j].arc })
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".pack-*.zip")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	zw := zip.NewWriter(tmp)
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	var names []string
	add := func(arc string, r io.Reader) error {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: arc, Method: zip.Deflate, Modified: now})
		if err != nil {
			return err
		}
		_, err = io.Copy(w, r)
		names = append(names, arc)
		return err
	}
	for _, it := range items {
		if it.text != nil {
			if err := add(it.arc, strings.NewReader(*it.text)); err != nil {
				zw.Close()
				tmp.Close()
				return nil, err
			}
			continue
		}
		f, err := os.Open(it.src)
		if err != nil {
			zw.Close()
			tmp.Close()
			return nil, err
		}
		err = add(it.arc, f)
		f.Close()
		if err != nil {
			zw.Close()
			tmp.Close()
			return nil, err
		}
	}
	readme := fmt.Sprintf("# %s\n\nMade with PZ Launcher on %s. Install it with PZ Launcher: Mods > Custom Packs > Local file > Install.\n", top, now.Format("2006-01-02"))
	if err := add(top+"/README.md", strings.NewReader(readme)); err != nil {
		zw.Close()
		tmp.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	return names, os.Rename(tmp.Name(), dest)
}
