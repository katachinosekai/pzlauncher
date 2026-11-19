package zbsetup

import (
	"regexp"
	"strings"
)

const (
	LinuxLoader   = "-javaagent:ZombieBuddy.jar"
	WindowsLoader = "-agentlib:zbNative"
	Separator     = "--"
	commandToken  = "%command%"
)

var (
	reLinuxLoader   = regexp.MustCompile(`(?i)^-javaagent:ZombieBuddy\.jar(=\S*)?$`)
	reWindowsLoader = regexp.MustCompile(`(?i)^-agentlib:zbNative(=\S*)?$`)
	reZBBatchToken  = regexp.MustCompile(`(?i)^-javaagent:ZBBatch\.jar(=\S*)?$`)
	reJVMToken      = regexp.MustCompile(`^-(D\S|X\S|javaagent:|agentlib:|agentpath:)`)
)

func LoaderFor(goos string) string {
	if goos == "windows" {
		return WindowsLoader
	}
	return LinuxLoader
}

func isLoaderFor(tok, goos string) bool {
	if goos == "windows" {
		return reWindowsLoader.MatchString(tok)
	}
	return reLinuxLoader.MatchString(tok)
}

func isAnyLoader(tok string) bool {
	return reLinuxLoader.MatchString(tok) || reWindowsLoader.MatchString(tok)
}

type span struct{ s, e int }

func splitTokens(v string) []span {
	var out []span
	i := 0
	for i < len(v) {
		for i < len(v) && isBlank(v[i]) {
			i++
		}
		if i >= len(v) {
			break
		}
		s := i
		for i < len(v) && !isBlank(v[i]) {
			i++
		}
		out = append(out, span{s, i})
	}
	return out
}

func isBlank(c byte) bool { return c == ' ' || c == '\t' }

func tokText(v string, t span) string { return v[t.s:t.e] }

func commandIndex(v string, toks []span) int {
	for i, t := range toks {
		if tokText(v, t) == commandToken {
			return i
		}
	}
	return -1
}

func HasAnyLoader(v string) bool {
	for _, t := range splitTokens(v) {
		if isAnyLoader(tokText(v, t)) {
			return true
		}
	}
	return false
}

func HasLoader(v, goos string) bool {
	toks := splitTokens(v)
	last := -1
	for i, t := range toks {
		if isLoaderFor(tokText(v, t), goos) {
			last = i
		}
	}
	if last < 0 {
		return false
	}
	return separatorAfter(v, toks, last+1) >= 0
}

func separatorAfter(v string, toks []span, from int) int {
	for j := from; j < len(toks); j++ {
		tt := tokText(v, toks[j])
		if tt == Separator {
			return j
		}
		if !reJVMToken.MatchString(tt) {
			return -1
		}
	}
	return -1
}

func dropTokens(v string, toks []span, drop map[int]bool) string {
	if len(toks) == 0 {
		return v
	}
	var b strings.Builder
	b.WriteString(v[:toks[0].s])
	wrote := false
	saved := ""
	haveSaved := false
	prevEnd := toks[0].s
	for i, t := range toks {
		gap := v[prevEnd:t.s]
		prevEnd = t.e
		if drop[i] {
			if wrote && !haveSaved {
				saved, haveSaved = gap, true
			}
			continue
		}
		if wrote {
			if haveSaved {
				b.WriteString(saved)
			} else {
				b.WriteString(gap)
			}
		}
		b.WriteString(tokText(v, t))
		wrote = true
		haveSaved = false
	}
	if !wrote {
		return ""
	}
	b.WriteString(v[toks[len(toks)-1].e:])
	return b.String()
}

func RemoveLoader(v string) (string, bool) {
	toks := splitTokens(v)
	drop := map[int]bool{}
	first := -1
	for i, t := range toks {
		if isAnyLoader(tokText(v, t)) {
			drop[i] = true
			if first < 0 {
				first = i
			}
		}
	}
	if first < 0 {
		return v, false
	}
	start := 0
	if c := commandIndex(v, toks); c >= 0 && c < first {
		start = c + 1
	}
	for j := first + 1; j < len(toks); j++ {
		tt := tokText(v, toks[j])
		if tt == Separator {
			all := true
			for k := start; k < j; k++ {
				if !drop[k] && !reZBBatchToken.MatchString(tokText(v, toks[k])) {
					all = false
					break
				}
			}
			for k := start; k < j; k++ {
				if reZBBatchToken.MatchString(tokText(v, toks[k])) {
					drop[k] = true
				}
			}
			if all {
				drop[j] = true
			}
			break
		}
		if !reJVMToken.MatchString(tt) {
			break
		}
	}
	return dropTokens(v, toks, drop), true
}

func AddLoader(v, goos string) (string, bool) {
	toks := splitTokens(v)
	last := -1
	other := false
	for i, t := range toks {
		tt := tokText(v, t)
		if isLoaderFor(tt, goos) {
			last = i
		} else if isAnyLoader(tt) {
			other = true
		}
	}
	if last >= 0 && !other {
		if separatorAfter(v, toks, last+1) >= 0 {
			return v, false
		}
		end := last
		for end+1 < len(toks) && reJVMToken.MatchString(tokText(v, toks[end+1])) {
			end++
		}
		at := toks[end].e
		return v[:at] + " " + Separator + v[at:], true
	}
	if last >= 0 || other {
		v, _ = RemoveLoader(v)
		toks = splitTokens(v)
	}
	loader := LoaderFor(goos)
	c := commandIndex(v, toks)
	from := 0
	if c >= 0 {
		from = c + 1
	}
	ins := loader + " " + Separator
	if separatorAfter(v, toks, from) >= 0 {
		ins = loader
	}
	if c >= 0 {
		at := toks[c].e
		return v[:at] + " " + ins + v[at:], true
	}
	if strings.TrimSpace(v) == "" {
		return ins, true
	}
	if isBlank(v[0]) {
		return ins + v, true
	}
	return ins + " " + v, true
}
