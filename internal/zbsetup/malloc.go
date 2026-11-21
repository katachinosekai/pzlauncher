package zbsetup

import (
	"regexp"
	"strings"
)

// MALLOC_ARENA_MAX=2 limits glibc's malloc pools in the game process
// (about 1 GB less RAM with heavy mod lists, no frame-time cost). Linux only.
// The launcher starts the game through Steam, so the variable has to live in
// Steam's launch options, in front of %command%.
const (
	MallocVar   = "MALLOC_ARENA_MAX"
	MallocValue = "2"
	MallocToken = MallocVar + "=" + MallocValue
)

var reEnvAssign = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// gamescopeSep returns the index of the "--" that ends a gamescope wrapper
// before %command%, or -1. gamescope strips MALLOC_ARENA_MAX from its child,
// so inside a wrapper the variable has to come after that "--".
func gamescopeSep(v string, toks []span, c int) int {
	for i := 0; i < c; i++ {
		t := tokText(v, toks[i])
		if t[strings.LastIndex(t, "/")+1:] != "gamescope" {
			continue
		}
		for j := i + 1; j < c; j++ {
			if tokText(v, toks[j]) == Separator {
				return j
			}
		}
		return -1
	}
	return -1
}

// mallocRange is the token range [from, c) where an environment prefix for the
// game itself lives, and c is the %command% index (-1 when there is none).
func mallocRange(v string, toks []span) (from, c int) {
	c = commandIndex(v, toks)
	if c < 0 {
		return 0, -1
	}
	if g := gamescopeSep(v, toks, c); g >= 0 {
		return g + 1, c
	}
	return 0, c
}

// MallocArena returns the MALLOC_ARENA_MAX value the game gets from these
// launch options, if any.
func MallocArena(v string) (string, bool) {
	toks := splitTokens(v)
	from, c := mallocRange(v, toks)
	for i := from; i < c; i++ {
		if val, ok := strings.CutPrefix(tokText(v, toks[i]), MallocVar+"="); ok {
			return val, true
		}
	}
	return "", false
}

// AddMalloc puts MALLOC_ARENA_MAX=2 in front of the game command. A value the
// user set themselves is kept, and it is never added twice.
func AddMalloc(v string) (string, bool) {
	if _, ok := MallocArena(v); ok {
		return v, false
	}
	toks := splitTokens(v)
	c := commandIndex(v, toks)
	if c < 0 {
		// Without %command% Steam appends the options to the game command,
		// so "X %command% <options>" starts the game the same way.
		rest := strings.TrimLeft(v, " \t")
		if rest == "" {
			return MallocToken + " " + commandToken, true
		}
		return MallocToken + " " + commandToken + " " + rest, true
	}
	if g := gamescopeSep(v, toks, c); g >= 0 {
		at := toks[g].e
		return v[:at] + " env " + MallocToken + v[at:], true
	}
	at := toks[0].s
	return v[:at] + MallocToken + " " + v[at:], true
}

// RemoveMalloc drops only the exact MALLOC_ARENA_MAX=2 the launcher adds (and
// the "env" it adds inside gamescope). Other values are the user's and stay.
func RemoveMalloc(v string) (string, bool) {
	toks := splitTokens(v)
	from, c := mallocRange(v, toks)
	if c < 0 {
		return v, false
	}
	drop := map[int]bool{}
	for i := from; i < c; i++ {
		if tokText(v, toks[i]) != MallocToken {
			continue
		}
		drop[i] = true
		if i > from && tokText(v, toks[i-1]) == "env" && (i+1 >= c || !reEnvAssign.MatchString(tokText(v, toks[i+1]))) {
			drop[i-1] = true
		}
	}
	if len(drop) == 0 {
		return v, false
	}
	out := dropTokens(v, toks, drop)
	if strings.TrimSpace(out) == commandToken {
		return "", true
	}
	return out, true
}

// MallocStatus sums up the launch options of every Steam account: on when one
// of them has MALLOC_ARENA_MAX=2, other = a different value the user set.
func MallocStatus(values []string) (on bool, other string) {
	for _, v := range values {
		val, ok := MallocArena(v)
		switch {
		case !ok:
		case val == MallocValue:
			on = true
		case other == "":
			other = val
		}
	}
	return on, other
}

// MallocPlan adds (on) or removes MALLOC_ARENA_MAX=2 in every localconfig.vdf.
func MallocPlan(configs []ConfigFile, on bool) Plan {
	var p Plan
	fn := RemoveMalloc
	if on {
		fn = AddMalloc
	}
	editConfigs(configs, func(v string) string {
		nv, _ := fn(v)
		return nv
	}, &p)
	return p
}

// AddMallocToPlan adds MALLOC_ARENA_MAX=2 on top of the launch-option edits a
// plan already makes (ZombieBuddy setup), so Steam is closed only once and the
// preview shows one before/after line per entry.
func AddMallocToPlan(p *Plan, configs []ConfigFile) {
	fn := func(v string) string {
		nv, _ := AddMalloc(v)
		return nv
	}
	for _, c := range configs {
		idx, text := -1, c.Text
		for i, ce := range p.Configs {
			if ce.Path == c.Path {
				idx, text = i, ce.Text
				break
			}
		}
		nt, edits, err := EditLocalConfig(text, fn)
		if err != nil || len(edits) == 0 {
			continue
		}
		if idx < 0 {
			p.Configs = append(p.Configs, ConfigEdit{Path: c.Path, Text: nt, Edits: edits})
			continue
		}
		ce := &p.Configs[idx]
		ce.Text = nt
		used := make([]bool, len(ce.Edits))
	next:
		for _, e := range edits {
			for i := range ce.Edits {
				if !used[i] && ce.Edits[i].After == e.Before {
					ce.Edits[i].After = e.After
					used[i] = true
					continue next
				}
			}
			ce.Edits = append(ce.Edits, e)
			used = append(used, true)
		}
	}
}
