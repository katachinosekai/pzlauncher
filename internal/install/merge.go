package install

import (
	"bytes"
	"os"
	"path"

	"github.com/katachinosekai/pzlauncher/internal/kvfile"
	"github.com/katachinosekai/pzlauncher/internal/modoptions"
)

type settingChange struct {
	Name, Old, New string
	Added          bool
}

func (c settingChange) String() string {
	if c.Added {
		return c.Name + ": (new) -> " + c.New
	}
	return c.Name + ": " + c.Old + " -> " + c.New
}

var bom = []byte("\xef\xbb\xbf")

func mergeSettings(rel, src, dst string) (merged []byte, changes []settingChange, ok bool) {
	sb, err := os.ReadFile(src)
	if err != nil {
		return nil, nil, false
	}
	db, err := os.ReadFile(dst)
	if err != nil {
		return nil, nil, false
	}
	hadBOM := bytes.HasPrefix(db, bom)
	st, dt := string(bytes.TrimPrefix(sb, bom)), string(bytes.TrimPrefix(db, bom))
	var out string
	switch {
	case rel == "Lua/"+modoptions.FileName:
		var ch []modoptions.Change
		out, ch = modoptions.Merge(modoptions.Parse(dt), modoptions.Parse(st))
		for _, c := range ch {
			changes = append(changes, settingChange{Name: c.Mod + " " + c.ID, Old: c.Old, New: c.New, Added: c.Added})
		}
	case path.Dir(rel) != "mods" && kvfile.Simple(st) && kvfile.Simple(dt):
		var ch []kvfile.Change
		out, ch = kvfile.Merge(dt, st)
		for _, c := range ch {
			changes = append(changes, settingChange{Name: c.Name(), Old: c.Old, New: c.New, Added: c.Added})
		}
	default:
		return nil, nil, false
	}
	if hadBOM {
		out = string(bom) + out
	}
	return []byte(out), changes, true
}
