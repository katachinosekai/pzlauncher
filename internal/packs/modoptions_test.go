package packs

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildModOptions(t *testing.T) {
	z := t.TempDir()
	os.MkdirAll(filepath.Join(z, "Lua"), 0o755)
	os.WriteFile(filepath.Join(z, "Lua", "ModOptions.ini"), []byte("tickbox|ModA|on|true\r\nkeybind|ModA|key|24\r\nslider|ModB|v|3\r\n"), 0o644)
	os.WriteFile(filepath.Join(z, "Lua", "SampleMod.ini"), []byte("a=1\n"), 0o644)
	for _, s := range DetectSettings(filepath.Join(z, "Lua"), nil) {
		if s.Rel == "ModOptions.ini" {
			t.Fatal("ModOptions.ini offered as a whole file")
		}
	}
	dest := filepath.Join(t.TempDir(), "p.zip")
	if _, err := Build(dest, BuildOptions{Name: "P", ZomboidDir: z, ModOptions: []string{"ModA"}}); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	got := ""
	for _, f := range zr.File {
		if f.Name == "P/settings/Lua/ModOptions.ini" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			got = string(b)
		}
	}
	if got != "tickbox|ModA|on|true\r\n" {
		t.Fatalf("pack ModOptions.ini = %q", got)
	}
	if _, err := Build(dest, BuildOptions{Name: "P", ZomboidDir: z, Lua: []string{"ModOptions.ini"}}); err == nil {
		t.Fatal("whole ModOptions.ini accepted")
	}
	if _, err := Build(dest, BuildOptions{Name: "P", ZomboidDir: z, ModOptions: []string{"Nobody"}}); err == nil {
		t.Fatal("empty Mod Options choice accepted")
	}
	os.WriteFile(filepath.Join(z, "Lua", "ModOptions.ini"), []byte("textentry|ModA|owner|76561190000000000\r\n"), 0o644)
	if _, err := Build(dest, BuildOptions{Name: "P", ZomboidDir: z, ModOptions: []string{"ModA"}}); err == nil {
		t.Fatal("Steam ID accepted")
	}
}
