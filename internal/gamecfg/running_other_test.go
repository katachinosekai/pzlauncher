//go:build !windows

package gamecfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJavaGameRunning(t *testing.T) {
	d := t.TempDir()
	mk := func(pid, cmd string) {
		os.MkdirAll(filepath.Join(d, pid), 0o755)
		os.WriteFile(filepath.Join(d, pid, "cmdline"), []byte(strings.ReplaceAll(cmd, " ", "\x00")+"\x00"), 0o644)
	}
	mk("10", "/usr/bin/bash -c zombie.gameStates.MainScreenState")
	mk("11", "/opt/jre/bin/java -Xmx1g -jar other.jar")
	if javaGameRunning(d, 1) {
		t.Fatal("false positive")
	}
	mk("12", "./jre64/bin/java -Xmx8g zombie.gameStates.MainScreenState")
	if !javaGameRunning(d, 1) || javaGameRunning(d, 12) {
		t.Fatal("java game detection wrong")
	}
}
