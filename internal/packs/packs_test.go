package packs

import (
	"archive/zip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSource(t *testing.T) {
	for in, want := range map[string]Source{
		"me/pack":                                                      {Repo: "me/pack"},
		"https://github.com/me/pack":                                   {Repo: "me/pack"},
		"https://github.com/me/pack.git/":                              {Repo: "me/pack"},
		"https://github.com/me/pack/releases/tag/v2":                   {Repo: "me/pack", Tag: "v2"},
		"https://github.com/me/pack/releases/download/v2/PZ-Fixes.zip": {Repo: "me/pack", Tag: "v2", Asset: "PZ-Fixes.zip"},
		"https://github.com/me/pack/releases/latest/download/x.zip":    {Repo: "me/pack", Asset: "x.zip"},
		"https://example.com/files/pack.zip":                           {URL: "https://example.com/files/pack.zip"},
	} {
		got, err := ParseSource(in)
		if err != nil || got != want {
			t.Errorf("%q: got %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{"http://example.com/p.zip", "https://example.com/page", "file:///etc/passwd", "ftp://x/p.zip", "not a link", "https://github.com/onlyowner"} {
		if _, err := ParseSource(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestFetchLatestRelease(t *testing.T) {
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/me/pack/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"assets": []map[string]string{
			{"name": "notes.txt", "browser_download_url": "https://x/notes.txt"},
			{"name": "PZ-Fixes.zip", "browser_download_url": srv.URL + "/dl/PZ-Fixes.zip"},
		}})
	})
	mux.HandleFunc("/dl/PZ-Fixes.zip", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("zipdata")) })
	srv = httptest.NewTLSServer(mux)
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), APIBase: srv.URL}
	p, err := c.Fetch(context.Background(), Source{Repo: "me/pack"}, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "zipdata" || filepath.Base(p) != "PZ-Fixes.zip" {
		t.Fatalf("got %s %q", p, b)
	}
	if _, err := c.Fetch(context.Background(), Source{Repo: "me/missing"}, t.TempDir(), nil); err == nil || !strings.Contains(err.Error(), "private") {
		t.Fatalf("missing repo: %v", err)
	}
}

func TestDetectAndBuild(t *testing.T) {
	z := t.TempDir()
	lua := filepath.Join(z, "Lua")
	write := func(p, s string) {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(s), 0o644)
	}
	write(filepath.Join(lua, "knox_settings.txt"), "a=1")
	write(filepath.Join(lua, "knox_presets", "My preset.txt"), "p=1")
	write(filepath.Join(lua, "alife_menu_prefs.ini"), "x=1")
	write(filepath.Join(lua, "keysB42.ini"), "k=1")
	write(filepath.Join(lua, "AutoLua.txt"), "print(1)")
	write(filepath.Join(lua, "saved_builds.txt"), "b")
	write(filepath.Join(lua, "friends.ini"), "owner=76561198000000001")
	write(filepath.Join(z, "mods", "default.txt"), "mods\n{\n}\n")
	write(filepath.Join(z, "Sandbox Presets", "Solo.cfg"), "Zombies = 4")
	zb := t.TempDir()
	write(filepath.Join(zb, "zbbatch.properties"), "calibrated=true\n")
	mods := []ModRef{
		{ID: "KnoxInventory", Name: "Knox Inventory"},
		{ID: "ProjectALifeNPCs", Name: "Project A-Life"},
		{ID: "ALifeCorpseSync", Name: "A-Life Corpse Loot Sync", Requires: []string{"ProjectALifeNPCs"}},
	}
	got := map[string]Setting{}
	for _, s := range DetectSettings(lua, mods) {
		got[s.Rel] = s
	}
	if got["knox_settings.txt"].OwnerID != "KnoxInventory" || got["knox_presets"].OwnerID != "KnoxInventory" {
		t.Fatalf("knox owner: %+v", got["knox_settings.txt"])
	}
	if got["alife_menu_prefs.ini"].OwnerID != "ProjectALifeNPCs" {
		t.Fatalf("alife owner should be the base mod, got %q", got["alife_menu_prefs.ini"].OwnerID)
	}
	for _, n := range []string{"AutoLua.txt", "saved_builds.txt", "friends.ini"} {
		if got[n].Shareable() {
			t.Fatalf("%s must not be shareable", n)
		}
	}
	dest := filepath.Join(t.TempDir(), "My Pack.zip")
	o := BuildOptions{Name: "My Pack", ZomboidDir: z, ZBHome: zb, Lua: []string{"knox_settings.txt", "knox_presets", "keysB42.ini"}, ModList: true, Presets: []string{"Solo.cfg"}, ZBBatch: true}
	if _, err := Build(dest, o); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	have := map[string]bool{}
	for _, f := range zr.File {
		have[f.Name] = true
	}
	for _, n := range []string{"My Pack/settings/Lua/knox_settings.txt", "My Pack/settings/Lua/knox_presets/My preset.txt", "My Pack/settings/mods/default.txt", "My Pack/presets/Solo.cfg", "My Pack/zbbatch/zbbatch.properties", "My Pack/README.md"} {
		if !have[n] {
			t.Errorf("missing %s in %v", n, have)
		}
	}
	for _, bad := range [][]string{{"AutoLua.txt"}, {"friends.ini"}, {"../escape.txt"}} {
		o.Lua = bad
		if _, err := Build(filepath.Join(t.TempDir(), "x.zip"), o); err == nil {
			t.Errorf("%v packed", bad)
		}
	}
}

func TestBuildRefusesLinks(t *testing.T) {
	z := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("private"), 0o644)
	os.MkdirAll(filepath.Join(z, "Lua"), 0o755)
	if err := os.Symlink(outside, filepath.Join(z, "Lua", "knox_settings.txt")); err != nil {
		t.Skip(err)
	}
	for _, s := range DetectSettings(filepath.Join(z, "Lua"), nil) {
		t.Fatalf("link offered: %+v", s)
	}
	if _, err := Build(filepath.Join(t.TempDir(), "p.zip"), BuildOptions{ZomboidDir: z, Lua: []string{"knox_settings.txt"}}); err == nil {
		t.Fatal("link packed")
	}
}
