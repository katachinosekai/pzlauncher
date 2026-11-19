package gamecfg

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kata/pzlauncher/internal/install"
)

const linuxJSON = `{
    "mainClass": "zombie/gameStates/MainScreenState",
    "classpath": [
        ".",
        "projectzomboid.jar"
    ],
    "vmArgs": [
        "-Djava.awt.headless=true",
        "-Xmx3072m",
        "-Dzomboid.steam=1",
        "-XX:-OmitStackTraceInFastThrow"
    ]
}`

const harnessJSON = "{\r\n    \"mainClass\": \"zombie/gameStates/MainScreenState\",\r\n    \"vmArgs\": [\r\n        \"-Djava.awt.headless=true\",\r\n        \"-Xmx3072m\",\r\n        \"-XX:+UseZGC\",\r\n        \"-javaagent:ZBBatch.jar\"\r\n    ]\r\n}\r\n"

const oneLineJSON = `{"mainClass" : "zombie/gameStates/MainScreenState", "classpath" : [ ".", "projectzomboid.jar" ], "vmArgs" : [ "-Djava.awt.headless=true", "-Xmx3072m", "-Dzomboid.steam=1", "-XX:-OmitStackTraceInFastThrow" ]}`

const noXmxJSON = `{"mainClass" : "zombie/gameStates/MainScreenState", "vmArgs" : [ "-Djava.awt.headless=true", "-Dzomboid.steam=1" ]}`

const realLinuxJSON = `{
    "mainClass": "zombie/gameStates/MainScreenState",
    "classpath": [
        ".",
        "projectzomboid.jar"
    ],
    "vmArgs": [
        "-Djava.awt.headless=true",
        "--enable-native-access=ALL-UNNAMED",
        "-Xmx8192m",
        "-XX:SoftMaxHeapSize=6g",
        "-XX:+UnlockExperimentalVMOptions",
        "-XX:ShenandoahUncommitDelay=30000",
        "-Dzomboid.steam=1",
        "-XX:+UseShenandoahGC",
        "-XX:-OmitStackTraceInFastThrow",
        "-XX:+UseCompactObjectHeaders",
        "-XX:+UseStringDeduplication",
        "-javaagent:ZBBatch.jar"
    ]
}`

func args(t *testing.T, text string) []string {
	t.Helper()
	var d struct {
		VMArgs []string `json:"vmArgs"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(text, "\ufeff")), &d); err != nil {
		t.Fatalf("invalid JSON %v:\n%s", err, text)
	}
	return d.VMArgs
}

func without(list []string, drop func(string) bool) []string {
	var out []string
	for _, v := range list {
		if !drop(v) {
			out = append(out, v)
		}
	}
	return out
}

func isPackOrXmx(v string) bool {
	if _, ok := xmxMB(v); ok {
		return true
	}
	for _, f := range install.JVMFlags {
		if matchFlag(f, v) {
			return true
		}
	}
	return false
}

func allOn(xmx int) JVMWant {
	w := JVMWant{XmxMB: xmx, Flags: map[string]bool{}}
	for _, f := range install.JVMFlags {
		w.Flags[f] = true
	}
	return w
}

func TestReadJVM(t *testing.T) {
	info, err := ReadJVM(realLinuxJSON)
	if err != nil {
		t.Fatal(err)
	}
	if info.XmxMB != 8192 || info.Xmx != "-Xmx8192m" || info.SoftMaxMB != 6144 || info.GC != "Shenandoah" {
		t.Fatalf("%+v", info)
	}
	if info.Flags["-XX:ZUncommitDelay=30"] || !info.Flags["-XX:+UseCompactObjectHeaders"] {
		t.Fatalf("flags %+v", info.Flags)
	}
	if info.Args[len(info.Args)-1] != "-javaagent:ZBBatch.jar" {
		t.Fatal("last arg lost")
	}
	if _, err := ReadJVM(`{"mainClass":"x"}`); !errors.Is(err, ErrNoVMArgs) {
		t.Fatalf("want ErrNoVMArgs, got %v", err)
	}
	g, _ := ReadJVM(harnessJSON)
	if g.GC != "ZGC" || g.XmxMB != 3072 {
		t.Fatalf("%+v", g)
	}
}

func TestApplyJVMKeepsOthers(t *testing.T) {
	fixtures := map[string]string{
		"linux": linuxJSON, "harness": harnessJSON, "oneline": oneLineJSON,
		"noxmx": noXmxJSON, "real": realLinuxJSON, "bom": "\ufeff" + linuxJSON,
		"empty": `{"vmArgs" : [ ]}`,
	}
	for name, in := range fixtures {
		t.Run(name, func(t *testing.T) {
			before := without(args(t, in), isPackOrXmx)
			out, _, err := ApplyJVM(in, allOn(8192))
			if err != nil {
				t.Fatal(err)
			}
			got := args(t, out)
			if !reflect.DeepEqual(without(got, isPackOrXmx), before) {
				t.Fatalf("other entries changed:\n%v\n%v", before, got)
			}
			info, _ := ReadJVM(out)
			if info.XmxMB != 8192 {
				t.Fatalf("xmx %d", info.XmxMB)
			}
			for _, f := range install.JVMFlags {
				if !info.Flags[f] {
					t.Fatalf("%s missing: %s", f, out)
				}
			}
			again, _, _ := ApplyJVM(out, allOn(8192))
			if again != out {
				t.Fatal("second apply changed the file")
			}
			off := allOn(8192)
			for f := range off.Flags {
				off.Flags[f] = false
			}
			o2, _, err := ApplyJVM(out, off)
			if err != nil {
				t.Fatal(err)
			}
			i2, _ := ReadJVM(o2)
			for _, f := range install.JVMFlags {
				if f == FlagUnlock {
					continue
				}
				if i2.Flags[f] {
					t.Fatalf("%s still on", f)
				}
			}
			if !reflect.DeepEqual(without(args(t, o2), isPackOrXmx), before) {
				t.Fatal("other entries changed after turning flags off")
			}
			if strings.HasPrefix(in, "\ufeff") && !strings.HasPrefix(o2, "\ufeff") {
				t.Fatal("BOM lost")
			}
		})
	}
}

func TestApplyJVMOnlyXmx(t *testing.T) {
	info, _ := ReadJVM(realLinuxJSON)
	w := info.Want()
	w.XmxMB = 12288
	out, notes, err := ApplyJVM(realLinuxJSON, w)
	if err != nil || len(notes) != 0 {
		t.Fatal(err, notes)
	}
	if want := strings.Replace(realLinuxJSON, `"-Xmx8192m"`, `"-Xmx12288m"`, 1); out != want {
		t.Fatalf("more than -Xmx changed:\n%s", out)
	}
	unchanged, _, _ := ApplyJVM(realLinuxJSON, info.Want())
	if unchanged != realLinuxJSON {
		t.Fatal("no-op apply changed the file")
	}
	g, _, _ := ApplyJVM(`{"vmArgs":["-Xmx8g","-javaagent:ZBBatch.jar"]}`, JVMWant{XmxMB: 8192})
	if g != `{"vmArgs":["-Xmx8g","-javaagent:ZBBatch.jar"]}` {
		t.Fatalf("8g == 8192m should not change: %s", g)
	}
}

func TestApplyJVMLinuxTrailingAgent(t *testing.T) {
	out, _, err := ApplyJVM(linuxJSON, allOn(8192))
	if err != nil {
		t.Fatal(err)
	}
	withAgent, _, err := install.AddVMArg(out, "-javaagent:ZBBatch.jar")
	if err != nil {
		t.Fatal(err)
	}
	w := allOn(6144)
	w.Flags["-XX:+UseStringDeduplication"] = false
	o2, _, err := ApplyJVM(withAgent, w)
	if err != nil {
		t.Fatal(err)
	}
	a := args(t, o2)
	if a[len(a)-1] != "-javaagent:ZBBatch.jar" || a[0] != "-Djava.awt.headless=true" {
		t.Fatalf("order broken: %v", a)
	}
	if strings.Contains(o2, "UseStringDeduplication") {
		t.Fatal("flag not removed")
	}
}

func TestSoftMaxBelowXmx(t *testing.T) {
	for _, xmx := range []int{2048, 3072, 4096, 6144, 7168, 8192, 16384} {
		info, _ := ReadJVM(realLinuxJSON)
		w := info.Want()
		w.XmxMB = xmx
		out, _, err := ApplyJVM(realLinuxJSON, w)
		if err != nil {
			t.Fatal(err)
		}
		i2, _ := ReadJVM(out)
		if i2.SoftMaxMB <= 0 || i2.SoftMaxMB >= i2.XmxMB {
			t.Fatalf("xmx %d soft %d", i2.XmxMB, i2.SoftMaxMB)
		}
	}
	w := allOn(8192)
	w.SoftMaxMB = 9000
	_, notes, _ := ApplyJVM(linuxJSON, w)
	if len(notes) == 0 {
		t.Fatal("expected a note about lowering SoftMaxHeapSize")
	}
}

func TestUnlockKeptForShenandoah(t *testing.T) {
	info, _ := ReadJVM(realLinuxJSON)
	w := info.Want()
	w.Flags[FlagUnlock] = false
	out, notes, _ := ApplyJVM(realLinuxJSON, w)
	if out != realLinuxJSON || len(notes) == 0 {
		t.Fatal("Unlock was removed while ShenandoahUncommitDelay is on")
	}
	w.Flags[FlagShenUncommit] = false
	out, _, _ = ApplyJVM(realLinuxJSON, w)
	if strings.Contains(out, "Unlock") || strings.Contains(out, "ShenandoahUncommit") {
		t.Fatal("not removed")
	}
	if !strings.Contains(out, "-XX:+UseShenandoahGC") {
		t.Fatal("GC flag touched")
	}
}

func TestXmxBounds(t *testing.T) {
	cases := []struct{ ram, lo, hi int }{
		{32 * 1024, 2, 30}, {31 * 1024, 2, 29}, {16000, 2, 14}, {3 * 1024, 2, 2}, {0, 2, 16},
	}
	for _, c := range cases {
		if lo, hi := XmxRangeGB(c.ram); lo != c.lo || hi != c.hi {
			t.Fatalf("ram %d: %d-%d", c.ram, lo, hi)
		}
	}
	if _, _, err := ApplyJVM(linuxJSON, JVMWant{XmxMB: 1024}); err == nil {
		t.Fatal("expected error below 2 GB")
	}
}

const iniLF = "version=8\n3DGroundItem=true\nambientVolume=5\nborderless=true\nfontSize=6\nframeRate=60\nfullScreen=false\nheight=1440\nmusicVolume=2\nseenNews=\nsoundVolume=10\nuncappedFPS=false\nvoiceRecordDeviceName=fifine Chat Analog Stereo\nvsync=false\nwidth=3440\nzoomLevels2x=250;225;200;175;150;125;75;50"

func TestINIRoundTrip(t *testing.T) {
	inputs := []string{
		iniLF,
		strings.ReplaceAll(iniLF, "\n", "\r\n") + "\r\n",
		"\ufeff" + iniLF + "\n",
		"key=caf\xe9\nweird line\n\n=x\nwidth=1\n",
	}
	for _, in := range inputs {
		ini := ParseINI([]byte(in))
		if string(ini.Bytes()) != in {
			t.Fatalf("round trip changed %q", in)
		}
		o := ReadOptions(ini)
		n, err := ApplyOptions(ini, o)
		if err != nil && !strings.Contains(in, "width=1\n") {
			t.Fatal(err)
		}
		if n != 0 || string(ini.Bytes()) != in {
			t.Fatalf("unchanged apply modified %q", in)
		}
	}
}

func TestINIEdit(t *testing.T) {
	in := strings.ReplaceAll(iniLF, "\n", "\r\n")
	ini := ParseINI([]byte(in))
	o := ReadOptions(ini)
	if o.Mode != ModeBorderless || o.Width != 3440 || o.Height != 1440 || o.FrameRate != 60 || o.FontSize != 6 || o.Volumes["soundVolume"] != 10 {
		t.Fatalf("%+v", o)
	}
	if o.Present["jumpScareVolume"] {
		t.Fatal("absent key reported present")
	}
	o.Mode = ModeFullscreen
	o.Width, o.Height = 2560, 1080
	o.VSync = true
	o.Volumes["musicVolume"] = 7
	o.Volumes["jumpScareVolume"] = 3
	n, err := ApplyOptions(ini, o)
	if err != nil {
		t.Fatal(err)
	}
	if n != 6 {
		t.Fatalf("changed %d", n)
	}
	want := in
	for _, r := range [][2]string{{"borderless=true", "borderless=false"}, {"fullScreen=false", "fullScreen=true"}, {"width=3440", "width=2560"}, {"height=1440", "height=1080"}, {"vsync=false", "vsync=true"}, {"musicVolume=2", "musicVolume=7"}} {
		want = strings.Replace(want, r[0], r[1], 1)
	}
	if got := string(ini.Bytes()); got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
	if strings.Contains(string(ini.Bytes()), "jumpScare") {
		t.Fatal("absent key was added")
	}
	o.Width = 100
	if _, err := ApplyOptions(ini, o); err == nil {
		t.Fatal("expected width range error")
	}
}

func testEnv(t *testing.T) (Env, *bool) {
	t.Helper()
	d := t.TempDir()
	running := false
	e := Env{
		GameDir:    filepath.Join(d, "game"),
		ZomboidDir: filepath.Join(d, "Zomboid"),
		ConfigDir:  filepath.Join(d, "cfg", "PZLauncher"),
		Running:    func() bool { return running },
		Now:        func() time.Time { return time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC) },
	}
	os.MkdirAll(e.GameDir, 0o755)
	os.MkdirAll(e.ZomboidDir, 0o755)
	if err := os.WriteFile(e.JSONPath(), []byte(linuxJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.OptionsPath(), []byte(iniLF), 0o644); err != nil {
		t.Fatal(err)
	}
	return e, &running
}

func TestRefuseWhileRunning(t *testing.T) {
	e, running := testEnv(t)
	*running = true
	if _, _, err := e.SaveJVM(allOn(8192)); !errors.Is(err, ErrGameRunning) {
		t.Fatalf("got %v", err)
	}
	_, o, _ := e.LoadOptions()
	o.VSync = true
	if _, err := e.SaveOptions(o); !errors.Is(err, ErrGameRunning) {
		t.Fatalf("got %v", err)
	}
	b, _ := os.ReadFile(e.JSONPath())
	c, _ := os.ReadFile(e.OptionsPath())
	if string(b) != linuxJSON || string(c) != iniLF {
		t.Fatal("file changed while game running")
	}
	ents, _ := os.ReadDir(e.GameDir)
	if len(ents) != 1 {
		t.Fatal("backup written while game running")
	}
}

func TestBackupAndRemember(t *testing.T) {
	e, _ := testEnv(t)
	changed, _, err := e.SaveJVM(allOn(8192))
	if err != nil || !changed {
		t.Fatal(err, changed)
	}
	bp := BackupPath(e.JSONPath(), e.Now())
	if !strings.HasSuffix(bp, ".launcher-bak-20261119") {
		t.Fatal(bp)
	}
	if b, _ := os.ReadFile(bp); string(b) != linuxJSON {
		t.Fatal("backup is not the original")
	}
	if _, _, err := e.SaveJVM(JVMWant{XmxMB: 4096, Flags: map[string]bool{}}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(bp); string(b) != linuxJSON {
		t.Fatal("backup overwritten on second change of the day")
	}
	ents, _ := os.ReadDir(e.GameDir)
	for _, en := range ents {
		if strings.Contains(en.Name(), ".tmp-") {
			t.Fatal("temp file left behind")
		}
	}
	if err := e.SetDebug(true); err != nil {
		t.Fatal(err)
	}
	rem, err := e.Remembered()
	if err != nil || rem.XmxMB != 4096 {
		t.Fatal(rem, err)
	}
	if on, _ := e.Debug(); !on {
		t.Fatal("debug lost")
	}
	os.WriteFile(e.JSONPath(), []byte(linuxJSON), 0o644)
	if _, _, err := e.Reapply(); err != nil {
		t.Fatal(err)
	}
	info, _ := e.LoadJVM()
	if info.XmxMB != 4096 {
		t.Fatal(info.XmxMB)
	}
	if on, _ := e.Debug(); !on {
		t.Fatal("debug lost after reapply")
	}

	_, o, _ := e.LoadOptions()
	o.Volumes["soundVolume"] = 4
	if n, err := e.SaveOptions(o); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if b, _ := os.ReadFile(BackupPath(e.OptionsPath(), e.Now())); string(b) != iniLF {
		t.Fatal("options backup wrong")
	}
	c, _ := os.ReadFile(e.OptionsPath())
	if string(c) != strings.Replace(iniLF, "soundVolume=10", "soundVolume=4", 1) {
		t.Fatalf("%q", c)
	}
}

func TestNothingRemembered(t *testing.T) {
	e, _ := testEnv(t)
	if _, _, err := e.Reapply(); !errors.Is(err, ErrNothingRemembered) {
		t.Fatal(err)
	}
}
