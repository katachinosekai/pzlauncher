package install

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/katachinosekai/pzlauncher/internal/safeurl"
)

type osSystem struct{}

func (osSystem) Running(names ...string) bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		exe := strings.TrimSuffix(strings.ToLower(windows.UTF16ToString(pe.ExeFile[:])), ".exe")
		for _, n := range names {
			if strings.EqualFold(exe, n) {
				return true
			}
		}
	}
	return false
}

func (osSystem) OpenURL(url string) error {
	if err := safeurl.Check(url); err != nil {
		return err
	}
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

func (s osSystem) CloseSteam(ctx context.Context, steamRoot string) error {
	return s.OpenURL("steam://exit")
}

func (osSystem) StartSteam(steamRoot string) error {
	exe := filepath.Join(steamRoot, "steam.exe")
	if _, err := os.Stat(exe); err != nil {
		return err
	}
	cmd := exec.Command(exe)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

func Elevated() bool { return windows.GetCurrentProcessToken().IsElevated() }
