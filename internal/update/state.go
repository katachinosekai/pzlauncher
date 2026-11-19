package update

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type State struct {
	PackVersion string    `json:"pack_version"`
	InstalledAt time.Time `json:"installed_at,omitempty"`
}

func ConfigDir() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "PZLauncher"), nil
}

func StatePath() (string, error) {
	d, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "state.json"), nil
}

func LoadStateFrom(p string) (State, error) {
	var s State
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(b, &s)
	return s, err
}

func SaveStateTo(p string, s State) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func LoadState() (State, error) {
	p, err := StatePath()
	if err != nil {
		return State{}, err
	}
	return LoadStateFrom(p)
}

func SaveState(s State) error {
	p, err := StatePath()
	if err != nil {
		return err
	}
	return SaveStateTo(p, s)
}

func MarkPackInstalled(version string) error {
	return SaveState(State{PackVersion: version, InstalledAt: time.Now().UTC()})
}
