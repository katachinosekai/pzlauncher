package gamecfg

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/katachinosekai/pzlauncher/internal/install"
)

const (
	MinXmxGB         = 2
	RecommendedXmxGB = 8
	FlagUnlock       = "-XX:+UnlockExperimentalVMOptions"
	FlagShenUncommit = "-XX:ShenandoahUncommitDelay=30000"
	softMaxPrefix    = "-XX:SoftMaxHeapSize="
)

var (
	reVMArgsOpen = regexp.MustCompile(`(?i)"vmArgs"\s*:\s*\[`)
	reXmxArg     = regexp.MustCompile(`(?i)^-Xmx([0-9]+)([kmg]?)$`)
	reSizeArg    = regexp.MustCompile(`(?i)^([0-9]+)([kmg]?)$`)
)

var ErrNoVMArgs = errors.New("no vmArgs array in ProjectZomboid64.json")

func PackFlags() []string { return append([]string(nil), install.JVMFlags...) }

type element struct {
	start, end int
	value      string
}

type vmArray struct {
	open, close int
	elems       []element
}

func scanVMArgs(text string) (*vmArray, error) {
	loc := reVMArgsOpen.FindStringIndex(text)
	if loc == nil {
		return nil, ErrNoVMArgs
	}
	a := &vmArray{open: loc[1] - 1, close: -1}
	i := loc[1]
	for i < len(text) {
		c := text[i]
		switch {
		case isSpace(c) || c == ',':
			i++
		case c == ']':
			a.close = i
			return a, nil
		case c == '"':
			j := i + 1
			for j < len(text) && text[j] != '"' {
				if text[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(text) {
				return nil, errors.New("unterminated string in vmArgs")
			}
			var v string
			if err := json.Unmarshal([]byte(text[i:j+1]), &v); err != nil {
				return nil, fmt.Errorf("bad string in vmArgs: %w", err)
			}
			a.elems = append(a.elems, element{start: i, end: j + 1, value: v})
			i = j + 1
		default:
			return nil, errors.New("vmArgs contains a non-string entry")
		}
	}
	return nil, errors.New("unterminated vmArgs array")
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

func quote(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimRight(b.String(), "\n")
}

func (a *vmArray) separator(text string) string {
	if len(a.elems) >= 2 {
		return text[a.elems[0].end:a.elems[1].start]
	}
	if len(a.elems) == 1 {
		ws := text[a.open+1 : a.elems[0].start]
		if strings.ContainsAny(ws, "\n") {
			return "," + ws
		}
	}
	return ", "
}

func replaceElem(text string, a *vmArray, k int, val string) string {
	e := a.elems[k]
	return text[:e.start] + quote(val) + text[e.end:]
}

func insertElem(text string, a *vmArray, after int, val string) (string, error) {
	if len(a.elems) == 0 {
		out, _, err := install.AddVMArg(text, val)
		return out, err
	}
	sep := a.separator(text)
	if after < 0 {
		s := a.elems[0].start
		return text[:s] + quote(val) + sep + text[s:], nil
	}
	e := a.elems[after].end
	return text[:e] + sep + quote(val) + text[e:], nil
}

func removeElem(text string, a *vmArray, k int) string {
	switch {
	case k > 0:
		return text[:a.elems[k-1].end] + text[a.elems[k].end:]
	case len(a.elems) > 1:
		return text[:a.elems[0].start] + text[a.elems[1].start:]
	default:
		return text[:a.elems[0].start] + text[a.elems[0].end:]
	}
}

func ParseSizeMB(s string) (int, bool) {
	m := reSizeArg.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, false
	}
	switch strings.ToLower(m[2]) {
	case "g":
		n *= 1024
	case "m":
	case "k":
		n /= 1024
	default:
		n /= 1024 * 1024
	}
	if n > 1<<30 {
		return 0, false
	}
	return int(n), true
}

func FormatSize(mb int) string {
	if mb%1024 == 0 {
		return strconv.Itoa(mb/1024) + "g"
	}
	return strconv.Itoa(mb) + "m"
}

func xmxMB(arg string) (int, bool) {
	m := reXmxArg.FindStringSubmatch(arg)
	if m == nil {
		return 0, false
	}
	return ParseSizeMB(m[1] + m[2])
}

func isSoftMax(flag string) bool {
	return strings.HasPrefix(strings.ToLower(flag), strings.ToLower(softMaxPrefix))
}

func boolOpposite(flag string) string {
	if strings.HasPrefix(flag, "-XX:+") {
		return "-XX:-" + flag[5:]
	}
	return ""
}

func matchFlag(flag, arg string) bool {
	if isSoftMax(flag) {
		return isSoftMax(arg)
	}
	return strings.EqualFold(flag, arg)
}

type JVMInfo struct {
	Args      []string
	Xmx       string
	XmxMB     int
	GC        string
	Flags     map[string]bool
	SoftMax   string
	SoftMaxMB int
}

func DetectGC(args []string) string {
	gc := "G1 (default)"
	for _, a := range args {
		switch strings.ToLower(a) {
		case "-xx:+usezgc":
			gc = "ZGC"
		case "-xx:+useshenandoahgc":
			gc = "Shenandoah"
		case "-xx:+useg1gc":
			gc = "G1"
		case "-xx:+useparallelgc":
			gc = "Parallel"
		case "-xx:+useserialgc":
			gc = "Serial"
		}
	}
	return gc
}

func ReadJVM(text string) (JVMInfo, error) {
	a, err := scanVMArgs(text)
	if err != nil {
		return JVMInfo{}, err
	}
	info := JVMInfo{Flags: map[string]bool{}}
	for _, e := range a.elems {
		info.Args = append(info.Args, e.value)
		if mb, ok := xmxMB(e.value); ok {
			info.Xmx, info.XmxMB = e.value, mb
		}
	}
	for _, f := range install.JVMFlags {
		for _, v := range info.Args {
			if matchFlag(f, v) {
				info.Flags[f] = true
				if isSoftMax(f) {
					info.SoftMax = v
					info.SoftMaxMB, _ = ParseSizeMB(v[len(softMaxPrefix):])
				}
			}
		}
	}
	info.GC = DetectGC(info.Args)
	return info, nil
}

type JVMWant struct {
	XmxMB     int             `json:"xmxMB"`
	Flags     map[string]bool `json:"flags"`
	SoftMaxMB int             `json:"softMaxMB,omitempty"`
}

func (i JVMInfo) Want() JVMWant {
	w := JVMWant{XmxMB: i.XmxMB, Flags: map[string]bool{}, SoftMaxMB: i.SoftMaxMB}
	for _, f := range install.JVMFlags {
		w.Flags[f] = i.Flags[f]
	}
	return w
}

func DefaultSoftMaxMB(xmx int) int {
	pack, _ := ParseSizeMB(strings.TrimPrefix(packSoftMax(), softMaxPrefix))
	if xmx-2048 >= pack {
		return pack
	}
	if xmx >= 4096 {
		return xmx - 2048
	}
	return xmx * 3 / 4
}

func packSoftMax() string {
	for _, f := range install.JVMFlags {
		if isSoftMax(f) {
			return f
		}
	}
	return softMaxPrefix + "6g"
}

func XmxRangeGB(totalMB int) (int, int) {
	hi := 16
	if totalMB > 0 {
		hi = (totalMB+512)/1024 - 2
	}
	if hi < MinXmxGB {
		hi = MinXmxGB
	}
	return MinXmxGB, hi
}

func ApplyJVM(text string, want JVMWant) (string, []string, error) {
	var notes []string
	if want.XmxMB < MinXmxGB*1024 {
		return text, nil, fmt.Errorf("memory must be at least %d GB", MinXmxGB)
	}
	a, err := scanVMArgs(text)
	if err != nil {
		return text, nil, err
	}
	out := text
	xi := -1
	for k, e := range a.elems {
		if _, ok := xmxMB(e.value); ok {
			xi = k
		}
	}
	nx := "-Xmx" + strconv.Itoa(want.XmxMB) + "m"
	if xi >= 0 {
		if cur, _ := xmxMB(a.elems[xi].value); cur != want.XmxMB {
			out = replaceElem(out, a, xi, nx)
		}
	} else {
		if out, err = insertElem(out, a, -1, nx); err != nil {
			return text, nil, err
		}
	}
	flags := map[string]bool{}
	for k, v := range want.Flags {
		flags[k] = v
	}
	if flags[FlagShenUncommit] && !flags[FlagUnlock] {
		flags[FlagUnlock] = true
		notes = append(notes, FlagUnlock+" kept on: "+FlagShenUncommit+" needs it")
	}
	softMB := want.SoftMaxMB
	if softMB <= 0 {
		softMB = DefaultSoftMaxMB(want.XmxMB)
	}
	if softMB >= want.XmxMB {
		nsoft := DefaultSoftMaxMB(want.XmxMB)
		notes = append(notes, fmt.Sprintf("SoftMaxHeapSize lowered from %s to %s to stay below the memory limit", FormatSize(softMB), FormatSize(nsoft)))
		softMB = nsoft
	}
	for fi, f := range install.JVMFlags {
		on, set := flags[f]
		if !set {
			continue
		}
		if a, err = scanVMArgs(out); err != nil {
			return text, nil, err
		}
		target := f
		if isSoftMax(f) {
			target = softMaxPrefix + FormatSize(softMB)
		}
		idx, opp := -1, -1
		for k, e := range a.elems {
			if matchFlag(f, e.value) && idx < 0 {
				idx = k
			}
			if o := boolOpposite(f); o != "" && strings.EqualFold(o, e.value) && opp < 0 {
				opp = k
			}
		}
		switch {
		case on && idx >= 0:
			if isSoftMax(f) {
				cur, _ := ParseSizeMB(a.elems[idx].value[len(softMaxPrefix):])
				if cur != softMB {
					out = replaceElem(out, a, idx, target)
				}
			}
		case on && opp >= 0:
			out = replaceElem(out, a, opp, target)
		case on:
			after := -1
			for k, e := range a.elems {
				if _, ok := xmxMB(e.value); ok {
					after = k
				}
			}
			for _, prev := range install.JVMFlags[:fi] {
				for k, e := range a.elems {
					if matchFlag(prev, e.value) {
						after = k
					}
				}
			}
			if after < 0 {
				after = len(a.elems) - 1
			}
			if out, err = insertElem(out, a, after, target); err != nil {
				return text, nil, err
			}
		case !on && idx >= 0:
			for idx >= 0 {
				out = removeElem(out, a, idx)
				if a, err = scanVMArgs(out); err != nil {
					return text, nil, err
				}
				idx = -1
				for k, e := range a.elems {
					if matchFlag(f, e.value) {
						idx = k
						break
					}
				}
			}
		}
	}
	if !json.Valid([]byte(strings.TrimPrefix(out, "\ufeff"))) {
		return text, nil, errors.New("edited ProjectZomboid64.json would not be valid JSON; nothing changed")
	}
	return out, notes, nil
}
