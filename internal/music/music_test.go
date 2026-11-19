package music

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gopxl/beep/v2"
)

func TestScanFolder(t *testing.T) {
	d := t.TempDir()
	for _, n := range []string{"b.mp3", "a.OGG", "sub/c.flac", "sub/d.wav", "notes.txt", "cover.jpg"} {
		p := filepath.Join(d, n)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}
	got, err := ScanFolder(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("want 4 music files, got %v", got)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "PZLauncher", "music.json")
	c, err := LoadConfig(p)
	if err != nil || c.Asked || c.Volume != 60 || !c.Shuffle {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	c = Config{Asked: true, Enabled: true, Folder: "/music", Volume: 140, Shuffle: false}
	if err := SaveConfig(p, c); err != nil {
		t.Fatal(err)
	}
	back, err := LoadConfig(p)
	if err != nil || !back.Asked || !back.Enabled || back.Folder != "/music" || back.Volume != 100 || back.Shuffle {
		t.Fatalf("round trip: %+v %v", back, err)
	}
}

type tone struct{ left int }

func (t *tone) Stream(s [][2]float64) (int, bool) {
	if t.left == 0 {
		return 0, false
	}
	n := min(len(s), t.left)
	for i := 0; i < n; i++ {
		s[i] = [2]float64{0.5, 0.5}
	}
	t.left -= n
	return n, true
}

func (t *tone) Err() error { return nil }

func TestPlaylistAdvancesAndSkipsBadFiles(t *testing.T) {
	d := t.TempDir()
	bad := filepath.Join(d, "bad.mp3")
	os.WriteFile(bad, []byte("not audio"), 0o644)
	pl := &playlist{p: &Player{}, tracks: []string{bad}, order: []int{0}}
	buf := make([][2]float64, 64)
	n, ok := pl.Stream(buf)
	if n != 64 || !ok {
		t.Fatalf("bad files should give silence, got %d %v", n, ok)
	}
	pl.cur = &tone{left: 10}
	n, ok = pl.Stream(buf)
	if n != 64 || !ok || buf[0][0] != 0.5 || buf[20][0] != 0 {
		t.Fatalf("expected tone then silence, got %d %v %v %v", n, ok, buf[0], buf[20])
	}
	var _ beep.Streamer = pl
}

func TestVolumeLevel(t *testing.T) {
	if _, silent := volumeLevel(0); !silent {
		t.Fatal("0 should be silent")
	}
	if l, silent := volumeLevel(100); silent || l != 0 {
		t.Fatalf("100 should be full volume, got %v %v", l, silent)
	}
}
