package savebak

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/katachinosekai/pzlauncher/internal/paths"
	"github.com/katachinosekai/pzlauncher/internal/play"
)

const stampLayout = "20060102-150405"

var ErrGameRunning = errors.New("Project Zomboid is running. Close the game first: its save files are in use, so a backup or restore now could be inconsistent")

type NoSpaceError struct {
	Dir  string
	Need uint64
	Free uint64
}

func (e *NoSpaceError) Error() string {
	return fmt.Sprintf("not enough free disk space in %s: need about %s, %s free", e.Dir, HumanSize(int64(e.Need)), HumanSize(int64(e.Free)))
}

type Progress struct {
	Phase      string
	Files      int
	TotalFiles int
	Bytes      int64
	TotalBytes int64
}

type Backup struct {
	Path  string
	Mode  string
	World string
	Time  time.Time
	Label string
	Size  int64
}

func (b Backup) Auto() bool { return b.Label == AutoLabel || b.Label == SafetyLabel }

type SaveBackups struct {
	Mode    string
	World   string
	SaveDir string
	Exists  bool
	Backups []Backup
	Size    int64
}

type Manager struct {
	SavesDir  string
	Root      string
	Running   func() (bool, error)
	FreeSpace func(dir string) (uint64, error)
	Now       func() time.Time
}

func NewManager(cfg Config) (*Manager, error) {
	z, err := paths.Zomboid()
	if err != nil {
		return nil, err
	}
	root, err := cfg.RootDir()
	if err != nil {
		return nil, err
	}
	return &Manager{
		SavesDir:  filepath.Join(z, "Saves"),
		Root:      root,
		Running:   play.IsRunning,
		FreeSpace: diskFree,
		Now:       time.Now,
	}, nil
}

func BeforeLaunch(ctx context.Context, save play.Save, progress func(Progress)) error {
	p, err := DefaultConfigPath()
	if err != nil {
		return err
	}
	cfg, err := LoadConfig(p)
	if err != nil {
		return fmt.Errorf("read %s: %w", p, err)
	}
	if !cfg.AutoFor(save.Mode, save.World) {
		return nil
	}
	m, err := NewManager(cfg)
	if err != nil {
		return err
	}
	if save.Dir != "" {
		m.SavesDir = filepath.Dir(filepath.Dir(save.Dir))
	}
	_, err = m.AutoBackup(ctx, save.Mode, save.World, cfg.KeepFor(save.Mode, save.World), progress)
	return err
}

func (m *Manager) AutoBackup(ctx context.Context, mode, world string, keep int, progress func(Progress)) (Backup, error) {
	b, err := m.Backup(ctx, mode, world, AutoLabel, progress)
	if err != nil {
		return b, err
	}
	m.Prune(mode, world, keep)
	return b, nil
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Manager) checkRunning() error {
	if m.Running == nil {
		return nil
	}
	r, err := m.Running()
	if err != nil {
		return fmt.Errorf("could not check whether Project Zomboid is running: %w", err)
	}
	if r {
		return ErrGameRunning
	}
	return nil
}

func (m *Manager) checkFree(dir string, need uint64) error {
	if m.FreeSpace == nil {
		return nil
	}
	d := dir
	for {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			break
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	free, err := m.FreeSpace(d)
	if err != nil {
		return fmt.Errorf("could not read free disk space of %s: %w", d, err)
	}
	if free < need {
		return &NoSpaceError{Dir: d, Need: need, Free: free}
	}
	return nil
}

func validName(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, "/\\\x00") && !strings.ContainsAny(s, "\r\n")
}

func validSave(mode, world string) error {
	if !validName(mode) || !validName(world) {
		return fmt.Errorf("invalid save name %q / %q", mode, world)
	}
	return nil
}

func (m *Manager) SaveDir(mode, world string) string {
	return filepath.Join(m.SavesDir, mode, world)
}

func (m *Manager) BackupDir(mode, world string) string {
	return filepath.Join(m.Root, mode, world)
}

func (m *Manager) checkRoot() error {
	if m.Root == "" {
		return errors.New("no backup folder set")
	}
	return CheckRoot(m.Root, m.SavesDir)
}

func CheckRoot(root, savesDir string) error {
	if !filepath.IsAbs(root) {
		return fmt.Errorf("backup folder must be an absolute path: %s", root)
	}
	r, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	s, err := filepath.Abs(savesDir)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(s, r)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("backup folder %s is inside the Saves folder", root)
	}
	return nil
}

func CleanLabel(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.TrimSpace(s) {
		ok := unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.'
		if !ok {
			if !dash {
				b.WriteRune('-')
				dash = true
			}
			continue
		}
		b.WriteRune(r)
		dash = false
	}
	out := strings.Trim(b.String(), "-.")
	if rs := []rune(out); len(rs) > 48 {
		out = strings.Trim(string(rs[:48]), "-.")
	}
	return out
}

func fileName(world string, t time.Time, label string) string {
	n := world + "-" + t.Format(stampLayout)
	if label != "" {
		n += "-" + label
	}
	return n + ".zip"
}

func parseName(world, name string) (time.Time, string, bool) {
	if !strings.HasSuffix(strings.ToLower(name), ".zip") || !strings.HasPrefix(name, world+"-") {
		return time.Time{}, "", false
	}
	rest := name[len(world)+1 : len(name)-4]
	if len(rest) < len(stampLayout) {
		return time.Time{}, "", false
	}
	t, err := time.ParseInLocation(stampLayout, rest[:len(stampLayout)], time.Local)
	if err != nil {
		return time.Time{}, "", false
	}
	rest = rest[len(stampLayout):]
	if rest == "" {
		return t, "", true
	}
	if rest[0] != '-' {
		return time.Time{}, "", false
	}
	return t, rest[1:], true
}

func (m *Manager) freePath(dir, world string, t time.Time, label string) string {
	for i := 0; ; i++ {
		p := filepath.Join(dir, fileName(world, t.Add(time.Duration(i)*time.Second), label))
		if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) {
			return p
		}
	}
}

func (m *Manager) List(mode, world string) ([]Backup, error) {
	if err := validSave(mode, world); err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(m.BackupDir(mode, world))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Backup
	for _, e := range ents {
		if !e.Type().IsRegular() {
			continue
		}
		t, label, ok := parseName(world, e.Name())
		if !ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Backup{
			Path: filepath.Join(m.BackupDir(mode, world), e.Name()),
			Mode: mode, World: world, Time: t, Label: label, Size: info.Size(),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Time.Equal(out[j].Time) {
			return out[i].Path > out[j].Path
		}
		return out[i].Time.After(out[j].Time)
	})
	return out, nil
}

func (m *Manager) Get(mode, world string) (SaveBackups, error) {
	g := SaveBackups{Mode: mode, World: world, SaveDir: m.SaveDir(mode, world)}
	if st, err := os.Stat(g.SaveDir); err == nil && st.IsDir() {
		g.Exists = true
	}
	bs, err := m.List(mode, world)
	if err != nil {
		return g, err
	}
	g.Backups = bs
	for _, b := range bs {
		g.Size += b.Size
	}
	return g, nil
}

func (m *Manager) ListAll() ([]SaveBackups, error) {
	keys := map[string][2]string{}
	var order []string
	add := func(mode, world string) {
		k := mode + "/" + world
		if _, ok := keys[k]; !ok {
			keys[k] = [2]string{mode, world}
			order = append(order, k)
		}
	}
	saves, err := (&play.Env{ZomboidDir: filepath.Dir(m.SavesDir)}).ListSaves()
	if err != nil {
		return nil, err
	}
	for _, s := range saves {
		add(s.Mode, s.World)
	}
	modes, err := os.ReadDir(m.Root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, md := range modes {
		if !md.IsDir() || !validName(md.Name()) {
			continue
		}
		worlds, err := os.ReadDir(filepath.Join(m.Root, md.Name()))
		if err != nil {
			continue
		}
		for _, w := range worlds {
			if w.IsDir() && validName(w.Name()) {
				add(md.Name(), w.Name())
			}
		}
	}
	var out []SaveBackups
	for _, k := range order {
		g, err := m.Get(keys[k][0], keys[k][1])
		if err != nil {
			return nil, err
		}
		if !g.Exists && len(g.Backups) == 0 {
			continue
		}
		out = append(out, g)
	}
	return out, nil
}

func (m *Manager) Orphans() ([]SaveBackups, error) {
	all, err := m.ListAll()
	if err != nil {
		return nil, err
	}
	var out []SaveBackups
	for _, g := range all {
		if !g.Exists && len(g.Backups) > 0 {
			out = append(out, g)
		}
	}
	return out, nil
}

func (m *Manager) TotalSize() (int64, error) {
	all, err := m.ListAll()
	if err != nil {
		return 0, err
	}
	var n int64
	for _, g := range all {
		n += g.Size
	}
	return n, nil
}

func (m *Manager) Delete(b Backup) error {
	if err := m.owns(b); err != nil {
		return err
	}
	return os.Remove(b.Path)
}

func (m *Manager) Rename(b Backup, label string) (Backup, error) {
	if err := m.owns(b); err != nil {
		return b, err
	}
	label = CleanLabel(label)
	if label == b.Label {
		return b, nil
	}
	dir := filepath.Dir(b.Path)
	np := filepath.Join(dir, fileName(b.World, b.Time, label))
	if _, err := os.Lstat(np); err == nil {
		return b, fmt.Errorf("a backup named %s already exists", filepath.Base(np))
	}
	if err := os.Rename(b.Path, np); err != nil {
		return b, err
	}
	b.Path, b.Label = np, label
	return b, nil
}

func (m *Manager) owns(b Backup) error {
	if err := validSave(b.Mode, b.World); err != nil {
		return err
	}
	if filepath.Dir(b.Path) != m.BackupDir(b.Mode, b.World) {
		return fmt.Errorf("%s is not in the backup folder", b.Path)
	}
	if _, _, ok := parseName(b.World, filepath.Base(b.Path)); !ok {
		return fmt.Errorf("%s is not a save backup", b.Path)
	}
	return nil
}

func (m *Manager) Prune(mode, world string, keep int) (int, int64, error) {
	if keep < 0 {
		keep = 0
	}
	bs, err := m.List(mode, world)
	if err != nil {
		return 0, 0, err
	}
	n, freed, kept := 0, int64(0), 0
	var firstErr error
	for _, b := range bs {
		if !b.Auto() {
			continue
		}
		kept++
		if kept <= keep {
			continue
		}
		if err := os.Remove(b.Path); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		n++
		freed += b.Size
	}
	return n, freed, firstErr
}

func (m *Manager) PruneAll(cfg Config) (int, int64, error) {
	all, err := m.ListAll()
	if err != nil {
		return 0, 0, err
	}
	n, freed := 0, int64(0)
	var firstErr error
	for _, g := range all {
		c, f, err := m.Prune(g.Mode, g.World, cfg.KeepFor(g.Mode, g.World))
		n += c
		freed += f
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return n, freed, firstErr
}

func Thumb(b Backup) ([]byte, error) {
	zr, err := zip.OpenReader(b.Path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "thumb.png" {
			continue
		}
		if f.UncompressedSize64 > 16<<20 {
			return nil, errors.New("thumb.png too large")
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	return nil, nil
}

func HumanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func (g SaveBackups) Save() play.Save {
	return play.Save{Mode: g.Mode, World: g.World, Dir: g.SaveDir}
}

func Default() (*Manager, Config, error) {
	p, err := DefaultConfigPath()
	if err != nil {
		return nil, Config{}, err
	}
	cfg, err := LoadConfig(p)
	if err != nil {
		return nil, cfg, fmt.Errorf("read %s: %w", p, err)
	}
	m, err := NewManager(cfg)
	return m, cfg, err
}

func Orphans() ([]SaveBackups, error) {
	m, _, err := Default()
	if err != nil {
		return nil, err
	}
	return m.Orphans()
}
