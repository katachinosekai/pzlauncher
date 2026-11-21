package zbsetup

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func makeJar(t *testing.T, path, version, premain string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("META-INF/MANIFEST.MF")
	m := "Manifest-Version: 1.0\r\n"
	if premain != "" {
		m += "Premain-Class: " + premain + "\r\n"
	}
	if version != "" {
		m += "Implementation-Version: " + version + "\r\n"
	}
	m += "\r\nName: a/B.class\r\nImplementation-Version: 9.9.9\r\n"
	w.Write([]byte(m))
	c, _ := zw.Create("a/B.class")
	c.Write([]byte(version))
	zw.Close()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseManifest(t *testing.T) {
	m := ParseManifest(strings.NewReader("Manifest-Version: 1.0\r\nPremain-Class: me.zed_0xff.zombie_buddy.Agent\r\nImplementation-Version: 2.3.4\r\nLong: abc\r\n def\r\n\r\nName: x\r\nImplementation-Version: 1.0\r\n"))
	if m["Implementation-Version"] != "2.3.4" || m["Premain-Class"] != PremainClass || m["Long"] != "abcdef" {
		t.Fatalf("%v", m)
	}
	dir := t.TempDir()
	good := filepath.Join(dir, "good.jar")
	makeJar(t, good, "2.3.4", PremainClass)
	if info, err := VerifyJar(good); err != nil || info.Version != "2.3.4" {
		t.Fatal(info, err)
	}
	bad := filepath.Join(dir, "bad.jar")
	makeJar(t, bad, "1.0", "other.Agent")
	if _, err := VerifyJar(bad); !errors.Is(err, ErrNotZB) {
		t.Fatal(err)
	}
	notzip := filepath.Join(dir, "x.jar")
	os.WriteFile(notzip, []byte("hello"), 0o644)
	if _, err := VerifyJar(notzip); err == nil {
		t.Fatal("not a zip accepted")
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{{"2.3.4", "2.3.3", 1}, {"v2.3.4", "2.3.4", 0}, {"2.3.10", "2.3.9", 1}, {"2.3", "2.3.0", 0}, {"2.2.9", "2.3.0", -1}}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%s,%s)=%d", c.a, c.b, got)
		}
	}
	if !Newer("2.3.4", "") || Newer("", "2.3.4") || Newer("2.3.3", "2.3.4") {
		t.Fatal("Newer")
	}
}

const releasesFixture = `[
 {"tag_name":"windows_installer_4.4","draft":false,"prerelease":false,"published_at":"2026-10-20T19:27:21Z","assets":[{"name":"ZombieBuddyInstaller_v4.4.exe","browser_download_url":"https://x/i.exe","size":5}]},
 {"tag_name":"v2.3.3","draft":false,"prerelease":false,"published_at":"2026-07-30T09:46:25Z","assets":[
   {"name":"zbNative.dll","browser_download_url":"https://x/v2.3.3/zbNative.dll","size":5120,"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
   {"name":"ZombieBuddy.jar","browser_download_url":"https://x/v2.3.3/ZombieBuddy.jar","size":13254997,"digest":"sha256:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"},
   {"name":"ZombieBuddy.jar.zbs","browser_download_url":"https://x/v2.3.3/ZombieBuddy.jar.zbs","size":171}]},
 {"tag_name":"v2.3.4","draft":false,"prerelease":false,"published_at":"2026-10-03T13:03:19Z","assets":[{"name":"ZombieBuddy.jar","browser_download_url":"https://x/v2.3.4/ZombieBuddy.jar","size":13254999}]},
 {"tag_name":"v2.4.0","draft":false,"prerelease":true,"published_at":"2026-11-01T00:00:00Z","assets":[{"name":"ZombieBuddy.jar","browser_download_url":"https://x/v2.4.0/ZombieBuddy.jar","size":1}]},
 {"tag_name":"v9.0.0","draft":true,"assets":[{"name":"ZombieBuddy.jar"}]},
 {"tag_name":"v2.0.0","draft":false,"assets":[]}
]`

func TestParseReleases(t *testing.T) {
	rels, err := ParseReleases([]byte(releasesFixture))
	if err != nil {
		t.Fatal(err)
	}
	var tags []string
	for _, r := range rels {
		tags = append(tags, r.Tag)
	}
	if strings.Join(tags, " ") != "v2.4.0 v2.3.4 v2.3.3" {
		t.Fatal(tags)
	}
	if l := Latest(rels); l == nil || l.Tag != "v2.3.4" {
		t.Fatal(l)
	}
	r := rels[2]
	if r.Jar.SHA256 != strings.Repeat("b", 64) || r.Native == nil || r.ZBS == nil || r.ZBS.SHA256 != "" || r.Version != "2.3.3" {
		t.Fatalf("%+v", r)
	}
	if rels[1].Jar.SHA256 != "" {
		t.Fatal("no digest expected")
	}
}

func TestReleasesCache(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls > 1 {
			http.Error(w, "rate limited", http.StatusForbidden)
			return
		}
		w.Write([]byte(releasesFixture))
	}))
	defer srv.Close()
	now := time.Now()
	c := &Client{API: srv.URL, CacheDir: t.TempDir(), TTL: time.Hour, Now: func() time.Time { return now }}
	r, err := c.Releases(context.Background(), false)
	if err != nil || r.FromCache || len(r.Releases) != 3 {
		t.Fatal(r, err)
	}
	r, err = c.Releases(context.Background(), false)
	if err != nil || !r.FromCache || calls != 1 {
		t.Fatal(r, err, calls)
	}
	now = now.Add(2 * time.Hour)
	r, err = c.Releases(context.Background(), false)
	if err != nil || !r.Stale || r.FetchErr == nil || len(r.Releases) != 3 {
		t.Fatal(r, err)
	}
}

func TestLaunchEdits(t *testing.T) {
	cases := []struct {
		name, goos, in, add, rm string
	}{
		{"empty linux", "linux", "", "-javaagent:ZombieBuddy.jar --", ""},
		{"empty windows", "windows", "", "-agentlib:zbNative --", ""},
		{"user real", "linux", "gamemoderun %command% -javaagent:ZombieBuddy.jar -- -", "gamemoderun %command% -javaagent:ZombieBuddy.jar -- -", "gamemoderun %command% -"},
		{"malloc prefix kept", "linux", "MALLOC_ARENA_MAX=2 gamemoderun %command% -javaagent:ZombieBuddy.jar -- -", "MALLOC_ARENA_MAX=2 gamemoderun %command% -javaagent:ZombieBuddy.jar -- -", "MALLOC_ARENA_MAX=2 gamemoderun %command% -"},
		{"malloc prefix add", "linux", "MALLOC_ARENA_MAX=2 gamemoderun %command% -", "MALLOC_ARENA_MAX=2 gamemoderun %command% -javaagent:ZombieBuddy.jar -- -", "MALLOC_ARENA_MAX=2 gamemoderun %command% -"},
		{"gamemoderun add", "linux", "gamemoderun %command% -", "gamemoderun %command% -javaagent:ZombieBuddy.jar -- -", "gamemoderun %command% -"},
		{"gamemoderun bare", "linux", "gamemoderun %command%", "gamemoderun %command% -javaagent:ZombieBuddy.jar --", "gamemoderun %command%"},
		{"no command, game args", "linux", "-debug", "-javaagent:ZombieBuddy.jar -- -debug", "-debug"},
		{"windows present policy", "windows", "-agentlib:zbNative=policy=deny-new --", "-agentlib:zbNative=policy=deny-new --", ""},
		{"windows zbbatch", "windows", "-agentlib:zbNative -javaagent:ZBBatch.jar -- -nosound", "-agentlib:zbNative -javaagent:ZBBatch.jar -- -nosound", "-nosound"},
		{"missing separator", "linux", "%command% -javaagent:ZombieBuddy.jar", "%command% -javaagent:ZombieBuddy.jar --", "%command%"},
		{"other jvm flag keeps --", "linux", "%command% -Dfoo=1 -- -debug", "%command% -javaagent:ZombieBuddy.jar -Dfoo=1 -- -debug", "%command% -Dfoo=1 -- -debug"},
		{"wrong os loader", "linux", "%command% -agentlib:zbNative --", "%command% -javaagent:ZombieBuddy.jar --", "%command%"},
		{"gamescope -- before command", "linux", "gamescope -f -- gamemoderun %command%", "gamescope -f -- gamemoderun %command% -javaagent:ZombieBuddy.jar --", "gamescope -f -- gamemoderun %command%"},
		{"env with escaped quotes", "linux", `X=\"a b\" %command%`, `X=\"a b\" %command% -javaagent:ZombieBuddy.jar --`, `X=\"a b\" %command%`},
	}
	for _, c := range cases {
		got, _ := AddLoader(c.in, c.goos)
		if got != c.add {
			t.Errorf("%s add: %q want %q", c.name, got, c.add)
		}
		if !HasLoader(got, c.goos) {
			t.Errorf("%s: HasLoader false after add", c.name)
		}
		again, changed := AddLoader(got, c.goos)
		if changed || again != got {
			t.Errorf("%s: add not idempotent: %q", c.name, again)
		}
		rm, _ := RemoveLoader(got)
		if rm != c.rm {
			t.Errorf("%s rm: %q want %q", c.name, rm, c.rm)
		}
		if HasAnyLoader(rm) {
			t.Errorf("%s: loader left", c.name)
		}
	}
	if v, ch := RemoveLoader("gamemoderun %command%"); ch || v != "gamemoderun %command%" {
		t.Fatal("remove without loader changed", v)
	}
}

const vdfReal = "\"UserLocalConfigStore\"\n{\n\t\"Software\"\n\t{\n\t\t\"Valve\"\n\t\t{\n\t\t\t\"Steam\"\n\t\t\t{\n\t\t\t\t\"apps\"\n\t\t\t\t{\n\t\t\t\t\t\"108600\"\n\t\t\t\t\t{\n\t\t\t\t\t\t\"LastPlayed\"\t\t\"1\"\n\t\t\t\t\t\t\"LaunchOptions\"\t\t\"gamemoderun %command% -javaagent:ZombieBuddy.jar -- -\"\n\t\t\t\t\t}\n\t\t\t\t\t\"9420\"\n\t\t\t\t\t{\n\t\t\t\t\t\t\"LaunchOptions\"\t\t\"gamemoderun %command%\"\n\t\t\t\t\t}\n\t\t\t\t}\n\t\t\t}\n\t\t}\n\t}\n\t\"apps\"\n\t{\n\t\t\"108600\"\n\t\t{\n\t\t\t\"DefaultLaunchOption\"\n\t\t\t{\n\t\t\t\t\"x\"\t\t\"0\"\n\t\t\t}\n\t\t}\n\t}\n}\n"

func TestEditLocalConfig(t *testing.T) {
	rm := func(v string) string { s, _ := RemoveLoader(v); return s }
	add := func(v string) string { s, _ := AddLoader(v, "linux"); return s }
	out, edits, err := EditLocalConfig(vdfReal, rm)
	if err != nil || len(edits) != 1 || edits[0].After != "gamemoderun %command% -" {
		t.Fatal(edits, err)
	}
	if strings.Replace(vdfReal, "gamemoderun %command% -javaagent:ZombieBuddy.jar -- -", "gamemoderun %command% -", 1) != out {
		t.Fatal(out)
	}
	back, edits, _ := EditLocalConfig(out, add)
	if back != vdfReal || len(edits) != 1 {
		t.Fatal(back)
	}
	noLO := strings.Replace(vdfReal, "\t\t\t\t\t\t\"LaunchOptions\"\t\t\"gamemoderun %command% -javaagent:ZombieBuddy.jar -- -\"\n", "", 1)
	ins, edits, err := EditLocalConfig(noLO, add)
	if err != nil || len(edits) != 1 || !edits[0].Inserted {
		t.Fatal(edits, err)
	}
	want := strings.Replace(vdfReal, "gamemoderun %command% -javaagent:ZombieBuddy.jar -- -", "-javaagent:ZombieBuddy.jar --", 1)
	want = strings.Replace(want, "\t\t\t\t\t\t\"LastPlayed\"\t\t\"1\"\n\t\t\t\t\t\t\"LaunchOptions\"", "\t\t\t\t\t\t\"LastPlayed\"\t\t\"1\"\n\t\t\t\t\t\t\"LaunchOptions\"", 1)
	if ins != want {
		t.Fatalf("insert:\n%s", ins)
	}
	if opts, _ := ReadLaunchOptions(ins); len(opts) != 1 || opts[0] != "-javaagent:ZombieBuddy.jar --" {
		t.Fatal(opts)
	}
	if _, edits, _ := EditLocalConfig(noLO, rm); len(edits) != 0 {
		t.Fatal("remove inserted something")
	}
	noApp := "\"UserLocalConfigStore\"\r\n{\r\n\t\"Software\"\r\n\t{\r\n\t\t\"Valve\"\r\n\t\t{\r\n\t\t\t\"Steam\"\r\n\t\t\t{\r\n\t\t\t\t\"apps\"\r\n\t\t\t\t{\r\n\t\t\t\t}\r\n\t\t\t}\r\n\t\t}\r\n\t}\r\n}\r\n"
	ins, _, err = EditLocalConfig(noApp, add)
	if err != nil || !strings.Contains(ins, "\t\t\t\t\t\"108600\"\r\n\t\t\t\t\t{\r\n\t\t\t\t\t\t\"LaunchOptions\"\t\t\"-javaagent:ZombieBuddy.jar --\"\r\n\t\t\t\t\t}\r\n\t\t\t\t}") {
		t.Fatalf("%q %v", ins, err)
	}
	if opts, _ := ReadLaunchOptions(ins); len(opts) != 1 {
		t.Fatal(opts)
	}
	if _, _, err := EditLocalConfig("\"UserLocalConfigStore\"\n{\n}\n", add); !errors.Is(err, ErrNoApps) {
		t.Fatal(err)
	}
	if _, _, err := EditLocalConfig("\"a\"\n{\n", add); err == nil {
		t.Fatal("broken vdf accepted")
	}
}

func TestReplaceJar(t *testing.T) {
	game := t.TempDir()
	src := t.TempDir()
	makeJar(t, filepath.Join(game, JarName), "2.3.3", PremainClass)
	makeJar(t, filepath.Join(src, "new.jar"), "2.3.4", PremainClass)
	makeJar(t, filepath.Join(src, "bad.jar"), "2.3.4", "x.Agent")
	running := true
	env := JarEnv{GameDir: game, GOOS: "linux", Running: func() bool { return running }}
	if _, err := ReplaceJar(env, filepath.Join(src, "new.jar")); !errors.Is(err, ErrGameRunning) {
		t.Fatal(err)
	}
	if InstalledVersion(game) != "2.3.3" {
		t.Fatal("jar changed while running")
	}
	running = false
	if _, err := ReplaceJar(env, filepath.Join(src, "bad.jar")); !errors.Is(err, ErrNotZB) {
		t.Fatal(err)
	}
	r, err := ReplaceJar(env, filepath.Join(src, "new.jar"))
	if err != nil || r.OldVersion != "2.3.3" || r.NewVersion != "2.3.4" || r.Backup != filepath.Join(game, JarName+".launcher-bak-2.3.3") {
		t.Fatal(r, err)
	}
	if InstalledVersion(game) != "2.3.4" || JarVersion(r.Backup) != "2.3.3" {
		t.Fatal("replace/backup")
	}
	if r, err := ReplaceJar(env, filepath.Join(src, "new.jar")); err != nil || !r.Unchanged {
		t.Fatal(r, err)
	}
	os.WriteFile(r.Backup, []byte("different"), 0o644)
	makeJar(t, filepath.Join(game, JarName), "2.3.3", PremainClass)
	r, err = ReplaceJar(env, filepath.Join(src, "new.jar"))
	if err != nil || r.Backup != filepath.Join(game, JarName+".launcher-bak-2.3.3-2") {
		t.Fatal(r, err)
	}
}

func TestStageNewWindows(t *testing.T) {
	game := t.TempDir()
	src := filepath.Join(t.TempDir(), "z.jar")
	makeJar(t, filepath.Join(game, JarName), "2.3.3", PremainClass)
	makeJar(t, src, "2.3.4", PremainClass)
	env := JarEnv{GameDir: game, GOOS: "windows", Running: func() bool { return true }}
	r, err := StageNew(env, src)
	if err != nil || !r.Staged || r.Jar != filepath.Join(game, NewJarName) {
		t.Fatal(r, err)
	}
	if StagedVersion(game) != "2.3.4" || InstalledVersion(game) != "2.3.3" {
		t.Fatal("staging")
	}
	if _, err := StageNew(JarEnv{GameDir: game, GOOS: "linux"}, src); err == nil {
		t.Fatal("linux staging allowed")
	}
}

func TestPlans(t *testing.T) {
	game := t.TempDir()
	libs := t.TempDir()
	makeJar(t, filepath.Join(libs, JarName), "2.3.4", PremainClass)
	os.WriteFile(filepath.Join(libs, NativeName), []byte("dll"), 0o644)
	cfg := []ConfigFile{{Path: "a.vdf", Text: vdfReal}}
	p := SetupPlan("windows", game, libs, cfg)
	if len(p.Copies) != 2 || len(p.Configs) != 1 || p.Configs[0].Edits[0].After != "gamemoderun %command% -agentlib:zbNative -- -" {
		t.Fatalf("%+v", p)
	}
	p = SetupPlan("linux", game, libs, cfg)
	if len(p.Copies) != 1 || len(p.Configs) != 0 {
		t.Fatalf("%+v", p)
	}
	makeJar(t, filepath.Join(game, JarName), "2.3.3", PremainClass)
	p = SetupPlan("linux", game, libs, cfg)
	if len(p.Copies) != 1 || !p.Copies[0].Replace || p.Copies[0].FromVersion != "2.3.3" {
		t.Fatalf("%+v", p)
	}
	p = SetupPlan("linux", game, "", nil)
	if len(p.Copies) != 0 || len(p.Warnings) != 0 {
		t.Fatalf("%+v", p)
	}
	p = RemovePlan(game, cfg, false)
	if len(p.Deletes) != 0 || len(p.Configs) != 1 {
		t.Fatalf("%+v", p)
	}
	p = RemovePlan(game, cfg, true)
	if len(p.Deletes) != 1 {
		t.Fatalf("%+v", p)
	}
}

func TestApplyCustomJar(t *testing.T) {
	game, libs, own := t.TempDir(), t.TempDir(), t.TempDir()
	makeJar(t, filepath.Join(libs, JarName), "2.3.4", PremainClass)
	makeJar(t, filepath.Join(game, JarName), "2.3.0", PremainClass)
	custom := filepath.Join(own, "MyZB.jar")
	makeJar(t, custom, "2.3.1-custom", PremainClass)
	p := SetupPlan("linux", game, libs, nil)
	ApplyCustomJar(&p, game, custom)
	if len(p.Copies) != 1 || p.Copies[0].Src != custom || !p.Copies[0].Replace || p.Copies[0].ToVersion != "2.3.1-custom" {
		t.Fatalf("custom jar not used: %+v", p.Copies)
	}
	p = SetupPlan("linux", game, libs, nil)
	ApplyCustomJar(&p, game, filepath.Join(own, "missing.jar"))
	if len(p.Copies) != 0 || len(p.Warnings) == 0 {
		t.Fatalf("bad custom jar must block the Workshop copy and warn: %+v", p)
	}
}
