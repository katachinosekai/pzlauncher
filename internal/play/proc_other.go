//go:build !windows

package play

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func openURL(u string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	cmd := exec.Command(name, u)
	cmd.Env = cleanEnv()
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

func cleanEnv() []string {
	if os.Getenv("APPIMAGE") == "" {
		return os.Environ()
	}
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "LD_LIBRARY_PATH", "LD_PRELOAD", "APPDIR", "APPIMAGE", "ARGV0", "OWD", "PYTHONHOME", "PYTHONPATH", "GSETTINGS_SCHEMA_DIR", "GIO_MODULE_DIR", "GTK_PATH", "QT_PLUGIN_PATH":
			continue
		}
		env = append(env, kv)
	}
	return env
}

func gameRunning() (bool, error) {
	return procRunning("/proc")
}

func procRunning(root string) (bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if runtime.GOOS != "linux" {
			return pgrepRunning()
		}
		return false, err
	}
	for _, e := range entries {
		if !e.IsDir() || strings.Trim(e.Name(), "0123456789") != "" {
			continue
		}
		comm, err := os.ReadFile(filepath.Join(root, e.Name(), "comm"))
		if err != nil {
			continue
		}
		c := strings.TrimSpace(string(comm))
		if strings.HasPrefix(c, "ProjectZomboid") {
			return true, nil
		}
		if c == "java" {
			cmdline, err := os.ReadFile(filepath.Join(root, e.Name(), "cmdline"))
			if err == nil && bytes.Contains(bytes.ToLower(cmdline), []byte("zombie.gameclient.main")) {
				return true, nil
			}
		}
	}
	return false, nil
}

func pgrepRunning() (bool, error) {
	err := exec.Command("pgrep", "-x", "ProjectZomboid6").Run()
	if err == nil {
		return true, nil
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}
