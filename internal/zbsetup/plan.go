package zbsetup

import (
	"fmt"
	"path/filepath"
)

type ConfigFile struct {
	Path string
	Text string
}

type FileCopy struct {
	Name        string
	Src         string
	Dst         string
	FromVersion string
	ToVersion   string
	Replace     bool
}

type ConfigEdit struct {
	Path  string
	Text  string
	Edits []LaunchEdit
}

type Plan struct {
	Copies   []FileCopy
	Deletes  []string
	Configs  []ConfigEdit
	Warnings []string
}

func (p Plan) Empty() bool {
	return len(p.Copies) == 0 && len(p.Deletes) == 0 && len(p.Configs) == 0
}

func (c FileCopy) String() string {
	switch {
	case c.Replace && c.FromVersion != "" && c.ToVersion != "":
		return fmt.Sprintf("%s: %s -> %s (copied from %s)", c.Dst, c.FromVersion, c.ToVersion, c.Src)
	case c.Replace:
		return fmt.Sprintf("%s: replaced with %s", c.Dst, c.Src)
	}
	v := ""
	if c.ToVersion != "" {
		v = " (" + c.ToVersion + ")"
	}
	return fmt.Sprintf("%s copied to %s%s", c.Src, c.Dst, v)
}

func editConfigs(configs []ConfigFile, fn func(string) string, p *Plan) {
	for _, c := range configs {
		nt, edits, err := EditLocalConfig(c.Text, fn)
		if err != nil {
			p.Warnings = append(p.Warnings, fmt.Sprintf("%s: %v; launch options not changed there", c.Path, err))
			continue
		}
		if len(edits) > 0 {
			p.Configs = append(p.Configs, ConfigEdit{Path: c.Path, Text: nt, Edits: edits})
		}
	}
}

func SetupPlan(goos, gameDir, libsDir string, configs []ConfigFile) Plan {
	var p Plan
	srcJar := filepath.Join(libsDir, JarName)
	dstJar := filepath.Join(gameDir, JarName)
	srcVer := ""
	if libsDir == "" || !isFile(srcJar) {
		if !isFile(dstJar) {
			p.Warnings = append(p.Warnings, "ZombieBuddy's Workshop files were not found and the game folder has no ZombieBuddy.jar; subscribe to ZombieBuddy (Workshop item "+WorkshopItem+") and let Steam download it")
		}
	} else if info, err := VerifyJar(srcJar); err != nil {
		p.Warnings = append(p.Warnings, fmt.Sprintf("%s: %v; not copied", srcJar, err))
	} else {
		srcVer = info.Version
		if !isFile(dstJar) {
			p.Copies = append(p.Copies, FileCopy{Name: JarName, Src: srcJar, Dst: dstJar, ToVersion: srcVer})
		} else if cur := JarVersion(dstJar); Newer(srcVer, cur) {
			p.Copies = append(p.Copies, FileCopy{Name: JarName, Src: srcJar, Dst: dstJar, FromVersion: cur, ToVersion: srcVer, Replace: true})
		}
	}
	if goos == "windows" && libsDir != "" {
		src := filepath.Join(libsDir, NativeName)
		dst := filepath.Join(gameDir, NativeName)
		switch {
		case !isFile(src):
			if !isFile(dst) {
				p.Warnings = append(p.Warnings, "zbNative.dll was not found in ZombieBuddy's Workshop files; ZombieBuddy can't start on Windows without it")
			}
		case !isFile(dst):
			p.Copies = append(p.Copies, FileCopy{Name: NativeName, Src: src, Dst: dst})
		case !sameBytes(src, dst):
			p.Copies = append(p.Copies, FileCopy{Name: NativeName, Src: src, Dst: dst, Replace: true})
		}
	}
	editConfigs(configs, func(v string) string {
		nv, _ := AddLoader(v, goos)
		return nv
	}, &p)
	return p
}

func RemovePlan(gameDir string, configs []ConfigFile, deleteFiles bool) Plan {
	var p Plan
	editConfigs(configs, func(v string) string {
		nv, _ := RemoveLoader(v)
		return nv
	}, &p)
	if deleteFiles {
		for _, n := range []string{JarName, NewJarName, NativeName} {
			if f := filepath.Join(gameDir, n); isFile(f) {
				p.Deletes = append(p.Deletes, f)
			}
		}
	}
	return p
}

func ApplyCustomJar(p *Plan, gameDir, custom string) {
	if custom == "" {
		return
	}
	kept := p.Copies[:0]
	for _, c := range p.Copies {
		if c.Name != JarName {
			kept = append(kept, c)
		}
	}
	p.Copies = kept
	info, err := VerifyJar(custom)
	if err != nil {
		p.Warnings = append(p.Warnings, fmt.Sprintf("custom ZombieBuddy jar %s: %v; not copied", custom, err))
		return
	}
	dst := filepath.Join(gameDir, JarName)
	switch {
	case !isFile(dst):
		p.Copies = append(p.Copies, FileCopy{Name: JarName, Src: custom, Dst: dst, ToVersion: info.Version})
	case !sameBytes(custom, dst):
		p.Copies = append(p.Copies, FileCopy{Name: JarName, Src: custom, Dst: dst, FromVersion: JarVersion(dst), ToVersion: info.Version, Replace: true})
	}
}
