package savebak

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fixture struct {
	m       *Manager
	running bool
	free    uint64
	clock   time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	base := t.TempDir()
	f := &fixture{free: 1 << 40, clock: time.Date(2026, 11, 19, 10, 0, 0, 0, time.Local)}
	f.m = &Manager{
		SavesDir:  filepath.Join(base, "Zomboid", "Saves"),
		Root:      filepath.Join(base, "backups"),
		Running:   func() (bool, error) { return f.running, nil },
		FreeSpace: func(string) (uint64, error) { return f.free, nil },
		Now: func() time.Time {
			f.clock = f.clock.Add(time.Minute)
			return f.clock
		},
	}
	return f
}

func writeFile(t *testing.T, p string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func makeSave(t *testing.T, m *Manager, mode, world string, seed int64) string {
	t.Helper()
	dir := m.SaveDir(mode, world)
	r := rand.New(rand.NewSource(seed))
	rnd := func(n int) []byte { b := make([]byte, n); r.Read(b); return b }
	writeFile(t, filepath.Join(dir, "thumb.png"), append([]byte("\x89PNG\r\n\x1a\n"), rnd(500)...))
	writeFile(t, filepath.Join(dir, "mods.txt"), []byte("mod = A\nmod = B\n"))
	writeFile(t, filepath.Join(dir, "players.db"), rnd(8192))
	writeFile(t, filepath.Join(dir, "players.db-journal"), rnd(512))
	writeFile(t, filepath.Join(dir, "vehicles.db"), rnd(4096))
	writeFile(t, filepath.Join(dir, "map_t.bin"), bytes.Repeat([]byte("abc"), 10000))
	writeFile(t, filepath.Join(dir, "empty.bin"), nil)
	for i := 0; i < 30; i++ {
		writeFile(t, filepath.Join(dir, "map", "10", "map_10_"+string(rune('a'+i%26))+strings.Repeat("x", i/26)+".bin"), rnd(100+i))
	}
	writeFile(t, filepath.Join(dir, "isoregiondata", "deep", "nested", "f.bin"), rnd(77))
	for _, d := range []string{"chunkdata", "metagrid/empty"} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			out[filepath.ToSlash(rel)+"/"] = ""
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameTree(t *testing.T, want, got map[string]string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("entry count %d != %d", len(got), len(want))
	}
	for k, v := range want {
		g, ok := got[k]
		if !ok {
			t.Fatalf("missing %s", k)
		}
		if g != v {
			t.Fatalf("%s differs", k)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	dir := makeSave(t, f.m, "Sandbox", "World-1", 1)
	orig := snapshot(t, dir)

	var last Progress
	b, err := f.m.Backup(ctx, "Sandbox", "World-1", "my label!", func(p Progress) { last = p })
	if err != nil {
		t.Fatal(err)
	}
	if b.Label != "my-label" || !strings.HasSuffix(b.Path, "-my-label.zip") || !strings.HasPrefix(filepath.Base(b.Path), "World-1-20261119-") {
		t.Fatalf("backup %+v", b)
	}
	if last.Files != last.TotalFiles || last.Bytes != last.TotalBytes || last.TotalFiles != 38 {
		t.Fatalf("progress %+v", last)
	}
	zr, err := zip.OpenReader(b.Path)
	if err != nil {
		t.Fatal(err)
	}
	methods := map[string]uint16{}
	for _, zf := range zr.File {
		methods[zf.Name] = zf.Method
	}
	zr.Close()
	if methods["thumb.png"] != zip.Store || methods["map_t.bin"] != zip.Deflate || methods["chunkdata/"] != zip.Store {
		t.Fatalf("methods %v", methods)
	}
	for _, n := range []string{"players.db", "players.db-journal", "vehicles.db", "mods.txt", "metagrid/empty/"} {
		if _, ok := methods[n]; !ok {
			t.Fatalf("missing %s in zip", n)
		}
	}
	th, err := Thumb(b)
	if err != nil || !bytes.HasPrefix(th, []byte("\x89PNG")) {
		t.Fatalf("thumb %v", err)
	}

	writeFile(t, filepath.Join(dir, "players.db"), []byte("changed"))
	writeFile(t, filepath.Join(dir, "new.bin"), []byte("new"))
	os.RemoveAll(filepath.Join(dir, "map"))

	res, err := f.m.Restore(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Safety == nil || res.Safety.Label != SafetyLabel {
		t.Fatalf("safety %+v", res.Safety)
	}
	sameTree(t, orig, snapshot(t, dir))
	ents, _ := os.ReadDir(filepath.Dir(dir))
	if len(ents) != 1 {
		t.Fatalf("leftovers next to save: %v", ents)
	}
	bs, err := f.m.List("Sandbox", "World-1")
	if err != nil || len(bs) != 2 || bs[0].Label != SafetyLabel {
		t.Fatalf("list %v %+v", err, bs)
	}
}

func TestRestoreDeletedSave(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	dir := makeSave(t, f.m, "Survivor", "Gone", 2)
	orig := snapshot(t, dir)
	b, err := f.m.Backup(ctx, "Survivor", "Gone", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(filepath.Dir(dir))
	orph, err := f.m.Orphans()
	if err != nil || len(orph) != 1 || orph[0].World != "Gone" || orph[0].Exists {
		t.Fatalf("orphans %v %+v", err, orph)
	}
	res, err := f.m.Restore(ctx, orph[0].Backups[0], nil)
	if err != nil || res.Safety != nil {
		t.Fatalf("%v %+v", err, res)
	}
	sameTree(t, orig, snapshot(t, dir))
	_ = b
}

func TestCancelRemovesPartial(t *testing.T) {
	f := newFixture(t)
	makeSave(t, f.m, "Sandbox", "W", 3)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := f.m.Backup(ctx, "Sandbox", "W", "", func(p Progress) {
		if p.Phase == "backup" && p.Files >= 0 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	ents, _ := os.ReadDir(f.m.BackupDir("Sandbox", "W"))
	if len(ents) != 0 {
		t.Fatalf("left %v", ents)
	}
}

func TestRollbackOnCorruptZip(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	dir := makeSave(t, f.m, "Sandbox", "W", 4)
	b, err := f.m.Backup(ctx, "Sandbox", "W", "good", nil)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "players.db"), []byte("current state"))
	cur := snapshot(t, dir)

	data, _ := os.ReadFile(b.Path)
	zr, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	var off int64
	for _, zf := range zr.File {
		if zf.Name == "vehicles.db" {
			off, _ = zf.DataOffset()
		}
	}
	if off == 0 {
		t.Fatal("no offset")
	}
	data[off+100] ^= 0xff
	os.WriteFile(b.Path, data, 0o644)

	_, err = f.m.Restore(ctx, b, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	sameTree(t, cur, snapshot(t, dir))
	ents, _ := os.ReadDir(filepath.Dir(dir))
	if len(ents) != 1 {
		t.Fatalf("leftovers %v", ents)
	}
}

func TestZipSlip(t *testing.T) {
	f := newFixture(t)
	dir := makeSave(t, f.m, "Sandbox", "W", 5)
	cur := snapshot(t, dir)
	bdir := f.m.BackupDir("Sandbox", "W")
	os.MkdirAll(bdir, 0o755)
	for i, name := range []string{"../evil.txt", "/abs.txt", "a/../../evil.txt", `..\evil.txt`, "C:/x.txt"} {
		p := filepath.Join(bdir, fileName("W", f.clock.Add(time.Duration(i)*time.Hour), "bad"))
		zf, _ := os.Create(p)
		zw := zip.NewWriter(zf)
		w, _ := zw.Create("ok.txt")
		w.Write([]byte("ok"))
		w, _ = zw.CreateHeader(&zip.FileHeader{Name: name})
		w.Write([]byte("evil"))
		zw.Close()
		zf.Close()
		bs, _ := f.m.List("Sandbox", "W")
		var b Backup
		for _, x := range bs {
			if x.Path == p {
				b = x
			}
		}
		if _, err := f.m.Restore(context.Background(), b, nil); err == nil || !strings.Contains(err.Error(), "unsafe path") {
			t.Fatalf("%q: %v", name, err)
		}
	}
	sameTree(t, cur, snapshot(t, dir))
	if _, err := os.Stat(filepath.Join(f.m.SavesDir, "Sandbox", "evil.txt")); err == nil {
		t.Fatal("evil written")
	}
}

func TestPruneKeepsManual(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	makeSave(t, f.m, "Sandbox", "W", 6)
	if _, err := f.m.Backup(ctx, "Sandbox", "W", "manual", nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if _, err := f.m.AutoBackup(ctx, "Sandbox", "W", 2, nil); err != nil {
			t.Fatal(err)
		}
	}
	bs, _ := f.m.List("Sandbox", "W")
	auto, manual := 0, 0
	for _, b := range bs {
		if b.Auto() {
			auto++
		} else {
			manual++
		}
	}
	if auto != 2 || manual != 1 {
		t.Fatalf("auto %d manual %d", auto, manual)
	}
	n, _, err := f.m.PruneAll(Config{KeepAuto: 1})
	if err != nil || n != 1 {
		t.Fatalf("pruneall %d %v", n, err)
	}
	bs, _ = f.m.List("Sandbox", "W")
	if len(bs) != 2 || bs[0].Auto() != true {
		t.Fatalf("after %+v", bs)
	}
	nb, err := f.m.Rename(bs[0], "keep me")
	if err != nil || nb.Label != "keep-me" || nb.Auto() {
		t.Fatalf("rename %+v %v", nb, err)
	}
	if n, _, _ := f.m.Prune("Sandbox", "W", 0); n != 0 {
		t.Fatal("pruned a renamed backup")
	}
	if err := f.m.Delete(nb); err != nil {
		t.Fatal(err)
	}
	bs, _ = f.m.List("Sandbox", "W")
	if len(bs) != 1 || bs[0].Label != "manual" {
		t.Fatalf("after delete %+v", bs)
	}
}

func TestGameRunningRefused(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	dir := makeSave(t, f.m, "Sandbox", "W", 7)
	b, err := f.m.Backup(ctx, "Sandbox", "W", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	cur := snapshot(t, dir)
	f.running = true
	if _, err := f.m.Backup(ctx, "Sandbox", "W", "", nil); !errors.Is(err, ErrGameRunning) {
		t.Fatalf("backup %v", err)
	}
	if _, err := f.m.Restore(ctx, b, nil); !errors.Is(err, ErrGameRunning) {
		t.Fatalf("restore %v", err)
	}
	bs, _ := f.m.List("Sandbox", "W")
	if len(bs) != 1 {
		t.Fatalf("list %+v", bs)
	}
	sameTree(t, cur, snapshot(t, dir))
}

func TestFreeSpace(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	makeSave(t, f.m, "Sandbox", "W", 8)
	f.free = 1024
	_, err := f.m.Backup(ctx, "Sandbox", "W", "", nil)
	var ns *NoSpaceError
	if !errors.As(err, &ns) {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(f.m.BackupDir("Sandbox", "W")); err == nil {
		ents, _ := os.ReadDir(f.m.BackupDir("Sandbox", "W"))
		if len(ents) != 0 {
			t.Fatalf("left %v", ents)
		}
	}
	f.free = 1 << 40
	b, err := f.m.Backup(ctx, "Sandbox", "W", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	f.free = 1024
	if _, err := f.m.Restore(ctx, b, nil); !errors.As(err, &ns) {
		t.Fatalf("restore err %v", err)
	}
}

func TestRootInsideSavesRejected(t *testing.T) {
	f := newFixture(t)
	makeSave(t, f.m, "Sandbox", "W", 9)
	f.m.Root = filepath.Join(f.m.SavesDir, "Sandbox", "W", "bk")
	if _, err := f.m.Backup(context.Background(), "Sandbox", "W", "", nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseNameAndLabel(t *testing.T) {
	tm := time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local)
	for _, label := range []string{"", "auto", "before-restore", "x-y_z"} {
		n := fileName("my-world", tm, label)
		got, l, ok := parseName("my-world", n)
		if !ok || !got.Equal(tm) || l != label {
			t.Fatalf("%s -> %v %q %v", n, got, l, ok)
		}
	}
	if _, _, ok := parseName("w", "w-2026.zip"); ok {
		t.Fatal("bad parsed")
	}
	if got := CleanLabel(`  a/b\c:"d"  `); got != "a-b-c-d" {
		t.Fatalf("clean %q", got)
	}
}

func TestConfig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x", "backups.json")
	c, err := LoadConfig(p)
	if err != nil || !c.Auto || c.KeepAuto != 5 {
		t.Fatalf("%+v %v", c, err)
	}
	c.SetSave("Sandbox", "W", false, 3)
	c.Root = "/somewhere"
	if err := SaveConfig(p, c); err != nil {
		t.Fatal(err)
	}
	c2, err := LoadConfig(p)
	if err != nil || c2.AutoFor("Sandbox", "W") || c2.KeepFor("Sandbox", "W") != 3 || !c2.AutoFor("Sandbox", "Other") || c2.KeepFor("A", "B") != 5 || c2.Root != "/somewhere" {
		t.Fatalf("%+v %v", c2, err)
	}
	c2.SetSave("Sandbox", "W", true, 5)
	if len(c2.Saves) != 0 {
		t.Fatalf("%+v", c2.Saves)
	}
}
