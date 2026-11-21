package diag

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/katachinosekai/pzlauncher/internal/javamods"
	"github.com/katachinosekai/pzlauncher/internal/modlist"
	"github.com/katachinosekai/pzlauncher/internal/paths"
	"github.com/katachinosekai/pzlauncher/internal/play"
)

const maxFileBytes = 32 << 20

var (
	reSteamID   = regexp.MustCompile(`7656119\d{10}`)
	rePassword  = regexp.MustCompile(`(?i)(password\s*=\s*)[^\s&;,"']+`)
	rePassJSON  = regexp.MustCompile(`(?i)("[a-z_]*password"\s*:\s*)"(?:[^"\\]|\\.)*"`)
	rePassSpace = regexp.MustCompile(`(?i)(-password\s+)\S+`)
	reWinUser   = regexp.MustCompile(`(?i)([a-z]:[\\/]+(?:users|documents and settings)[\\/]+)[^\\/\s"':;]+`)
	reNixUser   = regexp.MustCompile(`(/(?:home|Users)/|/run/media/|/media/)[^/\s"':;]+`)
	reModLine   = regexp.MustCompile(`^\s*mod\s*=\s*\\?([^,]+?)\s*,?\s*$`)
	reSecretVar = regexp.MustCompile(`(?m)^(\s*[A-Z0-9_]*(?:TOKEN|KEY|SECRET|PASSWORD|PASSWD|CREDENTIAL|AUTH)[A-Z0-9_]*\s*=\s*).+$`)
)

func Redact(s string) string {
	s = reSteamID.ReplaceAllString(s, "7656119##########")
	s = reSecretVar.ReplaceAllString(s, "${1}<redacted>")
	s = rePassword.ReplaceAllString(s, "${1}<redacted>")
	s = rePassJSON.ReplaceAllString(s, `${1}"<redacted>"`)
	s = rePassSpace.ReplaceAllString(s, "${1}<redacted>")
	s = reWinUser.ReplaceAllString(s, "${1}<user>")
	s = reNixUser.ReplaceAllString(s, "${1}<user>")
	return s
}

type CollectOptions struct {
	ZomboidDir       string
	KeptDir          string
	InstallerLogDirs []string
	GameDir          string
	HomeDir          string
	ZombieBuddyDir   string
	Mods             *modlist.Index
	LauncherVersion  string
	Now              func() time.Time
	LogsOnly         bool
	Extra            map[string]string
}

func DefaultCollectOptions(launcherVersion string) CollectOptions {
	o := CollectOptions{LauncherVersion: launcherVersion, Now: time.Now}
	o.ZomboidDir, _ = paths.Zomboid()
	o.KeptDir, _ = LogsDir()
	o.InstallerLogDirs = InstallerLogDirs()
	if h, err := os.UserHomeDir(); err == nil {
		o.HomeDir = h
		o.ZombieBuddyDir = filepath.Join(h, ".zombie_buddy")
	}
	if root, err := paths.SteamRoot(); err == nil {
		o.GameDir, _ = paths.GameDir(root)
	}
	if idx, err := play.ScanInstalled(); err == nil {
		o.Mods = idx.Index
	}
	return o
}

func InstallerLogDirs() []string {
	var out []string
	if c, err := os.UserConfigDir(); err == nil {
		out = append(out, filepath.Join(c, "PZLauncher", "install-logs"))
	}
	if c, err := os.UserCacheDir(); err == nil {
		base := filepath.Join(c, "PZLauncher", "packs")
		for _, pat := range []string{filepath.Join(base, "*", "logs"), filepath.Join(base, "*", "*", "logs")} {
			m, _ := filepath.Glob(pat)
			out = append(out, m...)
		}
	}
	out = append(out, filepath.Join(os.TempDir(), "PZ-Fixes-logs"))
	return out
}

type zipper struct {
	zw    *zip.Writer
	now   time.Time
	names map[string]bool
	files []string
}

func (z *zipper) addText(name, text string) error {
	if z.names[name] {
		return nil
	}
	z.names[name] = true
	w, err := z.zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: z.now})
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, Redact(text)); err != nil {
		return err
	}
	z.files = append(z.files, name)
	return nil
}

func (z *zipper) addFile(name, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil
	}
	head := ""
	if st.Size() > maxFileBytes {
		if _, err := f.Seek(st.Size()-maxFileBytes, io.SeekStart); err != nil {
			return nil
		}
		head = fmt.Sprintf("[truncated: only the last %d MiB of %d bytes]\n", maxFileBytes>>20, st.Size())
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil
	}
	return z.addText(name, head+string(b))
}

func Collect(w io.Writer, o CollectOptions) ([]string, error) {
	now := time.Now
	if o.Now != nil {
		now = o.Now
	}
	z := &zipper{zw: zip.NewWriter(w), now: now(), names: map[string]bool{}}
	var steps []func() error
	extras := make([]string, 0, len(o.Extra))
	for n := range o.Extra {
		extras = append(extras, n)
	}
	sort.Strings(extras)
	for _, n := range extras {
		steps = append(steps, func() error { return z.addText(n, o.Extra[n]) })
	}
	if o.LogsOnly {
		steps = append(steps,
			func() error { return addKept(z, o) },
			func() error {
				if o.ZomboidDir == "" {
					return nil
				}
				for _, s := range currentLogs(o.ZomboidDir) {
					if err := z.addFile("current/"+filepath.Base(s.path), s.path); err != nil {
						return err
					}
				}
				return nil
			},
			func() error { return addCrashDumps(z, o) },
		)
	} else {
		steps = append(steps, fullSteps(z, o, now)...)
	}
	for _, s := range steps {
		if err := s(); err != nil {
			z.zw.Close()
			return z.files, err
		}
	}
	return z.files, z.zw.Close()
}

func addKept(z *zipper, o CollectOptions) error {
	for _, n := range sortedFiles(o.KeptDir, ".txt", 0) {
		if err := z.addFile("kept-logs/"+n, filepath.Join(o.KeptDir, n)); err != nil {
			return err
		}
	}
	return nil
}

func fullSteps(z *zipper, o CollectOptions, now func() time.Time) []func() error {
	return []func() error{
		func() error { return z.addText("system.txt", systemSummary(o, now())) },
		func() error { return addKept(z, o) },
		func() error {
			if o.ZomboidDir == "" {
				return nil
			}
			for _, s := range currentLogs(o.ZomboidDir) {
				if err := z.addFile("current/"+filepath.Base(s.path), s.path); err != nil {
					return err
				}
			}
			return z.addFile("mods/default.txt", filepath.Join(o.ZomboidDir, "mods", "default.txt"))
		},
		func() error {
			for i, d := range o.InstallerLogDirs {
				for _, n := range sortedFiles(d, ".log", 10) {
					if err := z.addFile(fmt.Sprintf("installer/%d-%s", i, n), filepath.Join(d, n)); err != nil {
						return err
					}
				}
			}
			return nil
		},
		func() error {
			if o.GameDir == "" {
				return nil
			}
			return z.addFile("ProjectZomboid64.json", filepath.Join(o.GameDir, "ProjectZomboid64.json"))
		},
		func() error { return z.addText("mods.txt", modList(o)) },
		func() error { return addAllLogs(z, o) },
	}
}

func sortedFiles(dir, ext string, last int) []string {
	if dir == "" {
		return nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range ents {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ext) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if last > 0 && len(names) > last {
		names = names[len(names)-last:]
	}
	return names
}

func systemSummary(o CollectOptions, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Collected: %s\n", now.Format(time.RFC3339))
	fmt.Fprintf(&b, "Launcher version: %s\n", o.LauncherVersion)
	fmt.Fprintf(&b, "OS: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	if d := osDescription(); d != "" {
		fmt.Fprintf(&b, "OS release: %s\n", d)
	}
	if ram := totalRAM(); ram > 0 {
		fmt.Fprintf(&b, "RAM: %.1f GiB\n", float64(ram)/(1<<30))
	} else {
		b.WriteString("RAM: unknown\n")
	}
	fmt.Fprintf(&b, "CPUs: %d\n", runtime.NumCPU())
	fmt.Fprintf(&b, "Game dir: %s\n", o.GameDir)
	fmt.Fprintf(&b, "Zomboid dir: %s\n", o.ZomboidDir)
	if o.ZomboidDir != "" {
		if v, err := os.ReadFile(filepath.Join(o.ZomboidDir, "version.txt")); err == nil {
			fmt.Fprintf(&b, "Zomboid version.txt: %s\n", strings.TrimSpace(string(v)))
		}
	}
	return b.String()
}

func osDescription() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "PRETTY_NAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

func EnabledMods(defaultTxt string) []string {
	var out []string
	in := false
	for _, l := range strings.Split(strings.ReplaceAll(defaultTxt, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(l)
		if !in {
			if strings.HasPrefix(t, "mods") && strings.HasSuffix(t, "{") || t == "mods" {
				in = true
			}
			continue
		}
		if strings.HasPrefix(t, "}") {
			break
		}
		if m := reModLine.FindStringSubmatch(l); m != nil {
			out = append(out, strings.TrimSpace(m[1]))
		}
	}
	return out
}

func modList(o CollectOptions) string {
	var b strings.Builder
	var mods []*javamods.ModInfo
	if o.Mods != nil {
		for _, m := range o.Mods.AllCopies() {
			mods = append(mods, javamods.FromMod(m))
		}
	}
	byID := map[string][]*javamods.ModInfo{}
	for _, m := range mods {
		byID[m.ID] = append(byID[m.ID], m)
	}
	enabled := map[string]bool{}
	if o.ZomboidDir != "" {
		if t, err := os.ReadFile(filepath.Join(o.ZomboidDir, "mods", "default.txt")); err == nil {
			ids := EnabledMods(string(t))
			fmt.Fprintf(&b, "Enabled in mods/default.txt (%d):\n", len(ids))
			for _, id := range ids {
				enabled[id] = true
				ver, ws := "?", ""
				if ms := byID[id]; len(ms) > 0 {
					ver, ws = ms[0].Version, ms[0].WorkshopID
				} else {
					ver = "NOT INSTALLED"
				}
				fmt.Fprintf(&b, "  %s\t%s\t%s\n", id, ver, ws)
			}
			b.WriteString("\n")
		}
	}
	fmt.Fprintf(&b, "Installed mods (%d), * = enabled:\n", len(mods))
	sort.SliceStable(mods, func(i, j int) bool { return strings.ToLower(mods[i].ID) < strings.ToLower(mods[j].ID) })
	for _, m := range mods {
		mark := " "
		if enabled[m.ID] {
			mark = "*"
		}
		jar := ""
		if m.JarRel != "" {
			jar = "\tjava:" + m.JarRel
		}
		fmt.Fprintf(&b, "%s %s\t%s\t%s\t%s\t%s%s\n", mark, m.ID, m.Version, m.WorkshopID, m.Source, m.Name, jar)
	}
	return b.String()
}

const (
	keepArchives = 3
	keepDumps    = 3
	keepGC       = 3
)

func addAllLogs(z *zipper, o CollectOptions) error {
	if o.ZomboidDir != "" {
		logs := filepath.Join(o.ZomboidDir, "Logs")
		for _, n := range sortedFiles(logs, "", 0) {
			if err := z.addFile("game-logs/"+n, filepath.Join(logs, n)); err != nil {
				return err
			}
		}
		archives, _ := filepath.Glob(filepath.Join(logs, "logs_*"))
		sort.Strings(archives)
		if len(archives) > keepArchives {
			archives = archives[len(archives)-keepArchives:]
		}
		for _, a := range archives {
			for _, n := range sortedFiles(a, "", 0) {
				if err := z.addFile("game-logs/"+filepath.Base(a)+"/"+n, filepath.Join(a, n)); err != nil {
					return err
				}
			}
		}
		for _, n := range []string{"server-console.txt", "coop-console.txt"} {
			if err := z.addFile("current/"+n, filepath.Join(o.ZomboidDir, n)); err != nil {
				return err
			}
		}
		if err := addNewest(z, "gc-logs/", []string{o.ZomboidDir}, "gc-*.log", keepGC); err != nil {
			return err
		}
	}
	if err := addCrashDumps(z, o); err != nil {
		return err
	}
	if o.ZombieBuddyDir != "" {
		for _, n := range sortedFiles(o.ZombieBuddyDir, ".log", 0) {
			if err := z.addFile("zombiebuddy/"+n, filepath.Join(o.ZombieBuddyDir, n)); err != nil {
				return err
			}
		}
		for _, n := range []string{"mod_approvals.json", "config.json"} {
			if err := z.addFile("zombiebuddy/"+n, filepath.Join(o.ZombieBuddyDir, n)); err != nil {
				return err
			}
		}
	}
	return nil
}

var dumpPatterns = []string{"hs_err_pid*.log", "replay_pid*.log"}

func dumpDirs(o CollectOptions) []string { return []string{o.GameDir, o.HomeDir, o.ZomboidDir} }

func addCrashDumps(z *zipper, o CollectOptions) error {
	for _, pat := range dumpPatterns {
		if err := addNewest(z, "java-crashes/", dumpDirs(o), pat, keepDumps); err != nil {
			return err
		}
	}
	return nil
}

func CrashDumpsSince(o CollectOptions, since time.Time) []string {
	var out []string
	for _, d := range dumpDirs(o) {
		if d == "" {
			continue
		}
		m, _ := filepath.Glob(filepath.Join(d, dumpPatterns[0]))
		for _, p := range m {
			if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() && st.ModTime().After(since) {
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out
}

func addNewest(z *zipper, prefix string, dirs []string, pattern string, keep int) error {
	type hit struct {
		path string
		t    time.Time
	}
	var hits []hit
	for _, d := range dirs {
		if d == "" {
			continue
		}
		m, _ := filepath.Glob(filepath.Join(d, pattern))
		for _, p := range m {
			if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
				hits = append(hits, hit{p, st.ModTime()})
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].t.After(hits[j].t) })
	if len(hits) > keep {
		hits = hits[:keep]
	}
	for _, h := range hits {
		if err := z.addFile(prefix+filepath.Base(h.path), h.path); err != nil {
			return err
		}
	}
	return nil
}
