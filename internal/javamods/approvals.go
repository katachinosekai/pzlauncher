package javamods

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kata/pzlauncher/internal/install"
)

type Status int

const (
	NeverSeen Status = iota
	Approved
	ApprovedOlder
	Denied
	DeniedOlder
	NoJar
)

func (s Status) String() string {
	switch s {
	case Approved:
		return "approved"
	case ApprovedOlder:
		return "approved for an older jar (mod updated, needs re-approval)"
	case Denied:
		return "denied"
	case DeniedOlder:
		return "denied for an older jar (will ask again)"
	case NoJar:
		return "jar missing"
	}
	return "never seen (ZombieBuddy will ask)"
}

type Entry struct {
	ID         string
	WorkshopID string
	JarHash    string
	Decision   bool
}

func DefaultApprovalsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".zombie_buddy", "mod_approvals.json"), nil
}

func ParseEntries(data []byte) ([]Entry, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	var top struct {
		Mods []json.RawMessage `json:"mods"`
	}
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("mod_approvals.json: %w", err)
	}
	var out []Entry
	for _, raw := range top.Mods {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var e struct {
			ID         string          `json:"id"`
			WorkshopID json.RawMessage `json:"workshop_id"`
			JarHash    string          `json:"jar_hash"`
			Decision   bool            `json:"decision"`
		}
		if err := dec.Decode(&e); err != nil {
			continue
		}
		ws := strings.Trim(string(bytes.TrimSpace(e.WorkshopID)), `"`)
		if ws == "null" {
			ws = ""
		}
		out = append(out, Entry{ID: e.ID, WorkshopID: ws, JarHash: strings.ToLower(e.JarHash), Decision: e.Decision})
	}
	return out, nil
}

func StatusOf(entries []Entry, id, hash string) Status {
	if hash == "" {
		return NoJar
	}
	exact, found := false, false
	older, olderApproved := false, false
	for _, e := range entries {
		if !strings.EqualFold(e.ID, id) {
			continue
		}
		if strings.EqualFold(e.JarHash, hash) {
			found, exact = true, e.Decision
			continue
		}
		older = true
		if e.Decision {
			olderApproved = true
		}
	}
	switch {
	case found && exact:
		return Approved
	case found:
		return Denied
	case olderApproved:
		return ApprovedOlder
	case older:
		return DeniedOlder
	}
	return NeverSeen
}

var ErrGameRunning = errors.New("Project Zomboid is running; close the game before changing approvals")

type Store struct {
	Path    string
	Running func() bool
	Now     func() time.Time
}

func DefaultStore() (*Store, error) {
	p, err := DefaultApprovalsPath()
	if err != nil {
		return nil, err
	}
	return &Store{Path: p, Running: install.GameRunning, Now: time.Now}, nil
}

func (s *Store) Load() ([]Entry, error) {
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseEntries(b)
}

func (s *Store) Set(m *JavaMod, decision bool) (bool, error) {
	if m.JarHash == "" {
		return false, errors.New("no jar hash for " + m.ID)
	}
	if s.Running != nil && s.Running() {
		return false, ErrGameRunning
	}
	var existing []byte
	mode := os.FileMode(0o644)
	if st, err := os.Stat(s.Path); err == nil {
		mode = st.Mode().Perm()
		if existing, err = os.ReadFile(s.Path); err != nil {
			return false, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	out, changed, err := install.SetApprovalDecision(existing, install.ApprovalDecision{
		ID: m.ID, WorkshopID: m.WorkshopID, JarHash: m.JarHash, Decision: decision,
	})
	if err != nil || !changed {
		return false, err
	}
	if existing != nil {
		if err := s.backup(existing, mode); err != nil {
			return false, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return false, err
	}
	return true, atomicWrite(s.Path, out, mode)
}

func (s *Store) backup(data []byte, mode os.FileMode) error {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	bk := s.Path + ".launcher-bak-" + now().Format("20060102")
	if _, err := os.Stat(bk); err == nil {
		return nil
	}
	return atomicWrite(bk, data, mode)
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	return nil
}

type JavaMod struct {
	*ModInfo
	Jar     string
	JarHash string
	HasZBS  bool
	Status  Status
	Err     error
}

func (m *JavaMod) ShortHash() string {
	if len(m.JarHash) > 12 {
		return m.JarHash[:12]
	}
	return m.JarHash
}

func List(roots []string, entries []Entry) []*JavaMod {
	var out []*JavaMod
	for _, mi := range ScanAll(roots) {
		if mi.JarRel == "" {
			continue
		}
		jm := &JavaMod{ModInfo: mi, Jar: mi.JarPath()}
		if h, err := HashFile(jm.Jar); err == nil {
			jm.JarHash = h
		} else {
			jm.Err = err
		}
		if st, err := os.Stat(jm.Jar + ".zbs"); err == nil && !st.IsDir() {
			jm.HasZBS = true
		}
		jm.Status = StatusOf(entries, jm.ID, jm.JarHash)
		out = append(out, jm)
	}
	return out
}

func (m *JavaMod) Refresh(entries []Entry) { m.Status = StatusOf(entries, m.ID, m.JarHash) }
