package paths

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

const AppID = "108600"

func Zomboid() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Zomboid"), nil
}

func ZomboidLua() (string, error) {
	z, err := Zomboid()
	if err != nil {
		return "", err
	}
	return filepath.Join(z, "Lua"), nil
}

func steamRootCandidates() []string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		var c []string
		for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
			if v := os.Getenv(env); v != "" {
				c = append(c, filepath.Join(v, "Steam"))
			}
		}
		return append(c, `C:\Program Files (x86)\Steam`)
	}
	return []string{
		filepath.Join(home, ".local", "share", "Steam"),
		filepath.Join(home, ".steam", "steam"),
		filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"),
	}
}

func SteamRoot() (string, error) {
	if runtime.GOOS == "windows" {
		if p := steamRootFromRegistry(); p != "" {
			return p, nil
		}
	}
	for _, c := range steamRootCandidates() {
		if st, err := os.Stat(filepath.Join(c, "steamapps")); err == nil && st.IsDir() {
			return c, nil
		}
	}
	return "", errors.New("Steam folder not found")
}

var vdfPath = regexp.MustCompile(`^\s*"path"\s+"(.+)"\s*$`)

func SteamLibraries(root string) []string {
	libs := []string{root}
	f, err := os.Open(filepath.Join(root, "steamapps", "libraryfolders.vdf"))
	if err != nil {
		return libs
	}
	defer f.Close()
	seen := map[string]bool{filepath.Clean(root): true}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if m := vdfPath.FindStringSubmatch(sc.Text()); m != nil {
			p := filepath.Clean(strings.ReplaceAll(m[1], `\\`, `\`))
			if !seen[p] {
				seen[p] = true
				libs = append(libs, p)
			}
		}
	}
	return libs
}

func GameDir(root string) (string, error) {
	for _, lib := range SteamLibraries(root) {
		if _, err := os.Stat(filepath.Join(lib, "steamapps", "appmanifest_"+AppID+".acf")); err == nil {
			base := filepath.Join(lib, "steamapps", "common", "ProjectZomboid")
			if runtime.GOOS != "windows" {
				if _, err := os.Stat(filepath.Join(base, "projectzomboid")); err == nil {
					return filepath.Join(base, "projectzomboid"), nil
				}
			}
			return base, nil
		}
	}
	return "", errors.New("Project Zomboid is not installed in any Steam library")
}

func WorkshopDir(root string) (string, error) {
	for _, lib := range SteamLibraries(root) {
		if _, err := os.Stat(filepath.Join(lib, "steamapps", "appmanifest_"+AppID+".acf")); err == nil {
			return filepath.Join(lib, "steamapps", "workshop", "content", AppID), nil
		}
	}
	return "", errors.New("Project Zomboid is not installed in any Steam library")
}
