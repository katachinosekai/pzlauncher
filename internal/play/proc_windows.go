package play

import (
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const createNoWindow = 0x08000000

func hidden(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd
}

func openURL(u string) error {
	cmd := hidden(exec.Command("rundll32", "url.dll,FileProtocolHandler", u))
	if err := cmd.Start(); err == nil {
		go cmd.Wait()
		return nil
	}
	cmd = hidden(exec.Command("cmd", "/c", "start", "", u))
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

func processNames() ([]string, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	var names []string
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		names = append(names, strings.ToLower(windows.UTF16ToString(pe.ExeFile[:])))
	}
	return names, nil
}

func gameRunning() (bool, error) {
	names, err := processNames()
	if err != nil {
		return false, err
	}
	java := false
	for _, n := range names {
		if strings.HasPrefix(n, "projectzomboid") {
			return true, nil
		}
		if n == "java.exe" || n == "javaw.exe" {
			java = true
		}
	}
	if !java {
		return false, nil
	}
	out, err := hidden(exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"Get-CimInstance Win32_Process -Filter \"Name='java.exe' OR Name='javaw.exe'\" | ForEach-Object { $_.CommandLine }")).Output()
	if err != nil {
		return false, nil
	}
	for _, line := range strings.Split(strings.ToLower(string(out)), "\n") {
		if strings.Contains(line, "zombie.network.gameserver") {
			continue
		}
		if strings.Contains(line, "projectzomboid") || strings.Contains(line, "zombie.gameclient.main") {
			return true, nil
		}
	}
	return false, nil
}
