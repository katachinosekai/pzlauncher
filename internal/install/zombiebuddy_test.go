package install

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/katachinosekai/pzlauncher/internal/zbsetup"
)

func zbJar(t *testing.T, p, version string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("META-INF/MANIFEST.MF")
	w.Write([]byte("Manifest-Version: 1.0\r\nPremain-Class: " + zbsetup.PremainClass + "\r\nImplementation-Version: " + version + "\r\n\r\n"))
	zw.Close()
	put(t, p, buf.String())
}

func zbTree(t *testing.T, goos, launch string) *tree {
	tr := newTree(t, goos)
	libs := zbsetup.WorkshopLibs(tr.workshop)
	zbJar(t, filepath.Join(libs, zbsetup.JarName), "2.3.4")
	put(t, filepath.Join(libs, zbsetup.NativeName), "dll")
	put(t, filepath.Join(tr.workshop, zbsetup.WorkshopItem, "mods", "ZombieBuddy", "common", "mod.info"), "id=ZombieBuddy\n")
	vdf := "\"UserLocalConfigStore\"\n{\n\t\"Software\"\n\t{\n\t\t\"Valve\"\n\t\t{\n\t\t\t\"Steam\"\n\t\t\t{\n\t\t\t\t\"apps\"\n\t\t\t\t{\n\t\t\t\t\t\"108600\"\n\t\t\t\t\t{\n\t\t\t\t\t\t\"LaunchOptions\"\t\t\"" + launch + "\"\n\t\t\t\t\t}\n\t\t\t\t}\n\t\t\t}\n\t\t}\n\t}\n}\n"
	os.RemoveAll(filepath.Join(tr.steam, "userdata"))
	put(t, filepath.Join(tr.steam, "userdata", "111", "config", "localconfig.vdf"), vdf)
	put(t, filepath.Join(tr.steam, "userdata", "222", "config", "localconfig.vdf"), strings.Replace(vdf, launch, "-nosound", 1))
	return tr
}

func launchValues(t *testing.T, tr *tree) []string {
	var out []string
	for _, f := range LaunchFiles(tr.steam) {
		v, err := zbsetup.ReadLaunchOptions(get(t, f))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, v...)
	}
	return out
}

func TestZombieBuddySetupLinux(t *testing.T) {
	tr := zbTree(t, "linux", "gamemoderun %command% -")
	var log []string
	o := tr.opts("linux", &log)
	o.SetupZombieBuddy = true
	o.DryRun = true
	before := snapshot(t, tr.root)
	r, err := ZombieBuddy(context.Background(), o)
	if err != nil || len(r.Changes) != 3 {
		t.Fatal(err, r.Changes)
	}
	o.NoLogFile = true
	if d := snapshot(t, tr.root); len(d) != len(before) {
		t.Fatal("dry run wrote files")
	}
	o.DryRun = false
	log = nil
	r, err = ZombieBuddy(context.Background(), o)
	if err != nil {
		t.Fatal(err, log)
	}
	if got := strings.Join(launchValues(t, tr), "|"); got != "gamemoderun %command% -javaagent:ZombieBuddy.jar -- -|-javaagent:ZombieBuddy.jar -- -nosound" {
		t.Fatal(got)
	}
	if zbsetup.InstalledVersion(tr.game) != "2.3.4" || isFile(filepath.Join(tr.game, zbsetup.NativeName)) {
		t.Fatal("jar copy")
	}
	if tr.sys.closes != 1 || tr.sys.starts != 1 {
		t.Fatal("steam", tr.sys.closes, tr.sys.starts)
	}
	joined := strings.Join(r.Changes, "\n")
	if !strings.Contains(joined, "before: gamemoderun %command% -\n    after:  gamemoderun %command% -javaagent:ZombieBuddy.jar -- -") {
		t.Fatal(joined)
	}
	if len(backupDirs(t, tr.root)) != 1 {
		t.Fatal("no backup")
	}
	r, _ = ZombieBuddy(context.Background(), o)
	if len(r.Changes) != 0 {
		t.Fatal("second run changed", r.Changes)
	}

	gameJSON := filepath.Join(tr.game, "ProjectZomboid64.json")
	if err := os.WriteFile(gameJSON, []byte("{\n    \"vmArgs\": [\n        \"-Xmx8192m\",\n        \"-javaagent:ZBBatch.jar\"\n    ]\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	o.SetupZombieBuddy, o.RemoveZombieBuddy = false, true
	r, err = ZombieBuddy(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(gameJSON); strings.Contains(string(b), "ZBBatch") || !strings.Contains(string(b), "-Xmx8192m") {
		t.Fatalf("ZBBatch should leave ProjectZomboid64.json with ZombieBuddy:\n%s", b)
	}
	if got := strings.Join(launchValues(t, tr), "|"); got != "gamemoderun %command% -|-nosound" {
		t.Fatal(got)
	}
	if !isFile(filepath.Join(tr.game, zbsetup.JarName)) {
		t.Fatal("jar removed without asking")
	}
	o.RemoveZombieBuddyFiles = true
	if r, err = ZombieBuddy(context.Background(), o); err != nil || isFile(filepath.Join(tr.game, zbsetup.JarName)) {
		t.Fatal("jar not removed", err, r.Changes)
	}
}

func TestZombieBuddyRefusesWhileRunning(t *testing.T) {
	tr := zbTree(t, "linux", "")
	tr.sys.running["ProjectZomboid64"] = true
	var log []string
	o := tr.opts("linux", &log)
	o.SetupZombieBuddy = true
	if _, err := ZombieBuddy(context.Background(), o); err == nil {
		t.Fatal("ran while the game was running")
	}
	if isFile(filepath.Join(tr.game, zbsetup.JarName)) {
		t.Fatal("copied")
	}
}

func TestRunWithZombieBuddyWindows(t *testing.T) {
	tr := zbTree(t, "windows", "-nojoy")
	var log []string
	o := tr.opts("windows", &log)
	o.SetupZombieBuddy = true
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err, strings.Join(log, "\n"))
	}
	if got := strings.Join(launchValues(t, tr), "|"); got != "-agentlib:zbNative -javaagent:ZBBatch.jar -- -nojoy|-agentlib:zbNative -javaagent:ZBBatch.jar -- -nosound" {
		t.Fatal(got)
	}
	if !isFile(filepath.Join(tr.game, zbsetup.NativeName)) || zbsetup.InstalledVersion(tr.game) != "2.3.4" {
		t.Fatal("files")
	}
	if tr.sys.running[steamProcess] != true {
		t.Fatal("Steam not started again")
	}
	o.SetupZombieBuddy, o.RemoveZombieBuddy = false, true
	if _, err := ZombieBuddy(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(launchValues(t, tr), "|"); got != "-nojoy|-nosound" {
		t.Fatal(got)
	}
}

func TestRunWithoutZombieBuddyOption(t *testing.T) {
	tr := zbTree(t, "linux", "gamemoderun %command%")
	var log []string
	if _, err := Run(context.Background(), tr.opts("linux", &log)); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(launchValues(t, tr), "|"); got != "gamemoderun %command%|-nosound" {
		t.Fatal(got)
	}
	if isFile(filepath.Join(tr.game, zbsetup.JarName)) {
		t.Fatal("ZombieBuddy set up without the option")
	}
}
