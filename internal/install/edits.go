package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const XmxTarget = "-Xmx6g"

var JVMFlags = []string{
	"-XX:+UseCompactObjectHeaders",
	"-XX:+UseStringDeduplication",
	"-XX:SoftMaxHeapSize=6g",
	"-XX:+UnlockExperimentalVMOptions",
	"-XX:ShenandoahUncommitDelay=30000",
	"-XX:ZUncommitDelay=30",
}

var (
	reXmx         = regexp.MustCompile(`-Xmx[0-9]+[kKmMgG]?`)
	reVMArgs      = regexp.MustCompile(`(?i)"vmArgs"\s*:\s*\[`)
	reVMArgsNext  = regexp.MustCompile(`(?i)"vmArgs"\s*:\s*\[\s*[,"]`)
	reXmxQuoted   = regexp.MustCompile(`(?i)"-Xmx6g"`)
	reAgentAny    = regexp.MustCompile(`(?i)-javaagent:ZBBatch\.jar`)
	reAgentComma  = regexp.MustCompile(`(?i),\s*"-javaagent:ZBBatch\.jar"`)
	reAgentAfter  = regexp.MustCompile(`(?i)"-javaagent:ZBBatch\.jar"\s*,?\s*`)
	reShimLine    = regexp.MustCompile(`(?i)^\s*mod\s*=\s*\\?PZArrayListCompat\s*,\s*$`)
	reZBLine      = regexp.MustCompile(`(?i)^\s*mod\s*=\s*\\?ZombieBuddy\s*,\s*$`)
	reZBWord      = regexp.MustCompile(`(?i)ZombieBuddy`)
	reModLine     = regexp.MustCompile(`(?i)^(\s*mod\s*=\s*)(\\?)([^,]*?)\s*,\s*$`)
	reModsOpen    = regexp.MustCompile(`(?i)^\s*mods\s*$`)
	reAppBlock    = regexp.MustCompile(`"108600"\s*\{`)
	reLaunchOpt   = regexp.MustCompile(`("LaunchOptions"\s*")((?:[^"\\]|\\.)*)(")`)
	reZBNative    = regexp.MustCompile(`(?i)-agentlib:zbNative\S*`)
	reZBJavaagent = regexp.MustCompile(`(?i)-javaagent:ZombieBuddy\.jar`)
	reApproval    = regexp.MustCompile(`(?i)^(\S+)\s+([0-9a-f]{64})\s*$`)
	reVersionMax  = regexp.MustCompile(`(?m)^versionMax=42\.21([ \t\r]*)$`)
	reModVersion  = regexp.MustCompile(`(?i)^\s*modversion\s*=\s*(.+)$`)
	reNightly     = regexp.MustCompile(`(?i)-rc\d*|nightly|alpha|beta`)
)

type XmxResult struct {
	Text    string
	Old     string
	Changed bool
	NoArray bool
}

func SetXmx(text string) XmxResult {
	if m := reXmx.FindString(text); m != "" {
		if m == XmxTarget {
			return XmxResult{Text: text, Old: m}
		}
		return XmxResult{Text: reXmx.ReplaceAllLiteralString(text, XmxTarget), Old: m, Changed: true}
	}
	if reVMArgs.MatchString(text) {
		rep := `"vmArgs" : [ "` + XmxTarget + `"`
		if reVMArgsNext.MatchString(text) {
			rep += ","
		}
		return XmxResult{Text: reVMArgs.ReplaceAllLiteralString(text, rep), Changed: true}
	}
	return XmxResult{Text: text, NoArray: true}
}

func AddJVMFlags(text string) (string, []string) {
	var added []string
	for _, flag := range JVMFlags {
		if strings.Contains(strings.ToLower(text), strings.ToLower(`"`+flag+`"`)) {
			continue
		}
		if !reXmxQuoted.MatchString(text) {
			continue
		}
		after := XmxTarget
		if len(added) > 0 {
			after = added[len(added)-1]
		}
		needle := `"` + after + `"`
		i := strings.Index(text, needle)
		if i < 0 {
			continue
		}
		text = text[:i] + needle + `, "` + flag + `"` + text[i+len(needle):]
		added = append(added, flag)
	}
	return text, added
}

func HasZBBatchAgent(text string) bool { return reAgentAny.MatchString(text) }

func RemoveZBBatchAgent(text string) string {
	text = reAgentComma.ReplaceAllLiteralString(text, "")
	return reAgentAfter.ReplaceAllLiteralString(text, "")
}

func AddVMArg(text, arg string) (string, bool, error) {
	if strings.Contains(strings.ToLower(text), strings.ToLower(`"`+arg+`"`)) {
		return text, false, nil
	}
	loc := reVMArgs.FindStringIndex(text)
	if loc == nil {
		return text, false, errors.New("no vmArgs array")
	}
	open := loc[1] - 1
	lastComma := -1
	end := -1
	for i := loc[1]; i < len(text); i++ {
		c := text[i]
		if c == '"' {
			i++
			for i < len(text) && text[i] != '"' {
				if text[i] == '\\' {
					i++
				}
				i++
			}
			continue
		}
		if c == ',' {
			lastComma = i
		}
		if c == ']' {
			end = i
			break
		}
		if c == '[' || c == '{' {
			return text, false, errors.New("nested vmArgs array")
		}
	}
	if end < 0 {
		return text, false, errors.New("unterminated vmArgs array")
	}
	lastEnd := end
	for lastEnd > open+1 && isSpace(text[lastEnd-1]) {
		lastEnd--
	}
	var out string
	if lastEnd == open+1 {
		out = text[:open+1] + ` "` + arg + `" ` + text[end:]
	} else {
		sep := ", "
		if lastComma > 0 {
			j := lastComma + 1
			for j < len(text) && isSpace(text[j]) {
				j++
			}
			sep = text[lastComma:j]
		} else {
			j := open + 1
			for j < len(text) && isSpace(text[j]) {
				j++
			}
			if j > open+1 {
				sep = "," + text[open+1:j]
			}
		}
		out = text[:lastEnd] + sep + `"` + arg + `"` + text[lastEnd:]
	}
	if !json.Valid([]byte(out)) {
		return text, false, errors.New("result is not valid JSON")
	}
	return out, true, nil
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

type ShimResult struct {
	Text      string
	NoZB      bool
	Unchanged bool
}

func splitLines(text string) ([]string, string) {
	nl := "\n"
	if strings.Contains(text, "\r\n") {
		nl = "\r\n"
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines, nl
}

func PlaceShim(text string) ShimResult {
	lines, nl := splitLines(text)
	out := make([]string, 0, len(lines)+1)
	done := false
	for _, l := range lines {
		if reShimLine.MatchString(l) {
			continue
		}
		out = append(out, l)
		if !done && reZBLine.MatchString(l) {
			out = append(out, reZBWord.ReplaceAllLiteralString(l, "PZArrayListCompat"))
			done = true
		}
	}
	if !done {
		return ShimResult{Text: text, NoZB: true}
	}
	nt := strings.Join(out, nl)
	return ShimResult{Text: nt, Unchanged: nt == text}
}

func EnableMods(text string, ids []string) (string, []string, error) {
	lines, nl := splitLines(text)
	present := map[string]bool{}
	start, closeAt := -1, -1
	prefix, slash := "    mod = ", ""
	inMods := false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if start < 0 && reModsOpen.MatchString(l) {
			start = i
			continue
		}
		if start >= 0 && !inMods && t == "{" {
			inMods = true
			continue
		}
		if inMods {
			if t == "}" {
				closeAt = i
				break
			}
			if m := reModLine.FindStringSubmatch(l); m != nil {
				present[strings.ToLower(m[3])] = true
				prefix, slash = m[1], m[2]
			}
		}
	}
	if closeAt < 0 {
		return text, nil, errors.New("no mods { } block")
	}
	var add []string
	var addLines []string
	for _, id := range ids {
		if id == "" || present[strings.ToLower(id)] {
			continue
		}
		present[strings.ToLower(id)] = true
		add = append(add, id)
		addLines = append(addLines, prefix+slash+id+",")
	}
	if len(add) == 0 {
		return text, nil, nil
	}
	out := append([]string{}, lines[:closeAt]...)
	out = append(out, addLines...)
	out = append(out, lines[closeAt:]...)
	return strings.Join(out, nl), add, nil
}

type LaunchResult struct {
	Text     string
	Changed  int
	Already  int
	NoLoader int
	Values   []string
}

func scanAppBlocks(text string, edit func(value string, ok bool) string) string {
	var sb strings.Builder
	pos := 0
	for _, m := range reAppBlock.FindAllStringIndex(text, -1) {
		if m[0] < pos {
			continue
		}
		i := m[1]
		depth := 1
		for i < len(text) && depth > 0 {
			c := text[i]
			if c == '"' {
				i++
				for i < len(text) && text[i] != '"' {
					if text[i] == '\\' {
						i++
					}
					i++
				}
			} else if c == '{' {
				depth++
			} else if c == '}' {
				depth--
			}
			i++
		}
		if depth != 0 {
			break
		}
		if i > len(text) {
			i = len(text)
		}
		block := text[m[0]:i]
		if lo := reLaunchOpt.FindStringSubmatchIndex(block); lo != nil {
			v := block[lo[4]:lo[5]]
			nv := edit(v, true)
			block = block[:lo[4]] + nv + block[lo[5]:]
		} else {
			edit("", false)
		}
		sb.WriteString(text[pos:m[0]])
		sb.WriteString(block)
		pos = i
	}
	sb.WriteString(text[pos:])
	return sb.String()
}

func LaunchEditWindows(text string) LaunchResult {
	r := LaunchResult{}
	r.Text = scanAppBlocks(text, func(v string, ok bool) string {
		if !ok {
			r.NoLoader++
			return v
		}
		r.Values = append(r.Values, v)
		switch {
		case reAgentAny.MatchString(v):
			r.Already++
		case reZBNative.MatchString(v):
			loc := reZBNative.FindStringIndex(v)
			r.Changed++
			return v[:loc[1]] + " -javaagent:ZBBatch.jar" + v[loc[1]:]
		default:
			r.NoLoader++
		}
		return v
	})
	return r
}

func LaunchScanLinux(text string) LaunchResult {
	r := LaunchResult{Text: text}
	scanAppBlocks(text, func(v string, ok bool) string {
		if !ok {
			r.NoLoader++
			return v
		}
		r.Values = append(r.Values, v)
		switch {
		case reAgentAny.MatchString(v):
			r.Already++
		case reZBNative.MatchString(v) || reZBJavaagent.MatchString(v):
			r.Changed++
		default:
			r.NoLoader++
		}
		return v
	})
	return r
}

func HasZBLoader(s string) bool {
	return reZBNative.MatchString(s) || reZBJavaagent.MatchString(s)
}

type orderedObject struct {
	keys []string
	vals map[string]json.RawMessage
}

func parseOrdered(data []byte) (*orderedObject, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	t, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := t.(json.Delim); !ok || d != '{' {
		return nil, errors.New("not a JSON object")
	}
	o := &orderedObject{vals: map[string]json.RawMessage{}}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		k := kt.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		if _, dup := o.vals[k]; !dup {
			o.keys = append(o.keys, k)
		}
		o.vals[k] = raw
	}
	return o, nil
}

func (o *orderedObject) set(k string, v json.RawMessage) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

func (o *orderedObject) marshal() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		b.Write(o.vals[k])
	}
	b.WriteByte('}')
	var out bytes.Buffer
	if err := json.Indent(&out, b.Bytes(), "", "  "); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

type Approval struct {
	ID      string
	JarHash string
}

func ParseApprovals(text string) []Approval {
	var out []Approval
	lines, _ := splitLines(text)
	for _, l := range lines {
		if m := reApproval.FindStringSubmatch(l); m != nil {
			out = append(out, Approval{ID: m[1], JarHash: m[2]})
		}
	}
	return out
}

func MergeApprovals(existing []byte, want []Approval) ([]byte, int, error) {
	var o *orderedObject
	if existing == nil {
		o = &orderedObject{vals: map[string]json.RawMessage{}}
		o.set("formatVersion", json.RawMessage("2"))
		o.set("mods", json.RawMessage("[]"))
	} else {
		var err error
		o, err = parseOrdered(bytes.TrimPrefix(existing, []byte("\xef\xbb\xbf")))
		if err != nil {
			return nil, 0, fmt.Errorf("mod_approvals.json: %w", err)
		}
	}
	var mods []json.RawMessage
	if raw, ok := o.vals["mods"]; ok && string(bytes.TrimSpace(raw)) != "null" {
		if err := json.Unmarshal(raw, &mods); err != nil {
			var one json.RawMessage
			if json.Unmarshal(raw, &one) != nil {
				return nil, 0, fmt.Errorf("mod_approvals.json mods: %w", err)
			}
			mods = []json.RawMessage{one}
		}
	}
	type entry struct {
		ID      string `json:"id"`
		JarHash string `json:"jar_hash"`
	}
	var have []entry
	for _, m := range mods {
		var e entry
		_ = json.Unmarshal(m, &e)
		have = append(have, e)
	}
	added := 0
	for _, a := range want {
		found := false
		for _, e := range have {
			if strings.EqualFold(e.ID, a.ID) && strings.EqualFold(e.JarHash, a.JarHash) {
				found = true
				break
			}
		}
		if found {
			continue
		}
		nb, _ := json.Marshal(struct {
			ID       string `json:"id"`
			JarHash  string `json:"jar_hash"`
			Decision bool   `json:"decision"`
		}{a.ID, a.JarHash, true})
		mods = append(mods, nb)
		have = append(have, entry{a.ID, a.JarHash})
		added++
	}
	if added == 0 {
		return existing, 0, nil
	}
	if mods == nil {
		mods = []json.RawMessage{}
	}
	mb, err := json.Marshal(mods)
	if err != nil {
		return nil, 0, err
	}
	o.set("mods", mb)
	out, err := o.marshal()
	return out, added, err
}

func BumpVersionMax(text string) (string, bool) {
	if !reVersionMax.MatchString(text) {
		return text, false
	}
	return reVersionMax.ReplaceAllString(text, "versionMax=42.22$1"), true
}

func RemoveAcfItem(text, id string) (string, int) {
	re := regexp.MustCompile(`(?s)\r?\n[ \t]*"` + regexp.QuoteMeta(id) + `"[ \t]*\r?\n[ \t]*\{[^{}]*\}`)
	n := len(re.FindAllStringIndex(text, -1))
	return re.ReplaceAllLiteralString(text, ""), n
}

func NightlyFromModInfo(text string) (string, bool) {
	lines, _ := splitLines(text)
	for _, l := range lines {
		if m := reModVersion.FindStringSubmatch(l); m != nil {
			v := strings.TrimSpace(m[1])
			if reNightly.MatchString(v) {
				return v, true
			}
			return "", true
		}
	}
	return "", false
}

func jsonValid(s string) bool { return json.Valid([]byte(s)) }
