package sandbox

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Option struct {
	Name             string
	Type             string
	Default          string
	Min, Max         string
	NumValues        int
	Page             string
	Translation      string
	ValueTranslation string
}

var (
	reBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	reLineComment  = regexp.MustCompile(`(?m)^\s*--.*$`)
	reOption       = regexp.MustCompile(`(?s)option\s+([A-Za-z0-9_.]+)\s*\{(.*?)\}`)
)

func ParseOptions(text string) []Option {
	text = reBlockComment.ReplaceAllString(text, "")
	text = reLineComment.ReplaceAllString(text, "")
	var out []Option
	for _, m := range reOption.FindAllStringSubmatch(text, -1) {
		o := Option{Name: m[1]}
		for _, part := range strings.FieldsFunc(m[2], func(r rune) bool { return r == ',' || r == '\n' || r == '\r' }) {
			k, v, ok := strings.Cut(part, "=")
			if !ok {
				continue
			}
			k, v = strings.TrimSpace(k), strings.TrimSpace(v)
			switch k {
			case "type":
				o.Type = v
			case "default":
				o.Default = v
			case "min":
				o.Min = v
			case "max":
				o.Max = v
			case "numValues":
				o.NumValues, _ = strconv.Atoi(v)
			case "page":
				o.Page = v
			case "translation":
				o.Translation = v
			case "valueTranslation":
				o.ValueTranslation = v
			}
		}
		switch o.Type {
		case "boolean", "integer", "double", "enum", "string":
			out = append(out, o)
		}
	}
	return out
}

func (o Option) Group() string {
	g, _, ok := strings.Cut(o.Name, ".")
	if !ok {
		return ""
	}
	return g
}

func (o Option) Key() string {
	_, k, ok := strings.Cut(o.Name, ".")
	if !ok {
		return o.Name
	}
	return k
}

func (o Option) Valid(v string) bool {
	switch o.Type {
	case "boolean":
		return v == "true" || v == "false"
	case "integer", "enum":
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return false
		}
		if o.Type == "enum" {
			return n >= 1 && (o.NumValues == 0 || n <= int64(o.NumValues))
		}
		return inRange(float64(n), o.Min, o.Max)
	case "double":
		f, err := strconv.ParseFloat(v, 64)
		return err == nil && inRange(f, o.Min, o.Max)
	}
	return !strings.ContainsAny(v, "\r\n")
}

func inRange(f float64, lo, hi string) bool {
	if l, err := strconv.ParseFloat(lo, 64); err == nil && f < l {
		return false
	}
	if h, err := strconv.ParseFloat(hi, 64); err == nil && f > h {
		return false
	}
	return true
}

type ModOptions struct {
	ModID   string
	ModName string
	Options []Option
	Text    Translations
}

func mediaDirs(versionDir, commonDir string) []string {
	var out []string
	for _, d := range []string{versionDir, commonDir} {
		if d != "" {
			out = append(out, filepath.Join(d, "media"))
		}
	}
	return out
}

func LoadMod(modID, modName, versionDir, commonDir string) (ModOptions, bool) {
	mo := ModOptions{ModID: modID, ModName: modName}
	seen := map[string]bool{}
	for _, m := range mediaDirs(versionDir, commonDir) {
		b, err := os.ReadFile(filepath.Join(m, "sandbox-options.txt"))
		if err != nil {
			continue
		}
		for _, o := range ParseOptions(string(b)) {
			if !seen[o.Name] {
				seen[o.Name] = true
				mo.Options = append(mo.Options, o)
			}
		}
	}
	if len(mo.Options) == 0 {
		return mo, false
	}
	var tdirs []string
	for _, m := range mediaDirs(versionDir, commonDir) {
		tdirs = append(tdirs, filepath.Join(m, "lua", "shared", "Translate", "EN"))
	}
	mo.Text = LoadTranslations(tdirs...)
	return mo, true
}
