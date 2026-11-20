package gamecfg

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/katachinosekai/pzlauncher/internal/paths"
)

var ErrGameRunning = errors.New("Project Zomboid is running; quit the game before saving settings")

var ErrNothingRemembered = errors.New("no memory settings remembered yet; save them once first")

type Env struct {
	GameDir    string
	ZomboidDir string
	ConfigDir  string
	Running    func() bool
	Now        func() time.Time
}

func DefaultEnv() (Env, []error) {
	var errs []error
	e := Env{Running: GameRunning, Now: time.Now}
	root, err := paths.SteamRoot()
	if err != nil {
		errs = append(errs, err)
	} else {
		if gd, err := paths.GameDir(root); err != nil {
			errs = append(errs, err)
		} else {
			e.GameDir = gd
		}
	}
	if z, err := paths.Zomboid(); err != nil {
		errs = append(errs, err)
	} else {
		e.ZomboidDir = z
	}
	if d, err := ConfigDir(); err != nil {
		errs = append(errs, err)
	} else {
		e.ConfigDir = d
	}
	return e, errs
}

func (e Env) JSONPath() string     { return filepath.Join(e.GameDir, "ProjectZomboid64.json") }
func (e Env) OptionsPath() string  { return filepath.Join(e.ZomboidDir, "options.ini") }
func (e Env) RememberPath() string { return filepath.Join(e.ConfigDir, "settings.json") }

func (e Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e Env) checkRunning() error {
	if e.Running != nil && e.Running() {
		return ErrGameRunning
	}
	return nil
}

func BackupPath(path string, t time.Time) string {
	return path + ".launcher-bak-" + t.Format("20060102")
}

func backupOnce(path string, orig []byte, mode fs.FileMode, t time.Time) error {
	bp := BackupPath(path, t)
	f, err := os.OpenFile(bp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("backup %s: %w", bp, err)
	}
	if _, err := f.Write(orig); err != nil {
		f.Close()
		os.Remove(bp)
		return fmt.Errorf("backup %s: %w", bp, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(bp)
		return fmt.Errorf("backup %s: %w", bp, err)
	}
	return nil
}

func WriteAtomic(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, mode.Perm()); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func (e Env) replaceFile(path string, orig, data []byte) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if err := backupOnce(path, orig, st.Mode(), e.now()); err != nil {
		return err
	}
	return WriteAtomic(path, data, st.Mode())
}

func (e Env) LoadJVM() (JVMInfo, error) {
	b, err := os.ReadFile(e.JSONPath())
	if err != nil {
		return JVMInfo{}, err
	}
	return ReadJVM(string(b))
}

func (e Env) SaveJVM(want JVMWant) (bool, []string, error) {
	if err := e.checkRunning(); err != nil {
		return false, nil, err
	}
	p := e.JSONPath()
	orig, err := os.ReadFile(p)
	if err != nil {
		return false, nil, err
	}
	out, notes, err := ApplyJVM(string(orig), want)
	if err != nil {
		return false, nil, err
	}
	changed := out != string(orig)
	if changed {
		if err := e.checkRunning(); err != nil {
			return false, nil, err
		}
		if err := e.replaceFile(p, orig, []byte(out)); err != nil {
			return false, notes, err
		}
	}
	info, err := ReadJVM(out)
	if err == nil {
		if rerr := e.Remember(info.Want()); rerr != nil {
			notes = append(notes, "could not remember the settings: "+rerr.Error())
		}
	}
	return changed, notes, nil
}

type Settings struct {
	XmxMB     int             `json:"xmxMB,omitempty"`
	Flags     map[string]bool `json:"flags,omitempty"`
	SoftMaxMB int             `json:"softMaxMB,omitempty"`
	Debug     bool            `json:"debug"`
}

func ConfigDir() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "PZLauncher"), nil
}

func LoadSettings(path string) (Settings, error) {
	var s Settings
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return Settings{}, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

func SaveSettings(path string, s Settings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return WriteAtomic(path, append(b, '\n'), 0o644)
}

func DebugEnabled() bool {
	d, err := ConfigDir()
	if err != nil {
		return false
	}
	s, err := LoadSettings(filepath.Join(d, "settings.json"))
	return err == nil && s.Debug
}

func (e Env) Debug() (bool, error) {
	s, err := LoadSettings(e.RememberPath())
	return s.Debug, err
}

func (e Env) SetDebug(on bool) error {
	s, err := LoadSettings(e.RememberPath())
	if err != nil {
		return err
	}
	s.Debug = on
	return SaveSettings(e.RememberPath(), s)
}

func (e Env) Remember(w JVMWant) error {
	s, err := LoadSettings(e.RememberPath())
	if err != nil {
		s = Settings{}
	}
	s.XmxMB, s.Flags, s.SoftMaxMB = w.XmxMB, w.Flags, w.SoftMaxMB
	return SaveSettings(e.RememberPath(), s)
}

func (e Env) Remembered() (JVMWant, error) {
	s, err := LoadSettings(e.RememberPath())
	if err != nil {
		return JVMWant{}, err
	}
	if s.XmxMB == 0 {
		return JVMWant{}, ErrNothingRemembered
	}
	return JVMWant{XmxMB: s.XmxMB, Flags: s.Flags, SoftMaxMB: s.SoftMaxMB}, nil
}

func (e Env) Reapply() (bool, []string, error) {
	w, err := e.Remembered()
	if err != nil {
		return false, nil, err
	}
	return e.SaveJVM(w)
}

func (e Env) LoadOptions() (*INI, GameOptions, error) {
	b, err := os.ReadFile(e.OptionsPath())
	if err != nil {
		return nil, GameOptions{}, err
	}
	ini := ParseINI(b)
	return ini, ReadOptions(ini), nil
}

func (e Env) SaveOptions(o GameOptions) (int, error) {
	if err := e.checkRunning(); err != nil {
		return 0, err
	}
	p := e.OptionsPath()
	orig, err := os.ReadFile(p)
	if err != nil {
		return 0, err
	}
	ini := ParseINI(orig)
	n, err := ApplyOptions(ini, o)
	if err != nil || n == 0 {
		return 0, err
	}
	if err := e.checkRunning(); err != nil {
		return 0, err
	}
	return n, e.replaceFile(p, orig, ini.Bytes())
}
