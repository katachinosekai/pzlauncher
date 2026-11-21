package packs

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/katachinosekai/pzlauncher/internal/modoptions"
)

type ModRef struct {
	ID       string
	Name     string
	Requires []string
}

type Setting struct {
	Rel     string
	Dir     bool
	Owner   string
	OwnerID string
	Blocked string
	Size    int64
}

func (s Setting) Shareable() bool { return s.Blocked == "" }

var gameFiles = map[string]string{
	"keysb42.ini":             "Game: key bindings",
	"pz_modlist_settings.cfg": "Game: saved mod lists (presets)",
}

var blockedExact = map[string]string{
	"saved_builds.txt":     "your saved character builds",
	"saved_outfits.txt":    "your saved outfits",
	"cheatpanel.ini":       "cheat panel",
	"options.ini":          "your graphics/game options",
	"layout.ini":           "your window layout",
	"screenresolution.ini": "your screen resolution",
	"host.ini":             "host settings (may hold passwords)",
	"2.txt":                "personal file",
	"modmanager":           "the game's own mod manager data",
	"skillrecoveryjournal": "your skill journals",
	"laststand":            "your Last Stand saves",
	"entitylayout.ini":     "your UI layout",
	"zbbatch-status.txt":   "ZBBatch's status file (written each start)",
	"luacensus.txt":        "a diagnostic dump",
	"that_damn_client.log": "a log",
	"dgvdoctor":            "diagnostic data",
}

var blockedPatterns = []struct {
	re     *regexp.Regexp
	reason string
}{
	{regexp.MustCompile(`(?i)^auto[a-z]*\.txt$`), "a launcher/test trigger"},
	{regexp.MustCompile(`(?i)^spncharcustom`), "your saved outfits"},
	{regexp.MustCompile(`(?i)^alife_creator_`), "your A-Life creator data"},
	{regexp.MustCompile(`(?i)^viewpoint`), "Viewpoint settings (never shipped)"},
	{regexp.MustCompile(`(?i)(log|trace|dump|debug|census)[^a-z]*(_prev)?(\.[a-z]+)?$|_log\.txt$|\.log$`), "a log or trace"},
	{regexp.MustCompile(`(?i)_dump(_\d+)?\.txt$|^alife_cars_dump`), "a diagnostic dump"},
	{regexp.MustCompile(`(?i)(\.bak|\.bak-.*|~\d*~|\.backup\.[a-z]+|^backup_.*)$`), "a backup copy"},
	{regexp.MustCompile(`(?i)_(seen|deleted_profiles|deaths|days|state|quiet)\.(ini|txt)$`), "per-player progress, not a setting"},
	{regexp.MustCompile(`(?i)_live\.ini$`), "live state, not a setting"},
}

var reSteamID = regexp.MustCompile(`7656119\d{10}`)

func blockReason(name string) string {
	low := strings.ToLower(name)
	if r, ok := blockedExact[low]; ok {
		return r
	}
	for _, p := range blockedPatterns {
		if p.re.MatchString(name) {
			return p.reason
		}
	}
	return ""
}

func norm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func stem(name string) string {
	name = strings.TrimSuffix(name, filepath.Ext(name))
	for _, sep := range []string{"_", "-", " "} {
		if i := strings.Index(name, sep); i > 0 {
			return name[:i]
		}
	}
	return name
}

func owner(name string, mods []ModRef) (string, string) {
	if g, ok := gameFiles[strings.ToLower(name)]; ok {
		return g, ""
	}
	full := norm(strings.TrimSuffix(name, filepath.Ext(name)))
	head := norm(stem(name))
	type hit struct {
		m     ModRef
		score int
	}
	var hits []hit
	for _, m := range mods {
		best := 0
		for _, cand := range []string{norm(m.ID), norm(m.Name)} {
			if len(cand) < 3 {
				continue
			}
			score := 0
			switch {
			case full == cand:
				score = 1000 + len(cand)
			case strings.HasPrefix(full, cand):
				score = 500 + len(cand)
			case len(head) >= 4 && strings.HasPrefix(cand, head):
				score = 100 + len(head)
			case len(head) >= 4 && strings.Contains(cand, head):
				score = 50 + len(head)
			}
			best = max(best, score)
		}
		if best > 0 {
			hits = append(hits, hit{m, best})
		}
	}
	matched := map[string]bool{}
	for _, h := range hits {
		matched[h.m.ID] = true
	}
	var pick *hit
	for i := range hits {
		h := &hits[i]
		dependent := false
		if h.score < 1000 {
			for _, r := range h.m.Requires {
				if matched[r] && r != h.m.ID {
					dependent = true
				}
			}
		}
		if dependent {
			continue
		}
		if pick == nil || h.score > pick.score || (h.score == pick.score && len(h.m.ID) < len(pick.m.ID)) {
			pick = h
		}
	}
	if pick == nil {
		return "", ""
	}
	return pick.m.Name, pick.m.ID
}

func DetectSettings(luaDir string, mods []ModRef) []Setting {
	ents, err := os.ReadDir(luaDir)
	if err != nil {
		return nil
	}
	var out []Setting
	for _, e := range ents {
		name := e.Name()
		if e.Type()&os.ModeSymlink != 0 || name == modoptions.FileName {
			continue
		}
		s := Setting{Rel: name, Dir: e.IsDir()}
		if info, err := e.Info(); err == nil {
			s.Size = info.Size()
		}
		s.Owner, s.OwnerID = owner(name, mods)
		s.Blocked = blockReason(name)
		if s.Blocked == "" && !s.Dir && hasSteamID(filepath.Join(luaDir, name)) {
			s.Blocked = "contains a Steam ID"
		}
		if s.Blocked == "" && s.Dir && dirHasSteamID(filepath.Join(luaDir, name)) {
			s.Blocked = "contains a Steam ID"
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Blocked == "") != (out[j].Blocked == "") {
			return out[i].Blocked == ""
		}
		return strings.ToLower(out[i].Rel) < strings.ToLower(out[j].Rel)
	})
	return out
}

func SettingsFor(all []Setting, modID string) []Setting {
	var out []Setting
	for _, s := range all {
		if s.OwnerID == modID {
			out = append(out, s)
		}
	}
	return out
}

func hasSteamID(p string) bool {
	st, err := os.Stat(p)
	if err != nil || st.Size() > 8<<20 {
		return err == nil
	}
	b, err := os.ReadFile(p)
	return err == nil && reSteamID.Match(b)
}

func dirHasSteamID(dir string) bool {
	found := false
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || found || d.IsDir() {
			return nil
		}
		if hasSteamID(p) {
			found = true
		}
		return nil
	})
	return found
}
