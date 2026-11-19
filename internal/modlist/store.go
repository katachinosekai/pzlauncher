package modlist

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrGameRunning     = errors.New("Project Zomboid is running: close the game before changing mods (it rewrites these files on exit)")
	ErrChangedOnDisk   = errors.New("the file was changed by something else since it was loaded; reload first")
	ErrUnsupportedFile = errors.New("default.txt has no VERSION = 1 header; the game ignores it, so the launcher will not edit it")
)

type Store struct {
	ZomboidDir string
	Running    func() (bool, error)
	Now        func() time.Time
}

func (s *Store) DefaultPath() string { return filepath.Join(s.ZomboidDir, "mods", "default.txt") }
func (s *Store) PresetsPath() string {
	return filepath.Join(s.ZomboidDir, "Lua", "pz_modlist_settings.cfg")
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Store) checkRunning() error {
	if s.Running == nil {
		return nil
	}
	run, err := s.Running()
	if err != nil {
		return fmt.Errorf("could not check whether the game is running: %w", err)
	}
	if run {
		return ErrGameRunning
	}
	return nil
}

func readOptional(path string) (string, bool, error) {
	d, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(d), true, nil
}

func (s *Store) LoadDefault() (*DefaultFile, error) {
	raw, ok, err := readOptional(s.DefaultPath())
	if err != nil {
		return nil, err
	}
	if !ok {
		return NewDefault(), nil
	}
	return ParseDefault(raw), nil
}

func (s *Store) LoadPresets() (*Presets, error) {
	raw, _, err := readOptional(s.PresetsPath())
	if err != nil {
		return nil, err
	}
	return ParsePresets(raw), nil
}

const backupTag = ".launcher-bak-"

func (s *Store) Backup(path string) (string, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	now := s.now()
	day := filepath.Base(path) + backupTag + now.Format("20060102") + "-"
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), day) {
			return "", nil
		}
	}
	dst := filepath.Join(filepath.Dir(path), day+now.Format("150405"))
	d, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if err := writeAtomic(dst, d); err != nil {
		return "", err
	}
	return dst, nil
}

func writeAtomic(path string, data []byte) error {
	mode := fs.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	_ = os.Chmod(name, mode)
	return os.Rename(name, path)
}

type SaveResult struct {
	Written bool
	Backup  string
}

func (s *Store) SaveDefault(f *DefaultFile) (SaveResult, error) {
	var res SaveResult
	if err := s.checkRunning(); err != nil {
		return res, err
	}
	cur, ok, err := readOptional(s.DefaultPath())
	if err != nil {
		return res, err
	}
	if cur != f.Raw() {
		return res, ErrChangedOnDisk
	}
	if f.Raw() != "" && f.Version != 1 {
		return res, ErrUnsupportedFile
	}
	out := f.Render()
	if ok && out == cur {
		return res, nil
	}
	if res.Backup, err = s.Backup(s.DefaultPath()); err != nil {
		return res, fmt.Errorf("backup failed, nothing written: %w", err)
	}
	if err := writeAtomic(s.DefaultPath(), []byte(out)); err != nil {
		return res, err
	}
	res.Written = true
	f.Rebase()
	return res, nil
}

func (s *Store) UpdatePresets(edit func(p *Presets) error) (*Presets, SaveResult, error) {
	var res SaveResult
	if err := s.checkRunning(); err != nil {
		return nil, res, err
	}
	p, err := s.LoadPresets()
	if err != nil {
		return nil, res, err
	}
	before := p.Render()
	if err := edit(p); err != nil {
		return nil, res, err
	}
	out := p.Render()
	if out == before {
		return p, res, nil
	}
	if res.Backup, err = s.Backup(s.PresetsPath()); err != nil {
		return nil, res, fmt.Errorf("backup failed, nothing written: %w", err)
	}
	if err := writeAtomic(s.PresetsPath(), []byte(out)); err != nil {
		return nil, res, err
	}
	res.Written = true
	return p, res, nil
}

func (s *Store) SavePreset(name string, ids []string) (*Presets, SaveResult, error) {
	return s.UpdatePresets(func(p *Presets) error { return p.Set(name, ids) })
}

func (s *Store) DeletePreset(name string) (*Presets, SaveResult, error) {
	return s.UpdatePresets(func(p *Presets) error {
		if !p.Delete(name) {
			return fmt.Errorf("preset %q not found", name)
		}
		return nil
	})
}

func (s *Store) SetFavorite(id string, fav bool) (*Presets, SaveResult, error) {
	return s.UpdatePresets(func(p *Presets) error {
		var favs []string
		for _, f := range p.Favs {
			if f != id {
				favs = append(favs, f)
			}
		}
		if fav {
			favs = append(favs, id)
		}
		p.SetFavs(favs)
		return nil
	})
}
