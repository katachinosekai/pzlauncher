package workshop

import (
	"errors"
	"strings"
)

type VDF struct {
	Keys  []string
	Vals  map[string]string
	Child map[string]*VDF
}

func newVDF() *VDF { return &VDF{Vals: map[string]string{}, Child: map[string]*VDF{}} }

func (v *VDF) Get(path ...string) *VDF {
	cur := v
	for _, p := range path {
		if cur == nil {
			return nil
		}
		cur = cur.Child[p]
	}
	return cur
}

func vdfTokens(s string) ([]string, []bool, error) {
	var toks []string
	var quoted []bool
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c == '/' && i+1 < len(s) && s[i+1] == '/':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '{' || c == '}':
			toks = append(toks, string(c))
			quoted = append(quoted, false)
			i++
		case c == '"':
			var b strings.Builder
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' && i+1 < len(s) {
					i++
					switch s[i] {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					default:
						b.WriteByte(s[i])
					}
					i++
					continue
				}
				b.WriteByte(s[i])
				i++
			}
			if i >= len(s) {
				return nil, nil, errors.New("vdf: unterminated string")
			}
			i++
			toks = append(toks, b.String())
			quoted = append(quoted, true)
		default:
			j := i
			for j < len(s) && !strings.ContainsRune(" \t\r\n{}\"", rune(s[j])) {
				j++
			}
			toks = append(toks, s[i:j])
			quoted = append(quoted, true)
			i = j
		}
	}
	return toks, quoted, nil
}

func ParseVDF(s string) (*VDF, error) {
	toks, quoted, err := vdfTokens(strings.TrimPrefix(s, "\ufeff"))
	if err != nil {
		return nil, err
	}
	root := newVDF()
	stack := []*VDF{root}
	for i := 0; i < len(toks); i++ {
		cur := stack[len(stack)-1]
		if !quoted[i] && toks[i] == "}" {
			if len(stack) == 1 {
				return nil, errors.New("vdf: unbalanced }")
			}
			stack = stack[:len(stack)-1]
			continue
		}
		if !quoted[i] {
			return nil, errors.New("vdf: unexpected {")
		}
		key := toks[i]
		if i+1 >= len(toks) {
			return nil, errors.New("vdf: key without value")
		}
		i++
		if !quoted[i] && toks[i] == "{" {
			child := newVDF()
			if _, ok := cur.Child[key]; !ok {
				cur.Keys = append(cur.Keys, key)
			}
			cur.Child[key] = child
			stack = append(stack, child)
			continue
		}
		if !quoted[i] {
			return nil, errors.New("vdf: unexpected }")
		}
		if _, ok := cur.Vals[key]; !ok {
			cur.Keys = append(cur.Keys, key)
		}
		cur.Vals[key] = toks[i]
	}
	if len(stack) != 1 {
		return nil, errors.New("vdf: unbalanced {")
	}
	return root, nil
}

type ACF struct {
	Installed  map[string]bool
	Subscribed map[string]bool
	NeedsDL    bool
}

func ParseACF(s string) (*ACF, error) {
	v, err := ParseVDF(s)
	if err != nil {
		return nil, err
	}
	aw := v.Get("AppWorkshop")
	if aw == nil {
		return nil, errors.New("acf: no AppWorkshop block")
	}
	a := &ACF{Installed: map[string]bool{}, Subscribed: map[string]bool{}}
	if in := aw.Get("WorkshopItemsInstalled"); in != nil {
		for id := range in.Child {
			a.Installed[id] = true
		}
	}
	if d := aw.Get("WorkshopItemDetails"); d != nil {
		for id, c := range d.Child {
			if c.Vals["subscribedby"] != "0" {
				a.Subscribed[id] = true
			}
		}
	}
	a.NeedsDL = aw.Vals["NeedsDownload"] == "1"
	return a, nil
}
