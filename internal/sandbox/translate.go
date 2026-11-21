package sandbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Translations map[string]string

var (
	reTag    = regexp.MustCompile(`(?i)<\s*(line|br)\s*/?>`)
	reOther  = regexp.MustCompile(`<[^<>]{1,40}>`)
	reTxtKey = regexp.MustCompile(`(?m)^\s*([A-Za-z0-9_.]+)\s*=\s*"`)
)

func LoadTranslations(dirs ...string) Translations {
	t := Translations{}
	for _, d := range dirs {
		for _, n := range []string{"Sandbox_EN.txt"} {
			if b, err := os.ReadFile(filepath.Join(d, n)); err == nil {
				for k, v := range parseTxt(string(b)) {
					if _, ok := t[k]; !ok {
						t[k] = v
					}
				}
			}
		}
		for _, n := range []string{"Sandbox.json", "Sandbox_EN.json"} {
			if b, err := os.ReadFile(filepath.Join(d, n)); err == nil {
				var m map[string]string
				if json.Unmarshal(b, &m) == nil {
					for k, v := range m {
						if _, ok := t[k]; !ok {
							t[k] = v
						}
					}
				}
			}
		}
	}
	return t
}

func parseTxt(s string) map[string]string {
	out := map[string]string{}
	for _, loc := range reTxtKey.FindAllStringSubmatchIndex(s, -1) {
		key := s[loc[2]:loc[3]]
		i := loc[1] - 1
		var b strings.Builder
		for {
			str, next, ok := quoted(s, i)
			if !ok {
				break
			}
			b.WriteString(str)
			j := next
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\r' || s[j] == '\n') {
				j++
			}
			if !strings.HasPrefix(s[j:], "..") {
				break
			}
			j += 2
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\r' || s[j] == '\n') {
				j++
			}
			if j >= len(s) || s[j] != '"' {
				break
			}
			i = j
		}
		out[key] = b.String()
	}
	return out
}

func quoted(s string, i int) (string, int, bool) {
	if i >= len(s) || s[i] != '"' {
		return "", i, false
	}
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case '"':
			if v, err := strconv.Unquote(s[i : j+1]); err == nil {
				return v, j + 1, true
			}
			return s[i+1 : j], j + 1, true
		case '\n':
			return "", i, false
		}
	}
	return "", i, false
}

func clean(s string) string {
	s = reTag.ReplaceAllString(s, "\n")
	s = reOther.ReplaceAllString(s, "")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.Join(strings.Fields(l), " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func (t Translations) get(key string) string {
	if v, ok := t[key]; ok {
		return clean(v)
	}
	return ""
}

func (t Translations) Label(o Option) string {
	if o.Translation != "" {
		if v := t.get("Sandbox_" + o.Translation); v != "" {
			return v
		}
	}
	if v := t.get("Sandbox_" + o.Name); v != "" {
		return v
	}
	return o.Name
}

func (t Translations) Tooltip(o Option) string {
	if o.Translation != "" {
		if v := t.get("Sandbox_" + o.Translation + "_tooltip"); v != "" {
			return v
		}
	}
	return t.get("Sandbox_" + o.Name + "_tooltip")
}

func (t Translations) Page(page string) string {
	if v := t.get("Sandbox_" + page); v != "" {
		return v
	}
	return page
}

func (t Translations) EnumValues(o Option) []string {
	out := make([]string, o.NumValues)
	for i := range out {
		v := ""
		if o.ValueTranslation != "" {
			v = t.get("Sandbox_" + o.ValueTranslation + "_option" + strconv.Itoa(i+1))
		}
		if v == "" && o.Translation != "" {
			v = t.get("Sandbox_" + o.Translation + "_option" + strconv.Itoa(i+1))
		}
		if v == "" {
			v = strconv.Itoa(i + 1)
		}
		out[i] = v
	}
	return out
}
