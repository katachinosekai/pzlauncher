package zbsetup

import (
	"errors"
	"sort"
	"strings"
)

const AppID = "108600"

var ErrNoApps = errors.New("no apps section in localconfig.vdf")

type vnode struct {
	key        string
	block      bool
	valS, valE int
	open       int
	close      int
	children   []*vnode
	parent     *vnode
}

func (n *vnode) child(key string) *vnode {
	for _, c := range n.children {
		if strings.EqualFold(c.key, key) {
			return c
		}
	}
	return nil
}

func (n *vnode) path() []string {
	var p []string
	for x := n; x != nil && x.parent != nil; x = x.parent {
		p = append([]string{x.key}, p...)
	}
	return p
}

type vdfParser struct {
	t string
	i int
}

func (p *vdfParser) skip() {
	for p.i < len(p.t) {
		c := p.t[p.i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			p.i++
			continue
		}
		if c == '/' && p.i+1 < len(p.t) && p.t[p.i+1] == '/' {
			for p.i < len(p.t) && p.t[p.i] != '\n' {
				p.i++
			}
			continue
		}
		return
	}
}

func (p *vdfParser) str() (string, int, int, bool) {
	p.skip()
	if p.i >= len(p.t) || p.t[p.i] != '"' {
		return "", 0, 0, false
	}
	p.i++
	s := p.i
	for p.i < len(p.t) && p.t[p.i] != '"' {
		if p.t[p.i] == '\\' {
			p.i++
		}
		p.i++
	}
	if p.i >= len(p.t) {
		return "", 0, 0, false
	}
	e := p.i
	p.i++
	return p.t[s:e], s, e, true
}

func (p *vdfParser) body(n *vnode) error {
	for {
		p.skip()
		if p.i >= len(p.t) {
			if n.parent == nil {
				return nil
			}
			return errors.New("localconfig.vdf: unexpected end of file")
		}
		if p.t[p.i] == '}' {
			if n.parent == nil {
				return errors.New("localconfig.vdf: unbalanced }")
			}
			n.close = p.i
			p.i++
			return nil
		}
		key, _, _, ok := p.str()
		if !ok {
			return errors.New("localconfig.vdf: expected a quoted key")
		}
		c := &vnode{key: key, parent: n}
		p.skip()
		if p.i < len(p.t) && p.t[p.i] == '{' {
			c.block = true
			c.open = p.i
			p.i++
			if err := p.body(c); err != nil {
				return err
			}
		} else {
			_, s, e, ok := p.str()
			if !ok {
				return errors.New("localconfig.vdf: expected a value for " + key)
			}
			c.valS, c.valE = s, e
		}
		n.children = append(n.children, c)
	}
}

func parseVDF(text string) (*vnode, error) {
	root := &vnode{block: true}
	p := &vdfParser{t: text}
	if strings.HasPrefix(text, "\ufeff") {
		p.i = len("\ufeff")
	}
	if err := p.body(root); err != nil {
		return nil, err
	}
	return root, nil
}

func walk(n *vnode, fn func(*vnode)) {
	fn(n)
	for _, c := range n.children {
		walk(c, fn)
	}
}

func appBlocks(root *vnode) []*vnode {
	var out []*vnode
	walk(root, func(n *vnode) {
		if n.block && n.parent != nil && n.key == AppID {
			out = append(out, n)
		}
	})
	return out
}

func appsScore(n *vnode) int {
	s := 0
	for _, k := range n.path() {
		switch strings.ToLower(k) {
		case "software", "valve", "steam":
			s++
		}
	}
	return s
}

func bestApps(root *vnode) *vnode {
	var best *vnode
	score := -1
	walk(root, func(n *vnode) {
		if n.block && n.parent != nil && strings.EqualFold(n.key, "apps") {
			if s := appsScore(n); s > score {
				best, score = n, s
			}
		}
	})
	return best
}

type LaunchEdit struct {
	Before   string
	After    string
	Inserted bool
}

func ReadLaunchOptions(text string) ([]string, error) {
	root, err := parseVDF(text)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, b := range appBlocks(root) {
		if lo := b.child("LaunchOptions"); lo != nil && !lo.block {
			out = append(out, text[lo.valS:lo.valE])
		}
	}
	return out, nil
}

type splice struct {
	at, end int
	s       string
}

func lineIndent(text string, pos int) (string, int, bool) {
	ls := strings.LastIndex(text[:pos], "\n") + 1
	ind := text[ls:pos]
	if strings.TrimLeft(ind, " \t") != "" {
		return "", pos, false
	}
	return ind, ls, true
}

func EditLocalConfig(text string, edit func(string) string) (string, []LaunchEdit, error) {
	root, err := parseVDF(text)
	if err != nil {
		return text, nil, err
	}
	nl := "\n"
	if strings.Contains(text, "\r\n") {
		nl = "\r\n"
	}
	var sp []splice
	var edits []LaunchEdit
	found := false
	for _, b := range appBlocks(root) {
		lo := b.child("LaunchOptions")
		if lo == nil || lo.block {
			continue
		}
		found = true
		old := text[lo.valS:lo.valE]
		nv := edit(old)
		if nv != old {
			sp = append(sp, splice{lo.valS, lo.valE, nv})
			edits = append(edits, LaunchEdit{Before: old, After: nv})
		}
	}
	if !found {
		nv := edit("")
		if nv != "" {
			line := "\"LaunchOptions\"\t\t\"" + nv + "\""
			apps := bestApps(root)
			if apps == nil {
				return text, nil, ErrNoApps
			}
			if app := apps.child(AppID); app != nil && app.block {
				ind, at, ok := lineIndent(text, app.close)
				if ok {
					sp = append(sp, splice{at, at, ind + "\t" + line + nl})
				} else {
					sp = append(sp, splice{app.close, app.close, line + " "})
				}
			} else {
				ind, at, ok := lineIndent(text, apps.close)
				blk := "\"" + AppID + "\"" + nl + ind + "\t{" + nl + ind + "\t\t" + line + nl + ind + "\t}" + nl
				if ok {
					sp = append(sp, splice{at, at, ind + "\t" + blk})
				} else {
					sp = append(sp, splice{apps.close, apps.close, blk})
				}
			}
			edits = append(edits, LaunchEdit{Before: "", After: nv, Inserted: true})
		}
	}
	if len(sp) == 0 {
		return text, nil, nil
	}
	sort.Slice(sp, func(i, j int) bool { return sp[i].at > sp[j].at })
	out := text
	for _, s := range sp {
		out = out[:s.at] + s.s + out[s.end:]
	}
	return out, edits, nil
}
