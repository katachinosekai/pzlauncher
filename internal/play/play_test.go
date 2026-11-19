package play

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Env, *[]string) {
	t.Helper()
	root := t.TempDir()
	now := time.Now()
	mk := func(rel string, age time.Duration) {
		d := filepath.Join(root, "Saves", rel)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(d, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	mk("Sandbox/old world", 3*time.Hour)
	mk("Survival/mid", 2*time.Hour)
	mk("Apocalypse/new", time.Hour)
	mk("Builder/newest", 0)
	mk("Multiplayer/10.0.0.5_16261_abc", 0)
	if err := os.WriteFile(filepath.Join(root, "Saves", "Sandbox", "stray.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var opened []string
	e := &Env{
		ZomboidDir: root,
		OpenURL:    func(u string) error { opened = append(opened, u); return nil },
		Running:    func() (bool, error) { return false, nil },
	}
	return e, &opened
}

func TestListSaves(t *testing.T) {
	e, _ := fixture(t)
	saves, err := e.ListSaves()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range saves {
		got = append(got, s.Label())
	}
	want := "Builder / newest|Apocalypse / new|Survival / mid|Sandbox / old world"
	if strings.Join(got, "|") != want {
		t.Fatalf("got %v", got)
	}
}

func TestListSavesMissing(t *testing.T) {
	e := &Env{ZomboidDir: t.TempDir()}
	saves, err := e.ListSaves()
	if err != nil || len(saves) != 0 {
		t.Fatalf("got %v %v", saves, err)
	}
}

func makeDB(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "db"), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(root, "db", "ServerListSteam.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE server (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, ip TEXT NOT NULL, port INTEGER NOT NULL, serverPassword TEXT, description TEXT)`,
		`CREATE TABLE account (id INTEGER PRIMARY KEY AUTOINCREMENT, serverId INTEGER NOT NULL, playerFirstAndLastName TEXT, username TEXT NOT NULL, password TEXT, isSavePassword INTEGER DEFAULT 0, isUseSteamRelay INTEGER DEFAULT 0, authType INTEGER DEFAULT 1)`,
		`INSERT INTO server (name, ip, port, serverPassword) VALUES ('Test Server', '10.0.0.5', 16261, 'srvsecret'), ('Empty', '10.0.0.2', 16262, NULL)`,
		`INSERT INTO account (serverId, username, password, isSavePassword) VALUES (1, 'kata', 'pwsecret', 1), (1, 'alt', '', 0)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListServers(t *testing.T) {
	e, _ := fixture(t)
	makeDB(t, e.ZomboidDir)
	servers, err := e.ListServers()
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 {
		t.Fatalf("got %d servers", len(servers))
	}
	s := servers[0]
	if s.Name != "Test Server" || s.Addr() != "10.0.0.5:16261" || len(s.Accounts) != 2 {
		t.Fatalf("got %+v", s)
	}
	if !s.Accounts[0].PasswordSaved || s.Accounts[0].Username != "kata" || s.Accounts[1].PasswordSaved {
		t.Fatalf("accounts %+v", s.Accounts)
	}
	if len(servers[1].Accounts) != 0 {
		t.Fatalf("empty server has accounts")
	}
}

func TestListServersNoDB(t *testing.T) {
	e, _ := fixture(t)
	servers, err := e.ListServers()
	if err != nil || servers != nil {
		t.Fatalf("got %v %v", servers, err)
	}
}

func readLua(t *testing.T, e *Env, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e.ZomboidDir, "Lua", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLaunchSave(t *testing.T) {
	e, opened := fixture(t)
	if err := e.Launch(Target{Kind: KindSave, Mode: "Sandbox", World: "old world"}); err != nil {
		t.Fatal(err)
	}
	if got := readLua(t, e, "AutoLoadSave.txt"); got != "Sandbox old world\n" {
		t.Fatalf("AutoLoadSave %q", got)
	}
	if readLua(t, e, "AutoStart.txt") != "1\n" || readLua(t, e, "AutoConnect.txt") != "" {
		t.Fatal("bad AutoStart/AutoConnect")
	}
	if len(*opened) != 1 || (*opened)[0] != "steam://rungameid/108600" {
		t.Fatalf("opened %v", *opened)
	}
}

func TestLaunchLatestAndServer(t *testing.T) {
	e, _ := fixture(t)
	if err := e.Launch(Latest()); err != nil {
		t.Fatal(err)
	}
	if readLua(t, e, "AutoLoadSave.txt") != "latest\n" {
		t.Fatal("latest not written")
	}
	srv := Server{IP: "10.0.0.5", Port: 16261}
	if err := e.Launch(ServerTarget(srv, "kata")); err != nil {
		t.Fatal(err)
	}
	if got := readLua(t, e, "AutoConnect.txt"); got != "10.0.0.5 16261 kata\n" {
		t.Fatalf("AutoConnect %q", got)
	}
	if readLua(t, e, "AutoLoadSave.txt") != "" {
		t.Fatal("AutoLoadSave not cleared")
	}
}

func TestLaunchRejects(t *testing.T) {
	e, opened := fixture(t)
	cases := []Target{
		{Kind: KindSave, Mode: "Sandbox", World: "missing"},
		{Kind: KindSave, Mode: "Sand box", World: "x"},
		{Kind: KindServer, IP: "1.2.3.4", Port: 0},
		{Kind: KindServer, IP: "1.2.3.4", Port: 16261, Account: "two words"},
	}
	for _, c := range cases {
		if err := e.Launch(c); err == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
	e.Running = func() (bool, error) { return true, nil }
	if err := e.Launch(Latest()); !errors.Is(err, ErrRunning) {
		t.Fatalf("got %v", err)
	}
	if len(*opened) != 0 {
		t.Fatal("opened URL on rejected launch")
	}
}
