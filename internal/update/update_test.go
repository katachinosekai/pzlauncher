package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type fakeGH struct {
	srv   *httptest.Server
	files map[string][]byte
	man   Manifest
}

func newFakeGH(t *testing.T, files map[string][]byte, man Manifest) *fakeGH {
	f := &fakeGH{files: files, man: man}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/me/pzlauncher/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		var assets []map[string]any
		for name, b := range f.files {
			assets = append(assets, map[string]any{"name": name, "size": len(b), "browser_download_url": f.srv.URL + "/dl/" + name})
		}
		mb, _ := json.Marshal(f.man)
		assets = append(assets, map[string]any{"name": ManifestName, "size": len(mb), "browser_download_url": f.srv.URL + "/dl/" + ManifestName})
		json.NewEncoder(w).Encode(map[string]any{"tag_name": "v1.2.0", "name": "Pack 1.2.0", "assets": assets})
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/dl/")
		if name == ManifestName {
			json.NewEncoder(w).Encode(f.man)
			return
		}
		b, ok := f.files[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGH) client(t *testing.T) *Client {
	return &Client{Repo: "me/pzlauncher", APIBase: f.srv.URL, HTTP: f.srv.Client(), CacheDir: t.TempDir()}
}

func TestLatestAndFetchPack(t *testing.T) {
	z := makeZip(t, map[string]string{"PZ-Fixes/mods/a.txt": "hello", "PZ-Fixes/README.txt": "x"})
	bin := []byte("binary")
	f := newFakeGH(t, map[string][]byte{"PZ-Fixes-1.2.0.zip": z, WindowsBinary: bin, "other.txt": []byte("o")},
		Manifest{PackVersion: "1.2.0", LauncherVersion: "0.3.0", SHA256: map[string]string{"PZ-Fixes-1.2.0.zip": sum(z), WindowsBinary: sum(bin)}})
	c := f.client(t)
	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "v1.2.0" || rel.Pack == nil || rel.Pack.Name != "PZ-Fixes-1.2.0.zip" {
		t.Fatalf("release %+v", rel)
	}
	if rel.Manifest.PackVersion != "1.2.0" || rel.Manifest.LauncherVersion != "0.3.0" || rel.Asset(WindowsBinary) == nil {
		t.Fatalf("manifest %+v", rel.Manifest)
	}
	var calls int
	dir, err := c.FetchPack(context.Background(), rel, func(done, total int64) { calls++ })
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dir) != "PZ-Fixes" || calls == 0 {
		t.Fatalf("dir %s calls %d", dir, calls)
	}
	b, err := os.ReadFile(filepath.Join(dir, "mods", "a.txt"))
	if err != nil || string(b) != "hello" {
		t.Fatalf("extracted %q %v", b, err)
	}
}

func TestChecksumMismatch(t *testing.T) {
	z := makeZip(t, map[string]string{"a.txt": "a"})
	f := newFakeGH(t, map[string][]byte{"PZ-Fixes.zip": z},
		Manifest{PackVersion: "1", SHA256: map[string]string{"PZ-Fixes.zip": strings.Repeat("0", 64)}})
	c := f.client(t)
	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.FetchPack(context.Background(), rel, nil)
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("got %v", err)
	}
	left, _ := filepath.Glob(filepath.Join(c.CacheDir, "downloads", "*"))
	if len(left) != 0 {
		t.Fatalf("left files %v", left)
	}
}

func TestMissingHash(t *testing.T) {
	z := makeZip(t, map[string]string{"a.txt": "a"})
	f := newFakeGH(t, map[string][]byte{"PZ-Fixes.zip": z}, Manifest{PackVersion: "1"})
	c := f.client(t)
	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.FetchPack(context.Background(), rel, nil); err == nil {
		t.Fatal("download without a manifest hash succeeded")
	}
}

func TestUnconfiguredRepo(t *testing.T) {
	c := &Client{Repo: "OWNER/pzlauncher"}
	if _, err := c.Latest(context.Background()); err == nil {
		t.Fatal("placeholder repo accepted")
	}
}

func TestZipSlip(t *testing.T) {
	for _, bad := range []string{"../evil.txt", "ok/../../evil.txt", "/abs.txt", `..\evil.txt`, "C:/evil.txt"} {
		t.Run(bad, func(t *testing.T) {
			dir := t.TempDir()
			zp := filepath.Join(dir, "p.zip")
			os.WriteFile(zp, makeZip(t, map[string]string{"fine.txt": "f", bad: "x"}), 0o644)
			dest := filepath.Join(dir, "out")
			if err := Extract(zp, dest); err == nil {
				t.Fatalf("accepted %q", bad)
			}
			if _, err := os.Stat(filepath.Join(dir, "evil.txt")); err == nil {
				t.Fatal("file written outside dest")
			}
			if _, err := os.Stat(filepath.Join(dest, "fine.txt")); err == nil {
				t.Fatal("extraction started before validation")
			}
		})
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.0", "1.2", 0}, {"v1.10.0", "1.9.9", 1}, {"0.3.0", "0.3.1", -1}, {"2026.11.19", "2026.11.18", 1}, {"1.0.0-rc1", "1.0.0", 0},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
	if !Newer("1.0", "") || Newer("", "1.0") || LauncherOutdated("9.9", "dev") || !LauncherOutdated("0.2", "0.1") {
		t.Error("Newer/LauncherOutdated")
	}
}

func TestState(t *testing.T) {
	p := filepath.Join(t.TempDir(), "PZLauncher", "state.json")
	s, err := LoadStateFrom(p)
	if err != nil || s.PackVersion != "" {
		t.Fatalf("%+v %v", s, err)
	}
	if err := SaveStateTo(p, State{PackVersion: "1.2.0"}); err != nil {
		t.Fatal(err)
	}
	s, err = LoadStateFrom(p)
	if err != nil || s.PackVersion != "1.2.0" {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestReplace(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "PZLauncher")
	os.WriteFile(target, []byte("old"), 0o755)
	os.WriteFile(target+".new", []byte("new"), 0o755)
	if err := Replace(target, target+".new"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(target)
	if string(b) != "new" {
		t.Fatalf("got %q", b)
	}
}
