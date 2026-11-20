package javamods

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/katachinosekai/pzlauncher/internal/modlist"
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

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func uploadWorkshopID(modsRoot string) string {
	if filepath.Base(filepath.Dir(modsRoot)) != "Contents" {
		return ""
	}
	wt := parseInfo(filepath.Join(filepath.Dir(filepath.Dir(modsRoot)), "workshop.txt"))
	if id := wt["id"]; isDigits(id) {
		return id
	}
	return ""
}

var sourceLabels = map[modlist.Source]string{
	modlist.SourceSteam:    "Workshop",
	modlist.SourceWorkshop: "Zomboid/Workshop",
	modlist.SourceMods:     "Zomboid/mods",
}

func FromMod(m *modlist.Mod) *ModInfo {
	mi := &ModInfo{
		ID:         m.ID,
		Name:       m.Name,
		Version:    m.ModVersion,
		WorkshopID: m.WorkshopID,
		Dir:        m.Dir,
		InfoPath:   m.InfoPath,
		Source:     sourceLabels[m.Source],
		JarRel:     m.JavaJar,
		VersionDir: m.VersionDir,
		CommonDir:  m.CommonDir,
	}
	if mi.Source == "" {
		mi.Source = string(m.Source)
	}
	if mi.WorkshopID == "" {
		mi.WorkshopID = uploadWorkshopID(filepath.Dir(m.Dir))
	}
	if mi.WorkshopID == "" {
		if mm := reIDParam.FindStringSubmatch(m.URL); mm != nil {
			mi.WorkshopID = mm[1]
		}
	}
	return mi
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
