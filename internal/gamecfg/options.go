package gamecfg

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

const (
	KeyFullScreen  = "fullScreen"
	KeyBorderless  = "borderless"
	KeyWidth       = "width"
	KeyHeight      = "height"
	KeyVSync       = "vsync"
	KeyFrameRate   = "frameRate"
	KeyUncappedFPS = "uncappedFPS"
	KeyFontSize    = "fontSize"
)

const (
	ModeFullscreen = "Fullscreen"
	ModeBorderless = "Borderless window"
	ModeWindowed   = "Windowed"
)

var FrameRates = []int{244, 240, 165, 144, 120, 95, 90, 75, 60, 55, 45, 30, 24}

var FontSizes = []string{"16px", "19px", "26px", "33px", "38px", "Scale with window height"}

type VolumeKey struct {
	Key   string
	Label string
}

var Volumes = []VolumeKey{
	{"soundVolume", "Sound"},
	{"musicVolume", "Music"},
	{"jumpScareVolume", "Jump scares"},
	{"vehicleEngineVolume", "Vehicle engine"},
}

type INI struct {
	lines [][]byte
}

func ParseINI(data []byte) *INI {
	ini := &INI{}
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			ini.lines = append(ini.lines, data)
			break
		}
		ini.lines = append(ini.lines, data[:i+1])
		data = data[i+1:]
	}
	return ini
}

func (ini *INI) Bytes() []byte {
	var b bytes.Buffer
	for _, l := range ini.lines {
		b.Write(l)
	}
	return b.Bytes()
}

func splitLine(l []byte) (key []byte, valStart, valEnd int, ok bool) {
	eq := bytes.IndexByte(l, '=')
	if eq < 0 {
		return nil, 0, 0, false
	}
	end := len(l)
	if end > 0 && l[end-1] == '\n' {
		end--
	}
	if end > eq && l[end-1] == '\r' {
		end--
	}
	key = bytes.TrimSpace(bytes.TrimPrefix(l[:eq], []byte("\xef\xbb\xbf")))
	return key, eq + 1, end, true
}

func (ini *INI) find(key string) int {
	for i, l := range ini.lines {
		if k, _, _, ok := splitLine(l); ok && string(k) == key {
			return i
		}
	}
	return -1
}

func (ini *INI) Get(key string) (string, bool) {
	i := ini.find(key)
	if i < 0 {
		return "", false
	}
	_, s, e, _ := splitLine(ini.lines[i])
	return string(ini.lines[i][s:e]), true
}

func (ini *INI) Set(key, val string) (bool, error) {
	if strings.ContainsAny(val, "\r\n") {
		return false, fmt.Errorf("%s: value has a line break", key)
	}
	i := ini.find(key)
	if i < 0 {
		return false, fmt.Errorf("%s is not in options.ini", key)
	}
	l := ini.lines[i]
	_, s, e, _ := splitLine(l)
	if string(l[s:e]) == val {
		return false, nil
	}
	nl := make([]byte, 0, len(l)-(e-s)+len(val))
	nl = append(nl, l[:s]...)
	nl = append(nl, val...)
	nl = append(nl, l[e:]...)
	ini.lines[i] = nl
	return true, nil
}

type GameOptions struct {
	Present   map[string]bool
	Mode      string
	Width     int
	Height    int
	VSync     bool
	FrameRate int
	Uncapped  bool
	FontSize  int
	Volumes   map[string]int
}

func parseBool(s string) bool { return strings.EqualFold(strings.TrimSpace(s), "true") }

func ReadOptions(ini *INI) GameOptions {
	o := GameOptions{Present: map[string]bool{}, Volumes: map[string]int{}}
	get := func(k string) string {
		v, ok := ini.Get(k)
		if ok {
			o.Present[k] = true
		}
		return v
	}
	atoi := func(k string) int {
		n, _ := strconv.Atoi(strings.TrimSpace(get(k)))
		return n
	}
	fs, bl := parseBool(get(KeyFullScreen)), parseBool(get(KeyBorderless))
	switch {
	case fs:
		o.Mode = ModeFullscreen
	case bl:
		o.Mode = ModeBorderless
	default:
		o.Mode = ModeWindowed
	}
	o.Width, o.Height = atoi(KeyWidth), atoi(KeyHeight)
	o.VSync = parseBool(get(KeyVSync))
	o.FrameRate = atoi(KeyFrameRate)
	o.Uncapped = parseBool(get(KeyUncappedFPS))
	o.FontSize = atoi(KeyFontSize)
	for _, v := range Volumes {
		o.Volumes[v.Key] = atoi(v.Key)
	}
	return o
}

func ApplyOptions(ini *INI, o GameOptions) (int, error) {
	if o.Present[KeyWidth] && (o.Width < 640 || o.Width > 16384) {
		return 0, fmt.Errorf("width %d is out of range (640-16384)", o.Width)
	}
	if o.Present[KeyHeight] && (o.Height < 480 || o.Height > 16384) {
		return 0, fmt.Errorf("height %d is out of range (480-16384)", o.Height)
	}
	if o.Present[KeyFontSize] && (o.FontSize < 1 || o.FontSize > len(FontSizes)) {
		return 0, fmt.Errorf("font size %d is out of range (1-%d)", o.FontSize, len(FontSizes))
	}
	if o.Present[KeyFrameRate] && o.FrameRate <= 0 {
		return 0, fmt.Errorf("frame rate %d is not valid", o.FrameRate)
	}
	type kv struct{ k, v string }
	want := []kv{
		{KeyFullScreen, strconv.FormatBool(o.Mode == ModeFullscreen)},
		{KeyBorderless, strconv.FormatBool(o.Mode == ModeBorderless)},
		{KeyWidth, strconv.Itoa(o.Width)},
		{KeyHeight, strconv.Itoa(o.Height)},
		{KeyVSync, strconv.FormatBool(o.VSync)},
		{KeyFrameRate, strconv.Itoa(o.FrameRate)},
		{KeyUncappedFPS, strconv.FormatBool(o.Uncapped)},
		{KeyFontSize, strconv.Itoa(o.FontSize)},
	}
	for _, v := range Volumes {
		n := o.Volumes[v.Key]
		if o.Present[v.Key] && (n < 0 || n > 10) {
			return 0, fmt.Errorf("%s %d is out of range (0-10)", v.Label, n)
		}
		want = append(want, kv{v.Key, strconv.Itoa(n)})
	}
	changed := 0
	for _, w := range want {
		if !o.Present[w.k] {
			continue
		}
		old, _ := ini.Get(w.k)
		if same(old, w.v) {
			continue
		}
		if _, err := ini.Set(w.k, w.v); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

func same(old, nv string) bool {
	old = strings.TrimSpace(old)
	if strings.EqualFold(old, nv) {
		return true
	}
	a, e1 := strconv.Atoi(old)
	b, e2 := strconv.Atoi(nv)
	return e1 == nil && e2 == nil && a == b
}
