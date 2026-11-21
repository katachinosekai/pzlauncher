package kvfile

import (
	"sort"
	"strings"
)

type Line struct {
	Raw     string
	Key     string
	Val     string
	Section string
}

func isComment(t string) bool {
	return t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, ";") || strings.HasPrefix(t, "--") || strings.HasPrefix(t, "//")
}

func Parse(text string) ([]*Line, int) {
	var out []*Line
	section := ""
	n := 0
	for _, l := range strings.Split(text, "\n") {
		t := strings.TrimSpace(strings.TrimSuffix(l, "\r"))
		kl := &Line{Raw: l, Section: section}
		switch {
		case strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]"):
			section = strings.TrimSpace(t[1 : len(t)-1])
			kl.Section = section
		case isComment(t):
		default:
			if i := strings.Index(l, "="); i > 0 {
				kl.Key = strings.TrimSpace(l[:i])
				kl.Val = strings.TrimSpace(strings.TrimSuffix(l[i+1:], "\r"))
				n++
			}
		}
		out = append(out, kl)
	}
	return out, n
}

func Simple(text string) bool {
	lines, n := Parse(text)
	if n == 0 {
		return false
	}
	for _, l := range lines {
		t := strings.TrimSpace(strings.TrimSuffix(l.Raw, "\r"))
		if l.Key == "" && !isComment(t) && !(strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]")) {
			return false
		}
		if l.Key != "" && (strings.ContainsAny(l.Key, " \t{}\"") || strings.HasSuffix(l.Val, "{")) {
			return false
		}
	}
	return true
}

func setVal(raw, v string) string {
	i := strings.Index(raw, "=")
	rest := raw[i+1:]
	lead := rest[:len(rest)-len(strings.TrimLeft(rest, " \t"))]
	cr := ""
	if strings.HasSuffix(raw, "\r") {
		cr = "\r"
	}
	return raw[:i+1] + lead + v + cr
}

func Apply(lines []*Line, newVal func(*Line) (string, bool)) string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		v, changed := "", false
		if l.Key != "" {
			v, changed = newVal(l)
		}
		if !changed {
			out = append(out, l.Raw)
			continue
		}
		out = append(out, setVal(l.Raw, v))
	}
	return strings.Join(out, "\n")
}

type Change struct {
	Section, Key, Old, New string
	Added                  bool
}

func (c Change) Name() string {
	if c.Section != "" {
		return "[" + c.Section + "] " + c.Key
	}
	return c.Key
}

func Merge(dst, src string) (string, []Change) {
	type key struct{ s, k string }
	srcLines, _ := Parse(src)
	want := map[key]string{}
	var order []key
	for _, l := range srcLines {
		if l.Key == "" {
			continue
		}
		k := key{l.Section, l.Key}
		if _, ok := want[k]; !ok {
			order = append(order, k)
		}
		want[k] = l.Val
	}
	dstLines, _ := Parse(dst)
	nl := "\n"
	if strings.Contains(dst, "\r\n") {
		nl = "\r\n"
	}
	cr := strings.TrimSuffix(nl, "\n")
	seen := map[key]bool{}
	lastOfSection := map[string]int{}
	firstHeader := -1
	var changes []Change
	out := make([]string, 0, len(dstLines))
	for _, l := range dstLines {
		isHeader := l.Key == "" && strings.HasPrefix(strings.TrimSpace(l.Raw), "[")
		if isHeader && firstHeader < 0 {
			firstHeader = len(out)
		}
		if l.Key != "" || isHeader {
			lastOfSection[l.Section] = len(out)
		}
		if l.Key == "" {
			out = append(out, l.Raw)
			continue
		}
		k := key{l.Section, l.Key}
		v, ok := want[k]
		if !ok || seen[k] {
			out = append(out, l.Raw)
			continue
		}
		seen[k] = true
		if v != l.Val {
			changes = append(changes, Change{Section: l.Section, Key: l.Key, Old: l.Val, New: v})
			out = append(out, setVal(l.Raw, v))
			continue
		}
		out = append(out, l.Raw)
	}
	bySection := map[string][]string{}
	var sectionOrder []string
	for _, k := range order {
		if seen[k] {
			continue
		}
		if _, ok := bySection[k.s]; !ok {
			sectionOrder = append(sectionOrder, k.s)
		}
		bySection[k.s] = append(bySection[k.s], k.k+"="+want[k]+cr)
		changes = append(changes, Change{Section: k.s, Key: k.k, New: want[k], Added: true})
	}
	end := len(out)
	for end > 0 && strings.TrimSpace(out[end-1]) == "" {
		end--
	}
	type ins struct {
		at    int
		lines []string
	}
	var inserts []ins
	var tail []string
	for _, sec := range sectionOrder {
		at, ok := lastOfSection[sec]
		switch {
		case ok:
			inserts = append(inserts, ins{at + 1, bySection[sec]})
		case sec == "" && firstHeader >= 0:
			inserts = append(inserts, ins{firstHeader, bySection[sec]})
		default:
			if sec != "" {
				tail = append(tail, "["+sec+"]"+cr)
			}
			tail = append(tail, bySection[sec]...)
		}
	}
	sort.SliceStable(inserts, func(i, j int) bool { return inserts[i].at > inserts[j].at })
	for _, in := range inserts {
		at := min(in.at, end)
		out = append(out[:at], append(append([]string(nil), in.lines...), out[at:]...)...)
		end += len(in.lines)
	}
	if len(tail) > 0 {
		out = append(out[:end], append(tail, out[end:]...)...)
	}
	return strings.Join(out, "\n"), changes
}
