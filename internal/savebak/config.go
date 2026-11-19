package savebak

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const (
	DefaultKeepAuto = 5
	AutoLabel       = "auto"
	SafetyLabel     = "before-restore"
)

type SaveSettings struct {
	Auto *bool `json:"auto,omitempty"`
	Keep *int  `json:"keep,omitempty"`
}

type Config struct {
	Root     string                  `json:"root,omitempty"`
	Auto     bool                    `json:"autoBeforeLaunch"`
	KeepAuto int                     `json:"keepAuto"`
	Saves    map[string]SaveSettings `json:"saves,omitempty"`
}

func DefaultConfig() Config {
	return Config{Auto: true, KeepAuto: DefaultKeepAuto}
}

func configDir() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "PZLauncher"), nil
}

func DefaultConfigPath() (string, error) {
	d, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "backups.json"), nil
}

func DefaultRoot() (string, error) {
	d, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "save-backups"), nil
}

func LoadConfig(path string) (Config, error) {
	c := DefaultConfig()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return DefaultConfig(), err
	}
	if c.KeepAuto < 1 {
		c.KeepAuto = DefaultKeepAuto
	}
	return c, nil
}

func SaveConfig(path string, c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c Config) RootDir() (string, error) {
	if c.Root != "" {
		return c.Root, nil
	}
	return DefaultRoot()
}

func (c Config) AutoFor(mode, world string) bool {
	if s, ok := c.Saves[mode+"/"+world]; ok && s.Auto != nil {
		return *s.Auto
	}
	return c.Auto
}

func (c Config) KeepFor(mode, world string) int {
	if s, ok := c.Saves[mode+"/"+world]; ok && s.Keep != nil && *s.Keep > 0 {
		return *s.Keep
	}
	if c.KeepAuto < 1 {
		return DefaultKeepAuto
	}
	return c.KeepAuto
}

func (c *Config) SetSave(mode, world string, auto bool, keep int) {
	if c.Saves == nil {
		c.Saves = map[string]SaveSettings{}
	}
	s := SaveSettings{}
	if auto != c.Auto {
		s.Auto = &auto
	}
	if keep > 0 && keep != c.KeepAuto {
		s.Keep = &keep
	}
	key := mode + "/" + world
	if s.Auto == nil && s.Keep == nil {
		delete(c.Saves, key)
		return
	}
	c.Saves[key] = s
}
