package play

import (
	"fmt"
	"regexp"
	"strconv"
)

type GameVersion struct {
	Major  int
	Minor  int
	Suffix string
}

var gameVersionRe = regexp.MustCompile(`^([0-9]+)\.([0-9]+)(.*)$`)

func tryParseInt(s string) int {
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return 0
	}
	return int(n)
}

func ParseGameVersion(s string) (GameVersion, error) {
	m := gameVersionRe.FindStringSubmatch(s)
	if m == nil {
		return GameVersion{}, fmt.Errorf("invalid game version %q", s)
	}
	v := GameVersion{Major: tryParseInt(m[1]), Minor: tryParseInt(m[2]), Suffix: m[3]}
	if v.Minor < 0 || v.Minor > 999 {
		return GameVersion{}, fmt.Errorf("invalid game version %q: minor version must be from 0 to 999", s)
	}
	return v, nil
}

func (v GameVersion) Int() int { return v.Major*1000 + v.Minor }

func (v GameVersion) String() string { return fmt.Sprintf("%d.%d%s", v.Major, v.Minor, v.Suffix) }

func (v GameVersion) IsZero() bool { return v == GameVersion{} }

func versionIntFromName(name string) int {
	parts := splitDot(name)
	switch {
	case len(parts) == 1:
		return tryParseInt(parts[0]) * 1000
	case len(parts) >= 2:
		return tryParseInt(parts[0])*1000 + min(tryParseInt(parts[1]), 999)
	}
	return 0
}

func splitDot(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}
