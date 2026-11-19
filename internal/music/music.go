package music

import (
	"encoding/json"
	"errors"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/effects"
	"github.com/gopxl/beep/v2/flac"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/speaker"
	"github.com/gopxl/beep/v2/vorbis"
	"github.com/gopxl/beep/v2/wav"
)

var ErrNoMusic = errors.New("no music files (mp3, ogg, wav, flac) in that folder")

type Config struct {
	Asked   bool   `json:"asked"`
	Enabled bool   `json:"enabled"`
	Folder  string `json:"folder"`
	Volume  int    `json:"volume"`
	Shuffle bool   `json:"shuffle"`
}

func ConfigPath() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "PZLauncher", "music.json"), nil
}

func LoadConfig(path string) (Config, error) {
	c := Config{Volume: 60, Shuffle: true}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(b, &c)
	c.Volume = min(max(c.Volume, 0), 100)
	return c, err
}

func SaveConfig(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

var exts = map[string]bool{".mp3": true, ".ogg": true, ".oga": true, ".wav": true, ".flac": true}

func ScanFolder(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir {
				return err
			}
			return nil
		}
		if !d.IsDir() && exts[strings.ToLower(filepath.Ext(p))] {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

func decode(path string) (beep.StreamSeekCloser, beep.Format, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, beep.Format{}, err
	}
	var s beep.StreamSeekCloser
	var format beep.Format
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		s, format, err = mp3.Decode(f)
	case ".ogg", ".oga":
		s, format, err = vorbis.Decode(f)
	case ".wav":
		s, format, err = wav.Decode(f)
	case ".flac":
		s, format, err = flac.Decode(f)
	default:
		err = errors.New("unsupported file type")
	}
	if err != nil {
		f.Close()
		return nil, beep.Format{}, err
	}
	return s, format, nil
}

const sampleRate = beep.SampleRate(44100)

var initOnce sync.Once
var initErr error

func initSpeaker() error {
	initOnce.Do(func() { initErr = speaker.Init(sampleRate, sampleRate.N(time.Second/10)) })
	return initErr
}

type playlist struct {
	p       *Player
	cur     beep.Streamer
	closer  beep.StreamSeekCloser
	skip    bool
	tracks  []string
	order   []int
	pos     int
	shuffle bool
}

func (pl *playlist) next() bool {
	if pl.closer != nil {
		pl.closer.Close()
		pl.closer, pl.cur = nil, nil
	}
	for tries := 0; tries < len(pl.tracks); tries++ {
		if pl.pos >= len(pl.order) {
			pl.pos = 0
			if pl.shuffle {
				rand.Shuffle(len(pl.order), func(i, j int) { pl.order[i], pl.order[j] = pl.order[j], pl.order[i] })
			}
		}
		path := pl.tracks[pl.order[pl.pos]]
		pl.pos++
		s, format, err := decode(path)
		if err != nil {
			continue
		}
		pl.closer = s
		pl.cur = s
		if format.SampleRate != sampleRate {
			pl.cur = beep.Resample(3, format.SampleRate, sampleRate, s)
		}
		go pl.p.announce(path)
		return true
	}
	return false
}

func (pl *playlist) Stream(samples [][2]float64) (int, bool) {
	filled := 0
	for filled < len(samples) {
		if pl.skip || pl.cur == nil {
			pl.skip = false
			if !pl.next() {
				for i := filled; i < len(samples); i++ {
					samples[i] = [2]float64{}
				}
				return len(samples), true
			}
		}
		n, ok := pl.cur.Stream(samples[filled:])
		filled += n
		if !ok || n == 0 {
			pl.cur = nil
		}
	}
	return filled, true
}

func (pl *playlist) Err() error { return nil }

type Player struct {
	mu         sync.Mutex
	pl         *playlist
	ctrl       *beep.Ctrl
	vol        *effects.Volume
	userPaused bool
	gamePaused bool
	OnTrack    func(path string)
}

func NewPlayer() *Player { return &Player{} }

func (p *Player) announce(path string) {
	p.mu.Lock()
	cb := p.OnTrack
	p.mu.Unlock()
	if cb != nil {
		cb(path)
	}
}

func volumeLevel(v int) (float64, bool) {
	if v <= 0 {
		return 0, true
	}
	return float64(v-100) / 100 * 6, false
}

func (p *Player) Start(folder string, shuffle bool, volume int) error {
	tracks, err := ScanFolder(folder)
	if err != nil {
		return err
	}
	if len(tracks) == 0 {
		return ErrNoMusic
	}
	if err := initSpeaker(); err != nil {
		return err
	}
	p.Stop()
	order := make([]int, len(tracks))
	for i := range order {
		order[i] = i
	}
	if shuffle {
		rand.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	}
	pl := &playlist{p: p, tracks: tracks, order: order, shuffle: shuffle}
	level, silent := volumeLevel(volume)
	ctrl := &beep.Ctrl{Streamer: pl}
	vol := &effects.Volume{Streamer: ctrl, Base: 2, Volume: level, Silent: silent}
	p.mu.Lock()
	p.pl, p.ctrl, p.vol = pl, ctrl, vol
	p.userPaused = false
	ctrl.Paused = p.gamePaused
	p.mu.Unlock()
	speaker.Play(vol)
	return nil
}

func (p *Player) Stop() {
	p.mu.Lock()
	running := p.pl != nil
	p.pl, p.ctrl, p.vol = nil, nil, nil
	p.mu.Unlock()
	if running {
		speaker.Clear()
	}
}

func (p *Player) Playing() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pl != nil
}

func (p *Player) apply() {
	if p.ctrl == nil {
		return
	}
	speaker.Lock()
	p.ctrl.Paused = p.userPaused || p.gamePaused
	speaker.Unlock()
}

func (p *Player) SetPaused(paused bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.userPaused = paused
	p.apply()
}

func (p *Player) SetGamePaused(paused bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gamePaused = paused
	p.apply()
}

func (p *Player) Paused() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.userPaused
}

func (p *Player) SetVolume(v int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.vol == nil {
		return
	}
	level, silent := volumeLevel(min(max(v, 0), 100))
	speaker.Lock()
	p.vol.Volume, p.vol.Silent = level, silent
	speaker.Unlock()
}

func (p *Player) Next() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pl == nil {
		return
	}
	speaker.Lock()
	p.pl.skip = true
	speaker.Unlock()
}
