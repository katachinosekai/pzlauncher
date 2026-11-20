package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/katachinosekai/pzlauncher/internal/install"
	"github.com/katachinosekai/pzlauncher/internal/update"
)

type InstallFunc func(ctx context.Context, packDir string, dryRun bool, log func(string)) (summary string, err error)

var runInstall InstallFunc = realInstall

func realInstall(ctx context.Context, packDir string, dryRun bool, log func(string)) (string, error) {
	r, err := install.Run(ctx, install.Options{
		PackDir:          packDir,
		DryRun:           dryRun,
		Log:              log,
		SetupZombieBuddy: zbWanted(),
		BackupRoot:       installDataDir("install-backups"),
		LogDir:           installDataDir("install-logs"),
		OnWait: func(w install.WaitStatus) {
			if !w.Done {
				log(fmt.Sprintf("waiting for %s: %d of %d installed", w.What, w.Installed, w.Total))
			}
		},
	})
	summary := fmt.Sprintf("%d changes, %d warnings", len(r.Changes), len(r.Warnings))
	if r.BackupDir != "" {
		summary += ", backup in " + r.BackupDir
	}
	if r.LogFile != "" {
		summary += ", log " + r.LogFile
	}
	return summary, err
}

func installDataDir(sub string) string {
	d, err := update.ConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, sub)
}
