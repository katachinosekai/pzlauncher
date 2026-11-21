//go:build !windows

package install

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/katachinosekai/pzlauncher/internal/safeurl"
)

type osSystem struct{}

func (osSystem) Running(names ...string) bool {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	want := map[string]bool{}
	for _, n := range names {
		if len(n) > 15 {
			n = n[:15]
		}
		want[n] = true
	}
	for _, e := range ents {
		if !e.IsDir() || e.Name()[0] < '0' || e.Name()[0] > '9' {
			continue
		}
		b, err := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
		if err != nil {
			continue
		}
		if want[strings.TrimSpace(string(b))] {
			return true
		}
	}
	return false
}

func detached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

func (osSystem) OpenURL(url string) error {
	if err := safeurl.Check(url); err != nil {
		return err
	}
	return detached("xdg-open", url)
}

func steamCommand(args ...string) error {
	if _, err := exec.LookPath("steam"); err != nil {
		if _, ferr := exec.LookPath("flatpak"); ferr == nil {
			return detached("flatpak", append([]string{"run", "com.valvesoftware.Steam"}, args...)...)
		}
		return err
	}
	return detached("steam", args...)
}

func (osSystem) CloseSteam(ctx context.Context, steamRoot string) error {
	return steamCommand("-shutdown")
}

func (osSystem) StartSteam(steamRoot string) error { return steamCommand() }

func Elevated() bool { return os.Geteuid() == 0 }
