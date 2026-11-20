package diag

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/katachinosekai/pzlauncher/internal/modlist"
)

func write(t *testing.T, p, s string, mt time.Time) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	if !mt.IsZero() {
		os.Chtimes(p, mt, mt)
	}
}

func TestKeepRotation(t *testing.T) {
	z := t.TempDir()
	dest := filepath.Join(t.TempDir(), "PZLauncher", "logs")
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.Local)
	for i := 0; i < 13; i++ {
		mt := base.Add(time.Duration(i) * time.Hour)
		write(t, filepath.Join(z, "console.txt"), fmt.Sprintf("run %d", i), mt)
		kept, err := Keep(z, dest, 10)
		if err != nil || len(kept) != 1 {
			t.Fatalf("run %d: %v %v", i, kept, err)
		}
		if kept, _ := Keep(z, dest, 10); len(kept) != 0 {
			t.Fatal("same log kept twice")
		}
	}
	names := keptOf(dest, "console")
	if len(names) != 10 || names[0] != "console-"+base.Add(3*time.Hour).Format("20060102-150405")+".txt" {
		t.Fatal(names)
	}
	b, _ := os.ReadFile(filepath.Join(dest, names[9]))
	if string(b) != "run 12" {
		t.Fatal(string(b))
	}

	write(t, filepath.Join(z, "coop-console.txt"), "coop", base)
	write(t, filepath.Join(z, "Logs", "2026-11-19_04-24_DebugLog.txt"), "old debug", base)
	write(t, filepath.Join(z, "Logs", "2026-11-19_05-00_DebugLog.txt"), "new debug", base.Add(time.Hour))
	write(t, filepath.Join(z, "Logs", "2026-11-19_05-00_client chat Bob.txt"), "chat", base.Add(time.Hour))
	write(t, filepath.Join(z, "Logs", "logs_2026-11-02_16-25", "x_DebugLog.txt"), "archived", base.Add(5*time.Hour))
	kept, err := Keep(z, dest, 10)
	if err != nil || len(kept) != 2 {
		t.Fatal(kept, err)
	}
	if len(keptOf(dest, "console")) != 10 || len(keptOf(dest, "coop-console")) != 1 {
		t.Fatal("kinds mixed up")
	}
	d := keptOf(dest, "DebugLog")
	b, _ = os.ReadFile(filepath.Join(dest, d[0]))
	if len(d) != 1 || string(b) != "new debug" {
		t.Fatal(d, string(b))
	}
}

func TestKeepNothing(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "logs")
	kept, err := Keep(t.TempDir(), dest, 10)
	if err != nil || kept != nil {
		t.Fatal(kept, err)
	}
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("dest created without logs")
	}
}

func TestRedact(t *testing.T) {
	in := "user 76561198205856420 joined; ServerPassword=hunter2 password = s3cret&x=1\n\"password\": \"abc\\\"d\", -password pw1 id 76561197960265728\nAdminPassword=adm"
	out := Redact(in)
	for _, bad := range []string{"76561198205856420", "hunter2", "s3cret", "abc", "pw1", "adm\n", "76561197960265728"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q not redacted: %s", bad, out)
		}
	}
	if strings.Contains(out, "adm") && strings.HasSuffix(out, "adm") {
		t.Error(out)
	}
	if !strings.Contains(out, "ServerPassword=<redacted>") || !strings.Contains(out, "&x=1") || !strings.Contains(out, "7656119##########") {
		t.Error(out)
	}
	if Redact("build 42.22 seed 1234567890123456") != "build 42.22 seed 1234567890123456" {
		t.Error("unrelated numbers changed")
	}
}

func TestEnabledMods(t *testing.T) {
	got := EnabledMods("VERSION = 1,\n\nmods\n{\n    mod = ZombieBuddy,\n    mod = \\AutoLoadSave,\r\n}\n\nmaps\n{\n    map = Muldraugh, KY,\n}\n")
	if strings.Join(got, ",") != "ZombieBuddy,AutoLoadSave" {
		t.Fatal(got)
	}
}

func TestCollect(t *testing.T) {
	z := t.TempDir()
	kept := t.TempDir()
	inst := t.TempDir()
	game := t.TempDir()
	write(t, filepath.Join(z, "console.txt"), "steam id 76561198000000001 Password=pw", time.Time{})
	write(t, filepath.Join(z, "Logs", "2026-11-19_05-00_DebugLog.txt"), "debug", time.Time{})
	write(t, filepath.Join(z, "version.txt"), "42.22", time.Time{})
	write(t, filepath.Join(z, "mods", "default.txt"), "mods\n{\n    mod = ModA,\n    mod = Gone,\n}\n", time.Time{})
	write(t, filepath.Join(z, "mods", "ModA", "42", "mod.info"), "id=ModA\nname=Mod A\nmodversion=1.2\njavaJarFile=a.jar\n", time.Time{})
	write(t, filepath.Join(kept, "console-20261119-040000.txt"), "old 76561198000000002", time.Time{})
	for i := 0; i < 12; i++ {
		write(t, filepath.Join(inst, fmt.Sprintf("install-20261119-0%02d000.log", i)), "inst", time.Time{})
	}
	write(t, filepath.Join(game, "ProjectZomboid64.json"), `{"vmArgs":["-Xmx8192m"]}`, time.Time{})
	var buf bytes.Buffer
	files, err := Collect(&buf, CollectOptions{
		ZomboidDir: z, KeptDir: kept, InstallerLogDirs: []string{inst, filepath.Join(inst, "none")},
		GameDir: game, Mods: modlist.Scan(modlist.Roots{ZomboidDir: z}, modlist.Version{Major: 42, Minor: 22}), LauncherVersion: "1.2.3",
		Now: func() time.Time { return time.Date(2026, 11, 19, 6, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	content := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		content[f.Name] = string(b)
	}
	if len(content) != len(files) {
		t.Fatal(files)
	}
	for _, n := range []string{"system.txt", "kept-logs/console-20261119-040000.txt", "current/console.txt", "current/2026-11-19_05-00_DebugLog.txt", "mods/default.txt", "ProjectZomboid64.json", "mods.txt", "installer/0-install-20261119-011000.log"} {
		if _, ok := content[n]; !ok {
			t.Errorf("missing %s in %v", n, files)
		}
	}
	if _, ok := content["installer/0-install-20261119-001000.log"]; ok {
		t.Error("only the last 10 installer logs expected")
	}
	all := strings.Join(func() []string {
		var s []string
		for _, v := range content {
			s = append(s, v)
		}
		return s
	}(), "\n")
	if strings.Contains(all, "76561198000000001") || strings.Contains(all, "76561198000000002") || strings.Contains(all, "=pw") {
		t.Error("not redacted")
	}
	sys := content["system.txt"]
	if !strings.Contains(sys, "Launcher version: 1.2.3") || !strings.Contains(sys, "Game dir: "+game) || !strings.Contains(sys, "version.txt: 42.22") || !strings.Contains(sys, "RAM: ") {
		t.Error(sys)
	}
	ml := content["mods.txt"]
	if !strings.Contains(ml, "ModA\t1.2") || !strings.Contains(ml, "Gone\tNOT INSTALLED") || !strings.Contains(ml, "* ModA\t1.2") || !strings.Contains(ml, "java:a.jar") {
		t.Error(ml)
	}
}
