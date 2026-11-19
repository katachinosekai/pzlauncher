package install

import (
	"encoding/json"
	"strings"
	"testing"
)

func vmArgs(t *testing.T, text string) []string {
	t.Helper()
	var d struct {
		VMArgs []string `json:"vmArgs"`
	}
	if err := json.Unmarshal([]byte(text), &d); err != nil {
		t.Fatalf("invalid JSON %v:\n%s", err, text)
	}
	return d.VMArgs
}

func count(list []string, s string) int {
	n := 0
	for _, v := range list {
		if v == s {
			n++
		}
	}
	return n
}

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

func TestSetXmx(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		old     string
		changed bool
		noArray bool
	}{
		{"a: real file WITH -Xmx3072m", `{"mainClass" : "zombie/gameStates/MainScreenState", "classpath" : [ ".", "projectzomboid.jar" ], "vmArgs" : [ "-Djava.awt.headless=true", "-Xmx3072m", "-Dzomboid.steam=1", "-XX:-OmitStackTraceInFastThrow" ]}`, "-Xmx3072m", true, false},
		{"b: real file WITHOUT the -Xmx element", `{"mainClass" : "zombie/gameStates/MainScreenState", "classpath" : [ ".", "projectzomboid.jar" ], "vmArgs" : [ "-Djava.awt.headless=true", "-Dzomboid.steam=1", "-XX:-OmitStackTraceInFastThrow" ]}`, "", true, false},
		{"c: empty vmArgs array", `{"vmArgs" : [ ]}`, "", true, false},
		{"d: vmArgs, no spaces", `{"vmArgs":["-Dx=1"]}`, "", true, false},
		{"e: already 8192", `{"vmArgs":["-Xmx8192m"]}`, "-Xmx8192m", false, false},
		{"f: gigabytes", `{"vmArgs":["-Xmx4g"]}`, "-Xmx4g", true, false},
		{"g: multi-line linux file", linuxJSON, "-Xmx3072m", true, false},
		{"h: no vmArgs", `{"mainClass":"x"}`, "", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := SetXmx(c.in)
			if r.Old != c.old || r.Changed != c.changed || r.NoArray != c.noArray {
				t.Fatalf("got old=%q changed=%v noArray=%v", r.Old, r.Changed, r.NoArray)
			}
			if c.noArray {
				return
			}
			if n := count(vmArgs(t, r.Text), XmxTarget); n != 1 {
				t.Fatalf("expected one %s, got %d: %s", XmxTarget, n, r.Text)
			}
			if again := SetXmx(r.Text); again.Changed {
				t.Fatal("second run changed the text")
			}
		})
	}
}

func TestAddJVMFlags(t *testing.T) {
	r := SetXmx(linuxJSON)
	out, added := AddJVMFlags(r.Text)
	if len(added) != len(JVMFlags) {
		t.Fatalf("added %v", added)
	}
	args := vmArgs(t, out)
	i := 0
	for ; i < len(args) && args[i] != XmxTarget; i++ {
	}
	for k, f := range JVMFlags {
		if args[i+1+k] != f {
			t.Fatalf("flag order wrong: %v", args)
		}
	}
	if out2, again := AddJVMFlags(out); len(again) != 0 || out2 != out {
		t.Fatalf("not idempotent: %v", again)
	}
	partial := `{"vmArgs":["-Xmx8192m","-XX:+UseStringDeduplication"]}`
	out, added = AddJVMFlags(partial)
	if count(vmArgs(t, out), "-XX:+UseStringDeduplication") != 1 || len(added) != len(JVMFlags)-1 {
		t.Fatalf("partial: %v %s", added, out)
	}
	if _, added = AddJVMFlags(`{"vmArgs":["-Xmx3072m"]}`); len(added) != 0 {
		t.Fatal("flags added without -Xmx8192m")
	}
}

func TestZBBatchAgentJSON(t *testing.T) {
	cases := []struct{ name, in string }{
		{"harness last element CRLF", harnessJSON},
		{"middle", `{"vmArgs":["-Da=1", "-javaagent:ZBBatch.jar", "-Db=2"]}`},
		{"first", `{"vmArgs":["-javaagent:ZBBatch.jar","-Db=2"]}`},
		{"only", `{"vmArgs":[ "-javaagent:ZBBatch.jar" ]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !HasZBBatchAgent(c.in) {
				t.Fatal("not detected")
			}
			out := RemoveZBBatchAgent(c.in)
			if HasZBBatchAgent(out) || !jsonValid(out) {
				t.Fatalf("bad removal: %s", out)
			}
		})
	}
}

func TestAddVMArg(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"compact", `{"vmArgs":["-Da=1","-Db=2"]}`, `{"vmArgs":["-Da=1","-Db=2","-javaagent:ZBBatch.jar"]}`},
		{"spaced", `{"vmArgs" : [ "-Da=1", "-Db=2" ]}`, `{"vmArgs" : [ "-Da=1", "-Db=2", "-javaagent:ZBBatch.jar" ]}`},
		{"one element multi-line", "{\n  \"vmArgs\": [\n    \"-Da=1\"\n  ]\n}", "{\n  \"vmArgs\": [\n    \"-Da=1\",\n    \"-javaagent:ZBBatch.jar\"\n  ]\n}"},
		{"empty", `{"vmArgs":[]}`, `{"vmArgs":[ "-javaagent:ZBBatch.jar" ]}`},
		{"comma inside string", `{"vmArgs":["-Dx=a,b","-Dy=]"]}`, `{"vmArgs":["-Dx=a,b","-Dy=]","-javaagent:ZBBatch.jar"]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, changed, err := AddVMArg(c.in, "-javaagent:ZBBatch.jar")
			if err != nil || !changed || out != c.want {
				t.Fatalf("got %q changed=%v err=%v", out, changed, err)
			}
			if _, changed, _ := AddVMArg(out, "-javaagent:ZBBatch.jar"); changed {
				t.Fatal("second run changed")
			}
		})
	}
	ml, _, err := AddVMArg(linuxJSON, "-javaagent:ZBBatch.jar")
	if err != nil || !strings.Contains(ml, "\"-XX:-OmitStackTraceInFastThrow\",\n        \"-javaagent:ZBBatch.jar\"\n    ]") {
		t.Fatalf("linux layout: %v\n%s", err, ml)
	}
	if _, _, err := AddVMArg(`{"x":1}`, "-javaagent:ZBBatch.jar"); err == nil {
		t.Fatal("expected error without vmArgs")
	}
}

func TestPlaceShim(t *testing.T) {
	cases := []struct {
		name, in, want string
		noZB, same     bool
	}{
		{"add below ZB CRLF", "VERSION = 1,\r\n\r\nmods\r\n{\r\n    mod = ZombieBuddy,\r\n    mod = SomeOther,\r\n}\r\n",
			"VERSION = 1,\r\n\r\nmods\r\n{\r\n    mod = ZombieBuddy,\r\n    mod = PZArrayListCompat,\r\n    mod = SomeOther,\r\n}\r\n", false, false},
		{"move from below", "mods\n{\n    mod = ZombieBuddy,\n    mod = A,\n    mod = PZArrayListCompat,\n}\n",
			"mods\n{\n    mod = ZombieBuddy,\n    mod = PZArrayListCompat,\n    mod = A,\n}\n", false, false},
		{"move from above", "mods\n{\n    mod = PZArrayListCompat,\n    mod = ZombieBuddy,\n}\n",
			"mods\n{\n    mod = ZombieBuddy,\n    mod = PZArrayListCompat,\n}\n", false, false},
		{"already", "mods\n{\n    mod = ZombieBuddy,\n    mod = PZArrayListCompat,\n    mod = A,\n}\n",
			"mods\n{\n    mod = ZombieBuddy,\n    mod = PZArrayListCompat,\n    mod = A,\n}\n", false, true},
		{"backslash ids", "mods\n{\n    mod = \\ZombieBuddy,\n    mod = \\A,\n}\n",
			"mods\n{\n    mod = \\ZombieBuddy,\n    mod = \\PZArrayListCompat,\n    mod = \\A,\n}\n", false, false},
		{"no ZB", "mods\n{\n    mod = A,\n    mod = PZArrayListCompat,\n}\n", "mods\n{\n    mod = A,\n    mod = PZArrayListCompat,\n}\n", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := PlaceShim(c.in)
			if r.NoZB != c.noZB || r.Unchanged != c.same || r.Text != c.want {
				t.Fatalf("got noZB=%v same=%v\n%q", r.NoZB, r.Unchanged, r.Text)
			}
			if !c.noZB && !PlaceShim(r.Text).Unchanged {
				t.Fatal("second run changed")
			}
		})
	}
}

func TestEnableMods(t *testing.T) {
	in := "VERSION = 1,\r\n\r\nmods\r\n{\r\n    mod = ZombieBuddy,\r\n    mod = AutoLoadSave,\r\n}\r\n\r\nmaps\r\n{\r\n    map = Muldraugh, KY,\r\n}\r\n"
	out, added, err := EnableMods(in, []string{"AutoLoadSave", "NewMod"})
	want := "VERSION = 1,\r\n\r\nmods\r\n{\r\n    mod = ZombieBuddy,\r\n    mod = AutoLoadSave,\r\n    mod = NewMod,\r\n}\r\n\r\nmaps\r\n{\r\n    map = Muldraugh, KY,\r\n}\r\n"
	if err != nil || out != want || len(added) != 1 {
		t.Fatalf("got %q %v %v", out, added, err)
	}
	if _, added, _ := EnableMods(out, []string{"NewMod"}); len(added) != 0 {
		t.Fatal("not idempotent")
	}
	bs, _, _ := EnableMods("mods\n{\n    mod = \\A,\n}\n", []string{"B"})
	if bs != "mods\n{\n    mod = \\A,\n    mod = \\B,\n}\n" {
		t.Fatalf("backslash style: %q", bs)
	}
	empty, _, err := EnableMods("mods\n{\n}\n", []string{"B"})
	if err != nil || empty != "mods\n{\n    mod = B,\n}\n" {
		t.Fatalf("empty block: %q %v", empty, err)
	}
	if _, _, err := EnableMods("VERSION = 1,\n", []string{"B"}); err == nil {
		t.Fatal("expected error without mods block")
	}
}

const vdfHarness1 = "\"UserLocalConfigStore\"\r\n{\r\n\t\"Software\"\r\n\t{\r\n\t\t\"apps\"\r\n\t\t{\r\n\t\t\t\"999\"\r\n\t\t\t{\r\n\t\t\t\t\"LaunchOptions\"\t\t\"-agentlib:zbNative --\"\r\n\t\t\t}\r\n\t\t\t\"108600\"\r\n\t\t\t{\r\n\t\t\t\t\"cloud\"\r\n\t\t\t\t{\r\n\t\t\t\t\t\"last_sync_state\"\t\t\"synchronized\"\r\n\t\t\t\t}\r\n\t\t\t\t\"LaunchOptions\"\t\t\"-agentlib:zbNative --\"\r\n\t\t\t\t\"note\"\t\t\"a \\\"quoted\\\" {brace}\"\r\n\t\t\t}\r\n\t\t}\r\n\t\t\"108600\"\r\n\t\t{\r\n\t\t\t\"Playtime\"\t\t\"5\"\r\n\t\t}\r\n\t}\r\n}\r\n"

const vdfHarness2 = "\"UserLocalConfigStore\"\r\n{\r\n\t\"apps\"\r\n\t{\r\n\t\t\"108600\"\r\n\t\t{\r\n\t\t\t\"LaunchOptions\"\t\t\"-agentlib:zbNative=policy=deny-new --\"\r\n\t\t}\r\n\t}\r\n}\r\n"

const vdfLinux = "\"UserLocalConfigStore\"\n{\n\t\"apps\"\n\t{\n\t\t\"108600\"\n\t\t{\n\t\t\t\"LaunchOptions\"\t\t\"gamemoderun %command% -javaagent:ZombieBuddy.jar -- -\"\n\t\t}\n\t}\n}\n"

func TestLaunchEditWindows(t *testing.T) {
	cases := []struct {
		name                       string
		in, want                   string
		changed, already, noLoader int
	}{
		{"harness 1: other app untouched, quoted braces", vdfHarness1,
			strings.Replace(vdfHarness1, "\"LaunchOptions\"\t\t\"-agentlib:zbNative --\"\r\n\t\t\t\t\"note\"", "\"LaunchOptions\"\t\t\"-agentlib:zbNative -javaagent:ZBBatch.jar --\"\r\n\t\t\t\t\"note\"", 1), 1, 0, 1},
		{"harness 2: loader with options", vdfHarness2,
			strings.Replace(vdfHarness2, "deny-new --", "deny-new -javaagent:ZBBatch.jar --", 1), 1, 0, 0},
		{"already on", strings.Replace(vdfHarness2, "deny-new --", "deny-new -javaagent:ZBBatch.jar --", 1),
			strings.Replace(vdfHarness2, "deny-new --", "deny-new -javaagent:ZBBatch.jar --", 1), 0, 1, 0},
		{"no loader (linux style)", vdfLinux, vdfLinux, 0, 0, 1},
		{"no 108600", "\"x\"\n{\n}\n", "\"x\"\n{\n}\n", 0, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := LaunchEditWindows(c.in)
			if r.Text != c.want || r.Changed != c.changed || r.Already != c.already || r.NoLoader != c.noLoader {
				t.Fatalf("got changed=%d already=%d noLoader=%d\n%q", r.Changed, r.Already, r.NoLoader, r.Text)
			}
			if again := LaunchEditWindows(r.Text); again.Changed != 0 {
				t.Fatal("second run changed")
			}
		})
	}
}

func TestLaunchScanLinux(t *testing.T) {
	r := LaunchScanLinux(vdfLinux)
	if r.Changed != 1 || r.Already != 0 || r.Text != vdfLinux || len(r.Values) != 1 || r.Values[0] != "gamemoderun %command% -javaagent:ZombieBuddy.jar -- -" {
		t.Fatalf("%+v", r)
	}
	r = LaunchScanLinux(strings.Replace(vdfLinux, "ZombieBuddy.jar", "ZombieBuddy.jar -javaagent:ZBBatch.jar", 1))
	if r.Already != 1 {
		t.Fatalf("%+v", r)
	}
	if r = LaunchScanLinux(vdfHarness2); r.Changed != 1 {
		t.Fatalf("zbNative on linux: %+v", r)
	}
}

const approvalsTxt = "EngineLoadTimeFixes 077945af35b5fbef08b9d05b066740595f7d1570dbdcf0c7ba7568d72013d07a\r\nPZArrayListCompat 82e82b300a38c09df5f3fb09d81b8fcad1789c458922aeacc41a1644e6c2b93c\r\n# comment\r\nbad line\r\n"

func TestMergeApprovals(t *testing.T) {
	want := ParseApprovals(approvalsTxt)
	if len(want) != 2 {
		t.Fatalf("parsed %v", want)
	}
	harness := "{\r\n  \"formatVersion\": 2,\r\n  \"mods\": [\r\n    { \"id\": \"Other\", \"workshop_id\": 3809991837, \"jar_hash\": \"aa\", \"decision\": true, \"author_id\": 76561199113018152 }\r\n  ]\r\n}"
	cases := []struct {
		name     string
		existing []byte
		added    int
		total    int
	}{
		{"no file", nil, 2, 2},
		{"harness file keeps big ids", []byte(harness), 2, 3},
		{"one already, upper-case hash", []byte(`{"formatVersion":2,"mods":[{"id":"EngineLoadTimeFixes","jar_hash":"077945AF35B5FBEF08B9D05B066740595F7D1570DBDCF0C7BA7568D72013D07A","decision":true}],"extra":{"k":1}}`), 1, 2},
		{"old hash stays, new one added", []byte(`{"formatVersion":2,"mods":[{"id":"PZArrayListCompat","jar_hash":"00","decision":true}]}`), 2, 3},
		{"bom", append([]byte("\xef\xbb\xbf"), []byte(`{"formatVersion":2,"mods":[]}`)...), 2, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, added, err := MergeApprovals(c.existing, want)
			if err != nil || added != c.added {
				t.Fatalf("added=%d err=%v", added, err)
			}
			var d struct {
				FormatVersion int                          `json:"formatVersion"`
				Mods          []map[string]json.RawMessage `json:"mods"`
			}
			if err := json.Unmarshal(out, &d); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			if d.FormatVersion != 2 || len(d.Mods) != c.total {
				t.Fatalf("got %s", out)
			}
			if strings.Contains(string(c.existing), "76561199113018152") && !strings.Contains(string(out), "76561199113018152") {
				t.Fatalf("big number lost: %s", out)
			}
			if strings.Contains(string(c.existing), "extra") && !strings.Contains(string(out), `"extra"`) {
				t.Fatalf("unknown key lost: %s", out)
			}
			if _, again, _ := MergeApprovals(out, want); again != 0 {
				t.Fatal("second run added")
			}
		})
	}
	if _, _, err := MergeApprovals([]byte("not json"), want); err == nil {
		t.Fatal("expected error on bad JSON")
	}
}

func TestVersionMaxAndAcf(t *testing.T) {
	mi := "name=Test\r\nid=Test\r\nversionMin=42.0\r\nversionMax=42.21\r\n"
	out, ok := BumpVersionMax(mi)
	if !ok || out != "name=Test\r\nid=Test\r\nversionMin=42.0\r\nversionMax=42.22\r\n" {
		t.Fatalf("%q", out)
	}
	if _, ok := BumpVersionMax(out); ok {
		t.Fatal("second run changed")
	}
	if _, ok := BumpVersionMax("versionMax=42.215\n"); ok {
		t.Fatal("matched 42.215")
	}
	acf := "\"AppWorkshop\"\n{\n\t\"WorkshopItemsInstalled\"\n\t{\n\t\t\"3809306528\"\n\t\t{\n\t\t\t\"size\"\t\t\"1\"\n\t\t}\n\t\t\"111\"\n\t\t{\n\t\t\t\"size\"\t\t\"2\"\n\t\t}\n\t}\n\t\"WorkshopItemDetails\"\n\t{\n\t\t\"3809306528\"\n\t\t{\n\t\t\t\"manifest\"\t\t\"3\"\n\t\t}\n\t}\n}\n"
	got, n := RemoveAcfItem(acf, "3809306528")
	if n != 2 || strings.Contains(got, "3809306528") || !strings.Contains(got, "\"111\"") {
		t.Fatalf("n=%d %q", n, got)
	}
}

func TestNightlyFromModInfo(t *testing.T) {
	cases := []struct {
		in    string
		v     string
		found bool
	}{
		{"name=Viewpoint\nmodversion=0.1.7-rc4\n", "0.1.7-rc4", true},
		{"modversion = 0.2.0-nightly\r\n", "0.2.0-nightly", true},
		{"modversion=0.1.7\n", "", true},
		{"name=Viewpoint\n", "", false},
	}
	for _, c := range cases {
		v, found := NightlyFromModInfo(c.in)
		if v != c.v || found != c.found {
			t.Fatalf("%q: got %q %v", c.in, v, found)
		}
	}
}
