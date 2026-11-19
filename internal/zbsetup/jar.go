package zbsetup

import (
	"archive/zip"
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kata/pzlauncher/internal/update"
)

const (
	WorkshopItem = "3619862853"
	ModID        = "ZombieBuddy"
	JarName      = "ZombieBuddy.jar"
	NewJarName   = "ZombieBuddy.jar.new"
	NativeName   = "zbNative.dll"
	ZBSName      = "ZombieBuddy.jar.zbs"
	PremainClass = "me.zed_0xff.zombie_buddy.Agent"
	BackupInfix  = ".launcher-bak-"
	maxManifest  = 1 << 20
)

var (
	ErrGameRunning = errors.New("Project Zomboid is running; quit it first")
	ErrNotZB       = errors.New("not a ZombieBuddy jar")
)

type JarInfo struct {
	Version string
	Premain string
}

func ParseManifest(r io.Reader) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), maxManifest)
	last := ""
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, " ") {
			if last != "" {
				out[last] += line[1:]
			}
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		last = strings.TrimSpace(k)
		out[last] = strings.TrimSpace(v)
	}
	return out
}

func ReadJarInfo(path string) (JarInfo, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return JarInfo{}, fmt.Errorf("%s is not a valid jar: %w", filepath.Base(path), err)
	}
	defer zr.Close()
	return jarInfoFromZip(&zr.Reader)
}

func jarInfoFromZip(zr *zip.Reader) (JarInfo, error) {
	for _, f := range zr.File {
		if !strings.EqualFold(f.Name, "META-INF/MANIFEST.MF") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return JarInfo{}, err
		}
		b, err := io.ReadAll(io.LimitReader(rc, maxManifest))
		rc.Close()
		if err != nil {
			return JarInfo{}, err
		}
		m := ParseManifest(bytes.NewReader(b))
		return JarInfo{Version: m["Implementation-Version"], Premain: m["Premain-Class"]}, nil
	}
	return JarInfo{}, errors.New("jar has no META-INF/MANIFEST.MF")
}

func VerifyJar(path string) (JarInfo, error) {
	info, err := ReadJarInfo(path)
	if err != nil {
		return info, err
	}
	if info.Premain != PremainClass {
		return info, fmt.Errorf("%w (Premain-Class %q)", ErrNotZB, info.Premain)
	}
	if info.Version == "" {
		return info, fmt.Errorf("%w (no Implementation-Version)", ErrNotZB)
	}
	return info, nil
}

func Compare(a, b string) int { return update.Compare(a, b) }

func Newer(candidate, installed string) bool { return update.Newer(candidate, installed) }

func JarVersion(path string) string {
	info, err := ReadJarInfo(path)
	if err != nil {
		return ""
	}
	return info.Version
}

func WorkshopLibs(workshopDir string) string {
	return filepath.Join(workshopDir, WorkshopItem, "mods", ModID, "libs")
}

func LibsFromModDir(modDir string) string {
	return filepath.Join(modDir, "libs")
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func sameBytes(a, b string) bool {
	sa, err1 := os.Stat(a)
	sb, err2 := os.Stat(b)
	if err1 != nil || err2 != nil || sa.Size() != sb.Size() {
		return false
	}
	ba, err1 := os.ReadFile(a)
	bb, err2 := os.ReadFile(b)
	return err1 == nil && err2 == nil && bytes.Equal(ba, bb)
}

type JarEnv struct {
	GameDir string
	GOOS    string
	Running func() bool
}

type ReplaceResult struct {
	Jar        string
	Backup     string
	OldVersion string
	NewVersion string
	Staged     bool
	Unchanged  bool
}

func (e JarEnv) jar() string { return filepath.Join(e.GameDir, JarName) }

func (e JarEnv) running() bool { return e.Running != nil && e.Running() }

func InstalledVersion(gameDir string) string { return JarVersion(filepath.Join(gameDir, JarName)) }

func StagedVersion(gameDir string) string { return JarVersion(filepath.Join(gameDir, NewJarName)) }

func backupName(jar, version string) string {
	v := version
	if v == "" {
		v = "unknown"
	}
	base := jar + BackupInfix + v
	p := base
	for n := 2; ; n++ {
		if !isFile(p) || sameBytes(jar, p) {
			return p
		}
		p = fmt.Sprintf("%s-%d", base, n)
	}
}

func copyAtomic(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(name)
		}
	}()
	if _, err := io.Copy(tmp, in); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return err
	}
	if err := os.Rename(name, dst); err != nil {
		return err
	}
	ok = true
	return nil
}

func ReplaceJar(e JarEnv, src string) (ReplaceResult, error) {
	r := ReplaceResult{Jar: e.jar()}
	info, err := VerifyJar(src)
	if err != nil {
		return r, err
	}
	r.NewVersion = info.Version
	if e.running() {
		return r, ErrGameRunning
	}
	if isFile(r.Jar) {
		r.OldVersion = JarVersion(r.Jar)
		if sameBytes(src, r.Jar) {
			r.Unchanged = true
			return r, nil
		}
		r.Backup = backupName(r.Jar, r.OldVersion)
		if !isFile(r.Backup) {
			if err := copyAtomic(r.Jar, r.Backup); err != nil {
				return r, fmt.Errorf("backup %s: %w", r.Backup, err)
			}
		}
	}
	if err := copyAtomic(src, r.Jar); err != nil {
		return r, err
	}
	return r, nil
}

func StageNew(e JarEnv, src string) (ReplaceResult, error) {
	r := ReplaceResult{Jar: filepath.Join(e.GameDir, NewJarName), Staged: true}
	if e.GOOS != "windows" {
		return r, errors.New("ZombieBuddy.jar.new is only picked up by zbNative on Windows")
	}
	info, err := VerifyJar(src)
	if err != nil {
		return r, err
	}
	r.NewVersion = info.Version
	r.OldVersion = JarVersion(e.jar())
	if err := copyAtomic(src, r.Jar); err != nil {
		return r, err
	}
	return r, nil
}

const ReapproveNote = "ZombieBuddy changed: Java mods may need to be approved again on the next start, and ZBBatch recalibrates on the next boot (that first start is slower)."
