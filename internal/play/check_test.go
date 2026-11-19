package play

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

type classBuilder struct {
	cp  bytes.Buffer
	n   uint16
	idx map[string]uint16
}

func newClassBuilder() *classBuilder { return &classBuilder{n: 1, idx: map[string]uint16{}} }

func (b *classBuilder) add(key string, data []byte) uint16 {
	if i, ok := b.idx[key]; ok {
		return i
	}
	b.cp.Write(data)
	i := b.n
	b.n++
	b.idx[key] = i
	return i
}

func u2(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }

func (b *classBuilder) utf8(s string) uint16 {
	return b.add("u:"+s, append(append([]byte{1}, u2(uint16(len(s)))...), s...))
}

func (b *classBuilder) class(name string) uint16 {
	return b.add("c:"+name, append([]byte{7}, u2(b.utf8(name))...))
}

func (b *classBuilder) str(s string) uint16 {
	return b.add("s:"+s, append([]byte{8}, u2(b.utf8(s))...))
}

func (b *classBuilder) integer(v int32) uint16 {
	return b.add("i:"+string(rune(v)), append([]byte{3}, binary.BigEndian.AppendUint32(nil, uint32(v))...))
}

func (b *classBuilder) ref(tag byte, owner, name, desc string) uint16 {
	nt := b.add("nt:"+name+desc, append(append([]byte{12}, u2(b.utf8(name))...), u2(b.utf8(desc))...))
	return b.add("r:"+owner+name+desc, append(append([]byte{tag}, u2(b.class(owner))...), u2(nt)...))
}

type testMethod struct {
	name, desc string
	code       []byte
}

type testField struct {
	name, desc string
	value      int32
}

func (b *classBuilder) build(this string, fields []testField, methods []testMethod) []byte {
	thisIdx := b.class(this)
	super := b.class("java/lang/Object")
	codeName := b.utf8("Code")
	cvName := b.utf8("ConstantValue")
	type fieldIdx struct{ name, desc, val uint16 }
	var fs []fieldIdx
	for _, f := range fields {
		fs = append(fs, fieldIdx{b.utf8(f.name), b.utf8(f.desc), b.integer(f.value)})
	}
	type methodIdx struct{ name, desc uint16 }
	var ms []methodIdx
	for _, m := range methods {
		ms = append(ms, methodIdx{b.utf8(m.name), b.utf8(m.desc)})
	}
	var out bytes.Buffer
	out.Write([]byte{0xCA, 0xFE, 0xBA, 0xBE, 0, 0, 0, 61})
	out.Write(u2(b.n))
	out.Write(b.cp.Bytes())
	out.Write(u2(0x21))
	out.Write(u2(thisIdx))
	out.Write(u2(super))
	out.Write(u2(0))
	out.Write(u2(uint16(len(fs))))
	for _, f := range fs {
		out.Write(u2(0x19))
		out.Write(u2(f.name))
		out.Write(u2(f.desc))
		out.Write(u2(1))
		out.Write(u2(cvName))
		out.Write(binary.BigEndian.AppendUint32(nil, 2))
		out.Write(u2(f.val))
	}
	out.Write(u2(uint16(len(methods))))
	for i, m := range methods {
		out.Write(u2(0x09))
		out.Write(u2(ms[i].name))
		out.Write(u2(ms[i].desc))
		out.Write(u2(1))
		out.Write(u2(codeName))
		attr := append(append(append(u2(8), u2(1)...), binary.BigEndian.AppendUint32(nil, uint32(len(m.code)))...), m.code...)
		attr = append(attr, u2(0)...)
		attr = append(attr, u2(0)...)
		out.Write(binary.BigEndian.AppendUint32(nil, uint32(len(attr))))
		out.Write(attr)
	}
	out.Write(u2(0))
	return out.Bytes()
}

func isoWorldClass(worldVersion int16) []byte {
	b := newClassBuilder()
	code := append([]byte{0x11}, u2(uint16(worldVersion))...)
	code = append(code, 0xac)
	return b.build("zombie/iso/IsoWorld",
		[]testField{{"WorldVersion", "I", 999}},
		[]testMethod{
			{"<init>", "()V", []byte{0x2a, 0xb1}},
			{"getWorldVersion", "()I", code},
		})
}

func coreClass(major, minor int8, suffix string) []byte {
	b := newClassBuilder()
	gv := b.class("zombie/core/GameVersion")
	init := b.ref(10, "zombie/core/GameVersion", "<init>", "(IILjava/lang/String;)V")
	field := b.ref(9, "zombie/core/Core", "gameVersion", "Lzombie/core/GameVersion;")
	other := b.ref(9, "zombie/core/Core", "breakVersion", "Lzombie/core/GameVersion;")
	rev := b.ref(9, "zombie/core/Core", "gitRevision", "Ljava/lang/String;")
	s := b.str(suffix)
	empty := b.str("")
	sha := b.str("3d39148846")
	var code []byte
	code = append(code, 0xbb)
	code = append(code, u2(gv)...)
	code = append(code, 0x59, 0x10, 41, 0x03, 0x13)
	code = append(code, u2(empty)...)
	code = append(code, 0xb7)
	code = append(code, u2(init)...)
	code = append(code, 0xb3)
	code = append(code, u2(other)...)
	code = append(code, 0xbb)
	code = append(code, u2(gv)...)
	code = append(code, 0x59, 0x10, byte(major), 0x10, byte(minor), 0x13)
	code = append(code, u2(s)...)
	code = append(code, 0xb7)
	code = append(code, u2(init)...)
	code = append(code, 0xb3)
	code = append(code, u2(field)...)
	code = append(code, 0x13)
	code = append(code, u2(sha)...)
	code = append(code, 0xb3)
	code = append(code, u2(rev)...)
	code = append(code, 0xb1)
	return b.build("zombie/core/Core", nil, []testMethod{{"<clinit>", "()V", code}})
}

func writeJar(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "projectzomboid.jar"))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(data)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadGameInfo(t *testing.T) {
	dir := t.TempDir()
	writeJar(t, dir, map[string][]byte{
		"zombie/iso/IsoWorld.class": isoWorldClass(251),
		"zombie/core/Core.class":    coreClass(42, 22, ""),
	})
	gi, err := ReadGameInfo(dir)
	if err != nil {
		t.Fatal(err)
	}
	if gi.WorldVersion != 251 || gi.Version.String() != "42.22" {
		t.Fatalf("got %+v", gi)
	}
	dir2 := t.TempDir()
	writeJar(t, dir2, map[string][]byte{
		"zombie/iso/IsoWorld.class": isoWorldClass(260),
		"zombie/core/Core.class":    coreClass(43, 1, "-rc"),
	})
	gi, err = ReadGameInfo(dir2)
	if err != nil || gi.WorldVersion != 260 || gi.Version.String() != "43.1-rc" {
		t.Fatalf("got %+v %v", gi, err)
	}
	if _, err := ReadGameInfo(t.TempDir()); err == nil {
		t.Fatal("no jar should fail")
	}
}

func TestGameVersion(t *testing.T) {
	for in, want := range map[string]int{"42.22": 42022, "42.0": 42000, "41.78.16": 41078, "42.13.1": 42013} {
		v, err := ParseGameVersion(in)
		if err != nil || v.Int() != want {
			t.Errorf("%s: %v %v", in, v, err)
		}
	}
	v, _ := ParseGameVersion("42.5-beta")
	if v.String() != "42.5-beta" || v.Suffix != "-beta" {
		t.Errorf("suffix: %+v", v)
	}
	for _, bad := range []string{"42", "", "x.y", "42.1000"} {
		if _, err := ParseGameVersion(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
	for in, want := range map[string]int{"42": 42000, "42.19": 42019, "42.20.1": 42020, "common": 0, "media": 0, "42.5000": 42999} {
		if got := versionIntFromName(in); got != want {
			t.Errorf("versionIntFromName(%q) = %d, want %d", in, got, want)
		}
	}
}

func mapVer(version int32, name string) []byte {
	b := binary.BigEndian.AppendUint32(nil, uint32(version))
	chars := utf16.Encode([]rune(name))
	b = binary.BigEndian.AppendUint32(b, uint32(len(chars)))
	for _, c := range chars {
		b = binary.BigEndian.AppendUint16(b, c)
	}
	return append(b, 0, 0, 0, 0)
}

func TestParseMapVer(t *testing.T) {
	si, err := ParseMapVer(bytes.NewReader(mapVer(251, "RUSA Spawn 01;Real USA Country;Café, KY")))
	if err != nil || si.WorldVersion != 251 || si.MapName != "RUSA Spawn 01;Real USA Country;Café, KY" || !si.HasMapName {
		t.Fatalf("got %+v %v", si, err)
	}
	si, _ = ParseMapVer(bytes.NewReader(mapVer(195, "DEFAULT")))
	if si.MapName != "Muldraugh, KY" {
		t.Fatalf("DEFAULT: %+v", si)
	}
	si, err = ParseMapVer(bytes.NewReader(mapVer(251, "Muldraugh, KY")[:9]))
	if err == nil || !si.HasVersion || si.HasMapName {
		t.Fatalf("truncated: %+v %v", si, err)
	}
	if _, err := ParseMapVer(bytes.NewReader([]byte{0, 0})); err == nil {
		t.Fatal("short file should fail")
	}
}

func TestParseActiveMods(t *testing.T) {
	text := "VERSION = 1,\r\n\r\nmods\r\n{\r\n    mod = ZombieBuddy,\r\n    mod = \\AutoLoadSave,\r\n  /* mod = Commented, */\r\n    mod = ,\r\n    MOD = RealUSA,\r\n    other = x,\r\n}\r\n\r\nmaps\r\n{\r\n    map = Real USA Country,\r\n}\r\n"
	am := ParseActiveMods(text)
	if strings.Join(am.Mods, "|") != "ZombieBuddy|AutoLoadSave|RealUSA" {
		t.Fatalf("mods %q", am.Mods)
	}
	if strings.Join(am.Maps, "|") != "Real USA Country" {
		t.Fatalf("maps %q", am.Maps)
	}
	if am := ParseActiveMods("mods { mod = A, }"); len(am.Mods) != 0 {
		t.Fatalf("missing VERSION must give no mods: %q", am.Mods)
	}
	if am := ParseActiveMods("VERSION = 2,\nmods { mod = A, }"); len(am.Mods) != 0 {
		t.Fatalf("VERSION 2 must give no mods: %q", am.Mods)
	}
	if am := ParseActiveMods("VERSION = 1,\nmods { mod = A }"); strings.Join(am.Mods, "|") != "A" {
		t.Fatalf("last entry without comma: %q", am.Mods)
	}
}

type modTree struct {
	t    *testing.T
	env  *Env
	game string
}

func newModTree(t *testing.T) *modTree {
	root := t.TempDir()
	e := &Env{
		ZomboidDir:  filepath.Join(root, "Zomboid"),
		GameDir:     filepath.Join(root, "game"),
		WorkshopDir: filepath.Join(root, "workshop", "content", "108600"),
	}
	writeJar(t, e.GameDir, map[string][]byte{
		"zombie/iso/IsoWorld.class": isoWorldClass(251),
		"zombie/core/Core.class":    coreClass(42, 22, ""),
	})
	writeFile(t, filepath.Join(e.GameDir, "media", "maps", "Muldraugh, KY", "map.info"), "title=Muldraugh\nlots=Muldraugh, KY\n")
	writeFile(t, filepath.Join(e.GameDir, "media", "maps", "Riverside, KY", "map.info"), "lots=Riverside, KY\n")
	writeFile(t, filepath.Join(e.GameDir, "media", "maps", "NoInfo", "x.lotheader"), "")
	writeFile(t, filepath.Join(e.GameDir, "media", "maps", "challengemaps", "Kingsmouth", "map.info"), "lots=Kingsmouth\n")
	return &modTree{t: t, env: e}
}

func (m *modTree) local(dir, rel, info string) {
	writeFile(m.t, filepath.Join(m.env.ZomboidDir, "mods", dir, rel), info)
}

func (m *modTree) workshop(item, dir, rel, info string) {
	writeFile(m.t, filepath.Join(m.env.WorkshopDir, item, "mods", dir, rel), info)
}

func (m *modTree) staged(item, dir, rel, info string) {
	writeFile(m.t, filepath.Join(m.env.ZomboidDir, "Workshop", item, "Contents", "mods", dir, rel), info)
}

func TestModResolution(t *testing.T) {
	m := newModTree(t)
	m.local("A", "42/mod.info", "name=A\r\nid=ModA\r\nversionMin=42.0\r\n")
	m.local("B", "common/mod.info", "name=B\nid=ModB\n")
	m.local("C", "mod.info", "name=C\nid=ModC\n")
	m.local("D", "42/mod.info", "id=ModD\nrequire=\\ModA,\\ModB,\n")
	m.local("D", "42.30/mod.info", "id=ModD\nversionMin=42.30\n")
	m.local("E", "42/mod.info", "id=ModE\nversionMin=42.30\n")
	m.local("F", "42/mod.info", "id=ModF\nrequire=\\ModE\n")
	m.local("G", "42/mod.info", "id=ModG\nversionMax=42.13\n")
	m.local("H", "42/mod.info", "id=ModH\nrequire=\n")
	m.local("I", "42/mod.info", "id=ModI\nversionMin=garbage\n")
	m.local("J", "42/mod.info", "name=has id= inside\nid=ModJ\nrequire=\\ModK\n")
	m.local("K", "42/mod.info", "id=ModK\nrequire=\\ModJ\n")
	m.local("examplemod", "42/mod.info", "id=Example\n")
	m.local("Dup", "42/mod.info", "id=Shared\nversionMin=42.30\n")
	m.workshop("111", "Dup", "42.19/mod.info", "id=Shared\nversionMin=42.19\n")
	m.staged("222", "Staged", "common/mod.info", "id=StagedMod\n")
	game, _ := ParseGameVersion("42.22")
	idx := m.env.ScanMods(game)

	for id, want := range map[string]bool{
		"ModA": true, "ModB": true, "ModC": false, "ModD": true, "ModE": false, "ModF": false,
		"ModG": false, "ModH": false, "ModI": false, "ModJ": true, "ModK": true, "Example": false,
		"Shared": true, "StagedMod": true, "Nope": false,
	} {
		if got := idx.Available(id); got != want {
			t.Errorf("Available(%s) = %v, want %v (%s)", id, got, want, idx.Unavailable(id))
		}
	}
	if idx.Get("ModC") != nil || idx.Get("ModI") != nil {
		t.Error("root-only mod.info and bad versionMin must not count as installed")
	}
	if d := idx.Get("ModD"); d == nil || filepath.Base(d.VersionDir) != "42" {
		t.Errorf("ModD version dir: %+v", d)
	}
	if s := idx.Get("Shared"); s == nil || !strings.Contains(s.Dir, "workshop") {
		t.Errorf("workshop copy must win over ~/Zomboid/mods: %+v", s)
	}
	if !strings.Contains(idx.Unavailable("ModE"), "42.30") || !strings.Contains(idx.Unavailable("ModG"), "42.13") {
		t.Errorf("reasons: %q %q", idx.Unavailable("ModE"), idx.Unavailable("ModG"))
	}

	m.local("A", "common/media/maps/MapA/map.info", "lots=MapA\n")
	m.local("A", "42/media/maps/MapA42/map.info", "lots=MapA42\n")
	m.local("E", "42/media/maps/MapE/map.info", "lots=MapE\n")
	m.local("A", "common/media/maps/challengemaps/X/map.info", "lots=X\n")
	lots := idx.LotDirs(m.env.GameDir, []string{"ModA", "ModE"})
	for _, n := range []string{"MapA", "MapA42", "Muldraugh, KY", "Riverside, KY", "challengemaps/Kingsmouth"} {
		if !lots[n] {
			t.Errorf("lot dir %q missing: %v", n, lots)
		}
	}
	for _, n := range []string{"MapE", "NoInfo", "challengemaps", "challengemaps/X"} {
		if lots[n] {
			t.Errorf("lot dir %q must not be listed", n)
		}
	}
}

type saveFixture struct {
	*modTree
	checker func() *Checker
}

func newSaveFixture(t *testing.T, defaults string) *saveFixture {
	m := newModTree(t)
	m.local("AutoLoadSave", "42/mod.info", "id=AutoLoadSave\nversionMin=42.0\nrequire=\\ZombieBuddy\n")
	m.local("ZombieBuddy", "42/mod.info", "id=ZombieBuddy\n")
	m.local("Map", "42/mod.info", "id=MapMod\n")
	m.local("Map", "42/media/maps/Real USA Country/map.info", "lots=Real USA Country\n")
	m.local("Old", "42/mod.info", "id=OldMod\nversionMax=42.12\n")
	writeFile(t, filepath.Join(m.env.ZomboidDir, "mods", "default.txt"), defaults)
	return &saveFixture{modTree: m, checker: m.env.NewChecker}
}

func (f *saveFixture) save(mode, world string, worldVersion int32, mapName, mods string) Save {
	dir := filepath.Join(f.env.ZomboidDir, "Saves", mode, world)
	writeFile(f.t, filepath.Join(dir, "map_ver.bin"), string(mapVer(worldVersion, mapName)))
	if mods != "" {
		writeFile(f.t, filepath.Join(dir, "mods.txt"), mods)
	}
	return Save{Mode: mode, World: world, Dir: dir}
}

const enabledDefaults = "VERSION = 1,\nmods\n{\n    mod = ZombieBuddy,\n    mod = AutoLoadSave,\n}\n"

func issues(r CheckResult, s Severity) string {
	var out []string
	for _, i := range r.Issues {
		if i.Severity == s {
			out = append(out, i.Text)
		}
	}
	return strings.Join(out, " | ")
}

func TestCheckSave(t *testing.T) {
	f := newSaveFixture(t, enabledDefaults)
	c := f.checker()
	if c.GameErr != nil || c.Game.WorldVersion != 251 {
		t.Fatalf("game: %+v %v", c.Game, c.GameErr)
	}

	ok := f.save("Sandbox", "ok", 251, "Real USA Country;Muldraugh, KY", "VERSION = 1,\nmods\n{\n    mod = MapMod,\n    mod = AutoLoadSave,\n}\n")
	if r := c.CheckSave(ok); r.Worst() != SevInfo {
		t.Errorf("ok save: %s", r.Text())
	}

	newer := f.save("Sandbox", "newer", 252, "Muldraugh, KY", "")
	if r := c.CheckSave(newer); !r.Blocking() || !strings.Contains(issues(r, SevError), "newer game version") {
		t.Errorf("newer: %s", r.Text())
	}
	for _, v := range []int32{200, 150} {
		old := f.save("Sandbox", "old", v, "Muldraugh, KY", "")
		if r := c.CheckSave(old); !strings.Contains(issues(r, SevError), "too old") {
			t.Errorf("old %d: %s", v, r.Text())
		}
	}
	if r := c.CheckSave(f.save("Sandbox", "201", 201, "Muldraugh, KY", "")); r.Blocking() {
		t.Errorf("201 must load: %s", r.Text())
	}

	noMapMod := f.save("Sandbox", "nomap", 251, "Real USA Country;Muldraugh, KY", "VERSION = 1,\nmods\n{\n    mod = AutoLoadSave,\n}\n")
	if r := c.CheckSave(noMapMod); !strings.Contains(issues(r, SevError), "Real USA Country") || strings.Contains(issues(r, SevError), "Muldraugh") {
		t.Errorf("map missing: %s", r.Text())
	}

	defaultMods := f.save("Sandbox", "nomodstxt", 251, "Real USA Country", "")
	if r := c.CheckSave(defaultMods); !strings.Contains(issues(r, SevError), "Real USA Country") {
		t.Errorf("no mods.txt falls back to default.txt: %s", r.Text())
	}

	missing := f.save("Sandbox", "missingmods", 251, "Muldraugh, KY", "VERSION = 1,\nmods\n{\n    mod = GoneMod,\n    mod = OldMod,\n    mod = ZombieBuddy,\n}\n")
	r := c.CheckSave(missing)
	if r.Blocking() || !r.HasWarnings() || !strings.Contains(issues(r, SevWarning), "GoneMod (not installed)") || !strings.Contains(issues(r, SevWarning), "OldMod (only for game 42.12") {
		t.Errorf("missing mods: %s", r.Text())
	}

	corrupt := Save{Mode: "Sandbox", World: "corrupt", Dir: filepath.Join(f.env.ZomboidDir, "Saves", "Sandbox", "corrupt")}
	writeFile(t, filepath.Join(corrupt.Dir, "map_ver.bin"), "\x00\x00\x00")
	if r := c.CheckSave(corrupt); !strings.Contains(issues(r, SevError), "corrupt") {
		t.Errorf("corrupt: %s", r.Text())
	}
	gone := Save{Mode: "Sandbox", World: "gone", Dir: filepath.Join(f.env.ZomboidDir, "Saves", "Sandbox", "gone")}
	if r := c.CheckSave(gone); !strings.Contains(issues(r, SevError), "missing") {
		t.Errorf("gone: %s", r.Text())
	}

	if r := c.CheckLatest(nil); !r.Blocking() {
		t.Errorf("latest without saves: %s", r.Text())
	}
	if r := c.CheckLatest([]Save{newer, ok}); !strings.Contains(issues(r, SevError), "newer") {
		t.Errorf("latest checks the first save: %s", r.Text())
	}
}

func TestCheckSaveApocalypseDead(t *testing.T) {
	f := newSaveFixture(t, enabledDefaults)
	c := f.checker()
	s := f.save(apocalypseMode, "w", 251, "Muldraugh, KY", "")
	writeDB := func(dead int) {
		p := filepath.Join(s.Dir, "players.db")
		os.Remove(p)
		db, err := sql.Open("sqlite", p)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		for _, q := range []string{
			"CREATE TABLE localPlayers (id INTEGER PRIMARY KEY, name STRING, isDead BOOLEAN)",
		} {
			if _, err := db.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := db.Exec("INSERT INTO localPlayers (id, name, isDead) VALUES (1, 'x', ?)", dead); err != nil {
			t.Fatal(err)
		}
	}
	writeDB(1)
	if r := c.CheckSave(s); !strings.Contains(issues(r, SevError), "dead") {
		t.Errorf("dead apocalypse: %s", r.Text())
	}
	writeDB(0)
	if r := c.CheckSave(s); r.Blocking() {
		t.Errorf("alive apocalypse: %s", r.Text())
	}
	writeFile(t, filepath.Join(s.Dir, "map_p.bin"), "")
	writeDB(1)
	if r := c.CheckSave(s); r.Blocking() {
		t.Errorf("map_p.bin means alive: %s", r.Text())
	}
	sb := f.save("Sandbox", "deadsandbox", 251, "Muldraugh, KY", "")
	if r := c.CheckSave(sb); r.Blocking() {
		t.Errorf("dead check only applies to apocalypse: %s", r.Text())
	}
}

func TestAutoLoadSaveNotEnabled(t *testing.T) {
	for name, defaults := range map[string]string{
		"not listed": "VERSION = 1,\nmods\n{\n    mod = ZombieBuddy,\n}\n",
		"commented":  "VERSION = 1,\nmods\n{\n    /* mod = AutoLoadSave, */\n}\n",
		"no file":    "",
	} {
		f := newSaveFixture(t, defaults)
		if defaults == "" {
			os.Remove(filepath.Join(f.env.ZomboidDir, "mods", "default.txt"))
		}
		s := f.save("Sandbox", "w", 251, "Muldraugh, KY", "")
		r := f.checker().CheckSave(s)
		if !r.Blocking() || !strings.Contains(issues(r, SevError), "AutoLoadSave mod is not enabled") {
			t.Errorf("%s: %s", name, r.Text())
		}
		sr := f.checker().CheckServer(Server{IP: "1.2.3.4", Port: 1}, &Account{Username: "a", PasswordSaved: true}, ServerStatus{Online: true, HasPlayers: true, MaxPlayers: 8})
		if !strings.Contains(issues(sr, SevError), "AutoLoadSave") {
			t.Errorf("%s server: %s", name, sr.Text())
		}
	}
	f := newSaveFixture(t, enabledDefaults)
	os.RemoveAll(filepath.Join(f.env.ZomboidDir, "mods", "ZombieBuddy"))
	r := f.checker().CheckSave(f.save("Sandbox", "w", 251, "Muldraugh, KY", ""))
	if !strings.Contains(issues(r, SevError), "enabled but required mod ZombieBuddy is not installed") {
		t.Errorf("AutoLoadSave without ZombieBuddy: %s", r.Text())
	}
}

func TestCheckWithoutGame(t *testing.T) {
	f := newSaveFixture(t, enabledDefaults)
	f.env.GameDir = ""
	c := f.checker()
	r := c.CheckSave(f.save("Sandbox", "w", 999, "Nowhere", ""))
	if r.Blocking() || !strings.Contains(issues(r, SevWarning), "skipped") {
		t.Errorf("no game: %s", r.Text())
	}
}

func TestCheckServer(t *testing.T) {
	f := newSaveFixture(t, enabledDefaults)
	c := f.checker()
	srv := Server{Name: "S", IP: "10.0.0.5", Port: 16261, ServerPasswordSaved: true}
	acct := &Account{Username: "kata", PasswordSaved: true}
	good := ServerStatus{Online: true, HasPlayers: true, Players: 1, MaxPlayers: 8, Password: true,
		Rules: map[string]string{"version": "42.22", "open": "1", "modCount": "3", "mods:1/1": "ZombieBuddy;AutoLoadSave;NewOne"}}

	r := c.CheckServer(srv, acct, good)
	if r.Worst() != SevInfo || !strings.Contains(issues(r, SevInfo), "NewOne") || strings.Contains(issues(r, SevInfo), "ZombieBuddy,") {
		t.Errorf("good: %s", r.Text())
	}

	if r := c.CheckServer(srv, acct, ServerStatus{}); !strings.Contains(issues(r, SevError), "offline") {
		t.Errorf("offline: %s", r.Text())
	}
	full := good
	full.Players = 8
	if r := c.CheckServer(srv, acct, full); !strings.Contains(issues(r, SevError), "full") {
		t.Errorf("full: %s", r.Text())
	}
	for _, v := range []string{"42.21", "42.22-rc", "42.2"} {
		mm := good
		mm.Rules = map[string]string{"version": v}
		if r := c.CheckServer(srv, acct, mm); !strings.Contains(issues(r, SevError), "Version mismatch") {
			t.Errorf("version %s: %s", v, r.Text())
		}
	}
	kw := ServerStatus{Online: true, HasPlayers: true, MaxPlayers: 8, Keywords: "hidden;modded;pvp;VERSION:42.21"}
	if r := c.CheckServer(srv, acct, kw); !strings.Contains(issues(r, SevError), "42.21") {
		t.Errorf("keyword version: %s", r.Text())
	}

	noPwd := srv
	noPwd.ServerPasswordSaved = false
	if r := c.CheckServer(noPwd, acct, good); !strings.Contains(issues(r, SevWarning), "server password") {
		t.Errorf("password: %s", r.Text())
	}
	closed := good
	closed.Rules = map[string]string{"version": "42.22", "open": "0"}
	if r := c.CheckServer(srv, nil, closed); !strings.Contains(issues(r, SevWarning), "whitelist") {
		t.Errorf("closed no account: %s", r.Text())
	}
	if r := c.CheckServer(srv, acct, closed); strings.Contains(issues(r, SevWarning), "whitelist") {
		t.Errorf("closed with account: %s", r.Text())
	}
	if r := c.CheckServer(srv, &Account{Username: "kata"}, good); !strings.Contains(issues(r, SevWarning), "not saved") {
		t.Errorf("account password: %s", r.Text())
	}
	ping := ServerStatus{Online: true}
	if r := c.CheckServer(srv, acct, ping); r.Blocking() || !r.HasWarnings() {
		t.Errorf("ping only: %s", r.Text())
	}
}
