package workshop

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

type SubState int

const (
	SubInstalled SubState = iota
	SubNeedsUpdate
	SubDownloading
)

func (s SubState) String() string {
	switch s {
	case SubInstalled:
		return "installed"
	case SubNeedsUpdate:
		return "update pending"
	}
	return "downloading / queued"
}

type Sub struct {
	ID      string
	State   SubState
	Size    int64
	Updated int64
	Dir     string
}

func ParseSubscriptions(acfText, contentDir string) ([]Sub, error) {
	v, err := ParseVDF(acfText)
	if err != nil {
		return nil, err
	}
	aw := v.Get("AppWorkshop")
	if aw == nil {
		return nil, errors.New("acf: no AppWorkshop block")
	}
	installed := aw.Get("WorkshopItemsInstalled")
	details := aw.Get("WorkshopItemDetails")
	seen := map[string]bool{}
	var out []Sub
	add := func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		var in, det *VDF
		if installed != nil {
			in = installed.Child[id]
		}
		if details != nil {
			det = details.Child[id]
		}
		if det != nil && det.Vals["subscribedby"] == "0" {
			return
		}
		s := Sub{ID: id, State: SubDownloading}
		if contentDir != "" {
			s.Dir = filepath.Join(contentDir, id)
		}
		if in != nil {
			s.Size, _ = strconv.ParseInt(in.Vals["size"], 10, 64)
			s.Updated, _ = strconv.ParseInt(in.Vals["timeupdated"], 10, 64)
			s.State = SubInstalled
			if contentDir != "" && !hasContent(filepath.Join(contentDir, id)) {
				s.State = SubDownloading
			} else if det != nil && needsUpdate(in, det) {
				s.State = SubNeedsUpdate
			}
		}
		out = append(out, s)
	}
	if details != nil {
		for _, id := range details.Keys {
			if details.Child[id] != nil {
				add(id)
			}
		}
	}
	if installed != nil {
		for _, id := range installed.Keys {
			if installed.Child[id] != nil {
				add(id)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].State > out[j].State })
	return out, nil
}

func needsUpdate(in, det *VDF) bool {
	latest := det.Vals["latest_manifest"]
	if latest != "" && latest != "0" && latest != in.Vals["manifest"] {
		return true
	}
	lt, _ := strconv.ParseInt(det.Vals["latest_timeupdated"], 10, 64)
	t, _ := strconv.ParseInt(in.Vals["timeupdated"], 10, 64)
	return lt > 0 && t > 0 && lt > t
}

func hasContent(dir string) bool {
	ents, err := os.ReadDir(dir)
	return err == nil && len(ents) > 0
}

func LocalSubscriptions() ([]Sub, error) {
	dir, acf, err := Locate()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(acf)
	if err != nil {
		return nil, err
	}
	return ParseSubscriptions(string(b), dir)
}

func IsSubscribedLocally(id string) bool {
	dir, acf, err := Locate()
	if err != nil {
		return false
	}
	if hasContent(filepath.Join(dir, id)) {
		return true
	}
	b, err := os.ReadFile(acf)
	if err != nil {
		return false
	}
	a, err := ParseACF(string(b))
	return err == nil && (a.Subscribed[id] || a.Installed[id])
}
