package play

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Nicknames struct {
	path  string
	names map[string]string
}

func (s Save) Key() string { return s.Mode + "/" + s.World }

func NicknamesPath() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "PZLauncher", "nicknames.json"), nil
}

func LoadNicknames(path string) (*Nicknames, error) {
	n := &Nicknames{path: path, names: map[string]string{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return n, nil
	}
	if err != nil {
		return n, err
	}
	if err := json.Unmarshal(b, &n.names); err != nil {
		return &Nicknames{path: path, names: map[string]string{}}, err
	}
	return n, nil
}

func (n *Nicknames) Get(s Save) string { return n.names[s.Key()] }

func (n *Nicknames) Set(s Save, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		delete(n.names, s.Key())
	} else {
		n.names[s.Key()] = name
	}
	if err := os.MkdirAll(filepath.Dir(n.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(n.names, "", "  ")
	if err != nil {
		return err
	}
	tmp := n.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, n.path)
}

func (n *Nicknames) Display(s Save) string {
	if nick := n.Get(s); nick != "" {
		return nick + "  (" + s.Label() + ")"
	}
	return s.Label()
}
