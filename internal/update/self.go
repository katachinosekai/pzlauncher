package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

func SelfPath() (string, error) {
	if runtime.GOOS == "linux" {
		p := os.Getenv("APPIMAGE")
		if p == "" {
			return "", errors.New("self-update only works when running the AppImage")
		}
		return filepath.Abs(p)
	}
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return p, nil
}

func CleanupOld() {
	p, err := SelfPath()
	if err != nil {
		return
	}
	go func() {
		for i := 0; i < 15; i++ {
			err := os.Remove(p + ".old")
			if err == nil || errors.Is(err, os.ErrNotExist) {
				return
			}
			time.Sleep(time.Second)
		}
	}()
}

func (c *Client) SelfUpdate(ctx context.Context, rel *Release, progress Progress) (string, error) {
	name := CurrentBinaryName()
	if name == "" {
		return "", fmt.Errorf("no launcher build for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	a := rel.Asset(name)
	if a == nil {
		return "", fmt.Errorf("release has no %s", name)
	}
	target, err := SelfPath()
	if err != nil {
		return "", err
	}
	newPath := target + ".new"
	if err := c.Download(ctx, *a, rel.Hash(name), newPath, progress); err != nil {
		return "", err
	}
	if err := os.Chmod(newPath, 0o755); err != nil {
		os.Remove(newPath)
		return "", err
	}
	if err := Replace(target, newPath); err != nil {
		os.Remove(newPath)
		return "", err
	}
	return target, nil
}

func Replace(target, newPath string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(newPath, target)
	}
	old := target + ".old"
	os.Remove(old)
	if err := os.Rename(target, old); err != nil {
		return fmt.Errorf("moving the running launcher aside: %w", err)
	}
	if err := os.Rename(newPath, target); err != nil {
		if rerr := os.Rename(old, target); rerr != nil {
			return fmt.Errorf("installing new launcher: %v (restore failed: %v)", err, rerr)
		}
		return fmt.Errorf("installing new launcher: %w", err)
	}
	return nil
}

func Restart(path string) error {
	cmd := exec.Command(path, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
