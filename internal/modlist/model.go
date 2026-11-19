package modlist

import (
	"errors"
	"fmt"
	"slices"
	"sort"
)

type Model struct {
	Index    *Index
	Active   []string
	Maps     []string
	requires map[string]map[string]bool
	needed   map[string]map[string]bool
	incompat map[string]map[string]bool
}

func NewModel(x *Index, active, maps []string) *Model {
	m := &Model{Index: x, Maps: slices.Clone(maps)}
	seen := map[string]bool{}
	for _, id := range active {
		if !seen[id] {
			seen[id] = true
			m.Active = append(m.Active, id)
		}
	}
	m.requires = map[string]map[string]bool{}
	m.needed = map[string]map[string]bool{}
	m.incompat = map[string]map[string]bool{}
	add := func(mp map[string]map[string]bool, a, b string) {
		if mp[a] == nil {
			mp[a] = map[string]bool{}
		}
		mp[a][b] = true
	}
	for _, mod := range x.Mods {
		for _, r := range mod.Requires() {
			add(m.requires, mod.ID, r)
			add(m.needed, r, mod.ID)
		}
		for _, r := range mod.Incompatibles() {
			add(m.incompat, mod.ID, r)
			add(m.incompat, r, mod.ID)
		}
	}
	return m
}

func (m *Model) IsActive(id string) bool { return slices.Contains(m.Active, id) }

func (m *Model) setActive(id string, on bool) {
	if on {
		if !m.IsActive(id) {
			m.Active = append(m.Active, id)
		}
		return
	}
	m.Active = slices.DeleteFunc(m.Active, func(s string) bool { return s == id })
}

func sortedKeys(s map[string]bool) []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (m *Model) IncompatibleWith(id string) []string { return sortedKeys(m.incompat[id]) }

func (m *Model) ActiveIncompatible(id string) []string {
	var out []string
	for _, o := range m.IncompatibleWith(id) {
		if m.Index.Get(o) != nil && m.IsActive(o) {
			out = append(out, o)
		}
	}
	return out
}

func (m *Model) IsIncompatible(id string) bool { return len(m.ActiveIncompatible(id)) > 0 }

func (m *Model) NeededFor(id string) []string { return sortedKeys(m.needed[id]) }

var (
	ErrUnavailable  = errors.New("mod is unavailable")
	ErrIncompatible = errors.New("mod is incompatible with an enabled mod")
	ErrNotInstalled = errors.New("mod is not installed")
)

type Change struct {
	Enabled  []string
	Disabled []string
	Skipped  map[string]error
}

func (c *Change) skip(id string, err error) {
	if c.Skipped == nil {
		c.Skipped = map[string]error{}
	}
	if _, ok := c.Skipped[id]; !ok {
		c.Skipped[id] = err
	}
}

func (m *Model) ForceActivate(id string, activate bool) Change {
	var c Change
	m.forceActivate(id, activate, &c)
	return c
}

func (m *Model) forceActivate(id string, activate bool, c *Change) {
	if m.IsActive(id) == activate {
		return
	}
	mod := m.Index.Get(id)
	if activate {
		if mod == nil {
			c.skip(id, ErrNotInstalled)
			return
		}
		if !m.Index.Available(id) {
			c.skip(id, fmt.Errorf("%w: %s", ErrUnavailable, m.Index.Unavailable(id)))
			return
		}
		if inc := m.ActiveIncompatible(id); len(inc) > 0 {
			c.skip(id, fmt.Errorf("%w: %v", ErrIncompatible, inc))
			return
		}
		m.setActive(id, true)
		c.Enabled = append(c.Enabled, id)
		for _, r := range mod.Requires() {
			if m.Index.Get(r) != nil {
				m.forceActivate(r, true, c)
			}
		}
		return
	}
	m.setActive(id, false)
	c.Disabled = append(c.Disabled, id)
	for _, n := range m.NeededFor(id) {
		m.forceActivate(n, false, c)
	}
}

func (m *Model) CorrectOrder(data []string) {
	var order []string
	added := map[string]bool{}
	for _, id := range data {
		if mod := m.Index.Get(id); mod != nil {
			for _, r := range mod.Requires() {
				if !added[r] && m.Index.Get(r) != nil {
					order = append(order, r)
					added[r] = true
				}
			}
		}
		if !added[id] {
			order = append(order, id)
			added[id] = true
		}
	}
	m.Active = order
}

type PresetResult struct {
	Change
	Missing []string
}

func (m *Model) LoadPreset(ids []string) PresetResult {
	var res PresetResult
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	installed := map[string]bool{}
	for _, mod := range m.Index.Mods {
		if want[mod.ID] {
			if !m.IsActive(mod.ID) && m.Index.Available(mod.ID) {
				m.forceActivate(mod.ID, true, &res.Change)
			}
			installed[mod.ID] = true
		} else if m.IsActive(mod.ID) {
			m.forceActivate(mod.ID, false, &res.Change)
		}
	}
	var list []string
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if installed[id] {
			if m.Index.Available(id) {
				list = append(list, id)
			}
		} else {
			res.Missing = append(res.Missing, id)
		}
	}
	m.CorrectOrder(list)
	return res
}

func (m *Model) DisableAll() Change {
	var c Change
	for _, mod := range m.Index.Mods {
		if m.IsActive(mod.ID) {
			m.forceActivate(mod.ID, false, &c)
		}
	}
	return c
}

func (m *Model) EnableAll() Change {
	var c Change
	for _, mod := range m.Index.Mods {
		if !m.IsActive(mod.ID) && m.Index.Available(mod.ID) && mod.ID != "ModTemplate" {
			m.forceActivate(mod.ID, true, &c)
		}
	}
	return c
}

func (m *Model) Move(i, delta int) bool {
	j := i + delta
	if i < 0 || j < 0 || i >= len(m.Active) || j >= len(m.Active) {
		return false
	}
	m.Active[i], m.Active[j] = m.Active[j], m.Active[i]
	return true
}

func (m *Model) MoveMap(i, delta int) bool {
	j := i + delta
	if i < 0 || j < 0 || i >= len(m.Maps) || j >= len(m.Maps) {
		return false
	}
	m.Maps[i], m.Maps[j] = m.Maps[j], m.Maps[i]
	return true
}

func (m *Model) Missing() []string {
	var out []string
	for _, id := range m.Active {
		if m.Index.Get(id) == nil {
			out = append(out, id)
		}
	}
	return out
}

func (m *Model) RemoveMissing() []string {
	miss := m.Missing()
	m.Active = slices.DeleteFunc(m.Active, func(s string) bool { return m.Index.Get(s) == nil })
	return miss
}

func (m *Model) loadAfterAndRequire(mod *Mod) []string {
	return append(mod.After(), mod.Requires()...)
}

func insertAt(s []string, i int, v string) []string {
	return slices.Insert(s, i, v)
}

func (m *Model) AutoSort() {
	type item struct {
		id  string
		mod *Mod
	}
	var base []item
	inList := map[string]bool{}
	for _, id := range m.Active {
		inList[id] = true
		base = append(base, item{id, m.Index.Get(id)})
	}
	sort.SliceStable(base, func(i, j int) bool { return base[i].id < base[j].id })
	var auto []string
	added := map[string]bool{}
	for _, v := range base {
		var temp []string
		if v.mod != nil {
			for _, id := range m.loadAfterAndRequire(v.mod) {
				if !added[id] {
					temp = append(temp, id)
					added[id] = true
				}
			}
		}
		temp = append(temp, v.id)
		mIndex := len(temp)
		if v.mod != nil {
			for _, id := range v.mod.Before() {
				if !added[id] {
					temp = append(temp, id)
					added[id] = true
				}
			}
		}
		if added[v.id] {
			index := slices.Index(auto, v.id) + 1
			for j := mIndex + 1; j <= len(temp); j++ {
				auto = insertAt(auto, index, temp[j-1])
			}
			for j := 1; j <= mIndex-1; j++ {
				auto = insertAt(auto, index-1, temp[j-1])
			}
		} else {
			auto = append(auto, temp...)
			added[v.id] = true
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, id := range auto {
		if inList[id] && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	m.Active = out
}

type OrderIssue struct {
	ID     string
	Reason string
	Fatal  bool
}

func (m *Model) OrderIssues() []OrderIssue {
	var out []OrderIssue
	pos := map[string]int{}
	for i, id := range m.Active {
		pos[id] = i
	}
	loaded := map[string]bool{}
	for i, id := range m.Active {
		mod := m.Index.Get(id)
		if mod == nil {
			loaded[id] = true
			continue
		}
		for _, r := range mod.Requires() {
			if m.Index.Get(r) == nil {
				out = append(out, OrderIssue{ID: id, Reason: "required mod " + r + " is not installed", Fatal: true})
				break
			}
			if !loaded[r] {
				out = append(out, OrderIssue{ID: id, Reason: "required mod " + r + " is not loaded before it", Fatal: true})
				break
			}
		}
		for _, b := range mod.Before() {
			if p, ok := pos[b]; ok && p < i {
				out = append(out, OrderIssue{ID: id, Reason: "should load before " + b})
				break
			}
		}
		for _, a := range mod.After() {
			if p, ok := pos[a]; ok && p > i {
				out = append(out, OrderIssue{ID: id, Reason: "should load after " + a})
				break
			}
		}
		loaded[id] = true
	}
	return out
}
