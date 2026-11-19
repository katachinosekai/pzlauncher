package javamods

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kata/pzlauncher/internal/paths"
)

type ModInfo struct {
	ID         string
	Name       string
	Version    string
	WorkshopID string
	Dir        string
	InfoPath   string
	Source     string
	JarRel     string
	VersionDir string
	CommonDir  string
}

func DefaultRoots() []string {
	var roots []string
	if z, err := paths.Zomboid(); err == nil {
		roots = append(roots, filepath.Join(z, "mods"))
		ws, _ := filepath.Glob(filepath.Join(z, "Workshop", "*", "Contents", "mods"))
		sort.Strings(ws)
		roots = append(roots, ws...)
	}
	if root, err := paths.SteamRoot(); err == nil {
		if dir, err := paths.WorkshopDir(root); err == nil {
			roots = append(roots, WorkshopModRoots(dir)...)
		}
	}
	return roots
}

func WorkshopModRoots(contentDir string) []string {
	ms, _ := filepath.Glob(filepath.Join(contentDir, "*", "mods"))
	sort.Strings(ms)
	return ms
}

var reIDParam = regexp.MustCompile(`[?&]id=(\d+)`)

func parseInfo(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	first := true
	for sc.Scan() {
		l := sc.Text()
		if first {
			l = strings.TrimPrefix(l, "\ufeff")
			first = false
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		if _, dup := out[k]; !dup {
			out[k] = strings.TrimSpace(v)
		}
	}
	return out
}

func versionNum(name string) (float64, bool) {
	if name == "" || name[0] < '0' || name[0] > '9' {
		return 0, false
	}
	parts := strings.SplitN(name, ".", 3)
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, false
	}
	minor := 0
	if len(parts) > 1 {
		if minor, err = strconv.Atoi(parts[1]); err != nil {
			return 0, false
		}
	}
	return float64(major)*1000 + float64(minor), true
}

func versionDir(modDir string) string {
	ents, err := os.ReadDir(modDir)
	if err != nil {
		return ""
	}
	best, bestV := "", -1.0
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		v, ok := versionNum(e.Name())
		if ok && v < 43000 && v > bestV {
			best, bestV = filepath.Join(modDir, e.Name()), v
		}
	}
	return best
}

func workshopIDFor(modsRoot string) string {
	parent := filepath.Base(filepath.Dir(modsRoot))
	if isDigits(parent) {
		return parent
	}
	if filepath.Base(filepath.Dir(modsRoot)) == "Contents" {
		wt := parseInfo(filepath.Join(filepath.Dir(filepath.Dir(modsRoot)), "workshop.txt"))
		if id := wt["id"]; isDigits(id) {
			return id
		}
	}
	return ""
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func sourceOf(modsRoot string) string {
	p := filepath.ToSlash(modsRoot)
	switch {
	case strings.Contains(p, "/workshop/content/"):
		return "Workshop"
	case strings.Contains(p, "/Zomboid/Workshop/"):
		return "Zomboid/Workshop"
	}
	return "Zomboid/mods"
}

func ReadMod(modDir, source, workshopID string) *ModInfo {
	vd := versionDir(modDir)
	common := filepath.Join(modDir, "common")
	var infoPath string
	for _, c := range []string{vd, common, modDir} {
		if c == "" {
			continue
		}
		if st, err := os.Stat(filepath.Join(c, "mod.info")); err == nil && !st.IsDir() {
			infoPath = filepath.Join(c, "mod.info")
			break
		}
	}
	if infoPath == "" {
		return nil
	}
	info := parseInfo(infoPath)
	m := &ModInfo{
		ID:         info["id"],
		Name:       info["name"],
		Version:    info["modversion"],
		WorkshopID: workshopID,
		Dir:        modDir,
		InfoPath:   infoPath,
		Source:     source,
		JarRel:     info["javajarfile"],
		VersionDir: vd,
		CommonDir:  common,
	}
	if m.ID == "" {
		m.ID = filepath.Base(modDir)
	}
	if m.Name == "" {
		m.Name = m.ID
	}
	if m.WorkshopID == "" {
		if mm := reIDParam.FindStringSubmatch(info["url"]); mm != nil {
			m.WorkshopID = mm[1]
		}
	}
	return m
}

func ScanAll(roots []string) []*ModInfo {
	var out []*ModInfo
	seen := map[string]bool{}
	for _, root := range roots {
		ents, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		wsid := workshopIDFor(root)
		src := sourceOf(root)
		for _, e := range ents {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(root, e.Name())
			if seen[dir] {
				continue
			}
			seen[dir] = true
			if m := ReadMod(dir, src, wsid); m != nil {
				out = append(out, m)
			}
		}
	}
	return out
}

func (m *ModInfo) JarPath() string {
	if m.JarRel == "" {
		return ""
	}
	rel := filepath.FromSlash(strings.TrimLeft(strings.ReplaceAll(m.JarRel, `\`, "/"), "/"))
	var first string
	for _, base := range []string{m.VersionDir, m.CommonDir, m.Dir} {
		if base == "" {
			continue
		}
		p := filepath.Join(base, rel)
		if first == "" {
			first = p
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return first
}

func HashFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
