package modlist

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

var game = Version{Major: 42, Minor: 13}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	d, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(d)
}

func TestDefaultRoundTripReal(t *testing.T) {
	raw := readFixture(t, "default.txt")
	f := ParseDefault(raw)
	if f.Version != 1 || len(f.Mods) < 10 || len(f.Maps) != 1 {
		t.Fatalf("parse: version %d mods %d maps %v", f.Version, len(f.Mods), f.Maps)
	}
	if f.Mods[0] != "ZombieBuddy" {
		t.Fatalf("first mod %q", f.Mods[0])
	}
	if got := f.Render(); got != raw {
		t.Fatal("unchanged render is not byte-identical")
	}
	f.Mods = slices.Clone(f.Mods)
	f.Mods[0], f.Mods[1] = f.Mods[1], f.Mods[0]
	out := f.Render()
	if out == raw {
		t.Fatal("change not rendered")
	}
	g := ParseDefault(out)
	if !slices.Equal(g.Mods, f.Mods) || !slices.Equal(g.Maps, f.Maps) {
		t.Fatal("reparse mismatch")
	}
	mapsAt := strings.Index(raw, "maps")
	if raw[mapsAt:] != out[strings.Index(out, "maps"):] {
		t.Fatal("maps section changed")
	}
	f.Mods[0], f.Mods[1] = f.Mods[1], f.Mods[0]
	if f.Render() != raw {
		t.Fatal("swap back is not byte-identical")
	}
}

func TestDefaultPreservesUnknownAndStyle(t *testing.T) {
	raw := "VERSION = 1,\r\n/* note */\r\nextra = keep,\r\n\r\nmods\r\n{\r\n\tmod = \\A,\r\n\tmod = B,\r\n\tfoo = bar,\r\n}\r\n\r\nmaps\r\n{\r\n\tmap = M1,\r\n\tmap = M2,\r\n}\r\n"
	f := ParseDefault(raw)
	if !slices.Equal(f.Mods, []string{"A", "B"}) || !slices.Equal(f.Maps, []string{"M1", "M2"}) {
		t.Fatalf("parse %v %v", f.Mods, f.Maps)
	}
	f.Mods = []string{"B", "A", "C"}
	out := f.Render()
	want := "VERSION = 1,\r\n/* note */\r\nextra = keep,\r\n\r\nmods\r\n{\r\n\tmod = B,\r\n\tmod = \\A,\r\n\tmod = C,\r\n\tfoo = bar,\r\n}\r\n\r\nmaps\r\n{\r\n\tmap = M1,\r\n\tmap = M2,\r\n}\r\n"
	if out != want {
		t.Fatalf("got %q", out)
	}
	f = ParseDefault(raw)
	f.Maps = []string{"M2", "M1"}
	if out := f.Render(); !strings.Contains(out, "\tmap = M2,\r\n\tmap = M1,\r\n") || !strings.HasPrefix(out, raw[:strings.Index(raw, "maps")]) {
		t.Fatalf("maps reorder %q", out)
	}
}

func TestDefaultNewFile(t *testing.T) {
	f := NewDefault()
	f.NL = "\n"
	f.Mods = []string{"A"}
	want := "VERSION = 1,\n\nmods\n{\n    mod = A,\n}\n\nmaps\n{\n}\n"
	if got := f.Render(); got != want {
		t.Fatalf("got %q", got)
	}
	if g := ParseDefault(want); !slices.Equal(g.Mods, []string{"A"}) {
		t.Fatal(g.Mods)
	}
}

func TestDefaultBadVersion(t *testing.T) {
	f := ParseDefault("VERSION = 2,\nmods\n{\n mod = A,\n}\n")
	if len(f.Mods) != 0 {
		t.Fatal("game ignores version != 1")
	}
}

func TestPresetsRoundTripReal(t *testing.T) {
	raw := readFixture(t, "pz_modlist_settings.cfg")
	p := ParsePresets(raw)
	if p.Render() != raw {
		t.Fatal("not byte-identical")
	}
	c3, ok := p.Get("C3")
	if !ok || c3.IDs[0] != "ZombieBuddy" {
		t.Fatal("C3 missing")
	}
	if err := p.Set("Test", []string{"A", "B"}); err != nil {
		t.Fatal(err)
	}
	out := p.Render()
	if !strings.HasPrefix(out, raw) || !strings.HasSuffix(out, "Test:A;B;\n") {
		t.Fatalf("append changed existing lines: %q", out[len(out)-40:])
	}
	if !p.Delete("Test") || p.Render() != raw {
		t.Fatal("delete did not restore")
	}
}

func TestPresetsFormat(t *testing.T) {
	p := ParsePresets("")
	if err := p.Set("New", []string{"x", "y"}); err != nil {
		t.Fatal(err)
	}
	if got := p.Render(); got != "!fav!:\nNew:x;y;\n" {
		t.Fatalf("got %q", got)
	}
	p = ParsePresets("!fav!:a;b;\nP1:a;\nP2:b;c;\nP1:z;\nnocolon\n")
	if !slices.Equal(p.Favs, []string{"a", "b"}) {
		t.Fatal(p.Favs)
	}
	ls := p.List()
	if len(ls) != 2 || ls[0].Name != "P1" || !slices.Equal(ls[0].IDs, []string{"z"}) {
		t.Fatalf("%+v", ls)
	}
	if err := p.Set("P1", []string{"q"}); err != nil {
		t.Fatal(err)
	}
	if got := p.Render(); got != "!fav!:a;b;\nP1:q;\nP2:b;c;\nnocolon\n" {
		t.Fatalf("got %q", got)
	}
	p.SetFavs([]string{"b"})
	if got := p.Render(); !strings.HasPrefix(got, "!fav!:b;\nP1:q;\n") {
		t.Fatalf("got %q", got)
	}
	for _, bad := range []string{"", "a.b", "a:b", "a;b", `a\b`, "a/b", `a"b`} {
		if p.Set(bad, nil) == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	p = ParsePresets("P0:a;")
	p.SetFavs([]string{"f"})
	if got := p.Render(); got != "!fav!:f;\nP0:a;" {
		t.Fatalf("got %q", got)
	}
	if err := p.Set("P9", []string{"b"}); err != nil {
		t.Fatal(err)
	}
	if got := p.Render(); got != "!fav!:f;\nP0:a;\nP9:b;\n" {
		t.Fatalf("got %q", got)
	}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestModInfoParse(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "42", "poster.png"), "x")
	writeFile(t, filepath.Join(dir, "common", "icon.png"), "x")
	text := "name=My Mod\r\nid= MyMod \r\nposter=poster.png\r\nposter=p2.png\r\nicon=icon.png\r\ndescription=Line one <LINE> two\r\ndescription= more\r\nrequire=\\A, B,\r\nincompatible=C\r\nloadModAfter=D\r\nloadModBefore=E\r\nversionMin=42.5\r\nversionMax=\r\nauthor=me\r\nurl=https://theindiestone.com\r\ncategory=features\r\n"
	m, err := ParseModInfo(text, filepath.Join(dir, "42"), filepath.Join(dir, "common"))
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "MyMod" || m.Name != "My Mod" || m.Author != "me" || m.Category != "features" {
		t.Fatalf("%+v", m)
	}
	if !slices.Equal(m.Require, []string{"A", " B"}) || !slices.Equal(m.Requires(), []string{"A", "B"}) {
		t.Fatalf("require %q", m.Require)
	}
	if m.Posters[0] != filepath.Join(dir, "42", "poster.png") || m.Posters[1] != filepath.Join(dir, "common", "p2.png") {
		t.Fatal(m.Posters)
	}
	if m.Icon != filepath.Join(dir, "common", "icon.png") {
		t.Fatal(m.Icon)
	}
	if m.Description != "Line one <LINE> two more" || DescriptionText(m.Description) != "Line one\ntwo more" {
		t.Fatalf("%q", m.Description)
	}
	if m.VersionMin == nil || m.VersionMin.Int() != 42005 || m.VersionMax != nil {
		t.Fatal("versions")
	}
	if _, err := ParseModInfo("id=x\nversionMin=abc\n", "", ""); !errors.Is(err, ErrInvalidModInfo) {
		t.Fatal("bad versionMin must invalidate the mod")
	}
	if _, err := ParseModInfo("id=x\ntiledef=a 5\n", "", ""); !errors.Is(err, ErrInvalidModInfo) {
		t.Fatal("bad tiledef must invalidate the mod")
	}
}

func TestDescriptionText(t *testing.T) {
	got := DescriptionText("<RGB:1,0,0> Red <BR>Text<LINE><LINE>a &lt;b>")
	if got != "Red\nText\n\na <b>" {
		t.Fatalf("%q", got)
	}
}

func TestVersionDirName(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"41", "42", "42.10", "42.20", "43", "common"} {
		os.MkdirAll(filepath.Join(dir, d), 0o755)
	}
	if got := VersionDirName(dir, game); got != "42.10" {
		t.Fatal(got)
	}
	if got := VersionDirName(t.TempDir(), game); got != "42.0" {
		t.Fatal(got)
	}
}

type tree struct {
	zomboid, workshop string
}

func newTree(t *testing.T) tree {
	root := t.TempDir()
	return tree{filepath.Join(root, "Zomboid"), filepath.Join(root, "lib", "steamapps", "workshop", "content", "108600")}
}

func (tr tree) local(t *testing.T, folder, info string) {
	writeFile(t, filepath.Join(tr.zomboid, "mods", folder, "42", "mod.info"), info)
}

func (tr tree) steam(t *testing.T, item, folder, info string) {
	writeFile(t, filepath.Join(tr.workshop, item, "mods", folder, "42", "mod.info"), info)
}

func (tr tree) staged(t *testing.T, item, folder, info string) {
	writeFile(t, filepath.Join(tr.zomboid, "Workshop", item, "Contents", "mods", folder, "common", "mod.info"), info)
}

func TestScanPrecedence(t *testing.T) {
	tr := newTree(t)
	tr.local(t, "Dup", "id=Dup\nname=local")
	tr.steam(t, "200", "Dup", "id=Dup\nname=steam200")
	tr.steam(t, "100", "Dup", "id=Dup\nname=steam100")
	tr.staged(t, "MyUpload", "Dup2", "id=Dup2\nname=staged")
	tr.local(t, "Dup2", "id=Dup2\nname=local2")
	tr.local(t, "examplemod", "id=Example\nname=x")
	writeFile(t, filepath.Join(tr.zomboid, "mods", "RootOnly", "mod.info"), "id=RootOnly")
	tr.local(t, "Broken", "id=Broken\nversionMin=x")
	x := Scan(Roots{ZomboidDir: tr.zomboid, WorkshopDir: tr.workshop}, game)
	if m := x.Get("Dup"); m == nil || m.Name != "steam100" || m.Source != SourceSteam || m.WorkshopID != "100" {
		t.Fatalf("Dup: %+v", m)
	}
	if len(x.Shadowed["Dup"]) != 2 {
		t.Fatal("shadowed")
	}
	if m := x.Get("Dup2"); m == nil || m.Name != "staged" || m.Source != SourceWorkshop {
		t.Fatalf("Dup2: %+v", m)
	}
	if x.Get("Example") != nil || x.Get("RootOnly") != nil || x.Get("Broken") != nil {
		t.Fatal("examplemod / root-only / invalid mods must be skipped like the game")
	}
	if len(x.Problems) != 1 {
		t.Fatal(x.Problems)
	}
	if m := x.Get("Dup"); m.Source != SourceSteam {
		t.Fatal(m.Source)
	}
	x2 := Scan(Roots{ZomboidDir: tr.zomboid, WorkshopDir: tr.workshop, Order: []string{"mods", "steam", "workshop"}}, game)
	if x2.Get("Dup").Name != "local" {
		t.Fatal("custom order")
	}
}

func mod(id string, kv ...string) *Mod {
	m := &Mod{ID: id, Name: id}
	for i := 0; i+1 < len(kv); i += 2 {
		v := strings.Split(kv[i+1], ",")
		switch kv[i] {
		case "req":
			m.Require = v
		case "inc":
			m.Incompatible = v
		case "after":
			m.LoadAfter = v
		case "before":
			m.LoadBefore = v
		case "min":
			gv, _ := ParseVersion(kv[i+1])
			m.VersionMin = &gv
		case "max":
			gv, _ := ParseVersion(kv[i+1])
			m.VersionMax = &gv
		case "ws":
			m.WorkshopID = kv[i+1]
		case "name":
			m.Name = kv[i+1]
		}
	}
	return m
}

func TestRequiresResolution(t *testing.T) {
	x := NewIndex(game,
		mod("A", "req", "B"),
		mod("B", "req", "C"),
		mod("C"),
		mod("D", "req", "Missing"),
		mod("E", "max", "42.1"),
		mod("F", "req", "E"),
		mod("G", "inc", "C"),
	)
	if !x.Available("A") || x.Available("D") || x.Available("E") || x.Available("F") {
		t.Fatal("availability")
	}
	m := NewModel(x, nil, nil)
	c := m.ForceActivate("A", true)
	if !slices.Equal(m.Active, []string{"A", "B", "C"}) || len(c.Enabled) != 3 {
		t.Fatalf("enable cascade %v", m.Active)
	}
	c = m.ForceActivate("G", true)
	if m.IsActive("G") || !errors.Is(c.Skipped["G"], ErrIncompatible) {
		t.Fatal("incompatible must be refused")
	}
	if !slices.Equal(m.ActiveIncompatible("G"), []string{"C"}) {
		t.Fatal(m.ActiveIncompatible("G"))
	}
	c = m.ForceActivate("D", true)
	if m.IsActive("D") || !errors.Is(c.Skipped["D"], ErrUnavailable) {
		t.Fatal("unavailable must be refused")
	}
	m.ForceActivate("C", false)
	if len(m.Active) != 0 {
		t.Fatalf("disable cascade %v", m.Active)
	}
	if issues := NewModel(x, []string{"A", "B", "C"}, nil).OrderIssues(); len(issues) != 2 || !issues[0].Fatal {
		t.Fatalf("%+v", issues)
	}
	m = NewModel(x, []string{"A", "B", "C"}, nil)
	m.CorrectOrder(m.Active)
	if !slices.Equal(m.Active, []string{"B", "A", "C"}) {
		t.Fatal(m.Active)
	}
}

func TestLoadPreset(t *testing.T) {
	x := NewIndex(game, mod("A", "req", "B"), mod("B"), mod("C"), mod("E", "max", "42.1"))
	m := NewModel(x, []string{"C"}, []string{"map1"})
	res := m.LoadPreset([]string{"A", "Gone", "E"})
	if !slices.Equal(m.Active, []string{"B", "A"}) {
		t.Fatal(m.Active)
	}
	if !slices.Equal(res.Missing, []string{"Gone"}) || !slices.Equal(m.Maps, []string{"map1"}) {
		t.Fatal(res.Missing)
	}
}

func TestAutoSort(t *testing.T) {
	x := NewIndex(game,
		mod("a"),
		mod("b", "after", "c"),
		mod("c"),
		mod("d", "before", "a"),
		mod("e", "req", "d"),
	)
	m := NewModel(x, []string{"e", "d", "c", "b", "a"}, nil)
	m.AutoSort()
	if !slices.Equal(m.Active, []string{"a", "c", "b", "d", "e"}) {
		t.Fatalf("got %v", m.Active)
	}
	x = NewIndex(game, mod("a", "before", "x,y"), mod("x"), mod("y"), mod("z", "after", "q"))
	m = NewModel(x, []string{"z", "y", "x", "a"}, nil)
	m.AutoSort()
	if !slices.Equal(m.Active, []string{"a", "x", "y", "z"}) {
		t.Fatalf("got %v", m.Active)
	}
}

func TestViewSearchSort(t *testing.T) {
	mods := []*Mod{
		mod("ZombieBuddy", "ws", "3619862853", "name=Zombie Buddy"),
		mod("alpha", "name=alpha"),
		mod("Beta", "ws", "12", "name=Beta"),
	}
	got := View(mods, ViewOptions{Query: "https://steamcommunity.com/sharedfiles/filedetails/?id=3619862853"})
	if len(got) != 1 || got[0].ID != "ZombieBuddy" {
		t.Fatal(got)
	}
	if got := View(mods, ViewOptions{Query: "BUDDY"}); len(got) != 1 {
		t.Fatal(got)
	}
	ids := func(ms []*Mod) []string {
		var s []string
		for _, m := range ms {
			s = append(s, m.ID)
		}
		return s
	}
	if g := ids(View(mods, ViewOptions{Sort: SortWorkshopAsc})); !slices.Equal(g, []string{"Beta", "ZombieBuddy", "alpha"}) {
		t.Fatal(g)
	}
	if g := ids(View(mods, ViewOptions{Sort: SortWorkshopDesc})); !slices.Equal(g, []string{"ZombieBuddy", "Beta", "alpha"}) {
		t.Fatal(g)
	}
	if g := ids(View(mods, ViewOptions{Sort: SortNameDesc})); !slices.Equal(g, []string{"ZombieBuddy", "Beta", "alpha"}) {
		t.Fatal(g)
	}
	act := func(id string) bool { return id == "Beta" }
	if g := ids(View(mods, ViewOptions{Filter: FilterEnabled, IsActive: act})); !slices.Equal(g, []string{"Beta"}) {
		t.Fatal(g)
	}
	if g := ids(View(mods, ViewOptions{Filter: FilterDisabled, IsActive: act})); len(g) != 2 {
		t.Fatal(g)
	}
}

func TestPrefs(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x", "modstab.json")
	if got := LoadPrefs(p); got.Sort != SortNameAsc || got.Filter != FilterAll {
		t.Fatal(got)
	}
	if err := SavePrefs(p, Prefs{Sort: SortWorkshopDesc, Filter: FilterDisabled}); err != nil {
		t.Fatal(err)
	}
	if got := LoadPrefs(p); got.Sort != SortWorkshopDesc || got.Filter != FilterDisabled {
		t.Fatal(got)
	}
}

func newStore(t *testing.T, running *bool, now *time.Time) *Store {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mods", "default.txt"), readFixture(t, "default.txt"))
	writeFile(t, filepath.Join(dir, "Lua", "pz_modlist_settings.cfg"), readFixture(t, "pz_modlist_settings.cfg"))
	return &Store{
		ZomboidDir: dir,
		Running:    func() (bool, error) { return *running, nil },
		Now:        func() time.Time { return *now },
	}
}

func backups(t *testing.T, path string) []string {
	m, _ := filepath.Glob(path + backupTag + "*")
	return m
}

func TestStoreRunningRefusal(t *testing.T) {
	running := true
	now := time.Date(2026, 11, 19, 10, 0, 0, 0, time.Local)
	s := newStore(t, &running, &now)
	f, err := s.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	orig := f.Raw()
	f.Mods = f.Mods[1:]
	if _, err := s.SaveDefault(f); !errors.Is(err, ErrGameRunning) {
		t.Fatal(err)
	}
	if _, _, err := s.SavePreset("X", []string{"a"}); !errors.Is(err, ErrGameRunning) {
		t.Fatal(err)
	}
	if _, _, err := s.DeletePreset("C3"); !errors.Is(err, ErrGameRunning) {
		t.Fatal(err)
	}
	d, _ := os.ReadFile(s.DefaultPath())
	if string(d) != orig || len(backups(t, s.DefaultPath())) != 0 {
		t.Fatal("file touched while running")
	}
	s.Running = func() (bool, error) { return false, errors.New("boom") }
	if _, err := s.SaveDefault(f); err == nil {
		t.Fatal("check error must refuse")
	}
}

func TestStoreSaveAndBackup(t *testing.T) {
	running := false
	now := time.Date(2026, 11, 19, 10, 0, 0, 0, time.Local)
	s := newStore(t, &running, &now)
	f, _ := s.LoadDefault()
	orig := f.Raw()
	first := f.Mods[0]
	f.Mods = f.Mods[1:]
	res, err := s.SaveDefault(f)
	if err != nil || !res.Written {
		t.Fatal(err)
	}
	bk := backups(t, s.DefaultPath())
	if len(bk) != 1 || !strings.HasSuffix(bk[0], ".launcher-bak-20261119-100000") {
		t.Fatal(bk)
	}
	if d, _ := os.ReadFile(bk[0]); string(d) != orig {
		t.Fatal("backup content")
	}
	g, _ := s.LoadDefault()
	if slices.Contains(g.Mods, first) || len(g.Mods) != len(f.Mods) {
		t.Fatal("saved content")
	}
	now = now.Add(time.Hour)
	f.Mods = append([]string{first}, f.Mods...)
	if _, err := s.SaveDefault(f); err != nil {
		t.Fatal(err)
	}
	if len(backups(t, s.DefaultPath())) != 1 {
		t.Fatal("only the first backup per day is kept")
	}
	if d, _ := os.ReadFile(s.DefaultPath()); string(d) != orig {
		t.Fatal("restoring order must give the original bytes")
	}
	now = now.Add(24 * time.Hour)
	f.Mods = f.Mods[1:]
	if _, err := s.SaveDefault(f); err != nil {
		t.Fatal(err)
	}
	if len(backups(t, s.DefaultPath())) != 2 {
		t.Fatal("new day, new backup")
	}
	writeFile(t, s.DefaultPath(), orig)
	f.Mods = f.Mods[1:]
	if _, err := s.SaveDefault(f); !errors.Is(err, ErrChangedOnDisk) {
		t.Fatal(err)
	}

	cfgOrig := readFixture(t, "pz_modlist_settings.cfg")
	p, res, err := s.SavePreset("Mine", []string{"A", "B"})
	if err != nil || !res.Written || res.Backup == "" {
		t.Fatal(err, res)
	}
	d, _ := os.ReadFile(s.PresetsPath())
	if string(d) != cfgOrig+"Mine:A;B;\n" {
		t.Fatal("preset append")
	}
	if c3, _ := p.Get("C3"); len(c3.IDs) < 10 {
		t.Fatal("C3 lost")
	}
	if _, _, err := s.DeletePreset("Mine"); err != nil {
		t.Fatal(err)
	}
	if d, _ := os.ReadFile(s.PresetsPath()); string(d) != cfgOrig {
		t.Fatal("delete must leave the other presets byte-identical")
	}
	if _, _, err := s.DeletePreset("Nope"); err == nil {
		t.Fatal("missing preset")
	}
	if _, _, err := s.SetFavorite("ZombieBuddy", true); err != nil {
		t.Fatal(err)
	}
	if d, _ := os.ReadFile(s.PresetsPath()); !strings.HasPrefix(string(d), "!fav!:ZombieBuddy;\n") || !strings.HasSuffix(string(d), cfgOrig[len("!fav!:\n"):]) {
		t.Fatal("favorites")
	}
}

func TestStoreMissingFiles(t *testing.T) {
	s := &Store{ZomboidDir: t.TempDir()}
	f, err := s.LoadDefault()
	if err != nil || f.Raw() != "" {
		t.Fatal(err)
	}
	f.Mods = []string{"A"}
	if _, err := s.SaveDefault(f); err != nil {
		t.Fatal(err)
	}
	if g, _ := s.LoadDefault(); !slices.Equal(g.Mods, []string{"A"}) {
		t.Fatal(g.Mods)
	}
	if _, _, err := s.SavePreset("P", []string{"A"}); err != nil {
		t.Fatal(err)
	}
	if d, _ := os.ReadFile(s.PresetsPath()); string(d) != "!fav!:\nP:A;\n" {
		t.Fatalf("%q", d)
	}
}

func TestImageCache(t *testing.T) {
	dir := t.TempDir()
	src := image.NewNRGBA(image.Rect(0, 0, 100, 50))
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(dir, "p.png")
	writeFile(t, good, buf.String())
	c := NewImageCache(16, 2)
	done := make(chan image.Image, 1)
	if _, ok := c.Get(filepath.Join(dir, "nope.png"), func(img image.Image) { done <- img }); ok {
		t.Fatal("first get must be async")
	}
	if <-done != nil {
		t.Fatal("missing image must give nil")
	}
	c.Get(good, func(img image.Image) { done <- img })
	img := <-done
	if img == nil || img.Bounds().Dx() != 16 || img.Bounds().Dy() != 8 {
		t.Fatal("downscale")
	}
	if got, ok := c.Get(good, nil); !ok || got != img {
		t.Fatal("cache hit")
	}
}
