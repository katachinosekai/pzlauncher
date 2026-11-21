package install

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsMergeValueByValue(t *testing.T) {
	tr := newTree(t, "linux")
	put(t, filepath.Join(tr.zomboid, "Lua", "ModOptions.ini"), "tickbox|ModA|enabled|true\r\nslider|ModA|volume|40\r\nkeybind|ModA|open|24\r\ntickbox|Mine|only|true\r\n")
	put(t, filepath.Join(tr.pack, "settings", "Lua", "ModOptions.ini"), "tickbox|ModA|enabled|false\r\nslider|ModA|volume|40\r\ncombobox|ModB|mode|2\r\n")
	put(t, filepath.Join(tr.zomboid, "Lua", "SampleMod.ini"), "\xef\xbb\xbf# mine\r\n[Main]\r\nspeed=1\r\nmyown=yes\r\n")
	put(t, filepath.Join(tr.pack, "settings", "Lua", "SampleMod.ini"), "[Main]\nspeed=3\nfresh=on\n")

	var dry []string
	o := tr.opts("linux", &dry)
	o.DryRun = true
	rep, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(dry, "\n"))
	}
	all := strings.Join(rep.Changes, "\n")
	for _, want := range []string{
		"Lua/ModOptions.ini: ModA enabled: true -> false",
		"Lua/ModOptions.ini: ModB mode: (new) -> 2",
		"Lua/SampleMod.ini: [Main] speed: 1 -> 3",
		"Lua/SampleMod.ini: [Main] fresh: (new) -> on",
		"Lua/keysB42.ini: whole file replaced (yours is backed up)",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("preview misses %q:\n%s", want, all)
		}
	}
	if strings.Contains(all, "volume") {
		t.Errorf("unchanged value listed:\n%s", all)
	}
	if get(t, filepath.Join(tr.zomboid, "Lua", "ModOptions.ini")) != "tickbox|ModA|enabled|true\r\nslider|ModA|volume|40\r\nkeybind|ModA|open|24\r\ntickbox|Mine|only|true\r\n" {
		t.Fatal("dry run wrote ModOptions.ini")
	}

	var log []string
	if _, err := Run(context.Background(), tr.opts("linux", &log)); err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(log, "\n"))
	}
	if got := get(t, filepath.Join(tr.zomboid, "Lua", "ModOptions.ini")); got != "tickbox|ModA|enabled|false\r\nslider|ModA|volume|40\r\nkeybind|ModA|open|24\r\ntickbox|Mine|only|true\r\ncombobox|ModB|mode|2\r\n" {
		t.Fatalf("ModOptions.ini = %q", got)
	}
	if got := get(t, filepath.Join(tr.zomboid, "Lua", "SampleMod.ini")); got != "\xef\xbb\xbf# mine\r\n[Main]\r\nspeed=3\r\nmyown=yes\r\nfresh=on\r\n" {
		t.Fatalf("SampleMod.ini = %q", got)
	}
	bk := backupDirs(t, tr.root)
	if len(bk) == 0 {
		t.Fatal("no backup")
	}
	if got := get(t, filepath.Join(bk[0], "Zomboid", "Lua", "ModOptions.ini")); !strings.Contains(got, "ModA|enabled|true") {
		t.Fatalf("backup = %q", got)
	}
}
