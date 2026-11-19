package play

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/kata/pzlauncher/internal/paths"
	_ "modernc.org/sqlite"
)

const GameURL = "steam://rungameid/" + paths.AppID

var ErrRunning = errors.New("Project Zomboid is already running")

type Save struct {
	Mode     string
	World    string
	Dir      string
	Modified time.Time
}

func (s Save) Label() string { return s.Mode + " / " + s.World }

func (s Save) Thumb() string {
	p := filepath.Join(s.Dir, "thumb.png")
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p
	}
	return ""
}

type Account struct {
	Username      string
	PasswordSaved bool
}

type Server struct {
	ID       int64
	Name     string
	IP       string
	Port     int
	Accounts []Account
}

func (s Server) Addr() string { return fmt.Sprintf("%s:%d", s.IP, s.Port) }

type Kind int

const (
	KindLatest Kind = iota
	KindSave
	KindServer
)

type Target struct {
	Kind    Kind
	Mode    string
	World   string
	IP      string
	Port    int
	Account string
}

func Latest() Target           { return Target{Kind: KindLatest} }
func SaveTarget(s Save) Target { return Target{Kind: KindSave, Mode: s.Mode, World: s.World} }
func ServerTarget(s Server, account string) Target {
	return Target{Kind: KindServer, IP: s.IP, Port: s.Port, Account: account}
}

type Env struct {
	ZomboidDir string
	OpenURL    func(string) error
	Running    func() (bool, error)
}

func Default() (*Env, error) {
	z, err := paths.Zomboid()
	if err != nil {
		return nil, err
	}
	return &Env{ZomboidDir: z, OpenURL: openURL, Running: gameRunning}, nil
}

func ListSaves() ([]Save, error) {
	e, err := Default()
	if err != nil {
		return nil, err
	}
	return e.ListSaves()
}

func ListServers() ([]Server, error) {
	e, err := Default()
	if err != nil {
		return nil, err
	}
	return e.ListServers()
}

func Launch(t Target) error {
	e, err := Default()
	if err != nil {
		return err
	}
	return e.Launch(t)
}

func (e *Env) savesDir() string { return filepath.Join(e.ZomboidDir, "Saves") }
func (e *Env) luaDir() string   { return filepath.Join(e.ZomboidDir, "Lua") }
func (e *Env) dbPath() string   { return filepath.Join(e.ZomboidDir, "db", "ServerListSteam.db") }

func (e *Env) ListSaves() ([]Save, error) {
	modes, err := os.ReadDir(e.savesDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var saves []Save
	for _, m := range modes {
		if !m.IsDir() || strings.EqualFold(m.Name(), "Multiplayer") || strings.ContainsAny(m.Name(), " \t") {
			continue
		}
		modeDir := filepath.Join(e.savesDir(), m.Name())
		worlds, err := os.ReadDir(modeDir)
		if err != nil {
			continue
		}
		for _, w := range worlds {
			if !w.IsDir() || strings.ContainsAny(w.Name(), "\r\n") {
				continue
			}
			dir := filepath.Join(modeDir, w.Name())
			st, err := os.Stat(dir)
			if err != nil {
				continue
			}
			saves = append(saves, Save{Mode: m.Name(), World: w.Name(), Dir: dir, Modified: st.ModTime()})
		}
	}
	sort.SliceStable(saves, func(i, j int) bool { return saves[i].Modified.After(saves[j].Modified) })
	return saves, nil
}

func sqliteURI(p string) string {
	abs, err := filepath.Abs(p)
	if err == nil {
		p = abs
	}
	s := filepath.ToSlash(p)
	if runtime.GOOS == "windows" && !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	u := url.URL{Scheme: "file", Path: s, RawQuery: "mode=ro"}
	return u.String()
}

func (e *Env) ListServers() ([]Server, error) {
	p := e.dbPath()
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", sqliteURI(p))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT s.id, s.name, s.ip, s.port, a.username, COALESCE(a.isSavePassword, 0)
		FROM server s LEFT JOIN account a ON a.serverId = s.id
		ORDER BY s.id, a.id`)
	if err != nil {
		return nil, fmt.Errorf("reading server list: %w", err)
	}
	defer rows.Close()
	var servers []Server
	idx := map[int64]int{}
	for rows.Next() {
		var (
			id      int64
			name    string
			ip      string
			port    int
			user    sql.NullString
			savePwd int64
		)
		if err := rows.Scan(&id, &name, &ip, &port, &user, &savePwd); err != nil {
			return nil, err
		}
		i, ok := idx[id]
		if !ok {
			i = len(servers)
			idx[id] = i
			servers = append(servers, Server{ID: id, Name: name, IP: ip, Port: port})
		}
		if user.Valid && user.String != "" {
			servers[i].Accounts = append(servers[i].Accounts, Account{Username: user.String, PasswordSaved: savePwd != 0})
		}
	}
	return servers, rows.Err()
}

func validField(s string) bool {
	return s != "" && !strings.ContainsAny(s, " \t\r\n")
}

func (e *Env) triggers(t Target) (map[string]string, error) {
	switch t.Kind {
	case KindLatest:
		return map[string]string{"AutoLoadSave.txt": "latest\n", "AutoConnect.txt": ""}, nil
	case KindSave:
		if !validField(t.Mode) || t.World == "" || strings.ContainsAny(t.World, "\r\n") || strings.TrimSpace(t.World) != t.World {
			return nil, fmt.Errorf("invalid save %q / %q", t.Mode, t.World)
		}
		if st, err := os.Stat(filepath.Join(e.savesDir(), t.Mode, t.World)); err != nil || !st.IsDir() {
			return nil, fmt.Errorf("no save %s/%s", t.Mode, t.World)
		}
		return map[string]string{"AutoLoadSave.txt": t.Mode + " " + t.World + "\n", "AutoConnect.txt": ""}, nil
	case KindServer:
		if !validField(t.IP) || t.Port <= 0 || t.Port > 65535 {
			return nil, fmt.Errorf("invalid server %q:%d", t.IP, t.Port)
		}
		if t.Account != "" && !validField(t.Account) {
			return nil, fmt.Errorf("account name %q cannot contain spaces", t.Account)
		}
		line := fmt.Sprintf("%s %d", t.IP, t.Port)
		if t.Account != "" {
			line += " " + t.Account
		}
		return map[string]string{"AutoConnect.txt": line + "\n", "AutoLoadSave.txt": ""}, nil
	}
	return nil, fmt.Errorf("unknown launch target")
}

func (e *Env) Launch(t Target) error {
	if e.Running != nil {
		running, err := e.Running()
		if err != nil {
			return fmt.Errorf("checking for a running game: %w", err)
		}
		if running {
			return ErrRunning
		}
	}
	files, err := e.triggers(t)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(e.luaDir(), 0o755); err != nil {
		return err
	}
	files["AutoStart.txt"] = "1\n"
	for _, name := range []string{"AutoConnect.txt", "AutoLoadSave.txt", "AutoStart.txt"} {
		if err := os.WriteFile(filepath.Join(e.luaDir(), name), []byte(files[name]), 0o644); err != nil {
			return err
		}
	}
	if e.OpenURL == nil {
		return errors.New("no URL opener configured")
	}
	return e.OpenURL(GameURL)
}

func IsRunning() (bool, error) { return gameRunning() }
