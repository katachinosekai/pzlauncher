package javamods

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kata/pzlauncher/internal/modlist"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hashOf(t *testing.T, p string) string {
	h, err := HashFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestStatusOf(t *testing.T) {
	es := []Entry{
		{ID: "A", JarHash: "aa", Decision: true},
		{ID: "B", JarHash: "b1", Decision: true},
		{ID: "C", JarHash: "cc", Decision: false},
		{ID: "D", JarHash: "d1", Decision: false},
		{ID: "E", JarHash: "ee", Decision: true},
		{ID: "E", JarHash: "ee", Decision: false},
	}
	cases := []struct {
		id, hash string
		want     Status
	}{
		{"A", "AA", Approved}, {"a", "aa", Approved}, {"B", "b2", ApprovedOlder}, {"C", "cc", Denied},
		{"D", "d2", DeniedOlder}, {"Z", "zz", NeverSeen}, {"A", "", NoJar}, {"E", "ee", Denied},
	}
	for _, c := range cases {
		if got := StatusOf(es, c.id, c.hash); got != c.want {
			t.Errorf("%s/%s = %v want %v", c.id, c.hash, got, c.want)
		}
	}
}

func layout(t *testing.T) (idx *modlist.Index, content string) {
	base := t.TempDir()
	content = filepath.Join(base, "steamapps", "workshop", "content", "108600")
	vp := filepath.Join(content, "3809306528", "mods", "Viewpoint")
	write(t, filepath.Join(vp, "common", "mod.info"), "\ufeffname=Viewpoint\nid=Viewpoint\nmodversion=0.1.7\njavaJarFile=media/java/client/Viewpoint.jar\n")
	write(t, filepath.Join(vp, "42", "media", "java", "client", "Viewpoint.jar"), "jar-v2")
	write(t, filepath.Join(vp, "41", "media", "java", "client", "Viewpoint.jar"), "old")
	zb := filepath.Join(content, "3619862853", "mods", "ZombieBuddy")
	write(t, filepath.Join(zb, "42.13", "mod.info"), "name=ZB\nid=ZombieBuddy\njavaJarFile=libs/ZombieBuddy.jar\n")
	write(t, filepath.Join(zb, "common", "libs", "ZombieBuddy.jar"), "zbjar")
	write(t, filepath.Join(zb, "common", "libs", "ZombieBuddy.jar.zbs"), "sig")
	write(t, filepath.Join(content, "5", "mods", "LuaOnly", "42", "mod.info"), "id=LuaOnly\nname=Lua\n")
	zm := filepath.Join(base, "Zomboid", "mods")
	write(t, filepath.Join(zm, "Local", "42", "mod.info"), "id=Local\nurl=https://steamcommunity.com/sharedfiles/filedetails/?id=777\njavaJarFile=x.jar\n")
	write(t, filepath.Join(zm, "Local", "42", "x.jar"), "x")
	write(t, filepath.Join(zm, "B41Only", "mod.info"), "id=B41Only\njavaJarFile=x.jar\n")
	write(t, filepath.Join(zm, "NoJar", "42", "mod.info"), "id=NoJar\njavaJarFile=missing.jar\n")
	zw := filepath.Join(base, "Zomboid", "Workshop", "MyUpload", "Contents", "mods")
	write(t, filepath.Join(base, "Zomboid", "Workshop", "MyUpload", "workshop.txt"), "version=1\nid=4242\n")
	write(t, filepath.Join(zw, "Up", "42", "mod.info"), "id=Up\njavaJarFile=media/java/up.jar\n")
	write(t, filepath.Join(zw, "Up", "common", "media", "java", "up.jar"), "up")
	game, _ := modlist.ParseVersion("42.22")
	idx = modlist.Scan(modlist.Roots{ZomboidDir: filepath.Join(base, "Zomboid"), WorkshopDir: content}, game)
	return idx, content
}

func TestList(t *testing.T) {
	idx, _ := layout(t)
	mods := List(idx, nil)
	by := map[string]*JavaMod{}
	for _, m := range mods {
		by[m.ID] = m
	}
	if len(mods) != 5 || by["LuaOnly"] != nil {
		t.Fatalf("got %d mods", len(mods))
	}
	vp := by["Viewpoint"]
	if !strings.Contains(filepath.ToSlash(vp.Jar), "/42/media/java/client/Viewpoint.jar") || vp.WorkshopID != "3809306528" || vp.HasZBS || vp.Version != "0.1.7" || vp.Source != "Workshop" {
		t.Fatalf("%+v", vp)
	}
	if vp.JarHash != hashOf(t, vp.Jar) || len(vp.ShortHash()) != 12 {
		t.Fatal(vp.JarHash)
	}
	if !by["ZombieBuddy"].HasZBS || !strings.Contains(filepath.ToSlash(by["ZombieBuddy"].Jar), "/common/libs/") {
		t.Fatalf("%+v", by["ZombieBuddy"])
	}
	if by["Local"].WorkshopID != "777" || by["Local"].Source != "Zomboid/mods" {
		t.Fatalf("%+v", by["Local"])
	}
	if by["Up"].WorkshopID != "4242" || by["Up"].Source != "Zomboid/Workshop" {
		t.Fatalf("%+v", by["Up"])
	}
	if by["NoJar"].Status != NoJar || by["NoJar"].Err == nil {
		t.Fatalf("%+v", by["NoJar"])
	}
	if vp.Status != NeverSeen {
		t.Fatal(vp.Status)
	}
}

const realish = `{
  "formatVersion": 2,
  "mods": [
    {
      "id": "ModManagerExtensionZB",
      "workshop_id": 3809991837,
      "jar_hash": "687c01ab08294bd3e45cd672beb1d950795cc72750662e31dbf3c44f8741f47e",
      "decision": true,
      "author_id": 76561199113018152
    },
    {
      "id": "Viewpoint",
      "workshop_id": 3809306528,
      "jar_hash": "096bd37d63cb699ac95e9e7ea5245a1b9b5ff08c5161db35f6733b1d1c42b064",
      "decision": true
    }
  ],
  "extra": {"keep": 18446744073709551615}
}
`

func TestStoreSet(t *testing.T) {
	roots, _ := layout(t)
	var vp *JavaMod
	for _, m := range List(roots, nil) {
		if m.ID == "Viewpoint" {
			vp = m
		}
	}
	dir := filepath.Join(t.TempDir(), ".zombie_buddy")
	p := filepath.Join(dir, "mod_approvals.json")
	write(t, p, realish)
	day := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	running := true
	s := &Store{Path: p, Running: func() bool { return running }, Now: func() time.Time { return day }}

	if _, err := s.Set(vp, true); !errors.Is(err, ErrGameRunning) {
		t.Fatalf("want ErrGameRunning, got %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != realish {
		t.Fatal("file changed while game running")
	}
	running = false

	es, _ := s.Load()
	if StatusOf(es, vp.ID, vp.JarHash) != ApprovedOlder || es[0].WorkshopID != "3809991837" {
		t.Fatalf("%+v", es)
	}
	changed, err := s.Set(vp, true)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	bk := p + ".launcher-bak-20261119"
	if b, _ := os.ReadFile(bk); string(b) != realish {
		t.Fatal("backup missing or different")
	}
	out, _ := os.ReadFile(p)
	for _, want := range []string{"76561199113018152", "3809991837", "18446744073709551615", "096bd37d63cb699ac95e9e7ea5245a1b9b5ff08c5161db35f6733b1d1c42b064", `"workshop_id": 3809306528,
      "jar_hash": "` + vp.JarHash} {
		if !bytes.Contains(out, []byte(want)) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
	es, _ = s.Load()
	if len(es) != 3 || StatusOf(es, vp.ID, vp.JarHash) != Approved {
		t.Fatalf("%+v", es)
	}
	if changed, _ := s.Set(vp, true); changed {
		t.Fatal("second approve should be a no-op")
	}

	changed, err = s.Set(vp, false)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	es, _ = s.Load()
	if len(es) != 3 || StatusOf(es, vp.ID, vp.JarHash) != Denied || !es[1].Decision {
		t.Fatalf("%+v", es)
	}
	if b, _ := os.ReadFile(bk); string(b) != realish {
		t.Fatal("same-day backup must keep the first version")
	}
	out, _ = os.ReadFile(p)
	var top map[string]json.RawMessage
	if err := json.Unmarshal(out, &top); err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.Token()
	for dec.More() {
		k, _ := dec.Token()
		keys = append(keys, k.(string))
		var skip json.RawMessage
		dec.Decode(&skip)
	}
	if strings.Join(keys, ",") != "formatVersion,mods,extra" {
		t.Fatal(keys)
	}
	if !bytes.Contains(out, []byte("76561199113018152")) {
		t.Fatal("author id lost")
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatal("temp file left: " + e.Name())
		}
	}
}

func TestStoreNewFile(t *testing.T) {
	roots, _ := layout(t)
	var local *JavaMod
	for _, m := range List(roots, nil) {
		if m.ID == "Local" {
			local = m
		}
	}
	p := filepath.Join(t.TempDir(), "zb", "mod_approvals.json")
	s := &Store{Path: p, Running: func() bool { return false }}
	if changed, err := s.Set(local, false); err != nil || !changed {
		t.Fatal(changed, err)
	}
	es, err := s.Load()
	if err != nil || len(es) != 1 || es[0].Decision || es[0].WorkshopID != "777" {
		t.Fatalf("%+v %v", es, err)
	}
	b, _ := os.ReadFile(p)
	if !bytes.HasPrefix(b, []byte("{\n  \"formatVersion\": 2,")) {
		t.Fatalf("%s", b)
	}
	matches, _ := filepath.Glob(p + ".launcher-bak-*")
	if len(matches) != 0 {
		t.Fatal("no backup expected for a new file")
	}
}

func TestStoreBadJSON(t *testing.T) {
	p := filepath.Join(t.TempDir(), "mod_approvals.json")
	write(t, p, "{nope")
	s := &Store{Path: p, Running: func() bool { return false }}
	if _, err := s.Set(&JavaMod{ModInfo: &ModInfo{ID: "x"}, JarHash: "ab"}, true); err == nil {
		t.Fatal("want error")
	}
	if b, _ := os.ReadFile(p); string(b) != "{nope" {
		t.Fatal("bad file was rewritten")
	}
}
