package play

import (
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"
)

type Severity int

const (
	SevInfo Severity = iota
	SevWarning
	SevError
)

type Issue struct {
	Severity Severity
	Text     string
}

type CheckResult struct {
	Issues []Issue
}

func (r *CheckResult) add(s Severity, format string, args ...any) {
	r.Issues = append(r.Issues, Issue{s, fmt.Sprintf(format, args...)})
}

func (r CheckResult) Worst() Severity {
	w := SevInfo
	for _, i := range r.Issues {
		w = max(w, i.Severity)
	}
	return w
}

func (r CheckResult) Blocking() bool { return r.Worst() == SevError }

func (r CheckResult) HasWarnings() bool { return r.Worst() == SevWarning }

func (r CheckResult) Count(s Severity) int {
	n := 0
	for _, i := range r.Issues {
		if i.Severity == s {
			n++
		}
	}
	return n
}

func (r CheckResult) Text() string {
	var b strings.Builder
	for _, s := range []Severity{SevError, SevWarning, SevInfo} {
		for _, i := range r.Issues {
			if i.Severity != s {
				continue
			}
			b.WriteString([]string{"Info: ", "Warning: ", "Error: "}[s])
			b.WriteString(i.Text)
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

type SaveInfo struct {
	WorldVersion int
	HasVersion   bool
	MapName      string
	HasMapName   bool
}

const maxJavaString = 65536

func ParseMapVer(r io.Reader) (SaveInfo, error) {
	var si SaveInfo
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return si, err
	}
	si.WorldVersion = int(int32(binary.BigEndian.Uint32(buf[:])))
	si.HasVersion = true
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return si, err
	}
	n := int(int32(binary.BigEndian.Uint32(buf[:])))
	if n > maxJavaString {
		return si, errors.New("map name too long, corrupted save?")
	}
	chars := make([]uint16, 0, max(n, 0))
	if n > 0 {
		raw := make([]byte, 2*n)
		if _, err := io.ReadFull(r, raw); err != nil {
			return si, err
		}
		for i := 0; i < n; i++ {
			chars = append(chars, binary.BigEndian.Uint16(raw[2*i:]))
		}
	}
	si.MapName = string(utf16.Decode(chars))
	if si.MapName == "DEFAULT" {
		si.MapName = "Muldraugh, KY"
	}
	si.HasMapName = true
	return si, nil
}

func ReadSaveInfo(saveDir string) (SaveInfo, error) {
	f, err := os.Open(filepath.Join(saveDir, "map_ver.bin"))
	if err != nil {
		return SaveInfo{}, err
	}
	defer f.Close()
	return ParseMapVer(f)
}

func sqliteImmutableURI(p string) string {
	return sqliteURI(p) + "&immutable=1"
}

func localPlayerAlive(dbPath string) (bool, error) {
	db, err := sql.Open("sqlite", sqliteImmutableURI(dbPath))
	if err != nil {
		return false, err
	}
	defer db.Close()
	var dead sql.NullBool
	err = db.QueryRow("SELECT isDead FROM localPlayers WHERE id=?", 1).Scan(&dead)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !dead.Bool, nil
}

func PlayerAlive(saveDir string) bool {
	if isFile(filepath.Join(saveDir, "map_p.bin")) {
		return true
	}
	players := filepath.Join(saveDir, "players.db")
	if !isFile(players) {
		if v := filepath.Join(saveDir, "vehicles.db"); isFile(v) {
			if ok, err := localPlayerAlive(v); err == nil && ok {
				return true
			}
		}
		return false
	}
	ok, err := localPlayerAlive(players)
	return err == nil && ok
}

const (
	apocalypseMode  = "A Really CD DA"
	minWorldVersion = 200
	launcherModID   = "AutoLoadSave"
)

type Checker struct {
	env      *Env
	Game     GameInfo
	GameErr  error
	Mods     *ModIndex
	Defaults ActiveMods
	HasDefs  bool
}

func (e *Env) NewChecker() *Checker {
	c := &Checker{env: e}
	if e.GameDir == "" {
		c.GameErr = errors.New("Project Zomboid install not found")
	} else {
		c.Game, c.GameErr = ReadGameInfo(e.GameDir)
	}
	c.Mods = e.ScanMods(c.Game.Version)
	c.Defaults, c.HasDefs = ReadActiveMods(filepath.Join(e.ZomboidDir, "mods", "default.txt"))
	return c
}

func (c *Checker) checkLauncher(r *CheckResult) {
	enabled := false
	if c.HasDefs {
		for _, id := range c.Defaults.Mods {
			if id == launcherModID {
				enabled = true
			}
		}
	}
	if !enabled {
		r.add(SevError, "The %s mod is not enabled in the main menu mod list (Zomboid/mods/default.txt). Enable it in Mods from the main menu (or rerun the installer), otherwise the game stops at the main menu.", launcherModID)
		return
	}
	if c.GameErr != nil {
		return
	}
	if why := c.Mods.Unavailable(launcherModID); why != "" {
		r.add(SevError, "The %s mod is enabled but %s, so the game cannot auto-load. Rerun the installer.", launcherModID, why)
	}
}

func (c *Checker) checkGame(r *CheckResult) bool {
	if c.GameErr != nil {
		r.add(SevWarning, "Could not read the installed game version (%v); version, map and mod checks skipped.", c.GameErr)
		return false
	}
	return true
}

func (c *Checker) CheckSave(s Save) CheckResult {
	var r CheckResult
	c.checkLauncher(&r)
	si, err := ReadSaveInfo(s.Dir)
	if !si.HasVersion || !si.HasMapName {
		why := "map_ver.bin is missing"
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			why = "map_ver.bin is unreadable (" + err.Error() + ")"
		}
		r.add(SevError, "Save file not found or corrupt: %s.", why)
	}
	am, hasMods := ReadActiveMods(filepath.Join(s.Dir, "mods.txt"))
	active := am.Mods
	if !hasMods {
		active = c.Defaults.Mods
	}
	if c.checkGame(&r) && si.HasVersion && si.HasMapName {
		switch {
		case si.WorldVersion > c.Game.WorldVersion:
			r.add(SevError, "Save is from a newer game version (world version %d, this game supports up to %d).", si.WorldVersion, c.Game.WorldVersion)
		case si.WorldVersion <= minWorldVersion:
			r.add(SevError, "Save is too old for this game (world version %d, needs more than %d).", si.WorldVersion, minWorldVersion)
		}
		lots := c.Mods.LotDirs(c.env.GameDir, active)
		var missing []string
		for _, m := range strings.Split(si.MapName, ";") {
			if m != "" && !lots[m] {
				missing = append(missing, m)
			}
		}
		if len(missing) > 0 {
			r.add(SevError, "Map folder missing (map mod not installed, not enabled for this save, or not available for this game version): %s.", strings.Join(missing, ", "))
		}
	}
	if s.Mode == apocalypseMode && !PlayerAlive(s.Dir) {
		r.add(SevError, "The player in this Apocalypse save is dead.")
	}
	if hasMods && c.GameErr == nil {
		var missing []string
		for _, id := range am.Mods {
			if why := c.Mods.Unavailable(id); why != "" {
				missing = append(missing, id+" ("+why+")")
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			r.add(SevWarning, "%d mod(s) used by this save are missing or unavailable; the game asks to continue without them: %s.", len(missing), strings.Join(missing, ", "))
		}
	}
	if si.HasVersion && c.GameErr == nil && !r.Blocking() {
		r.add(SevInfo, "World version %d (game %s, world %d), %d mods.", si.WorldVersion, c.Game.Version, c.Game.WorldVersion, len(am.Mods))
	}
	return r
}

func (c *Checker) CheckLatest(saves []Save) CheckResult {
	if len(saves) == 0 {
		var r CheckResult
		c.checkLauncher(&r)
		r.add(SevError, "There are no single player saves to continue.")
		return r
	}
	return c.CheckSave(saves[0])
}

func CheckSave(s Save, e *Env) CheckResult { return e.NewChecker().CheckSave(s) }
