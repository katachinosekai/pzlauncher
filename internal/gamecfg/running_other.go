//go:build !windows

package gamecfg

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kata/pzlauncher/internal/install"
)

var gameNames = []string{"ProjectZomboid64", "ProjectZomboid32", "ProjectZomboid6"}

func GameRunning() bool {
	if install.DefaultSystem().Running(gameNames...) {
		return true
	}
	return javaGameRunning("/proc", os.Getpid())
}

func javaGameRunning(root string, self int) bool {
	ents, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, e.Name(), "cmdline"))
		if err != nil || len(b) == 0 {
			continue
		}
		args := bytes.Split(bytes.TrimRight(b, "\x00"), []byte{0})
		exe := strings.ToLower(filepath.Base(string(args[0])))
		if exe != "java" && exe != "javaw" {
			continue
		}
		for _, a := range args[1:] {
			s := string(a)
			if strings.Contains(s, "zombie.gameStates.MainScreenState") || strings.Contains(s, "zombie/gameStates/MainScreenState") {
				return true
			}
		}
	}
	return false
}
