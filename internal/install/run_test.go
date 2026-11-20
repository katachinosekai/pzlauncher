package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSystem struct {
	mu      sync.Mutex
	running map[string]bool
	urls    []string
	closes  int
	starts  int
	onURL   map[string]func()
}

func (f *fakeSystem) Running(names ...string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range names {
		if f.running[n] {
			return true
		}
	}
	return false
}

func (f *fakeSystem) OpenURL(u string) error {
	f.mu.Lock()
	f.urls = append(f.urls, u)
	if strings.HasPrefix(u, "steam://") {
		f.running[steamProcess] = true
	}
	fn := f.onURL[u]
	f.mu.Unlock()
	if fn != nil {
		fn()
	}
	return nil
}

func (f *fakeSystem) CloseSteam(ctx context.Context, root string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closes++
	f.running[steamProcess] = false
	return nil
}

func (f *fakeSystem) StartSteam(root string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	f.running[steamProcess] = true
	return nil
}

func put(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type tree struct {
	root, steam, lib, game, workshop, zomboid, zbhome, pack, logs string
	sys                                                           *fakeSystem
	api                                                           *httptest.Server
	apiFail                                                       bool
}

const packDefault = "VERSION = 1,\r\n\r\nmods\r\n{\r\n    mod = ZombieBuddy,\r\n    mod = PZArrayListCompat,\r\n    mod = SomeOther,\r\n}\r\n\r\nmaps\r\n{\r\n    map = Muldraugh, KY,\r\n}\r\n"

func newTree(t *testing.T, goos string) *tree {
	t.Helper()
	r := t.TempDir()
	tr := &tree{
		root:    r,
		steam:   filepath.Join(r, "steam"),
		lib:     filepath.Join(r, "lib"),
		zomboid: filepath.Join(r, "zomboid"),
		zbhome:  filepath.Join(r, "zbhome"),
		pack:    filepath.Join(r, "pack"),
	}
	tr.logs = t.TempDir()
	tr.workshop = filepath.Join(tr.lib, "steamapps", "workshop", "content", AppID)
	tr.game = filepath.Join(tr.lib, "steamapps", "common", "ProjectZomboid")
	if goos != "windows" {
		tr.game = filepath.Join(tr.game, "projectzomboid")
	}
	put(t, filepath.Join(tr.steam, "steamapps", "libraryfolders.vdf"), fmt.Sprintf("\"libraryfolders\"\n{\n\t\"0\"\n\t{\n\t\t\"path\"\t\t\"%s\"\n\t}\n\t\"1\"\n\t{\n\t\t\"path\"\t\t\"%s\"\n\t}\n}\n", tr.steam, tr.lib))
	put(t, filepath.Join(tr.lib, "steamapps", "appmanifest_"+AppID+".acf"), "\"AppState\"\n{\n}\n")
	if goos == "windows" {
		put(t, filepath.Join(tr.game, "ProjectZomboid64.json"), harnessJSON)
		put(t, filepath.Join(tr.steam, "userdata", "111", "config", "localconfig.vdf"), vdfHarness1)
		put(t, filepath.Join(tr.steam, "userdata", "222", "config", "localconfig.vdf"), vdfHarness2)
	} else {
		put(t, filepath.Join(tr.game, "ProjectZomboid64.json"), linuxJSON)
		put(t, filepath.Join(tr.steam, "userdata", "111", "config", "localconfig.vdf"), vdfLinux)
	}
	for _, id := range []string{"1001", "1002", "3820190473"} {
		put(t, filepath.Join(tr.workshop, id, "mods", "x", "mod.info"), "id=x\n")
	}
	agent := filepath.Join(tr.workshop, ZBBatchItem, "mods", "ZBBatch", "agent")
	put(t, filepath.Join(agent, "ZBBatch.jar"), "agent")
	put(t, filepath.Join(agent, "zbbatch.properties"), "calibrated=true\n")
	for _, rel := range VersionMaxFiles {
		put(t, filepath.Join(tr.workshop, filepath.FromSlash(rel)), "name=Test\r\nid=Test\r\nversionMin=42.0\r\nversionMax=42.21\r\n")
	}
	vp := filepath.Join(tr.workshop, ViewpointID, "mods", "Viewpoint")
	put(t, filepath.Join(vp, "common", "mod.info"), "name=Viewpoint\nmodversion=0.1.7-rc4\n")
	put(t, filepath.Join(vp, "42", "media", "java", "client", "Viewpoint.jar"), "nightly")
	put(t, filepath.Join(tr.lib, "steamapps", "workshop", "appworkshop_"+AppID+".acf"), "\"AppWorkshop\"\n{\n\t\"WorkshopItemsInstalled\"\n\t{\n\t\t\""+ViewpointID+"\"\n\t\t{\n\t\t\t\"size\"\t\t\"1\"\n\t\t}\n\t}\n}\n")

	put(t, filepath.Join(tr.zomboid, "mods", "default.txt"), "VERSION = 1,\r\n\r\nmods\r\n{\r\n    mod = ZombieBuddy,\r\n    mod = SomeOther,\r\n}\r\n")
	for _, m := range []string{"PZArrayListCompat", "ALifeCorpseSync", "ZBBatch", "SomeOther"} {
		put(t, filepath.Join(tr.zomboid, "mods", m, "42", "mod.info"), "id="+m)
		put(t, filepath.Join(tr.zomboid, "mods", m, "42", "media", "x.lua"), m)
	}
	put(t, filepath.Join(tr.zomboid, "Lua", "keysB42.ini"), "old keys")
	put(t, filepath.Join(tr.zomboid, "Sandbox Presets", "Mine.cfg"), "user preset")
	put(t, filepath.Join(tr.zbhome, "mod_approvals.json"), "{\r\n  \"formatVersion\": 2,\r\n  \"mods\": [\r\n    { \"id\": \"Other\", \"workshop_id\": 3809991837, \"jar_hash\": \"aa\", \"decision\": true, \"author_id\": 76561199113018152 }\r\n  ]\r\n}")

	put(t, filepath.Join(tr.pack, "settings", "Lua", "keysB42.ini"), "pack keys")
	put(t, filepath.Join(tr.pack, "settings", "Lua", "ModOptions.ini"), "opts")
	put(t, filepath.Join(tr.pack, "settings", "mods", "default.txt"), packDefault)
	put(t, filepath.Join(tr.pack, "presets", "Kata Solo.cfg"), "pack preset")
	put(t, filepath.Join(tr.pack, "presets", "Mine.cfg"), "pack copy of mine")
	put(t, filepath.Join(tr.pack, "payload", "approvals.txt"), approvalsTxt)
	put(t, filepath.Join(tr.pack, "localmods", "AutoLoadSave", "42", "mod.info"), "id=AutoLoadSave\n")
	put(t, filepath.Join(tr.pack, "localmods", "AutoLoadSave", "42", "media", "lua", "client", "AutoLoadSave.lua"), "-- lua\n")
	put(t, filepath.Join(tr.pack, "localmods", "enable.txt"), "# local mods to enable\nAutoLoadSave\n")

	tr.sys = &fakeSystem{running: map[string]bool{steamProcess: true}, onURL: map[string]func(){}}
	tr.sys.onURL["steam://url/CommunityFilePage/"+ViewpointID] = func() {
		os.MkdirAll(filepath.Join(vp, "common"), 0o755)
		os.WriteFile(filepath.Join(vp, "common", "mod.info"), []byte("name=Viewpoint\nmodversion=0.1.7\n"), 0o644)
		os.MkdirAll(filepath.Join(vp, "42", "media", "java", "client"), 0o755)
		os.WriteFile(filepath.Join(vp, "42", "media", "java", "client", "Viewpoint.jar"), []byte("release"), 0o644)
	}
	tr.sys.onURL["steam://url/CommunityFilePage/"+CollectionID] = func() {
		os.MkdirAll(filepath.Join(tr.workshop, "1004", "mods"), 0o755)
	}
	tr.api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tr.apiFail {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		r.ParseForm()
		id := r.Form.Get("publishedfileids[0]")
		var children string
		switch id {
		case CollectionID:
			children = `{"publishedfileid":"1001","sortorder":1,"filetype":0},{"publishedfileid":"1002","sortorder":2,"filetype":0},{"publishedfileid":"3820190473","sortorder":3,"filetype":0},{"publishedfileid":"` + ZBBatchItem + `","sortorder":4,"filetype":0},{"publishedfileid":"2000","sortorder":5,"filetype":2}`
		case "2000":
			children = `{"publishedfileid":"1004","sortorder":1,"filetype":0},{"publishedfileid":"1001","sortorder":2,"filetype":0}`
		}
		fmt.Fprintf(w, `{"response":{"result":1,"resultcount":1,"collectiondetails":[{"publishedfileid":"%s","result":1,"children":[%s]}]}}`, id, children)
	}))
	t.Cleanup(tr.api.Close)
	return tr
}

func (tr *tree) opts(goos string, log *[]string) Options {
	return Options{
		PackDir:           tr.pack,
		PackMemory:        true,
		PackZBBatch:       true,
		SteamRoot:         tr.steam,
		ZomboidDir:        tr.zomboid,
		ZombieBuddyHome:   tr.zbhome,
		BackupRoot:        tr.root,
		LogDir:            tr.logs,
		GOOS:              goos,
		System:            tr.sys,
		CollectionAPI:     tr.api.URL,
		PollInterval:      time.Millisecond,
		SteamPollInterval: time.Millisecond,
		Log:               func(s string) { *log = append(*log, s) },
	}
}

func backupDirs(t *testing.T, root string) []string {
	m, _ := filepath.Glob(filepath.Join(root, "bk-*"))
	return m
}

func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			out[rel+"/"] = ""
			return nil
		}
		b, _ := os.ReadFile(p)
		h := sha256.Sum256(b)
		out[rel] = hex.EncodeToString(h[:])
		return nil
	})
	return out
}

func checkCommon(t *testing.T, tr *tree, log []string) string {
	t.Helper()
	bks := backupDirs(t, tr.root)
	if len(bks) != 1 {
		t.Fatalf("backup dirs: %v", bks)
	}
	bk := bks[0]
	j := get(t, filepath.Join(tr.game, "ProjectZomboid64.json"))
	args := vmArgs(t, j)
	if count(args, XmxTarget) != 1 || count(args, "-Xmx3072m") != 0 {
		t.Fatalf("xmx: %v", args)
	}
	for _, f := range JVMFlags {
		if count(args, f) != 1 {
			t.Fatalf("flag %s: %v", f, args)
		}
	}
	if got := get(t, filepath.Join(bk, "ProjectZomboid", "ProjectZomboid64.json")); !strings.Contains(got, "-Xmx3072m") {
		t.Fatal("json backup is not the first version")
	}
	if get(t, filepath.Join(tr.game, "ZBBatch.jar")) != "agent" || get(t, filepath.Join(tr.zbhome, "zbbatch.properties")) != "calibrated=true\n" {
		t.Fatal("agent files not installed")
	}
	for _, m := range []string{"PZArrayListCompat", "ALifeCorpseSync", "ZBBatch"} {
		if exists(filepath.Join(tr.zomboid, "mods", m)) {
			t.Fatalf("%s not removed", m)
		}
		if get(t, filepath.Join(bk, "Zomboid-mods", m, "42", "media", "x.lua")) != m {
			t.Fatalf("%s not backed up", m)
		}
	}
	if !exists(filepath.Join(tr.zomboid, "mods", "SomeOther", "42", "mod.info")) {
		t.Fatal("unrelated mod removed")
	}
	if get(t, filepath.Join(tr.zomboid, "Lua", "keysB42.ini")) != "pack keys" || get(t, filepath.Join(bk, "Zomboid", "Lua", "keysB42.ini")) != "old keys" {
		t.Fatal("settings copy/backup")
	}
	if get(t, filepath.Join(tr.zomboid, "Sandbox Presets", "Kata Solo.cfg")) != "pack preset" || get(t, filepath.Join(tr.zomboid, "Sandbox Presets", "Mine.cfg")) != "user preset" {
		t.Fatal("presets")
	}
	if get(t, filepath.Join(tr.zomboid, "mods", "AutoLoadSave", "42", "media", "lua", "client", "AutoLoadSave.lua")) != "-- lua\n" {
		t.Fatal("local mod not installed")
	}
	if exists(filepath.Join(tr.zomboid, "mods", "enable.txt")) {
		t.Fatal("enable.txt copied as a mod")
	}
	wantList := "VERSION = 1,\r\n\r\nmods\r\n{\r\n    mod = ZombieBuddy,\r\n    mod = PZArrayListCompat,\r\n    mod = SomeOther,\r\n    mod = AutoLoadSave,\r\n}\r\n\r\nmaps\r\n{\r\n    map = Muldraugh, KY,\r\n}\r\n"
	if got := get(t, filepath.Join(tr.zomboid, "mods", "default.txt")); got != wantList {
		t.Fatalf("default.txt:\n%q", got)
	}
	if !strings.Contains(get(t, filepath.Join(bk, "Zomboid", "mods", "default.txt")), "SomeOther") {
		t.Fatal("default.txt backup")
	}
	for _, rel := range VersionMaxFiles {
		if !strings.Contains(get(t, filepath.Join(tr.workshop, filepath.FromSlash(rel))), "versionMax=42.22\r\n") {
			t.Fatalf("versionMax %s", rel)
		}
		if !strings.Contains(get(t, filepath.Join(bk, filepath.FromSlash(rel))), "versionMax=42.21") {
			t.Fatalf("versionMax backup %s", rel)
		}
	}
	var appr struct {
		Mods []map[string]any `json:"mods"`
	}
	ab := get(t, filepath.Join(tr.zbhome, "mod_approvals.json"))
	if err := json.Unmarshal([]byte(ab), &appr); err != nil || len(appr.Mods) != 3 || !strings.Contains(ab, "76561199113018152") {
		t.Fatalf("approvals: %v %s", err, ab)
	}
	if !exists(filepath.Join(bk, ".zombie_buddy", "mod_approvals.json")) {
		t.Fatal("approvals backup")
	}
	if get(t, filepath.Join(tr.workshop, ViewpointID, "mods", "Viewpoint", "42", "media", "java", "client", "Viewpoint.jar")) != "release" {
		t.Fatal("viewpoint not replaced")
	}
	acf := filepath.Join(tr.lib, "steamapps", "workshop", "appworkshop_"+AppID+".acf")
	if strings.Contains(get(t, acf), ViewpointID) {
		t.Fatal("acf still lists viewpoint")
	}
	if m, _ := filepath.Glob(acf + ".pzfix-*"); len(m) != 1 {
		t.Fatal("acf backup")
	}
	if !exists(filepath.Join(tr.workshop, "1004")) {
		t.Fatal("collection wait did not run")
	}
	if !strings.HasPrefix(log[len(log)-1], "Log saved to: ") {
		t.Fatalf("last line: %q", log[len(log)-1])
	}
	return bk
}

func rerun(t *testing.T, tr *tree, goos string) {
	t.Helper()
	before := snapshot(t, tr.root)
	var log []string
	rep, err := Run(context.Background(), tr.opts(goos, &log))
	if err != nil {
		t.Fatalf("rerun: %v\n%s", err, strings.Join(log, "\n"))
	}
	if len(rep.Changes) != 0 || rep.BackupDir != "" {
		t.Fatalf("rerun changed things: %v %s\n%s", rep.Changes, rep.BackupDir, strings.Join(log, "\n"))
	}
	after := snapshot(t, tr.root)
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatal("rerun changed files")
	}
	if !strings.Contains(strings.Join(log, "\n"), "Done. Nothing needed changing.") {
		t.Fatal("rerun done line")
	}
}

func TestRunWindows(t *testing.T) {
	tr := newTree(t, "windows")
	var log []string
	rep, err := Run(context.Background(), tr.opts("windows", &log))
	if err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(log, "\n"))
	}
	bk := checkCommon(t, tr, log)
	if rep.BackupDir != bk || rep.Failed {
		t.Fatalf("report %+v", rep)
	}
	j := get(t, filepath.Join(tr.game, "ProjectZomboid64.json"))
	if HasZBBatchAgent(j) {
		t.Fatal("windows json still has -javaagent:ZBBatch.jar")
	}
	v1 := get(t, filepath.Join(tr.steam, "userdata", "111", "config", "localconfig.vdf"))
	if v1 != LaunchEditWindows(vdfHarness1).Text || !strings.Contains(v1, "\"999\"\r\n\t\t\t{\r\n\t\t\t\t\"LaunchOptions\"\t\t\"-agentlib:zbNative --\"") {
		t.Fatalf("vdf1: %q", v1)
	}
	if !strings.Contains(get(t, filepath.Join(tr.steam, "userdata", "222", "config", "localconfig.vdf")), "-agentlib:zbNative=policy=deny-new -javaagent:ZBBatch.jar --") {
		t.Fatal("vdf2")
	}
	if get(t, filepath.Join(bk, "Steam", "userdata", "111", "config", "localconfig.vdf")) != vdfHarness1 {
		t.Fatal("vdf backup")
	}
	if tr.sys.closes != 2 || tr.sys.starts != 1 || !tr.sys.running[steamProcess] {
		t.Fatalf("steam closes=%d starts=%d", tr.sys.closes, tr.sys.starts)
	}
	tr.sys.closes, tr.sys.starts = 0, 0
	rerun(t, tr, "windows")
	if tr.sys.closes != 0 || tr.sys.starts != 0 {
		t.Fatal("rerun touched Steam")
	}
}

func TestRunLinux(t *testing.T) {
	tr := newTree(t, "linux")
	var log []string
	_, err := Run(context.Background(), tr.opts("linux", &log))
	if err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(log, "\n"))
	}
	checkCommon(t, tr, log)
	args := vmArgs(t, get(t, filepath.Join(tr.game, "ProjectZomboid64.json")))
	if args[len(args)-1] != "-javaagent:ZBBatch.jar" || count(args, "-javaagent:ZBBatch.jar") != 1 {
		t.Fatalf("linux vmArgs: %v", args)
	}
	if get(t, filepath.Join(tr.steam, "userdata", "111", "config", "localconfig.vdf")) != vdfLinux {
		t.Fatal("linux launch options changed")
	}
	if tr.sys.closes != 1 || tr.sys.starts != 0 {
		t.Fatalf("steam closes=%d starts=%d", tr.sys.closes, tr.sys.starts)
	}
	rerun(t, tr, "linux")
}

func TestRunLinuxNoLoader(t *testing.T) {
	tr := newTree(t, "linux")
	put(t, filepath.Join(tr.steam, "userdata", "111", "config", "localconfig.vdf"), strings.Replace(vdfLinux, " -javaagent:ZombieBuddy.jar -- -", "", 1))
	var log []string
	rep, err := Run(context.Background(), tr.opts("linux", &log))
	if err != nil {
		t.Fatal(err)
	}
	if HasZBBatchAgent(get(t, filepath.Join(tr.game, "ProjectZomboid64.json"))) {
		t.Fatal("ZBBatch switched on without a loader")
	}
	if !strings.Contains(strings.Join(rep.Warnings, "\n"), "loader") {
		t.Fatalf("warnings: %v", rep.Warnings)
	}
}

func TestDryRun(t *testing.T) {
	for _, goos := range []string{"windows", "linux"} {
		t.Run(goos, func(t *testing.T) {
			tr := newTree(t, goos)
			before := snapshot(t, tr.root)
			var log []string
			o := tr.opts(goos, &log)
			o.DryRun = true
			rep, err := Run(context.Background(), o)
			if err != nil {
				t.Fatalf("%v\n%s", err, strings.Join(log, "\n"))
			}
			if fmt.Sprint(before) != fmt.Sprint(snapshot(t, tr.root)) {
				t.Fatal("dry run changed files")
			}
			if len(rep.Changes) == 0 || rep.BackupDir != "" {
				t.Fatalf("report %+v", rep)
			}
			if tr.sys.closes != 0 || tr.sys.starts != 0 || len(tr.sys.urls) != 0 {
				t.Fatalf("dry run touched Steam: %+v", tr.sys)
			}
		})
	}
}

func TestGameRunning(t *testing.T) {
	tr := newTree(t, "linux")
	tr.sys.running["ProjectZomboid64"] = true
	before := snapshot(t, tr.root)
	var log []string
	rep, err := Run(context.Background(), tr.opts("linux", &log))
	var fe *FatalError
	if !errors.As(err, &fe) || !rep.Failed {
		t.Fatalf("err=%v", err)
	}
	if fmt.Sprint(before) != fmt.Sprint(snapshot(t, tr.root)) {
		t.Fatal("files changed")
	}
}

func TestCollectionAPIDown(t *testing.T) {
	tr := newTree(t, "linux")
	tr.apiFail = true
	var log []string
	rep, err := Run(context.Background(), tr.opts("linux", &log))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(rep.Warnings, "\n"), "could not read the collection") {
		t.Fatalf("warnings: %v", rep.Warnings)
	}
	if exists(filepath.Join(tr.workshop, "1004")) {
		t.Fatal("waited although the API was down")
	}
}

func TestWaitCancel(t *testing.T) {
	tr := newTree(t, "linux")
	delete(tr.sys.onURL, "steam://url/CommunityFilePage/"+CollectionID)
	ctx, cancel := context.WithCancel(context.Background())
	var seen []WaitStatus
	var log []string
	o := tr.opts("linux", &log)
	o.OnWait = func(s WaitStatus) {
		seen = append(seen, s)
		if s.What == "collection" && len(seen) > 3 {
			cancel()
		}
	}
	_, err := Run(ctx, o)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	last := seen[len(seen)-1]
	if last.Total != 5 || last.Installed != 4 || len(last.Missing) != 1 || last.Missing[0] != "1004" {
		t.Fatalf("status %+v", last)
	}
}

func TestNoWorkshop(t *testing.T) {
	tr := newTree(t, "linux")
	os.RemoveAll(filepath.Join(tr.lib, "steamapps", "workshop"))
	tr.apiFail = true
	var log []string
	_, err := Run(context.Background(), tr.opts("linux", &log))
	var fe *FatalError
	if !errors.As(err, &fe) || !strings.Contains(fe.Msg, "no Workshop folder") {
		t.Fatalf("err=%v", err)
	}
}

func TestPackLeavesMemoryAndZBBatchAlone(t *testing.T) {
	tr := newTree(t, "linux")
	before := get(t, filepath.Join(tr.game, "ProjectZomboid64.json"))
	var log []string
	o := tr.opts("linux", &log)
	o.PackMemory, o.PackZBBatch = false, false
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(log, "\n"))
	}
	if get(t, filepath.Join(tr.game, "ProjectZomboid64.json")) != before {
		t.Fatal("pack install changed ProjectZomboid64.json")
	}
	if _, err := os.Stat(filepath.Join(tr.game, "ZBBatch.jar")); err == nil {
		t.Fatal("pack install copied ZBBatch.jar")
	}
}
