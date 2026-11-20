package diag

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/katachinosekai/pzlauncher/internal/paths"
)

const KeepCount = 10

type source struct {
	kind string
	path string
}

func LogsDir() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg, "PZLauncher", "logs"), nil
}

func currentLogs(zdir string) []source {
	var out []source
	for _, n := range []string{"console.txt", "coop-console.txt"} {
		p := filepath.Join(zdir, n)
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			out = append(out, source{strings.TrimSuffix(n, ".txt"), p})
		}
	}
	if p := newestDebugLog(filepath.Join(zdir, "Logs")); p != "" {
		out = append(out, source{"DebugLog", p})
	}
	return out
}

func newestDebugLog(dir string) string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	best := ""
	var bestT int64
	for _, e := range ents {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), "_DebugLog.txt") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if t := info.ModTime().UnixNano(); best == "" || t > bestT {
			best, bestT = filepath.Join(dir, e.Name()), t
		}
	}
	return best
}

func KeepLastLog() ([]string, error) {
	z, err := paths.Zomboid()
	if err != nil {
		return nil, err
	}
	dest, err := LogsDir()
	if err != nil {
		return nil, err
	}
	return Keep(z, dest, KeepCount)
}

func Keep(zdir, dest string, keep int) ([]string, error) {
	srcs := currentLogs(zdir)
	if len(srcs) == 0 {
		return nil, nil
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, err
	}
	var kept []string
	var firstErr error
	for _, s := range srcs {
		st, err := os.Stat(s.path)
		if err != nil || st.Size() == 0 {
			continue
		}
		name := s.kind + "-" + st.ModTime().Format("20060102-150405") + ".txt"
		dst := filepath.Join(dest, name)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := copyFile(s.path, dst, st); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		kept = append(kept, dst)
		rotate(dest, s.kind, keep)
	}
	return kept, firstErr
}

func keptOf(dest, kind string) []string {
	ents, err := os.ReadDir(dest)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range ents {
		n := e.Name()
		rest, ok := strings.CutPrefix(n, kind+"-")
		if !ok || !strings.HasSuffix(n, ".txt") || len(rest) != len("20060102-150405.txt") || rest[0] < '0' || rest[0] > '9' {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func rotate(dest, kind string, keep int) {
	if keep <= 0 {
		return
	}
	names := keptOf(dest, kind)
	for len(names) > keep {
		os.Remove(filepath.Join(dest, names[0]))
		names = names[1:]
	}
}

func copyFile(src, dst string, st os.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	os.Chtimes(tmp, st.ModTime(), st.ModTime())
	return os.Rename(tmp, dst)
}
